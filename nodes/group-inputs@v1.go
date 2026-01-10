package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
)

//go:embed group-inputs@v1.yml
var groupInputsDefinition string

type GroupInputsNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

// By default, the cache type for each node is determined by the node's type.
// If the node is an execution node, the cache type is set to "persistent".
// If the node is a data node, the cache type is set to "ephemeral".
// However, input nodes are intermediary nodes. Especially if they happen to
// have the same ID as another inputs node in another group, this would interfere
// with cache. In that case we need to set the cache type to "ephemeral".
func (n *GroupInputsNode) GetCacheType() core.CacheType {
	return core.Permanent
}

func (n *GroupInputsNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	v, err := n.InputValueById(c, n, core.InputId(outputId), nil)
	if err != nil {
		return nil, err
	}

	return v, nil
}

func (n *GroupInputsNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	err := n.Execute(core.OutputId(inputId), c, nil)
	if err != nil {
		return err
	}

	return nil
}

func init() {
	err := core.RegisterNodeFactory(groupInputsDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &GroupInputsNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
