package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
)

//go:embed comment@v1.yml
var commentNodeDefinition string

type CommentNode struct {
	core.NodeBaseComponent
	core.Inputs
}

func init() {
	err := core.RegisterNodeFactory(commentNodeDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &CommentNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
