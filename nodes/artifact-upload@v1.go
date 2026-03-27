package nodes

import (
	_ "embed"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed artifact-upload@v1.yml
var artifactUploadDefinition string

type ArtifactUploadNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *ArtifactUploadNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	artifactURL := envOrOs(c, "BUILD_ARTIFACT_URL")
	artifactToken := envOrOs(c, "BUILD_ARTIFACT_TOKEN")

	if artifactURL == "" || artifactToken == "" {
		uploadErr := core.CreateErr(c, nil, "artifact upload requires BUILD_ARTIFACT_URL and BUILD_ARTIFACT_TOKEN environment variables (only available when running via agent)")
		return n.Execute(ni.Core_artifact_upload_v1_Output_exec_err, c, uploadErr)
	}

	dsf, err := core.InputValueById[core.DataStreamFactory](c, n, ni.Core_artifact_upload_v1_Input_data)
	if err != nil {
		return err
	}
	defer dsf.CloseStreamAndIgnoreError()

	filename, err := core.InputValueById[string](c, n, ni.Core_artifact_upload_v1_Input_name)
	if err != nil {
		return err
	}

	// Build multipart request using a pipe to avoid buffering the entire file in memory.
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, dsf.Reader); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.CloseWithError(writer.Close())
	}()

	req, err := http.NewRequest("POST", artifactURL, pr)
	if err != nil {
		return core.CreateErr(c, err, "failed to create artifact upload request")
	}
	req.Header.Set("Authorization", "Bearer "+artifactToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, uploadErr := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
		// Drain body for connection reuse.
		_, _ = io.Copy(io.Discard, resp.Body)
	}

	if uploadErr != nil {
		uploadErr = core.CreateErr(c, uploadErr, "artifact upload failed")
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		uploadErr = core.CreateErr(c, nil, "artifact upload failed with status %d", resp.StatusCode)
	}

	// Close the input stream; treat close failure as an error if upload itself succeeded.
	closeErr := dsf.CloseStream()
	if closeErr != nil && uploadErr == nil {
		uploadErr = closeErr
	}

	if uploadErr != nil {
		return n.Execute(ni.Core_artifact_upload_v1_Output_exec_err, c, uploadErr)
	}

	return n.Execute(ni.Core_artifact_upload_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(artifactUploadDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &ArtifactUploadNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
