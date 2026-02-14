package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestStartServerAndStop(t *testing.T) {
	rs, err := StartServer(Config{
		StorageDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer rs.Stop()

	if rs.URL == "" {
		t.Fatal("URL is empty")
	}
	if rs.RuntimeToken == "" {
		t.Fatal("RuntimeToken is empty")
	}
	if !strings.HasPrefix(rs.URL, "http://127.0.0.1:") {
		t.Fatalf("unexpected URL: %s", rs.URL)
	}

	// Verify server is reachable
	resp, err := http.Get(rs.URL + "/_services/token")
	if err != nil {
		t.Fatalf("server not reachable: %v", err)
	}
	resp.Body.Close()
	// 401 is expected (no auth header) but it confirms the server is running
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestInjectEnv(t *testing.T) {
	rs, err := StartServer(Config{
		StorageDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer rs.Stop()

	env := make(map[string]string)
	rs.InjectEnv(env)

	expected := []string{
		"ACTIONS_RUNTIME_URL",
		"ACTIONS_RUNTIME_TOKEN",
		"ACTIONS_CACHE_URL",
		"ACTIONS_RESULTS_URL",
		"ACTIONS_ID_TOKEN_REQUEST_URL",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN",
	}
	for _, key := range expected {
		if env[key] == "" {
			t.Errorf("missing env var: %s", key)
		}
	}

	if env["ACTIONS_RUNTIME_URL"] != rs.URL+"/" {
		t.Errorf("ACTIONS_RUNTIME_URL=%q, want %q", env["ACTIONS_RUNTIME_URL"], rs.URL+"/")
	}
	if env["ACTIONS_RUNTIME_TOKEN"] != rs.RuntimeToken {
		t.Error("ACTIONS_RUNTIME_TOKEN mismatch")
	}
	if env["ACTIONS_ID_TOKEN_REQUEST_URL"] != rs.URL+"/_services/token" {
		t.Errorf("ACTIONS_ID_TOKEN_REQUEST_URL=%q", env["ACTIONS_ID_TOKEN_REQUEST_URL"])
	}
}

func TestStartServerDefaultConfig(t *testing.T) {
	rs, err := StartServer(Config{
		StorageDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}
	defer rs.Stop()

	// Verify runtime token is a valid JWT that the server accepts
	req, _ := http.NewRequest("GET", rs.URL+"/_services/token", nil)
	req.Header.Set("Authorization", "Bearer "+rs.RuntimeToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d", resp.StatusCode)
	}
}

func TestStopMakesServerUnreachable(t *testing.T) {
	rs, err := StartServer(Config{
		StorageDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("StartServer: %v", err)
	}

	url := rs.URL
	rs.Stop()

	// Server should no longer be reachable
	_, err = http.Get(url + "/_services/token")
	if err == nil {
		t.Fatal("expected error after Stop, server still reachable")
	}
}
