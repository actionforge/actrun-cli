package nodes

import (
	_ "embed"
	"os"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed dir-create@v1.yml
var dirCreateDefinition string

type DirCreateNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *DirCreateNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	path, err := core.InputValueById[string](c, n, ni.Core_dir_create_v1_Input_path)
	if err != nil {
		return err
	}

	mkdirAll, err := core.InputValueById[bool](c, n, ni.Core_dir_create_v1_Input_create_parents)
	if err != nil {
		return err
	}

	var mkdirErr error
	if mkdirAll {
		mkdirErr = os.MkdirAll(path, 0755)
	} else {
		mkdirErr = os.Mkdir(path, 0755)
	}

	if mkdirErr != nil {
		err := core.CreateErr(c, mkdirErr, "failed to create directory")
		return n.Execute(ni.Core_dir_create_v1_Output_exec_err, c, err)
	}

	return n.Execute(ni.Core_dir_create_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(dirCreateDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &DirCreateNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
