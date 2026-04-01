package nodes

import (
	"archive/tar"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed repo-download@v1.yml
var repoDownloadDefinition string

type RepoDownloadNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *RepoDownloadNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	serverURL := envOrOs(c, "BUILD_SERVER_URL")
	repoID := envOrOs(c, "BUILD_REPO_ID")
	wsToken := envOrOs(c, "BUILD_REPO_TOKEN")

	if serverURL == "" || repoID == "" || wsToken == "" {
		downloadErr := core.CreateErr(c, nil, "repo download requires BUILD_SERVER_URL, BUILD_REPO_ID, and BUILD_REPO_TOKEN environment variables (only available for orchestrator repos)")
		return n.Execute(ni.Core_repo_download_v1_Output_exec_err, c, downloadErr)
	}

	paths, err := core.InputValueById[[]string](c, n, ni.Core_repo_download_v1_Input_paths)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Minute}

	// "*" means download the entire repo as a tar.gz archive.
	if len(paths) == 1 && paths[0] == "*" {
		if err := downloadAndExtractRepo(c, client, serverURL, repoID, wsToken); err != nil {
			return n.Execute(ni.Core_repo_download_v1_Output_exec_err, c, err)
		}
		return n.Execute(ni.Core_repo_download_v1_Output_exec_success, c, nil)
	}

	for _, filePath := range paths {
		if err := validateRelativePath(filePath); err != nil {
			return n.Execute(ni.Core_repo_download_v1_Output_exec_err, c, core.CreateErr(c, nil, "invalid path %q: %v", filePath, err))
		}
		if err := downloadRepoFile(c, client, serverURL, wsToken, repoID, filePath); err != nil {
			return n.Execute(ni.Core_repo_download_v1_Output_exec_err, c, err)
		}
	}

	return n.Execute(ni.Core_repo_download_v1_Output_exec_success, c, nil)
}

// validateRelativePath ensures the path is relative and doesn't escape the
// working directory via ".." components.
func validateRelativePath(p string) error {
	if p == "" {
		return fmt.Errorf("empty path")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("absolute paths not allowed")
	}
	cleaned := filepath.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path traversal not allowed")
	}
	return nil
}

func downloadAndExtractRepo(c *core.ExecutionState, client *http.Client, serverURL, repoID, wsToken string) error {
	reqURL := fmt.Sprintf("%s/api/v2/ci/runner/repo/%s?token=%s",
		serverURL, url.PathEscape(repoID), url.QueryEscape(wsToken))

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return core.CreateErr(c, err, "failed to create repo download request")
	}

	resp, err := client.Do(req)
	if err != nil {
		return core.CreateErr(c, err, "failed to download repo archive")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return core.CreateErr(c, nil, "repo download failed: status %d: %s", resp.StatusCode, string(body))
	}

	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return core.CreateErr(c, err, "failed to decompress repo archive")
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return core.CreateErr(c, err, "failed to read repo archive")
		}

		if err := validateRelativePath(hdr.Name); err != nil {
			continue // skip invalid entries
		}

		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(hdr.Name, 0755); err != nil {
				return core.CreateErr(c, err, "failed to create directory %s", hdr.Name)
			}
			continue
		}

		dir := filepath.Dir(hdr.Name)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return core.CreateErr(c, err, "failed to create directory for %s", hdr.Name)
			}
		}

		f, err := os.Create(hdr.Name)
		if err != nil {
			return core.CreateErr(c, err, "failed to create file %s", hdr.Name)
		}
		_, copyErr := io.Copy(f, tr)
		closeErr := f.Close()
		if copyErr != nil {
			return core.CreateErr(c, copyErr, "failed to write %s", hdr.Name)
		}
		if closeErr != nil {
			return core.CreateErr(c, closeErr, "failed to close %s", hdr.Name)
		}
	}

	return nil
}

func downloadRepoFile(c *core.ExecutionState, client *http.Client, serverURL, wsToken, repoID, filePath string) error {
	escapedPath := url.PathEscape(filePath)
	reqURL := fmt.Sprintf("%s/api/v2/ci/runner/repo/%s/file/%s?token=%s",
		serverURL, url.PathEscape(repoID), escapedPath, url.QueryEscape(wsToken))
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return core.CreateErr(c, err, "failed to create request for %s", filePath)
	}

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
	err := core.RegisterNodeFactory(repoDownloadDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &RepoDownloadNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
