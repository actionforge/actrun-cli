package nodes

import (
	_ "embed"
	"regexp"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed string-replace@v1.yml
var stringReplaceDefinition string

type StringReplaceNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *StringReplaceNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	str, err := core.InputValueById[string](c, n, ni.Core_string_replace_v1_Input_input)
	if err != nil {
		return nil, err
	}

	substring, err := core.InputValueById[string](c, n, ni.Core_string_replace_v1_Input_substring)
	if err != nil {
		return nil, err
	}

	replacement, err := core.InputValueById[string](c, n, ni.Core_string_replace_v1_Input_replacement)
	if err != nil {
		return nil, err
	}

	op, err := core.InputValueById[string](c, n, ni.Core_string_replace_v1_Input_op)
	if err != nil {
		return nil, err
	}

	switch op {
	case "string":
		return strings.ReplaceAll(str, substring, replacement), nil
	case "regex":
		re, err := regexp.Compile(substring)
		if err != nil {
			return nil, core.CreateErr(c, nil, "invalid regex pattern: %v", substring)
		}
		return re.ReplaceAllString(str, replacement), nil
	}

	return nil, core.CreateErr(c, nil, "unknown operation: %v", op)
}

func init() {
	err := core.RegisterNodeFactory(stringReplaceDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StringReplaceNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
