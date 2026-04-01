package agent

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/actionforge/actrun-cli/agent/vcs"
	"github.com/sirupsen/logrus"
)

// ErrConnectionLost is returned by Run when the server connection is persistently failing.
var ErrConnectionLost = fmt.Errorf("connection lost")

type Worker struct {
	client       *Client
	docker       DockerConfig
	vcsOpts      vcs.Options
	pollInterval time.Duration
	uuid         string
	log          *logrus.Entry

	metricsMu    sync.Mutex
	lastCounters *RawCounters
}

func NewWorker(client *Client, docker DockerConfig, vcsOpts vcs.Options) *Worker {
	return &Worker{
		client:       client,
		docker:       docker,
		vcsOpts:      vcsOpts,
		pollInterval: 1 * time.Second,
		uuid:         loadOrGenerateUUID(),
		log:          logrus.WithField("component", "agent"),
	}
}

// uuidFilePath returns the path to the persistent UUID file in the user's config directory.
func uuidFilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "actionforge", "agent-uuid")
}

// loadOrGenerateUUID loads a persistent UUID from disk, or generates and saves a new one.
func loadOrGenerateUUID() string {
	path := uuidFilePath()
	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); len(id) == 36 {
			return id
		}
	}

	// Generate UUID v4
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant 1
	id := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])

	_ = os.MkdirAll(filepath.Dir(path), 0700)
	_ = os.WriteFile(path, []byte(id+"\n"), 0600)
	return id
}

// maxConsecutiveErrors is the number of consecutive connection errors before
// Run returns ErrConnectionLost so the caller can decide to restart.
const maxConsecutiveErrors = 10

func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("starting")

	// Take initial snapshot for delta computation
	if snap, err := Snapshot(); err == nil {
		w.metricsMu.Lock()
		w.lastCounters = &snap
		w.metricsMu.Unlock()
	}

	// Send initial heartbeat
	if err := w.client.Heartbeat(w.buildHeartbeatRequest()); err != nil {
		w.log.WithError(err).Warn("initial heartbeat failed")
	}

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	// Run heartbeats in a dedicated goroutine so they are never blocked by
	// job execution (which can take minutes/hours).
	// Use a cancel to stop the goroutine on all exit paths (including ErrConnectionLost).
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-heartbeat.C:
				if err := w.client.Heartbeat(w.buildHeartbeatRequest()); err != nil {
					w.log.WithError(err).Warn("heartbeat error")
				}
			}
		}
	}()

	// waitHeartbeat ensures the heartbeat goroutine is stopped before returning.
	waitHeartbeat := func() {
		heartbeatCancel()
		<-heartbeatDone
	}

	consecutiveErrors := 0

	for {
		select {
		case <-ctx.Done():
			waitHeartbeat()
			w.log.Info("exiting")
			return nil
		default:
		}

		job, err := w.client.Claim()
		if err != nil {
			consecutiveErrors++
			w.log.WithError(err).Warn("claim error")
			if consecutiveErrors >= maxConsecutiveErrors {
				w.log.WithField("errors", consecutiveErrors).Warn("too many consecutive connection errors, returning for restart")
				waitHeartbeat()
				return ErrConnectionLost
			}
			select {
			case <-time.After(w.pollInterval):
			case <-ctx.Done():
				waitHeartbeat()
				return nil
			}
			continue
		}

		// Successful communication — reset counter
		consecutiveErrors = 0

		if job == nil {
			select {
			case <-time.After(w.pollInterval):
			case <-ctx.Done():
				waitHeartbeat()
				return nil
			}
			continue
		}

		w.log.WithFields(logrus.Fields{
			"run_id":   job.RunID,
			"job_id":   job.JobID,
			"owner":    job.Owner,
			"name":     job.Name,
			"pipeline": job.Pipeline,
		}).Info("claimed job")
		w.execute(ctx, job)
	}
}

func (w *Worker) sendErrorLog(jobID string, msg string) {
	_, _ = w.client.SendLogs(jobID, LogBatch{Lines: []LogEntry{
		{LineNum: 1, Stream: "stderr", Content: msg},
	}})
}

