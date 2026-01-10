package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed env-get@v1.yml
var envGetDefinition string

type EnvGetNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *EnvGetNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	env, err := core.InputValueById[string](c, n, ni.Core_env_get_v1_Input_env)
	if err != nil {
		return nil, err
	}

	resolvedEnv := c.Env[env]
	// ignore error, returning empty string

	return resolvedEnv, nil
}

func init() {
	err := core.RegisterNodeFactory(envGetDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &EnvGetNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
