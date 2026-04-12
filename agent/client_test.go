package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// newMockGateway returns an httptest.Server that implements the runner protocol
// endpoints with configurable behavior via the returned *mockGateway handle.
type mockGateway struct {
	ClaimCount     atomic.Int64
	HeartbeatCount atomic.Int64
	LogBatches     atomic.Int64
	StatusReports  atomic.Int64
	DeregisterCount atomic.Int64

	mu           sync.Mutex
	claimResp    *ClaimResponse // nil → 204 No Content
	heartbeatErr int            // non-zero → return this HTTP status
	logStatus    string         // returned in SendLogs response
}

func newMockGateway(t *testing.T) (*httptest.Server, *mockGateway) {
	t.Helper()
	gw := &mockGateway{logStatus: "running"}

	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/v2/ci/runner/claim", func(w http.ResponseWriter, r *http.Request) {
		gw.ClaimCount.Add(1)
		gw.mu.Lock()
		resp := gw.claimResp
		gw.mu.Unlock()

		if resp == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("POST /api/v2/ci/runner/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		gw.HeartbeatCount.Add(1)
		gw.mu.Lock()
		errCode := gw.heartbeatErr
		gw.mu.Unlock()

		if errCode != 0 {
			w.WriteHeader(errCode)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HeartbeatResponse{Labels: "test"})
	})

	mux.HandleFunc("POST /api/v2/ci/runner/logs/", func(w http.ResponseWriter, r *http.Request) {
		gw.LogBatches.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		gw.mu.Lock()
		status := gw.logStatus
		gw.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": status})
	})

	mux.HandleFunc("PATCH /api/v2/ci/runner/jobs/", func(w http.ResponseWriter, r *http.Request) {
		gw.StatusReports.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("POST /api/v2/ci/runner/jobs/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("POST /api/v2/ci/runner/deregister", func(w http.ResponseWriter, r *http.Request) {
		gw.DeregisterCount.Add(1)
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, gw
}

func (gw *mockGateway) SetClaim(resp *ClaimResponse) {
	gw.mu.Lock()
	gw.claimResp = resp
	gw.mu.Unlock()
}

func (gw *mockGateway) SetHeartbeatError(code int) {
	gw.mu.Lock()
	gw.heartbeatErr = code
	gw.mu.Unlock()
}

func (gw *mockGateway) SetLogStatus(s string) {
	gw.mu.Lock()
	gw.logStatus = s
	gw.mu.Unlock()
}

// --- Client HTTP tests ---

func TestClient_Claim_NoJob(t *testing.T) {
	srv, _ := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "test-secret"})

	job, err := c.Claim()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job != nil {
		t.Fatal("expected nil job for 204")
	}
}

func TestClient_Claim_WithJob(t *testing.T) {
	srv, gw := newMockGateway(t)
	gw.SetClaim(&ClaimResponse{
		RunID:    "run-1",
		JobID:    "job-1",
		Owner:    "org",
		Name:     "repo",
		Pipeline: "ci.sh",
		VCSType:  "git",
	})
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "test-secret"})

	job, err := c.Claim()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job == nil {
		t.Fatal("expected job, got nil")
	}
	if job.JobID != "job-1" {
		t.Fatalf("expected job-1, got %s", job.JobID)
	}
}

func TestClient_Heartbeat(t *testing.T) {
	srv, _ := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "test-secret"})

	resp, err := c.Heartbeat(HeartbeatRequest{CPUPercent: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Labels != "test" {
		t.Fatalf("expected labels 'test', got %q", resp.Labels)
	}
}

func TestClient_Heartbeat_Revoked(t *testing.T) {
	srv, gw := newMockGateway(t)
	gw.SetHeartbeatError(http.StatusUnauthorized)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "test-secret"})

	_, err := c.Heartbeat(HeartbeatRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != ErrInstanceRevoked.Error() {
		t.Fatalf("expected ErrInstanceRevoked, got %v", err)
	}
}

