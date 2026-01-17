package nodes

import (
	_ "embed"
	"io"
	"reflect"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed length@v1.yml
var lengthDefinition string

type LengthNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *LengthNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	inputs, err := core.InputValueById[any](c, n, ni.Core_length_v1_Input_input)
	if err != nil {
		return nil, err
	}

	if dsf, ok := inputs.(core.DataStreamFactory); ok {
		if dsf.Length != -1 {
			return dsf.Length, nil
		}
	} else if r, ok := inputs.(io.Reader); ok {
		return core.GetReaderLength(r), nil
	}

	v := reflect.ValueOf(inputs)
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len(), nil
	}

	// I considered return -1 here, but by mistake this could
	// lead to very long running loops if improperly handled.
	return 0, nil
}

func init() {
	err := core.RegisterNodeFactory(lengthDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &LengthNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
