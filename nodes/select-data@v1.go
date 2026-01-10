package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed select-data@v1.yml
var selectDataNodeDefinition string

type SelectDataNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *SelectDataNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {

	index, err := core.InputValueById[int](c, n, ni.Core_select_data_v1_Input_index)
	if err != nil {
		return nil, err
	}

	choices, err := core.InputArrayValueById[any](c, n, ni.Core_select_data_v1_Input_choices, core.GetInputValueOpts{
		Index: &index,
	})
	if err != nil {
		return nil, err
	}

	if index < 0 || index >= len(choices) {
		return nil, core.CreateErr(c, nil, "index out of range: %d, expected 0-%d", index, len(choices)-1)
	}

	return choices[index], nil
}

func init() {
	err := core.RegisterNodeFactory(selectDataNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &SelectDataNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