func TestClient_SendLogs(t *testing.T) {
	srv, gw := newMockGateway(t)
	gw.SetLogStatus("running")
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "test-secret"})

	status, err := c.SendLogs("job-1", LogBatch{Lines: []LogEntry{
		{LineNum: 1, Stream: "stdout", Content: "hello"},
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "running" {
		t.Fatalf("expected 'running', got %q", status)
	}
}

func TestClient_SendLogs_Cancelled(t *testing.T) {
	srv, gw := newMockGateway(t)
	gw.SetLogStatus("cancelled")
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "test-secret"})

	status, err := c.SendLogs("job-1", LogBatch{Lines: []LogEntry{
		{LineNum: 1, Stream: "stdout", Content: "hello"},
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("expected 'cancelled', got %q", status)
	}
}

func TestClient_ReportStatus(t *testing.T) {
	srv, _ := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "test-secret"})

	exitCode := 0
	err := c.ReportStatus("job-1", RunSuccess, &exitCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_Deregister(t *testing.T) {
	srv, gw := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "test-secret"})

	err := c.Deregister()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gw.DeregisterCount.Load() != 1 {
		t.Fatal("expected 1 deregister call")
	}
}

// --- Credential persistence tests ---

func TestCredentialRoundTrip(t *testing.T) {
	// Override config dir to temp
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	// macOS uses ~/Library but UserConfigDir falls back to XDG on test
	// We test the save/load logic, not the dir resolution.
	origCred := &InstanceCred{
		InstanceID:      "inst-1",
		InstanceSecret:  "secret-abc",
		PoolID:          "pool-1",
		PoolName:        "default",
		ServerURL:       "https://example.com",
		PoolFingerprint: poolFingerprint("token-xyz"),
	}

	err := saveInstanceCred("https://example.com", "token-xyz", origCred)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadInstanceCred("https://example.com", "token-xyz")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected credential, got nil")
	}
	if loaded.InstanceID != origCred.InstanceID {
		t.Fatalf("instance_id mismatch: %q vs %q", loaded.InstanceID, origCred.InstanceID)
	}
	if loaded.InstanceSecret != origCred.InstanceSecret {
		t.Fatalf("secret mismatch")
	}
}

func TestCredentialMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cred := &InstanceCred{
		InstanceID:      "inst-1",
		InstanceSecret:  "secret",
		PoolID:          "pool-1",
		PoolName:        "default",
		ServerURL:       "https://a.com",
		PoolFingerprint: poolFingerprint("token-a"),
	}
	_ = saveInstanceCred("https://a.com", "token-a", cred)

	// Different token → nil
	loaded, err := LoadInstanceCred("https://a.com", "token-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded != nil {
		t.Fatal("expected nil for mismatched token")
	}
}

func TestCredentialAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cred := &InstanceCred{
		InstanceID:      "inst-1",
		InstanceSecret:  "secret",
		PoolID:          "pool-1",
		PoolName:        "default",
		ServerURL:       "https://example.com",
		PoolFingerprint: poolFingerprint("tok"),
	}

	err := saveInstanceCred("https://example.com", "tok", cred)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	// Verify no .tmp file left behind
	path, _ := InstanceCredPath("https://example.com", "tok")
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file should not exist after atomic write")
	}

	// Verify actual file exists and is valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var check InstanceCred
	if err := json.Unmarshal(data, &check); err != nil {
		t.Fatalf("corrupt JSON: %v", err)
	}
}

func TestDeleteInstanceCred(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cred := &InstanceCred{
		InstanceID:      "inst-1",
		InstanceSecret:  "secret",
		PoolID:          "pool-1",
		PoolName:        "default",
		ServerURL:       "https://example.com",
		PoolFingerprint: poolFingerprint("tok"),
	}
	_ = saveInstanceCred("https://example.com", "tok", cred)

	err := DeleteInstanceCred("https://example.com", "tok")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	path, _ := InstanceCredPath("https://example.com", "tok")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("credential file should be gone")
	}
}

