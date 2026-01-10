package nodes

import (
	_ "embed"
	"path/filepath"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed filepath-op@v1.yml
var filepathOpDefinition string

type FilepathOp struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *FilepathOp) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {

	path, err := core.InputValueById[string](c, n, ni.Core_filepath_op_v1_Input_path)
	if err != nil {
		return nil, err
	}

	op, err := core.InputValueById[string](c, n, ni.Core_filepath_op_v1_Input_op)
	if err != nil {
		return nil, err
	}

	switch op {
	case "base":
		return filepath.Base(path), nil
	case "clean":
		return filepath.Clean(path), nil
	case "dir":
		return filepath.Dir(path), nil
	case "ext":
		return filepath.Ext(path), nil
	case "from_slash":
		return filepath.FromSlash(path), nil
	case "to_slash":
		return filepath.ToSlash(path), nil
	case "volume":
		return filepath.VolumeName(path), nil
	}

	return nil, core.CreateErr(c, nil, "unknown operation '%s'", op)
}

func init() {
	err := core.RegisterNodeFactory(filepathOpDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &FilepathOp{}, nil
	})
	if err != nil {
		panic(err)
	}
}
