package nodes

import (
	_ "embed"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"github.com/rossmacarthur/cases"
)

//go:embed string-transform@v1.yml
var stringTransformDefinition string

type StringTransform struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *StringTransform) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	input, err := core.InputValueById[string](c, n, ni.Core_string_transform_v1_Input_input)
	if err != nil {
		return nil, err
	}

	op, err := core.InputValueById[string](c, n, ni.Core_string_transform_v1_Input_op)
	if err != nil {
		return nil, err
	}

	var result string

	switch op {
	case "lower":
		result = cases.ToLower(input)
	case "upper":
		result = cases.ToUpper(input)
	case "title":
		result = cases.ToTitle(input)
	case "camel":
		result = cases.ToCamel(input)
	case "pascal":
		result = cases.ToPascal(input)
	case "snake":
		result = cases.ToSnake(input)
	case "kebab":
		result = cases.ToKebab(input)
	case "reverse":
		runes := []rune(input)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		result = string(runes)
	case "trim":
		result = strings.TrimSpace(input)
	case "trim_left":
		result = strings.TrimLeft(input, " \t\n\r")
	case "trim_right":
		result = strings.TrimRight(input, " \t\n\r")
	default:
		return nil, core.CreateErr(c, nil, "unknown operation '%s'", op)
	}

	return result, nil
}

func init() {
	err := core.RegisterNodeFactory(stringTransformDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &StringTransform{}, nil
	})
	if err != nil {
		panic(err)
	}
}
