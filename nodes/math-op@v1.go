package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
)

//go:embed math-op@v1.yml
var mathOpDefinition string

type MathOpNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *MathOpNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	return nil, nil
}

func init() {
	err := core.RegisterNodeFactory(mathOpDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &MathOpNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
