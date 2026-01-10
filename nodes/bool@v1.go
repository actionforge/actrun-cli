package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed bool-or@v1.yml
var boolOrDefinition string

//go:embed bool-and@v1.yml
var boolAndDefinition string

//go:embed bool-xor@v1.yml
var boolXorDefinition string

//go:embed bool-xand@v1.yml
var boolXandDefinition string

type BoolNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs

	op    func(bool, bool) bool
	opStr string // just for debugging
}

func (n *BoolNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	inputs, err := core.InputArrayValueById[bool](c, n, ni.Core_bool_or_v1_Input_inputs, core.GetInputValueOpts{})
	if err != nil {
		return nil, err
	}

	var result bool

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
		op         func(bool, bool) bool
		opStr      string
	}{
		{
			definition: boolOrDefinition,
			op: func(a bool, b bool) bool {
				return a || b
			},
			opStr: "OR",
		},
		{
			definition: boolAndDefinition,
			op: func(a bool, b bool) bool {
				return a && b
			},
			opStr: "AND",
		},
		{
			definition: boolXorDefinition,
			op: func(a bool, b bool) bool {
				return a != b
			},
			opStr: "XOR",
		},
		{
			definition: boolXandDefinition,
			op: func(a bool, b bool) bool {
				return a == b
			},
			opStr: "XAND",
		},
	}

	for _, op := range ops {
		err := core.RegisterNodeFactory(op.definition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
			return &BoolNode{
				op:    op.op,
				opStr: op.opStr,
			}, nil
		})
		if err != nil {
			panic(err)
		}
	}
}
