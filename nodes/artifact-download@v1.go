package nodes

import (
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed artifact-download@v1.yml
var artifactDownloadDefinition string

type ArtifactDownloadNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *ArtifactDownloadNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	serverURL := envOrOs(c, "BUILD_SERVER_URL")
	token := envOrOs(c, "BUILD_AGENT_TOKEN")

	if serverURL == "" || token == "" {
		downloadErr := core.CreateErr(c, nil, "artifact download requires BUILD_SERVER_URL and BUILD_AGENT_TOKEN environment variables (only available when running via agent)")
		return n.Execute(ni.Core_artifact_download_v1_Output_exec_err, c, downloadErr)
	}

	filename, err := core.InputValueById[string](c, n, ni.Core_artifact_download_v1_Input_name)
	if err != nil {
		return err
	}

	runID, err := core.InputValueById[string](c, n, ni.Core_artifact_download_v1_Input_run_id)
	if err != nil {
		return err
	}
	if runID == "" {
		runID = envOrOs(c, "BUILD_RUN_ID")
		if runID == "" {
			downloadErr := core.CreateErr(c, nil, "no run ID provided and BUILD_RUN_ID is not set")
			return n.Execute(ni.Core_artifact_download_v1_Output_exec_err, c, downloadErr)
		}
	}

	url := fmt.Sprintf("%s/api/v2/ci/runner/runs/%s/artifacts/%s", serverURL, runID, filename)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return core.CreateErr(c, err, "failed to create artifact download request")
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, downloadErr := client.Do(req)
	if resp != nil && downloadErr == nil && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		downloadErr = core.CreateErr(c, nil, "artifact download failed with status %d: %s", resp.StatusCode, string(body))
	}

	if downloadErr != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return n.Execute(ni.Core_artifact_download_v1_Output_exec_err, c, downloadErr)
	}

	dsf := core.DataStreamFactory{
		SourcePath: filename,
		Reader:     resp.Body,
		Length:     resp.ContentLength,
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_artifact_download_v1_Output_data, dsf, core.SetOutputValueOpts{})
	if err != nil {
		resp.Body.Close()
		return err
	}

	return n.Execute(ni.Core_artifact_download_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(artifactDownloadDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &ArtifactDownloadNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
