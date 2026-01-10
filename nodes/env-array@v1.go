package nodes

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"
)

//go:embed env-array@v1.yml
var envArrayDefinition string

type EnvArrayNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *EnvArrayNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	envs, err := core.InputArrayValueById[string](c, n, ni.Core_env_array_v1_Input_env, core.GetInputValueOpts{})
	if err != nil {
		return nil, err
	}

	contextEnvironMap := c.GetContextEnvironMapCopy()
	for portIndex, env := range envs {
		kv := strings.SplitN(env, "=", 2)
		if len(kv) != 2 {
			var emptyString string
			if env == "" {
				emptyString = " (empty string)"
			}
			ord := utils.Ordinal(portIndex)
			return nil, core.CreateErr(c, nil,
				"invalid string at **%s** input port, expected 'KEY=VALUE' format, got '%s'%s", ord, env, emptyString).
				SetHint("ensure that the **%s** input port has a valid 'KEY=VALUE' formatted string.", ord)
		}

		contextEnvironMap[kv[0]] = kv[1]
	}

	envArray := []string{}
	for k, v := range contextEnvironMap {
		envArray = append(envArray, fmt.Sprintf("%s=%s", k, v))
	}
	return envArray, nil
}

func init() {
	err := core.RegisterNodeFactory(envArrayDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &EnvArrayNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
