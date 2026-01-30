package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed secret-set@v1.yml
var setSecretDefinition string

type SetSecretNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *SetSecretNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	secretName, err := core.InputValueById[string](c, n, ni.Core_secret_set_v1_Input_name)
	if err != nil {
		return err
	}

	secretValue, err := core.InputValueById[string](c, n, ni.Core_secret_set_v1_Input_value)
	if err != nil {
		return err
	}

	c.Secrets[secretName] = secretValue

	return n.Execute(ni.Core_secret_set_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(setSecretDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &SetSecretNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
