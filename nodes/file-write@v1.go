package nodes

import (
	_ "embed"
	"io"
	"os"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"
)

//go:embed file-write@v1.yml
var fileWriteDefinition string

type FileWriteNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *FileWriteNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	reader, err := core.InputValueById[io.Reader](c, n, ni.Core_file_write_v1_Input_data)
	if err != nil {
		return err
	}

	defer utils.SafeCloseReaderAndIgnoreError(reader)

	p, err := core.InputValueById[string](c, n, ni.Core_file_write_v1_Input_path)
	if err != nil {
		return err
	}

	fw, err := os.Create(p)
	if err != nil {
		return core.CreateErr(c, err, "error creating file")
	}

	_, copyErr := io.Copy(fw, reader)
	if copyErr == nil {
		copyErr = fw.Close()
	}

	// Ensure the reader is closed in all cases.
	// If closing the reader fails without a prior error,
	// treat it as an error which is part of the copy op.
	err = utils.SafeCloseReader(reader)
	if err != nil && copyErr == nil {
		copyErr = err
	}

	if copyErr != nil {
		err = n.Execute(ni.Core_file_write_v1_Output_exec_err, c, copyErr)
		if err != nil {
			return err
		}
	} else {
		err = n.Execute(ni.Core_file_write_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(fileWriteDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &FileWriteNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
