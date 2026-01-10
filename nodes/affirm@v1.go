package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed affirm@v1.yml
var affirmDefinition string

type AffirmNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *AffirmNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	truthness, err := core.InputValueById[bool](c, n, ni.Core_affirm_v1_Input_input)
	if err != nil {
		return nil, err
	}

	return truthness, nil
}

func init() {
	err := core.RegisterNodeFactory(affirmDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &AffirmNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
