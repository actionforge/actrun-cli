package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed const-string@v1.yml
var constStringDefinition string

//go:embed const-number@v1.yml
var constNumberDefinition string

//go:embed const-bool@v1.yml
var constBoolDefinition string

type ConstNode[T any] struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs

	portValue core.InputId
}

func (n *ConstNode[T]) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	inputs, err := core.InputValueById[T](c, n, n.portValue)
	if err != nil {
		return nil, err
	}
	return inputs, nil
}

func init() {
	err := core.RegisterNodeFactory(constStringDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ConstNode[string]{
			portValue: ni.Core_const_string_v1_Input_value,
		}, nil
	})
	if err != nil {
		panic(err)
	}

	err = core.RegisterNodeFactory(constNumberDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ConstNode[int64]{
			portValue: ni.Core_const_number_v1_Input_value,
		}, nil
	})
	if err != nil {
		panic(err)
	}

	err = core.RegisterNodeFactory(constBoolDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &ConstNode[bool]{
			portValue: ni.Core_const_bool_v1_Input_value,
		}, nil
	})
	if err != nil {
		panic(err)
	}
}
