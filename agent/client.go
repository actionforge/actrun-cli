package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	serverURL  string
	token      string
	uuid       string
	httpClient *http.Client
}

func NewClient(serverURL, token string) *Client {
	return &Client{
		serverURL: serverURL,
		token:     token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) SetUUID(uuid string) {
	c.uuid = uuid
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
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.uuid != "" {
		req.Header.Set("X-Agent-UUID", c.uuid)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

func (c *Client) ServerURL() string {
	return c.serverURL
}

func (c *Client) Token() string {
	return c.token
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

func (c *Client) Heartbeat(req HeartbeatRequest) error {
	resp, err := c.doRequest("POST", "/api/v2/ci/runner/heartbeat", req)
	if err != nil {
		return err
	}
	return drainAndCheck(resp)
}


func (c *Client) Disconnect() error {
	resp, err := c.doRequest("POST", "/api/v2/ci/runner/disconnect", nil)
	if err != nil {
		return err
	}
	return drainAndCheck(resp)
}
