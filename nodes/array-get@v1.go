package nodes

import (
	_ "embed"
	"reflect"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed array-get@v1.yml
var arrayGetDefinition string

type ArrayGet struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *ArrayGet) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	input, err := core.InputValueById[any](c, n, ni.Core_array_get_v1_Input_array)
	if err != nil {
		return nil, err
	}

	index, err := core.InputValueById[int](c, n, ni.Core_array_get_v1_Input_index)
	if err != nil {
		return nil, err
	}

	boundCheck, err := core.InputValueById[bool](c, n, ni.Core_array_get_v1_Input_bound_check)
	if err != nil {
		return nil, err
	}

	inputValue := reflect.ValueOf(input)
	if inputValue.Kind() != reflect.Slice && inputValue.Kind() != reflect.Array {
		return nil, core.CreateErr(c, nil, "input is not an array or slice")
	}

	outOfBounds := index < 0 || index >= inputValue.Len()
	if outOfBounds {
		if boundCheck {
			return nil, core.CreateErr(c, nil, "index out of bounds: %d", index)
		} else {
			return reflect.New(inputValue.Type().Elem()).Elem().Interface(), nil
		}
	}

	return inputValue.Index(index).Interface(), nil
}

func init() {
	err := core.RegisterNodeFactory(arrayGetDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ArrayGet{}, nil
	})
	if err != nil {
		panic(err)
	}
}
