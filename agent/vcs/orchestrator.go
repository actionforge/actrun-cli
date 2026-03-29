package vcs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// OrchestratorProvider downloads only the pipeline file from the orchestrator
// workspace endpoint. The full workspace can later be fetched during graph
// execution via the workspace-download node.
type OrchestratorProvider struct {
	serverURL      string
	repoID         string
	workspaceToken string
}

func (o *OrchestratorProvider) Checkout(ctx context.Context, _ string, ref, pipeline, destDir string) (CheckoutResult, error) {
	if o.repoID == "" {
		return CheckoutResult{}, fmt.Errorf("orchestrator checkout requires a repo ID")
	}
	if o.serverURL == "" || o.workspaceToken == "" {
		return CheckoutResult{}, fmt.Errorf("orchestrator checkout requires server URL and workspace token")
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return CheckoutResult{}, fmt.Errorf("create checkout dir: %w", err)
	}

	// Download only the pipeline file via the workspace file endpoint.
	client := &http.Client{Timeout: 2 * time.Minute}
	reqURL := fmt.Sprintf("%s/api/v2/ci/runner/workspace/%s/file/%s?token=%s",
		o.serverURL, url.PathEscape(o.repoID), url.PathEscape(pipeline), url.QueryEscape(o.workspaceToken))

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("download pipeline file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return CheckoutResult{}, fmt.Errorf("download pipeline file: status %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("read pipeline response: %w", err)
	}

	// Write the pipeline file at the expected relative path inside destDir.
	filePath := filepath.Join(destDir, pipeline)
	if dir := filepath.Dir(filePath); dir != destDir {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return CheckoutResult{}, fmt.Errorf("create pipeline dir: %w", err)
		}
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return CheckoutResult{}, fmt.Errorf("write pipeline file: %w", err)
	}

	return CheckoutResult{Dir: destDir}, nil
}

func (o *OrchestratorProvider) Cleanup(ctx context.Context) error {
	return nil
}
