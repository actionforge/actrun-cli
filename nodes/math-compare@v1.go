package nodes

import (
	_ "embed"
	"errors"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed math-compare@v1.yml
var mathCompareDefinition string

type CompareNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *CompareNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	op1, err := core.InputValueById[float64](c, n, ni.Core_math_compare_op_v1_Input_op1)
	if err != nil {
		return nil, err
	}

	op2, err := core.InputValueById[float64](c, n, ni.Core_math_compare_op_v1_Input_op2)
	if err != nil {
		return nil, err
	}

	operator, err := core.InputValueById[string](c, n, ni.Core_math_compare_op_v1_Input_operator)
	if err != nil {
		return nil, err
	}

	var result bool
	switch operator {
	case ">":
		result = op1 > op2
	case "<":
		result = op1 < op2
	case ">=":
		result = op1 >= op2
	case "<=":
		result = op1 <= op2
	case "==":
		result = op1 == op2
	case "!=":
		result = op1 != op2
	default:
		return nil, errors.New("invalid operator")
	}

	return result, nil
}

func init() {
	err := core.RegisterNodeFactory(mathCompareDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &CompareNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
