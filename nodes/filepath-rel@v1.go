package nodes

import (
	_ "embed"
	"path/filepath"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed filepath-rel@v1.yml
var filepathRelativeDefinition string

type FilepathRelative struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *FilepathRelative) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	basepath, err := core.InputValueById[string](c, n, ni.Core_filepath_rel_v1_Input_basepath)
	if err != nil {
		return nil, err
	}

	targpath, err := core.InputValueById[string](c, n, ni.Core_filepath_rel_v1_Input_targpath)
	if err != nil {
		return nil, err
	}

	return filepath.Rel(basepath, targpath)
}

func init() {
	err := core.RegisterNodeFactory(filepathRelativeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &FilepathRelative{}, nil
	})
	if err != nil {
		panic(err)
	}
}
