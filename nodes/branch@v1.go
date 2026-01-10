package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed branch@v1.yml
var ifDefinition string

type BranchNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *BranchNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	condition, err := core.InputValueById[bool](c, n, ni.Core_branch_v1_Input_condition)
	if err != nil {
		return err
	}

	if condition {
		err = n.Execute(ni.Core_branch_v1_Output_exec_then, c, nil)
		if err != nil {
			return err
		}
	} else {
		err = n.Execute(ni.Core_branch_v1_Output_exec_otherwise, c, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func init() {
	err := core.RegisterNodeFactory(ifDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &BranchNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
