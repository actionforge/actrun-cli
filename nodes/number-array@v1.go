package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed number-array@v1.yml
var numberArrayDefinition string

type NumberArrayNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *NumberArrayNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	values, err := core.InputArrayValueById[float64](c, n, ni.Core_number_array_v1_Input_inputs, core.GetInputValueOpts{})
	if err != nil {
		return nil, err
	}

	return values, nil
}

func init() {
	err := core.RegisterNodeFactory(numberArrayDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &NumberArrayNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