func (w *Worker) execute(ctx context.Context, job *ClaimResponse) {
	runID := job.RunID
	jobID := job.JobID

	// Create a job-specific context that can be cancelled when the job is
	// cancelled on the server side (e.g. user clicks Cancel in the UI).
	jobCtx, jobCancel := context.WithCancel(ctx)
	defer jobCancel()

	// Create temp working directory
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("runner-build-%s-*", jobID))
	if err != nil {
		w.log.WithError(err).WithField("run_id", runID).Error("failed to create temp dir")
		w.sendErrorLog(jobID, fmt.Sprintf("failed to create temp dir: %v", err))
		exitCode := 1
		w.client.ReportStatus(jobID, RunFailure, &exitCode)
		return
	}
	defer os.RemoveAll(tmpDir)

	w.client.ReportStatus(jobID, RunRunning, nil)

	vcsOpts := w.vcsOpts
	vcsOpts.ServerURL = w.client.ServerURL()
	vcsOpts.RepoID = job.RepoID
	if job.Env != nil {
		vcsOpts.RepoToken = job.Env["BUILD_REPO_TOKEN"]
	}
	provider, err := vcs.New(job.VCSType, vcsOpts)
	if err != nil {
		w.log.WithError(err).WithField("run_id", runID).Error("unsupported VCS type")
		w.sendErrorLog(jobID, fmt.Sprintf("unsupported VCS type: %v", err))
		exitCode := 1
		w.client.ReportStatus(jobID, RunFailure, &exitCode)
		return
	}
	defer provider.Cleanup(ctx)

	// Set VCS credentials in the OS environment before checkout so gitAuth()
	// can pick them up. Unset immediately after so they don't leak into the
	// subprocess environment (graphs get them via ACT_INPUT_SECRET_ instead).
	if job.Env != nil {
		if v := job.Env["ACT_INPUT_SECRET_GIT_USERNAME"]; v != "" {
			os.Setenv("GIT_USERNAME", v)
		}
		if v := job.Env["ACT_INPUT_SECRET_GIT_PASSWORD"]; v != "" {
			os.Setenv("GIT_PASSWORD", v)
		}
	}
	// Fetch only the pipeline file from VCS. The full repository clone is the
	// graph's responsibility (e.g. via git-clone or repo-download nodes).
	ref := job.Ref
	if ref == "" {
		ref = "main"
	}
	w.log.WithFields(logrus.Fields{
		"vcs_type": job.VCSType,
		"vcs_url":  job.VCSURL,
		"ref":      ref,
		"run_id":   runID,
	}).Info("fetching pipeline script from VCS")
	checkout, err := provider.Checkout(jobCtx, job.VCSURL, ref, job.Pipeline, filepath.Join(tmpDir, "checkout"))
	os.Unsetenv("GIT_USERNAME")
	os.Unsetenv("GIT_PASSWORD")
	if err != nil {
		w.log.WithError(err).WithField("run_id", runID).Error("VCS fetch failed")
		w.sendErrorLog(jobID, fmt.Sprintf("VCS checkout failed: %v", err))
		exitCode := 1
		w.client.ReportStatus(jobID, RunFailure, &exitCode)
		return
	}

	var scriptPath string
	var scriptContent []byte
	var workDir string

	// Pipeline path is relative to checkout root
	srcScript := filepath.Join(checkout.Dir, job.Pipeline)
	scriptContent, err = os.ReadFile(srcScript)
	if err != nil {
		w.log.WithFields(logrus.Fields{
			"run_id": runID,
			"path":   srcScript,
		}).Error("pipeline script not found in repo")
		w.sendErrorLog(jobID, fmt.Sprintf("pipeline script not found: %s", job.Pipeline))
		exitCode := 1
		w.client.ReportStatus(jobID, RunFailure, &exitCode)
		return
	}

	// Report the resolved commit SHA back to the orchestrator
	if checkout.SHA != "" {
		if err := w.client.ReportRef(jobID, checkout.SHA); err != nil {
			w.log.WithError(err).WithField("run_id", runID).Warn("failed to report commit SHA")
		}
	}

	// Submit graph to server for visualization
	if err := w.client.SubmitGraph(jobID, string(scriptContent)); err != nil {
		w.log.WithError(err).WithField("run_id", runID).Warn("failed to submit graph")
	}

	if checkout.Persistent {
		// Persistent workspace: run directly in the workspace root
		workDir = checkout.Dir
		scriptPath = srcScript
	} else {
		// Temporary checkout: copy script to work dir, remove checkout
		workDir = filepath.Join(tmpDir, "work")
		if err := os.MkdirAll(filepath.Join(workDir, filepath.Dir(job.Pipeline)), 0755); err != nil {
			w.log.WithError(err).WithField("run_id", runID).Error("failed to create work dir")
			w.sendErrorLog(jobID, fmt.Sprintf("failed to create work dir: %v", err))
			exitCode := 1
			w.client.ReportStatus(jobID, RunFailure, &exitCode)
			return
		}
		scriptPath = filepath.Join(workDir, job.Pipeline)
		if err := os.WriteFile(scriptPath, scriptContent, 0755); err != nil {
			w.log.WithError(err).WithField("run_id", runID).Error("failed to write script")
			w.sendErrorLog(jobID, fmt.Sprintf("failed to write script: %v", err))
			exitCode := 1
			w.client.ReportStatus(jobID, RunFailure, &exitCode)
			return
		}
		os.RemoveAll(checkout.Dir)
	}

	// Ensure env map is initialized (server may omit it)
	if job.Env == nil {
		job.Env = make(map[string]string)
	}

	// Fetch BYOV secrets if configured
	var secretValues []string
	if len(job.VaultConfigs) > 0 {
		vaultEnv, svals, err := FetchVaultSecrets(jobCtx, job.VaultConfigs)
		if err != nil {
			w.log.WithError(err).WithField("run_id", runID).Error("vault fetch failed")
			w.sendErrorLog(jobID, fmt.Sprintf("vault secret fetch failed: %v", err))
			exitCode := 1
			w.client.ReportStatus(jobID, RunFailure, &exitCode)
			return
		}
		for k, v := range vaultEnv {
			job.Env[k] = v
		}
		secretValues = svals
	}

	// Inject storage config as environment variables for orchestrator-managed modes.
	// In agent-direct mode the orchestrator does not send storage config — the agent
	// operator is expected to configure BUILD_STORAGE_* env vars on the host directly.
	if job.StorageConfig != nil {
		job.Env["BUILD_STORAGE_PROVIDER"] = job.StorageConfig.Provider
		job.Env["BUILD_STORAGE_BUCKET"] = job.StorageConfig.Bucket
		job.Env["BUILD_STORAGE_REGION"] = job.StorageConfig.Region
		if job.StorageConfig.Endpoint != "" {
			job.Env["BUILD_STORAGE_ENDPOINT"] = job.StorageConfig.Endpoint
		}
		if job.StorageConfig.Prefix != "" {
			job.Env["BUILD_STORAGE_PREFIX"] = job.StorageConfig.Prefix
		}
		job.Env["BUILD_STORAGE_MODE"] = job.StorageConfig.Mode
	}

	// Build environment
	env := os.Environ()
	for k, v := range job.Env {
		env = append(env, k+"="+v)
	}
	env = append(env, "BUILD_RUN_ID="+runID)
	env = append(env, "BUILD_JOB_ID="+jobID)
	env = append(env, "BUILD_SERVER_URL="+w.client.ServerURL())
	env = append(env, "BUILD_AGENT_TOKEN="+w.client.Token())
	env = append(env, "BUILD_TMPDIR="+tmpDir)
	env = append(env, "BUILD_VCS_TYPE="+job.VCSType)
	env = append(env, "BUILD_VCS_URL="+job.VCSURL)
	if job.RepoID != "" {
		env = append(env, "BUILD_REPO_ID="+job.RepoID)
	}
	if checkout.SHA != "" {
		env = append(env, "BUILD_COMMIT_SHA="+checkout.SHA)
	}

	// Resolve env mappings from trigger config (if present)
	if len(job.EnvMappings) > 0 && job.MatrixValues != nil {
		resolved, err := resolveEnvMappings(job.EnvMappings, job.MatrixValues)
		if err != nil {
			w.log.WithError(err).WithField("run_id", runID).Error("env mapping resolution failed")
			w.sendErrorLog(jobID, fmt.Sprintf("env mapping resolution failed: %v", err))
			exitCode := 1
			w.client.ReportStatus(jobID, RunFailure, &exitCode)
			return
		}
		for k, v := range resolved {
			env = append(env, k+"="+v)
		}
	} else if job.MatrixValues != nil {
		// Inject matrix values as MATRIX_* even without env mappings
		for k, v := range job.MatrixValues {
			envKey := "MATRIX_" + strings.ToUpper(strings.ReplaceAll(k, "-", "_"))
			env = append(env, envKey+"="+v)
		}
	}

	// For bash scripts, expose VCS credentials as GIT_USERNAME/GIT_PASSWORD.
	// For graphs, credentials are available via the secret-get node through
	// ACT_INPUT_SECRET_GIT_USERNAME/ACT_INPUT_SECRET_GIT_PASSWORD.
	if !strings.HasSuffix(job.Pipeline, ".act") {
		if v, ok := job.Env["ACT_INPUT_SECRET_GIT_USERNAME"]; ok {
			env = append(env, "GIT_USERNAME="+v)
		}
		if v, ok := job.Env["ACT_INPUT_SECRET_GIT_PASSWORD"]; ok {
			env = append(env, "GIT_PASSWORD="+v)
		}
	}

	// Resolve Docker image
	dockerImage := ResolveDockerImage(w.docker, string(scriptContent))
	useDocker := dockerImage != ""

	if useDocker {
		if _, err := exec.LookPath("docker"); err != nil {
			w.log.WithField("run_id", runID).Error("docker not found in PATH")
			w.sendErrorLog(jobID, "docker executable not found in PATH but script requires Docker image: "+dockerImage)
			exitCode := 1
			w.client.ReportStatus(jobID, RunFailure, &exitCode)
			return
		}
		w.log.WithFields(logrus.Fields{
			"run_id": runID,
			"image":  dockerImage,
		}).Info("using Docker image")
	}

	// Build command
	var cmd *exec.Cmd
	if strings.HasSuffix(job.Pipeline, ".act") {
		self, err := os.Executable()
		if err != nil {
			w.log.WithError(err).WithField("run_id", runID).Error("failed to resolve executable path")
			w.sendErrorLog(jobID, fmt.Sprintf("failed to resolve executable path: %v", err))
			exitCode := 1
			w.client.ReportStatus(jobID, RunFailure, &exitCode)
			return
		}
		cmd = exec.CommandContext(jobCtx, self, scriptPath)
		cmd.Dir = workDir
		cmd.Env = env
	} else if useDocker {
		relPath, _ := filepath.Rel(tmpDir, scriptPath)
		cmd = buildDockerCommand(jobCtx, dockerImage, runID, tmpDir, relPath, string(scriptContent), env)
	} else {
		cmd = buildNativeCommand(jobCtx, workDir, scriptPath, env)
	}

	// Capture stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		w.log.WithError(err).WithField("run_id", runID).Error("stdout pipe error")
		w.sendErrorLog(jobID, fmt.Sprintf("stdout pipe error: %v", err))
		exitCode := 1
		w.client.ReportStatus(jobID, RunFailure, &exitCode)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		w.log.WithError(err).WithField("run_id", runID).Error("stderr pipe error")
		w.sendErrorLog(jobID, fmt.Sprintf("stderr pipe error: %v", err))
		exitCode := 1
		w.client.ReportStatus(jobID, RunFailure, &exitCode)
		return
	}

	if err := cmd.Start(); err != nil {
		w.log.WithError(err).WithField("run_id", runID).Error("start error")
		w.sendErrorLog(jobID, fmt.Sprintf("failed to start: %v", err))
		exitCode := 1
		w.client.ReportStatus(jobID, RunFailure, &exitCode)
		return
	}

	// Scan pipes and batch-send logs
	var lineNum int64
	var logMu sync.Mutex
	var pendingLogs []LogEntry

	flushLogs := func() {
		logMu.Lock()
		batch := LogBatch{Lines: make([]LogEntry, len(pendingLogs))}
		copy(batch.Lines, pendingLogs)
		pendingLogs = pendingLogs[:0]
		logMu.Unlock()

		status, err := w.client.SendLogs(jobID, batch)
		if err != nil {
			w.log.WithError(err).WithField("run_id", runID).Warn("log send error")
			return
		}
		if status == "cancelled" {
			w.log.WithField("job_id", jobID).Info("job cancelled by server, stopping")
			jobCancel()
		}
	}

	// Periodic flush
	flushTicker := time.NewTicker(1 * time.Second)
	stopFlush := make(chan struct{})
	flushDone := make(chan struct{})
	go func() {
		defer close(flushDone)
		for {
			select {
			case <-flushTicker.C:
				flushLogs()
			case <-stopFlush:
				return
			}
		}
	}()

	scanPipe := func(pipe *bufio.Scanner, stream string) {
		for pipe.Scan() {
			content := pipe.Text()
			// Agent-side log masking for BYOV secrets
			for _, sv := range secretValues {
				if sv != "" && strings.Contains(content, sv) {
					content = strings.ReplaceAll(content, sv, "***")
				}
			}
			n := int(atomic.AddInt64(&lineNum, 1))
			logMu.Lock()
			pendingLogs = append(pendingLogs, LogEntry{
				LineNum: n,
				Stream:  stream,
				Content: content,
			})
			logMu.Unlock()
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanPipe(bufio.NewScanner(stdout), "stdout")
	}()
	go func() {
		defer wg.Done()
		scanPipe(bufio.NewScanner(stderr), "stderr")
	}()
	wg.Wait()

	// Stop flush ticker and do final flush
	flushTicker.Stop()
	close(stopFlush)
	<-flushDone
	flushLogs()

	// Wait for command
	err = cmd.Wait()

	// Best-effort Docker container cleanup on cancellation
	if useDocker {
		cleanupDockerContainer(runID)
	}

	// If the job was cancelled by the server, the status is already set in the DB.
	// Don't report status again — ciDeriveRunStatus would overwrite the run's
	// "cancelled" status with "failure".
	if jobCtx.Err() != nil {
		w.log.WithField("run_id", runID).Info("run cancelled")
		return
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	status := RunSuccess
	if exitCode != 0 {
		status = RunFailure
	}

	w.client.ReportStatus(jobID, status, &exitCode)
	w.log.WithFields(logrus.Fields{
		"run_id":    runID,
		"status":    status,
		"exit_code": exitCode,
	}).Info("run finished")
}

func (w *Worker) buildHeartbeatRequest() HeartbeatRequest {
	snap, err := Snapshot()
	if err != nil {
		w.log.WithError(err).Warn("metrics snapshot error")
		return HeartbeatRequest{UUID: w.uuid}
	}

	w.metricsMu.Lock()
	defer w.metricsMu.Unlock()

	req := HeartbeatRequest{UUID: w.uuid}
	if w.lastCounters != nil {
		// CPU percent
		if snap.CPUInstant {
			req.CPUPercent = float64(snap.CPUBusy) / 100.0
		} else {
			dTotal := snap.CPUTotal - w.lastCounters.CPUTotal
			dBusy := snap.CPUBusy - w.lastCounters.CPUBusy
			if dTotal > 0 {
				req.CPUPercent = float64(dBusy) / float64(dTotal) * 100.0
			}
		}
		// Memory (instantaneous, not a delta)
		req.MemUsedBytes = int64(snap.MemUsedBytes)
		req.MemTotalBytes = int64(snap.MemTotalBytes)
		if snap.MemTotalBytes > 0 {
			req.MemPercent = float64(snap.MemUsedBytes) / float64(snap.MemTotalBytes) * 100.0
		}
		// Network deltas
		req.NetRxBytes = int64(snap.NetRxBytes - w.lastCounters.NetRxBytes)
		req.NetTxBytes = int64(snap.NetTxBytes - w.lastCounters.NetTxBytes)
	}
	w.lastCounters = &snap
	return req
}
