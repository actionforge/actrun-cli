package nodes

import (
	_ "embed"
	"path/filepath"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed filepath-join@v1.yml
var filepathJoinDefinition string

type FilepathJoin struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *FilepathJoin) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {

	segments, err := core.InputArrayValueById[string](c, n, ni.Core_filepath_join_v1_Input_segments, core.GetInputValueOpts{})
	if err != nil {
		return nil, err
	}

	return filepath.Join(segments...), nil
}

func init() {
	err := core.RegisterNodeFactory(filepathJoinDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &FilepathJoin{}, nil
	})
	if err != nil {
		panic(err)
	}
}
