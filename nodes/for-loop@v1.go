package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed for-loop@v1.yml
var loopDefinition string

type LoopNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions

	run bool
}

func (n *LoopNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	if inputId == ni.Core_for_each_loop_v1_Input_exec_break {
		n.run = false
		return nil
	}

	n.run = true
	firstIndex, err := core.InputValueById[int](c, n, ni.Core_for_loop_v1_Input_first_index)
	if err != nil {
		return err
	}

	lastIndex, err := core.InputValueById[int](c, n, ni.Core_for_loop_v1_Input_last_index)
	if err != nil {
		return err
	}

	if firstIndex > lastIndex {
		// zero executions
		return nil
	}

	_, ok := n.GetExecutionTarget(ni.Core_for_loop_v1_Output_exec_body)
	if ok {
		for i := firstIndex; i <= lastIndex && !c.IsCancelled() && n.run; i++ {

			err = n.Outputs.SetOutputValue(c, ni.Core_for_loop_v1_Output_index, i, core.SetOutputValueOpts{})
			if err != nil {
				return err
			}

			err = n.Execute(ni.Core_for_loop_v1_Output_exec_body, c, nil)
			if err != nil {
				return err
			}
		}
	}

	err = n.Execute(ni.Core_for_loop_v1_Output_exec_completed, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(loopDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &LoopNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
