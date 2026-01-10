package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed negate@v1.yml
var negateDefinition string

type NegateNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *NegateNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	truthness, err := core.InputValueById[bool](c, n, ni.Core_negate_v1_Input_input)
	if err != nil {
		return nil, err
	}

	return !truthness, nil
}

func init() {
	err := core.RegisterNodeFactory(negateDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &NegateNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
