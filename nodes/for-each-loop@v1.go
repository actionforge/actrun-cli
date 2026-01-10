package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed for-each-loop@v1.yml
var forEachLoopDefinition string

type ForEachLoopNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions

	run bool
}

func (n *ForEachLoopNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {

	if inputId == ni.Core_for_each_loop_v1_Input_exec_break {
		n.run = false
		return nil
	}

	n.run = true
	iter := func(key any, value any) error {
		err := n.Outputs.SetOutputValue(c, ni.Core_for_each_loop_v1_Output_key, key, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		err = n.Outputs.SetOutputValue(c, ni.Core_for_each_loop_v1_Output_value, value, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		err = n.Execute(ni.Core_for_each_loop_v1_Output_exec_body, c, nil)
		if err != nil {
			return err
		}
		return nil
	}

	iterable, err := core.InputValueById[core.Iterable](c, n, ni.Core_for_each_loop_v1_Input_input)
	if err != nil {
		return err
	}

	for n.run && iterable.Next() && !c.IsCancelled() {
		err := iter(iterable.Key(), iterable.Value())
		if err != nil {
			return err
		}
	}

	err = n.Execute(ni.Core_for_each_loop_v1_Output_exec_completed, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(forEachLoopDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ForEachLoopNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
