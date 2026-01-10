package nodes

import (
	_ "embed"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed string-split@v1.yml
var splitDefinition string

type SplitNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *SplitNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {

	str, err := core.InputValueById[string](c, n, ni.Core_string_split_v1_Input_string)
	if err != nil {
		return nil, err
	}

	delimiter, err := core.InputValueById[string](c, n, ni.Core_string_split_v1_Input_delimiter)
	if err != nil {
		return nil, err
	}

	maxSegments, err := core.InputValueById[int](c, n, ni.Core_string_split_v1_Input_max_segments)
	if err != nil {
		return nil, err
	}

	split := splitString(str, delimiter, maxSegments)
	return split, nil
}

func splitString(str, delimiter string, maxSegments int) []string {

	replacements := []struct {
		old string
		new string
	}{
		{"\\n", "\n"},
		{"\\t", "\t"},
		{"\\r", "\r"},
	}

	for _, r := range replacements {
		delimiter = strings.ReplaceAll(delimiter, r.old, r.new)
	}

	return strings.SplitN(str, delimiter, maxSegments)
}

func init() {
	err := core.RegisterNodeFactory(splitDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &SplitNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
