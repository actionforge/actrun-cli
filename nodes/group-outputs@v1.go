package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
)

//go:embed group-outputs@v1.yml
var groupOutputsDefinition string

type GroupOutputsNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *GroupOutputsNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	v, err := n.InputValueById(c, n, core.InputId(outputId), nil)
	if err != nil {
		return nil, err
	}

	return v, nil
}

func (n *GroupOutputsNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	// Forward errors from a previous node to the group node
	err := n.Execute(core.OutputId(inputId), c, prevError)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(groupOutputsDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &GroupOutputsNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
