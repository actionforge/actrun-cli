package nodes

import (
	_ "embed"
	"os"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed process-exit@v1.yml
var exitDefinition string

type ExitNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *ExitNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	code, err := core.InputValueById[int](c, n, ni.Core_process_exit_v1_Input_code)
	if err != nil {
		return err
	}

	os.Exit(code)
	return nil
}

func init() {
	err := core.RegisterNodeFactory(exitDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ExitNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
