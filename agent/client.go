package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/actionforge/actrun-cli/build"
)

// ErrInstanceRevoked is returned when the gateway reports this process's
// instance secret is no longer valid. The worker unwinds, re-registers
// with the agent token, and starts a fresh loop.
var ErrInstanceRevoked = errors.New("agent instance revoked: re-register required")

// ErrAgentAlreadyRunning is returned by AcquireInstanceLock when another
// process on this machine already holds the lock for the same (server, token).
var ErrAgentAlreadyRunning = errors.New("another agent is already running for this token")

// ErrInvalidAgentToken is returned by Register when the gateway rejects
// the enrollment token (HTTP 401). This is a fatal, non-retryable error.
var ErrInvalidAgentToken = errors.New("register: invalid agent token")

// InstanceCred is the per-process credential persisted between restarts.
// We never write the agent token to disk — only the server-minted
// secret, which is narrower (scoped to one instance, revocable).
type InstanceCred struct {
	InstanceID      string `json:"instance_id"`
	InstanceSecret  string `json:"instance_secret"`
	PoolID          string `json:"pool_id"`
	PoolName        string `json:"pool_name"`
	ServerURL       string `json:"server_url"`
	PoolFingerprint string `json:"pool_fingerprint"`
}

// registerRequest mirrors CIRunnerRegisterRequest on the gateway side.
type registerRequest struct {
	Name     string `json:"name,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	OS       string `json:"os,omitempty"`
	Arch     string `json:"arch,omitempty"`
	Version  string `json:"version,omitempty"`
}

// registerResponse mirrors CIRunnerRegisterResponse.
type registerResponse struct {
	InstanceID     string `json:"instance_id"`
	InstanceSecret string `json:"instance_secret"`
	PoolID         string `json:"pool_id"`
	PoolName       string `json:"pool_name"`
}

// Client talks to the gateway's runner protocol endpoints using an
// instance secret as the bearer. Construct via NewClientFromCred after
// obtaining a credential from Register or loadInstanceCred.
type Client struct {
	serverURL  string
	cred       *InstanceCred
	httpClient *http.Client
}

// NewClientFromCred wraps an existing credential for subsequent runner
// API calls.
func NewClientFromCred(serverURL string, cred *InstanceCred) *Client {
	return &Client{
		serverURL: serverURL,
		cred:      cred,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClientFromSecret builds a client from a bare instance secret, used
// by child processes (the node reporter in cmd_root) that inherit
// BUILD_AGENT_TOKEN / BUILD_SERVER_URL in their environment and don't
// need the full credential struct.
func NewClientFromSecret(serverURL, instanceSecret string) *Client {
	return NewClientFromCred(serverURL, &InstanceCred{InstanceSecret: instanceSecret})
}

// Cred returns the active instance credential.
func (c *Client) Cred() *InstanceCred {
	return c.cred
}

// InstanceCredPath returns the stable disk location for a credential
// keyed on (serverURL, agentToken). Keeping the hash short keeps
// paths readable while avoiding collisions on machines that run multiple
// pools at once.
func InstanceCredPath(serverURL, agentToken string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.TrimSuffix(serverURL, "/") + "\x00" + agentToken))
	return filepath.Join(dir, "actrun", "instance-"+hex.EncodeToString(sum[:])+".json"), nil
}

// poolFingerprint returns a stable hash of the enrollment token so a
// stored credential can detect "this file was written for a different
// token" without ever persisting the raw token itself.
func poolFingerprint(agentToken string) string {
	sum := sha256.Sum256([]byte(agentToken))
	return hex.EncodeToString(sum[:])
}

// LoadInstanceCred returns an existing credential if one is on disk and
// matches the given (serverURL, agentToken). A mismatch on either
// field invalidates the file so the caller can re-register against the
// new server or token.
func LoadInstanceCred(serverURL, agentToken string) (*InstanceCred, error) {
	path, err := InstanceCredPath(serverURL, agentToken)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cred InstanceCred
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, fmt.Errorf("parse instance cred: %w", err)
	}
	if cred.ServerURL != serverURL || cred.PoolFingerprint != poolFingerprint(agentToken) {
		return nil, nil
	}
	if cred.InstanceSecret == "" || cred.InstanceID == "" {
		return nil, nil
	}
	return &cred, nil
}

// saveInstanceCred persists a credential to disk with owner-only
// permissions so another user on the host can't steal the secret.
func saveInstanceCred(serverURL, agentToken string, cred *InstanceCred) error {
	path, err := InstanceCredPath(serverURL, agentToken)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// DeleteInstanceCred removes the on-disk credential file for a given
// (serverURL, agentToken). Called after a successful deregister, or
// after a 401 tells us the credential is no longer valid.
func DeleteInstanceCred(serverURL, agentToken string) error {
	path, err := InstanceCredPath(serverURL, agentToken)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// instanceLockPath returns the lockfile path next to the credential file.
func instanceLockPath(serverURL, agentToken string) (string, error) {
	credPath, err := InstanceCredPath(serverURL, agentToken)
	if err != nil {
		return "", err
	}
	return credPath + ".lock", nil
}

// AcquireInstanceLock and ReleaseInstanceLock are in
// lock_unix.go / lock_windows.go (platform-specific implementations).

// Register exchanges an agent token for a per-process agent credential.
// The resulting credential is persisted to disk so a future restart can
// skip this round trip entirely.
func Register(serverURL, agentToken string) (*InstanceCred, error) {
	hostname, _ := os.Hostname()
	body := registerRequest{
		Name:     hostname,
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Version:  build.Version,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/v2/ci/runner/register", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+agentToken)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
	case http.StatusUnauthorized:
		return nil, ErrInvalidAgentToken
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("register: %s %s", resp.Status, string(body))
	}

	var out registerResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode register response: %w", err)
	}
	cred := &InstanceCred{
		InstanceID:      out.InstanceID,
		InstanceSecret:  out.InstanceSecret,
		PoolID:          out.PoolID,
		PoolName:        out.PoolName,
		ServerURL:       serverURL,
		PoolFingerprint: poolFingerprint(agentToken),
	}
	if err := saveInstanceCred(serverURL, agentToken, cred); err != nil {
		// Persistence failure is non-fatal: the process can still run
		// with the in-memory credential, it just won't survive restart.
		_ = err
	}
	return cred, nil
}

func (c *Client) doRequest(method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.serverURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cred.InstanceSecret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		// Only treat 401 as revocation on runner-protocol endpoints.
		// A proxy or WAF returning 401 should not trigger re-registration.
		if strings.HasPrefix(path, "/api/v2/ci/runner/") {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return nil, ErrInstanceRevoked
		}
	}
	return resp, nil
}

func (c *Client) ServerURL() string {
	return c.serverURL
}

func (c *Client) Claim() (*ClaimResponse, error) {
	resp, err := c.doRequest("POST", "/api/v2/ci/runner/claim", nil)
	if err != nil {
		return nil, fmt.Errorf("claim request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("claim failed: %s %s", resp.Status, string(body))
	}

	var claim ClaimResponse
	if err := json.NewDecoder(resp.Body).Decode(&claim); err != nil {
		return nil, fmt.Errorf("decode claim response: %w", err)
	}
	return &claim, nil
}

// drainAndCheck reads the response body to allow connection reuse and returns
// an error if the status code is not in the 2xx range.
func drainAndCheck(resp *http.Response) error {
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}
	return nil
}

// SendLogs sends a batch of log lines and returns the current job status from the server.
func (c *Client) SendLogs(jobID string, batch LogBatch) (string, error) {
	resp, err := c.doRequest("POST", fmt.Sprintf("/api/v2/ci/runner/logs/%s", jobID), batch)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("unexpected status: %s", resp.Status)
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil // non-fatal: old server without status in response
	}
	return result.Status, nil
}

func (c *Client) ReportStatus(jobID string, status RunStatus, exitCode *int) error {
	report := StatusReport{
		Status:   status,
		ExitCode: exitCode,
	}
	resp, err := c.doRequest("PATCH", fmt.Sprintf("/api/v2/ci/runner/jobs/%s", jobID), report)
	if err != nil {
		return err
	}
	return drainAndCheck(resp)
}

func (c *Client) SubmitGraph(jobID string, graph string) error {
	resp, err := c.doRequest("POST", fmt.Sprintf("/api/v2/ci/runner/jobs/%s/graph", jobID), map[string]string{"graph": graph})
	if err != nil {
		return err
	}
	return drainAndCheck(resp)
}

func (c *Client) ReportRef(jobID, commitSHA string) error {
	resp, err := c.doRequest("POST", fmt.Sprintf("/api/v2/ci/runner/jobs/%s/ref", jobID), map[string]string{"commit_sha": commitSHA})
	if err != nil {
		return err
	}
	return drainAndCheck(resp)
}

func (c *Client) SubmitActiveNodes(jobID string, nodes []ActiveNode) error {
	resp, err := c.doRequest("POST", fmt.Sprintf("/api/v2/ci/runner/jobs/%s/nodes", jobID), map[string]interface{}{"active_nodes": nodes})
	if err != nil {
		return err
	}
	return drainAndCheck(resp)
}

// HeartbeatResponse carries pool metadata back from the gateway. Labels
// are included so the agent always has a fresh view without caching.
type HeartbeatResponse struct {
	Labels string `json:"labels"`
}

func (c *Client) Heartbeat(req HeartbeatRequest) (*HeartbeatResponse, error) {
	resp, err := c.doRequest("POST", "/api/v2/ci/runner/heartbeat", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}
	var out HeartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return &HeartbeatResponse{}, nil // non-fatal: old server without response body
	}
	return &out, nil
}

// Deregister tells the gateway this process is shutting down so the
// instance row is removed immediately. Graceful shutdown path only —
// crash-exit will be cleaned up by the gateway's stale sweeper.
func (c *Client) Deregister() error {
	resp, err := c.doRequest("POST", "/api/v2/ci/runner/deregister", nil)
	if err != nil {
		return err
	}
	return drainAndCheck(resp)
}
