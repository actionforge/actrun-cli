package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed select-exec@v1.yml
var selectExecNodeDefinition string

type SelectExecNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *SelectExecNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	_, index, ok := core.IsValidIndexPortId(string(inputId))
	if !ok {
		return core.CreateErr(c, nil, "invalid input id: %s", inputId)
	}

	err := n.Outputs.SetOutputValue(c, ni.Core_select_exec_v1_Output_index, index, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Execute(ni.Core_select_exec_v1_Output_exec, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(selectExecNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &SelectExecNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
