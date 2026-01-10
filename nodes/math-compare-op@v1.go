package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed math-modulo@v1.yml
var mathModuloDefinition string

//go:embed math-greater@v1.yml
var mathGreaterThanDefinition string

//go:embed math-lesser@v1.yml
var mathLessThanDefinition string

//go:embed math-equal@v1.yml
var mathEqualToDefinition string

//go:embed math-not-equal@v1.yml
var mathNotEqualToDefinition string

//go:embed math-greater-equal@v1.yml
var mathGreaterThanOrEqualToDefinition string

//go:embed math-lesser-equal@v1.yml
var mathLessThanOrEqualToDefinition string

type MathCompareOpNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs

	op func(float64, float64) any
}

func (n *MathCompareOpNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	op1, err := core.InputValueById[float64](c, n, ni.Core_math_modulo_v1_Input_op1)
	if err != nil {
		return nil, err
	}

	op2, err := core.InputValueById[float64](c, n, ni.Core_math_modulo_v1_Input_op2)
	if err != nil {
		return nil, err
	}

	result := n.op(op1, op2)

	return result, nil
}

func init() {

	ops := []struct {
		definition string
		op         func(float64, float64) any
	}{
		{
			definition: mathModuloDefinition,
			op: func(a float64, b float64) any {
				if b == 0 {
					return 0
				}
				return float64(int(a) % int(b))
			},
		},
		{
			definition: mathGreaterThanDefinition,
			op: func(a float64, b float64) any {
				return a > b
			},
		},
		{
			definition: mathLessThanDefinition,
			op: func(a float64, b float64) any {
				return a < b
			},
		},
		{
			definition: mathEqualToDefinition,
			op: func(a float64, b float64) any {
				return a == b
			},
		},
		{
			definition: mathNotEqualToDefinition,
			op: func(a float64, b float64) any {
				return a != b
			},
		},
		{
			definition: mathGreaterThanOrEqualToDefinition,
			op: func(a float64, b float64) any {
				return a >= b
			},
		},
		{
			definition: mathLessThanOrEqualToDefinition,
			op: func(a float64, b float64) any {
				return a <= b
			},
		},
	}

	for _, op := range ops {
		err := core.RegisterNodeFactory(op.definition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
			return &MathCompareOpNode{
				op: op.op,
			}, nil
		})
		if err != nil {
			panic(err)
		}
	}
}
