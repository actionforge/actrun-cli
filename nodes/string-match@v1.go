package nodes

import (
	_ "embed"
	"regexp"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed string-match@v1.yml
var stringMatchDefinition string

type StringMatchNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *StringMatchNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	str1, err := core.InputValueById[string](c, n, ni.Core_string_match_v1_Input_str1)
	if err != nil {
		return nil, err
	}

	str2, err := core.InputValueById[string](c, n, ni.Core_string_match_v1_Input_str2)
	if err != nil {
		return nil, err
	}

	op, err := core.InputValueById[string](c, n, ni.Core_string_match_v1_Input_op)
	if err != nil {
		return nil, err
	}

	switch op {
	case "contains":
		return strings.Contains(str1, str2), nil
	case "notcontains":
		return !strings.Contains(str1, str2), nil
	case "startswith":
		return strings.HasPrefix(str1, str2), nil
	case "endswith":
		return strings.HasSuffix(str1, str2), nil
	case "equals":
		return str1 == str2, nil
	case "regex":
		return regexp.MatchString(str2, str1)
	default:
		return nil, core.CreateErr(c, nil, "unknown operation: %v", op)
	}
}

func init() {
	err := core.RegisterNodeFactory(stringMatchDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StringMatchNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
