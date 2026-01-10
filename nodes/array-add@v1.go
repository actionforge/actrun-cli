package nodes

import (
	_ "embed"
	"reflect"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed array-add@v1.yml
var arrayAddNodeDefinition string

type ArrayAddNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *ArrayAddNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	array, err := core.InputValueById[core.Indexable](c, n, ni.Core_array_add_v1_Input_array)
	if err != nil {
		return err
	}

	item, err := core.InputValueById[reflect.Value](c, n, ni.Core_array_add_v1_Input_item)
	if err != nil {
		return err
	}

	err = array.AppendValue(item)
	if err != nil {
		return core.CreateErr(c, err, "failed to add item to array")
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_array_add_v1_Output_array, array.GetData().Interface(), core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	newIndex := array.GetData().Len() - 1

	err = n.Outputs.SetOutputValue(c, ni.Core_array_add_v1_Output_index, newIndex, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_array_add_v1_Output_array, array.GetData().Interface(), core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Execute(ni.Core_array_add_v1_Output_exec, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(arrayAddNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ArrayAddNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
