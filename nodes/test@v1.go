package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
)

//go:embed test@v1.yml
var testNodeDefinition string

type TestNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *TestNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	v, ok := c.GetDataFromOutputCache(n.GetCacheId(), string(outputId), core.Permanent)
	if ok {
		return v, nil
	}
	return n.Outputs.OutputValueById(c, outputId)
}

func init() {
	err := core.RegisterNodeFactory(testNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &TestNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
