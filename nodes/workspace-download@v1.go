package nodes

import (
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed workspace-download@v1.yml
var workspaceDownloadDefinition string

type WorkspaceDownloadNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *WorkspaceDownloadNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	serverURL := envOrOs(c, "BUILD_SERVER_URL")
	token := envOrOs(c, "BUILD_AGENT_TOKEN")
	repoID := envOrOs(c, "BUILD_REPO_ID")

	if serverURL == "" || token == "" || repoID == "" {
		downloadErr := core.CreateErr(c, nil, "workspace download requires BUILD_SERVER_URL, BUILD_AGENT_TOKEN, and BUILD_REPO_ID environment variables (only available when running via agent)")
		return n.Execute(ni.Core_workspace_download_v1_Output_exec_err, c, downloadErr)
	}

	paths, err := core.InputValueById[[]string](c, n, ni.Core_workspace_download_v1_Input_paths)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Minute}

	for _, filePath := range paths {
		if err := downloadWorkspaceFile(c, client, serverURL, token, repoID, filePath); err != nil {
			return n.Execute(ni.Core_workspace_download_v1_Output_exec_err, c, err)
		}
	}

	return n.Execute(ni.Core_workspace_download_v1_Output_exec_success, c, nil)
}

func downloadWorkspaceFile(c *core.ExecutionState, client *http.Client, serverURL, token, repoID, filePath string) error {
	url := fmt.Sprintf("%s/api/v2/ci/runner/workspace/%s/file/%s", serverURL, repoID, filePath)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return core.CreateErr(c, err, "failed to create request for %s", filePath)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return core.CreateErr(c, err, "failed to download %s", filePath)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return core.CreateErr(c, nil, "failed to download %s: status %d: %s", filePath, resp.StatusCode, string(body))
	}

	// Create parent directories and write the file at the same relative path.
	dir := filepath.Dir(filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return core.CreateErr(c, err, "failed to create directory for %s", filePath)
		}
	}

	f, err := os.Create(filePath)
	if err != nil {
		return core.CreateErr(c, err, "failed to create file %s", filePath)
	}

	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return core.CreateErr(c, copyErr, "failed to write %s", filePath)
	}
	if closeErr != nil {
		return core.CreateErr(c, closeErr, "failed to close %s", filePath)
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(workspaceDownloadDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &WorkspaceDownloadNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
