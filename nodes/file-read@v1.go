package nodes

import (
	_ "embed"
	"errors"
	"os"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"
)

//go:embed file-read@v1.yml
var fileReadDefinition string

type FileReadNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *FileReadNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	path, err := core.InputValueById[string](c, n, ni.Core_file_read_v1_Input_path)
	if err != nil {
		return err
	}

	cleanPath, pathErr := utils.ValidatePath(path)
	if pathErr != nil {
		return core.CreateErr(c, pathErr, "invalid file path")
	}

	fp, err := os.Open(cleanPath)
	if err != nil {
		return core.CreateErr(c, err)
	}

	dsf := core.DataStreamFactory{
		SourcePath: cleanPath,
		Reader:     fp,
		Length:     core.GetReaderLength(fp),
	}
	defer dsf.CloseStreamAndIgnoreError()

	err = n.Outputs.SetOutputValue(c, ni.Core_file_read_v1_Output_data, dsf, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	st, statErr := os.Stat(cleanPath)
	if statErr == nil {
		if st.IsDir() {
			statErr = core.CreateErr(c, nil, "stat %s: is a directory", cleanPath)
		}
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_file_read_v1_Output_exists, !errors.Is(statErr, os.ErrNotExist), core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	if statErr != nil {
		err = n.Execute(ni.Core_file_read_v1_Output_exec_err, c, statErr)
		if err != nil {
			return err
		}
	} else {
		err = n.Execute(ni.Core_file_read_v1_Output_exec_success, c, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(fileReadDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &FileReadNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
