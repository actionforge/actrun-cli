package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed freeze@v1.yml
var freezeNodeDefinition string

type FreezeNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *FreezeNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	var (
		value any
		err   error
	)

	switch inputId {
	case ni.Core_freeze_v1_Input_exec:
		value, err = core.InputValueById[any](c, n, ni.Core_freeze_v1_Input_init)
		if err != nil {
			return err
		}

		err = n.Outputs.SetOutputValue(c, ni.Core_freeze_v1_Output_value, value, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		err = n.Execute(ni.Core_freeze_v1_Output_exec, c, nil)
		if err != nil {
			return err
		}

		return nil
	case ni.Core_freeze_v1_Input_exec_reset:
		value, err = core.InputValueById[any](c, n, ni.Core_freeze_v1_Input_replace)
		if err != nil {
			return err
		}

		err = n.Outputs.SetOutputValue(c, ni.Core_freeze_v1_Output_value, value, core.SetOutputValueOpts{})
		if err != nil {
			return err
		}

		return nil
	}

	return core.CreateErr(c, nil, "unknown input '%s'", inputId)
}

func init() {
	err := core.RegisterNodeFactory(freezeNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &FreezeNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
