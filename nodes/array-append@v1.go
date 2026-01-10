package nodes

import (
	_ "embed"
	"reflect"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed array-append@v1.yml
var arrayAppendNodeDefinition string

type ArrayAppendNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *ArrayAppendNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	array1, err := core.InputValueById[core.Indexable](c, n, ni.Core_array_append_v1_Input_array1)
	if err != nil {
		return err
	}

	array2, err := core.InputValueById[core.Indexable](c, n, ni.Core_array_append_v1_Input_array2)
	if err != nil {
		return err
	}

	for i := 0; i < array2.GetData().Len(); i++ {
		item := array2.GetData().Index(i)
		err = array1.AppendValue(reflect.ValueOf(item.Interface()))
		if err != nil {
			return core.CreateErr(c, err, "failed to append item from array2 to array1")
		}
	}

	err = n.Outputs.SetOutputValue(c, ni.Core_array_append_v1_Output_array, array1.GetData().Interface(), core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	err = n.Execute(ni.Core_array_append_v1_Output_exec, c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(arrayAppendNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ArrayAppendNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
