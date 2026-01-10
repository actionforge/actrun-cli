package nodes

import (
	_ "embed"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed string-join@v1.yml
var stringJoinNodeDefinition string

type StringJoinNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *StringJoinNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {

	segments, err := core.InputArrayValueById[string](c, n, ni.Core_string_join_v1_Input_segments, core.GetInputValueOpts{})
	if err != nil {
		return nil, err
	}

	delimiter, err := core.InputValueById[string](c, n, ni.Core_string_join_v1_Input_delimiter)
	if err != nil {
		return nil, err
	}

	return strings.Join(segments, delimiter), nil
}

func init() {
	err := core.RegisterNodeFactory(stringJoinNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StringJoinNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
