package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed string-array@v1.yml
var stringArrayDefinition string

type StringArrayNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *StringArrayNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	values, err := core.InputArrayValueById[string](c, n, ni.Core_string_array_v1_Input_inputs, core.GetInputValueOpts{})
	if err != nil {
		return nil, err
	}

	return values, nil
}

func init() {
	err := core.RegisterNodeFactory(stringArrayDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StringArrayNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