func TestDeleteInstanceCred_NotExist(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Deleting non-existent credential should not error
	err := DeleteInstanceCred("https://example.com", "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstanceCredPath_Deterministic(t *testing.T) {
	p1, err := InstanceCredPath("https://example.com", "tok")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := InstanceCredPath("https://example.com", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		t.Fatalf("paths should be deterministic: %q vs %q", p1, p2)
	}

	// Different token → different path
	p3, _ := InstanceCredPath("https://example.com", "other")
	if p1 == p3 {
		t.Fatal("different tokens should produce different paths")
	}
}

func TestPoolFingerprint_Stable(t *testing.T) {
	fp1 := poolFingerprint("token-abc")
	fp2 := poolFingerprint("token-abc")
	if fp1 != fp2 {
		t.Fatal("fingerprint should be stable")
	}
	fp3 := poolFingerprint("token-xyz")
	if fp1 == fp3 {
		t.Fatal("different tokens should have different fingerprints")
	}
}

// --- Concurrent client access (race detector exercise) ---

func TestClient_ConcurrentRequests(t *testing.T) {
	srv, _ := newMockGateway(t)
	c := NewClientFromCred(srv.URL, &InstanceCred{InstanceSecret: "test-secret"})

	// Hammer multiple endpoints concurrently to exercise race detector
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			c.Claim()
		}()
		go func() {
			defer wg.Done()
			c.Heartbeat(HeartbeatRequest{CPUPercent: 42})
		}()
		go func() {
			defer wg.Done()
			c.SendLogs("job-1", LogBatch{Lines: []LogEntry{
				{LineNum: 1, Stream: "stdout", Content: "line"},
			}})
		}()
	}
	wg.Wait()
}

// --- Concurrent credential persistence (race detector exercise) ---

func TestCredential_ConcurrentSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			cred := &InstanceCred{
				InstanceID:      "inst-1",
				InstanceSecret:  "secret",
				PoolID:          "pool-1",
				PoolName:        "default",
				ServerURL:       "https://example.com",
				PoolFingerprint: poolFingerprint("tok"),
			}
			saveInstanceCred("https://example.com", "tok", cred)
		}()
		go func() {
			defer wg.Done()
			LoadInstanceCred("https://example.com", "tok")
		}()
	}
	wg.Wait()

	// Verify file is still valid after concurrent writes
	loaded, err := LoadInstanceCred("https://example.com", "tok")
	if err != nil {
		t.Fatalf("load after concurrent writes: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected valid credential after concurrent writes")
	}
}

// --- Lock tests (unix only, skipped on windows) ---

func TestInstanceLock_Exclusive(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a fake credential path structure
	lockPath := filepath.Join(tmpDir, "actrun", "test.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}

	// Use temp dir-based server/token to get predictable lock path
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	f1, err := AcquireInstanceLock("https://test.com", "tok1")
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer ReleaseInstanceLock(f1)

	// Second lock with same params should fail
	_, err = AcquireInstanceLock("https://test.com", "tok1")
	if err != ErrAgentAlreadyRunning {
		t.Fatalf("expected ErrAgentAlreadyRunning, got %v", err)
	}

	// Different token should succeed
	f2, err := AcquireInstanceLock("https://test.com", "tok2")
	if err != nil {
		t.Fatalf("different token lock: %v", err)
	}
	ReleaseInstanceLock(f2)
}

func TestInstanceLock_ReleaseAndReacquire(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	f1, err := AcquireInstanceLock("https://test.com", "tok")
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	ReleaseInstanceLock(f1)

	// Should be reacquirable after release
	f2, err := AcquireInstanceLock("https://test.com", "tok")
	if err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
	ReleaseInstanceLock(f2)
}
