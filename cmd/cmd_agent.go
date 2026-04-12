package cmd

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/actionforge/actrun-cli/agent"
	"github.com/actionforge/actrun-cli/agent/vcs"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)


var (
	flagAgentServer             string
	flagAgentToken              string
	flagAgentDockerDisabled     bool
	flagAgentDockerDefaultImage string
	flagAgentP4Client           string
)

var cmdAgent = &cobra.Command{
	Use:   "agent",
	Short: "Start an agent that polls the server for jobs",
	Run:   cmdAgentRun,
}

func init() {
	cmdRoot.AddCommand(cmdAgent)

	cmdAgent.Flags().StringVar(&flagAgentServer, "server", envOr("ACT_AGENT_SERVER", "https://orch.actionforge.dev"), "Server base URL (env: ACT_AGENT_SERVER)")
	cmdAgent.Flags().StringVar(&flagAgentToken, "token", envOr("ACT_AGENT_TOKEN", ""), "Agent token (bsa_) (env: ACT_AGENT_TOKEN)")
	if os.Getenv("ACT_AGENT_TOKEN") == "" {
		_ = cmdAgent.MarkFlagRequired("token")
	}
	cmdAgent.Flags().BoolVar(&flagAgentDockerDisabled, "docker-disabled", envOrBool("ACT_AGENT_DOCKER_DISABLED", false), "Disable Docker execution, always run natively")
	cmdAgent.Flags().StringVar(&flagAgentDockerDefaultImage, "docker-default-image", envOr("ACT_AGENT_DOCKER_DEFAULT_IMAGE", ""), "Force this Docker image for all scripts")
	cmdAgent.Flags().StringVar(&flagAgentP4Client, "p4-client", envOr("ACT_AGENT_P4CLIENT", ""), "Reuse an existing Perforce workspace instead of creating a temporary one (env: ACT_AGENT_P4CLIENT)")
}

// cmdAgentRun is the entry point for `actrun agent`. It owns two loops:
// the outer loop handles registration (register once, re-register on
// revocation) and the inner Worker loop does the actual job polling.
// Restart budgets, signal handling, and graceful deregister live here.
func cmdAgentRun(cmd *cobra.Command, args []string) {
	runAgentLoop(flagAgentServer, flagAgentToken, agent.DockerConfig{
		Disabled:     flagAgentDockerDisabled,
		DefaultImage: flagAgentDockerDefaultImage,
	}, vcs.Options{
		P4Client: flagAgentP4Client,
	})
}

func runAgentLoop(serverURL, agentToken string, dockerCfg agent.DockerConfig, vcsOpts vcs.Options) {
	log := logrus.WithField("component", "agent")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Acquire a per-(server, token) lockfile so a second terminal with the
	// same token on this machine gets a clear error instead of silently
	// sharing the same instance credential.
	lockFile, err := agent.AcquireInstanceLock(serverURL, agentToken)
	if err != nil {
		if errors.Is(err, agent.ErrAgentAlreadyRunning) {
			log.Fatal("an agent is already running for this token on this machine")
		}
		log.WithError(err).Fatal("failed to acquire instance lock")
	}
	defer agent.ReleaseInstanceLock(lockFile)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.WithField("signal", sig).Info("received signal, shutting down gracefully...")
		cancel()
	}()

	const maxRestarts = 3
	const restartWindow = 5 * time.Minute
	const maxRevocations = 3
	var restartTimes []time.Time
	var revocationTimes []time.Time

	for {
		if ctx.Err() != nil {
			return
		}

		// Phase 1: load or mint an instance credential. LoadInstanceCred
		// silently returns nil when the file is missing or keyed on a
		// different (server, token) pair, at which point we register.
		cred, err := agent.LoadInstanceCred(serverURL, agentToken)
		if err != nil {
			log.WithError(err).Warn("failed to read cached instance credential, re-registering")
			cred = nil
		}
		if cred == nil {
			log.WithField("server", serverURL).Info("registering new runner instance")
			cred, err = registerWithRetry(ctx, log, serverURL, agentToken)
			if err != nil {
				// registerWithRetry only returns on ctx cancel or on a
				// fatal error like an invalid agent token.
				if ctx.Err() != nil {
					return
				}
				log.WithError(err).Fatal("failed to register with gateway")
			}
		}

		// Phase 2: run the worker loop with the instance credential.
		client := agent.NewClientFromCred(serverURL, cred)
		w := agent.NewWorker(client, dockerCfg, vcsOpts)

		log.WithFields(logrus.Fields{
			"server":      serverURL,
			"pool":        cred.PoolName,
			"instance_id": cred.InstanceID,
		}).Info("connecting")
		err = w.Run(ctx)

		// Graceful shutdown (ctx cancelled): deregister and exit.
		if ctx.Err() != nil {
			log.Info("disconnecting from server")
			_ = client.Deregister()
			_ = agent.DeleteInstanceCred(serverURL, agentToken)
			return
		}

		// Revoked mid-run: drop the stale credential and loop back to
		// phase 1 so the next iteration registers a fresh instance.
		// Rate-limit revocations to avoid hammering the server if a proxy
		// or gateway is flapping 401s.
		if errors.Is(err, agent.ErrInstanceRevoked) {
			now := time.Now()
			validRevocations := revocationTimes[:0]
			for _, t := range revocationTimes {
				if now.Sub(t) < restartWindow {
					validRevocations = append(validRevocations, t)
				}
			}
			revocationTimes = append(validRevocations, now)
			if len(revocationTimes) > maxRevocations {
				log.WithFields(logrus.Fields{"revocations": len(revocationTimes), "window": restartWindow}).Fatal("too many revocations, giving up (server may be rejecting our credentials)")
			}
			log.Warn("instance revoked by server, re-registering")
			_ = agent.DeleteInstanceCred(serverURL, agentToken)
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return
			}
			continue
		}

		// Worker returned for a non-fatal reason (ErrConnectionLost or
		// similar). Apply the restart budget then reuse the existing
		// credential — the process is the same one the server knows.
		now := time.Now()
		valid := restartTimes[:0]
		for _, t := range restartTimes {
			if now.Sub(t) < restartWindow {
				valid = append(valid, t)
			}
		}
		restartTimes = valid

		if len(restartTimes) >= maxRestarts {
			log.WithFields(logrus.Fields{"restarts": maxRestarts, "window": restartWindow}).Fatal("too many restarts, giving up")
		}

		restartTimes = append(restartTimes, now)
		log.WithFields(logrus.Fields{"restart": len(restartTimes), "max": maxRestarts, "window": restartWindow}).Info("connection lost, restarting...")

		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

// registerWithRetry calls agent.Register with exponential backoff so a
// transient gateway outage during agent startup doesn't take the process
// down. An invalid agent token is the only fatal case and bubbles
// up immediately; everything else retries until the context is cancelled.
func registerWithRetry(ctx context.Context, log *logrus.Entry, serverURL, agentToken string) (*agent.InstanceCred, error) {
	backoff := 2 * time.Second
	const maxBackoff = 60 * time.Second
	attempt := 0
	for {
		attempt++
		cred, err := agent.Register(serverURL, agentToken)
		if err == nil {
			return cred, nil
		}
		// Unrecoverable auth error — do not retry.
		if errors.Is(err, agent.ErrInvalidAgentToken) {
			return nil, err
		}
		log.WithError(err).WithFields(logrus.Fields{
			"attempt": attempt,
			"backoff": backoff,
		}).Warn("register failed, retrying")

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v == "true" || v == "1" || v == "yes"
}
