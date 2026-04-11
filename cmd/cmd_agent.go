package cmd

import (
	"context"
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
		cmdAgent.MarkFlagRequired("token")
	}
	cmdAgent.Flags().BoolVar(&flagAgentDockerDisabled, "docker-disabled", envOrBool("ACT_AGENT_DOCKER_DISABLED", false), "Disable Docker execution, always run natively")
	cmdAgent.Flags().StringVar(&flagAgentDockerDefaultImage, "docker-default-image", envOr("ACT_AGENT_DOCKER_DEFAULT_IMAGE", ""), "Force this Docker image for all scripts")
	cmdAgent.Flags().StringVar(&flagAgentP4Client, "p4-client", envOr("ACT_AGENT_P4CLIENT", ""), "Reuse an existing Perforce workspace instead of creating a temporary one (env: ACT_AGENT_P4CLIENT)")
}

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

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.WithField("signal", sig).Info("received signal, shutting down gracefully...")
		cancel()
	}()

	const maxRestarts = 3
	const restartWindow = 5 * time.Minute
	var restartTimes []time.Time

	for {
		client := agent.NewClient(serverURL, agentToken)
		w := agent.NewWorker(client, dockerCfg, vcsOpts)

		log.WithField("server", serverURL).Info("connecting")
		err := w.Run(ctx)

		if err == nil || ctx.Err() != nil {
			log.Info("disconnecting from server")
			_ = client.Disconnect()
			return
		}

		// Connection lost — check restart budget
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
