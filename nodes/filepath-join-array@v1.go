package nodes

import (
	_ "embed"
	"path/filepath"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed filepath-join-array@v1.yml
var filepathJoinFromArrayNodeDefinition string

type FilepathJoinFromArrayNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *FilepathJoinFromArrayNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {

	segments, err := core.InputArrayValueById[string](c, n, ni.Core_filepath_join_array_v1_Input_segments, core.GetInputValueOpts{})
	if err != nil {
		return nil, err
	}

	return filepath.Join(segments...), nil
}

func init() {
	err := core.RegisterNodeFactory(filepathJoinFromArrayNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &FilepathJoinFromArrayNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
