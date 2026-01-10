package nodes

import (
	_ "embed"
	"regexp"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed string-match-regex@v1.yml
var stringMatchRegexDefinition string

type StringMatchRegexNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *StringMatchRegexNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	str, err := core.InputValueById[string](c, n, ni.Core_string_match_regex_v1_Input_input)
	if err != nil {
		return nil, err
	}

	pattern, err := core.InputValueById[string](c, n, ni.Core_string_match_regex_v1_Input_pattern)
	if err != nil {
		return nil, err
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, core.CreateErr(c, nil, "invalid regex pattern: %v", pattern)
	}

	matches := re.FindStringSubmatch(str)
	if len(matches) == 0 {
		// if the regular expression does not contain any
		// capturing groups, the function will
		// return a slice with a single element, which is
		// the entire match of the regular expression.
		return []string{}, nil
	}

	return matches[1:], nil
}

func init() {
	err := core.RegisterNodeFactory(stringMatchRegexDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StringMatchRegexNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
