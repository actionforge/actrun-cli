package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed math-add@v1.yml
var mathAddDefinition string

//go:embed math-subtract@v1.yml
var mathSubtractDefinition string

//go:embed math-multiply@v1.yml
var mathMultiplyDefinition string

//go:embed math-divide@v1.yml
var mathDivideDefinition string

type MathNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs

	op func(float64, float64) float64
}

func (n *MathNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	inputs, err := core.InputArrayValueById[float64](c, n, ni.Core_math_add_v1_Input_inputs, core.GetInputValueOpts{})
	if err != nil {
		return nil, err
	}

	var result float64

	if len(inputs) > 0 {
		result = inputs[0]

		for _, input := range inputs[1:] {
			result = n.op(result, input)
		}
	}

	return result, nil
}

func init() {

	ops := []struct {
		definition string
		op         func(float64, float64) float64
	}{
		{
			definition: mathAddDefinition,
			op: func(a float64, b float64) float64 {
				return a + b
			},
		},
		{
			definition: mathSubtractDefinition,
			op: func(a float64, b float64) float64 {
				return a - b
			},
		},
		{
			definition: mathMultiplyDefinition,
			op: func(a float64, b float64) float64 {
				return a * b
			},
		},
		{
			definition: mathDivideDefinition,
			op: func(a float64, b float64) float64 {
				// division follows a safe approach and defines 0/0 as 0
				if b == 0 {
					return 0
				}
				return a / b
			},
		},
	}

	for _, op := range ops {
		err := core.RegisterNodeFactory(op.definition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
			return &MathNode{
				op: op.op,
			}, nil
		})
		if err != nil {
			panic(err)
		}
	}
}
