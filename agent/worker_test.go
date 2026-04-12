package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/actionforge/actrun-cli/agent/vcs"
)

func TestBuildHeartbeatRequest_Deltas(t *testing.T) {
	srv, _ := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "s"})
	w := NewWorker(c, DockerConfig{}, defaultVcsOpts())

	// Seed initial counters
	w.metricsMu.Lock()
	w.lastCounters = &RawCounters{
		CPUBusy:       100,
		CPUTotal:      1000,
		NetRxBytes:    500,
		NetTxBytes:    200,
		MemUsedBytes:  1024 * 1024,
		MemTotalBytes: 4 * 1024 * 1024,
	}
	w.metricsMu.Unlock()

	req := w.buildHeartbeatRequest()

	// Memory should be reported (instantaneous, not delta)
	if req.MemTotalBytes == 0 {
		t.Fatal("expected non-zero MemTotalBytes")
	}
}

// TestBuildHeartbeatRequest_Concurrent exercises the metrics mutex under the
// race detector by calling buildHeartbeatRequest from multiple goroutines
// simultaneously, mimicking the heartbeat ticker firing while the main
// loop also reads metrics.
func TestBuildHeartbeatRequest_Concurrent(t *testing.T) {
	srv, _ := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "s"})
	w := NewWorker(c, DockerConfig{}, defaultVcsOpts())

	w.metricsMu.Lock()
	w.lastCounters = &RawCounters{
		CPUBusy:  50,
		CPUTotal: 1000,
	}
	w.metricsMu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.buildHeartbeatRequest()
		}()
	}
	wg.Wait()
}

// TestWorkerRun_IdlePollAndShutdown starts a Worker.Run against a mock server
// that never returns jobs, verifies the heartbeat goroutine runs, then
// cancels context for graceful shutdown. This exercises:
// - Heartbeat goroutine vs main claim loop (concurrent access)
// - Context cancellation shutdown path
// - waitHeartbeat synchronization
func TestWorkerRun_IdlePollAndShutdown(t *testing.T) {
	srv, gw := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "s"})
	w := NewWorker(c, DockerConfig{}, defaultVcsOpts())
	// Speed up polling for test
	w.pollInterval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
	}()

	// Wait for at least one claim attempt (initial heartbeat calls
	// Snapshot() which shells out to top/netstat on macOS, ~0.5s)
	deadline := time.After(5 * time.Second)
	for gw.ClaimCount.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for first claim")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected clean shutdown, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Worker.Run did not return after cancel")
	}
}

// TestWorkerRun_HeartbeatRevocation verifies that when the heartbeat gets
// a 401, the worker cancels its context and returns ErrInstanceRevoked.
func TestWorkerRun_HeartbeatRevocation(t *testing.T) {
	srv, gw := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "s"})
	w := NewWorker(c, DockerConfig{}, defaultVcsOpts())
	w.pollInterval = 50 * time.Millisecond
	w.heartbeatInterval = 100 * time.Millisecond

	// Initial heartbeat succeeds, then revoke after worker starts
	go func() {
		time.Sleep(200 * time.Millisecond)
		gw.SetHeartbeatError(401)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := w.Run(ctx)
	if err != ErrInstanceRevoked {
		t.Fatalf("expected ErrInstanceRevoked, got %v", err)
	}
}

// TestWorkerRun_ConnectionLost verifies that consecutive claim errors
// cause the worker to return ErrConnectionLost.
func TestWorkerRun_ConnectionLost(t *testing.T) {
	// Use a server that immediately closes connections
	srv := newBrokenServer(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "s"})
	w := NewWorker(c, DockerConfig{}, defaultVcsOpts())
	w.pollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := w.Run(ctx)
	if err != ErrConnectionLost {
		t.Fatalf("expected ErrConnectionLost, got %v", err)
	}
}

// TestLogBuffering_Concurrent mimics the log buffering pattern used in
// execute(): multiple scanner goroutines append to a shared slice under
// a mutex while a flush goroutine periodically drains it.
func TestLogBuffering_Concurrent(t *testing.T) {
	var logMu sync.Mutex
	var pendingLogs []LogEntry
	var lineNum int64

	// Flush function (mirrors execute's flushLogs)
	flush := func() []LogEntry {
		logMu.Lock()
		batch := make([]LogEntry, len(pendingLogs))
		copy(batch, pendingLogs)
		pendingLogs = pendingLogs[:0]
		logMu.Unlock()
		return batch
	}

	// Append function (mirrors execute's scanPipe)
	appendLog := func(stream, content string) {
		n := int(atomic.AddInt64(&lineNum, 1))
		logMu.Lock()
		pendingLogs = append(pendingLogs, LogEntry{
			LineNum: n,
			Stream:  stream,
			Content: content,
		})
		logMu.Unlock()
	}

	// Run concurrent appenders + periodic flusher
	ctx, cancel := context.WithCancel(context.Background())
	var totalFlushed int64

	// Flusher goroutine
	flusherDone := make(chan struct{})
	go func() {
		defer close(flusherDone)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				batch := flush()
				atomic.AddInt64(&totalFlushed, int64(len(batch)))
			case <-ctx.Done():
				return
			}
		}
	}()

	// Scanner goroutines
	var wg sync.WaitGroup
	for s := 0; s < 2; s++ {
		stream := "stdout"
		if s == 1 {
			stream = "stderr"
		}
		wg.Add(1)
		go func(st string) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				appendLog(st, "line")
			}
		}(stream)
	}
	wg.Wait()

	// Stop flusher, do final flush
	cancel()
	<-flusherDone
	remaining := flush()
	total := atomic.LoadInt64(&totalFlushed) + int64(len(remaining))

	if total != 1000 {
		t.Fatalf("expected 1000 lines total, got %d", total)
	}
}

// --- helpers ---

func defaultVcsOpts() vcs.Options {
	return vcs.Options{}
}

// newBrokenServer returns a server that returns 500 on heartbeat (so initial
// heartbeat doesn't block) and closes connections on claim.
func newBrokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/ci/runner/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Force connection close to simulate network error
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				conn.Close()
				return
			}
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
