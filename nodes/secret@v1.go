package nodes

import (
	_ "embed"
	"fmt"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed secret@v1.yml
var secretDefinition string

type SecretNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *SecretNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	secretName, err := core.InputValueById[string](c, n, ni.Core_secret_v1_Input_name)
	if err != nil {
		return nil, err
	}

	prefix, err := core.InputValueById[string](c, n, ni.Core_secret_v1_Input_prefix)
	if err != nil {
		return nil, err
	}

	var secretValue string

	var ok bool
	secretValue, ok = c.Secrets[secretName]
	if !ok {
		// return an empty string if the secret is not found
		return "", nil
	}

	return fmt.Sprintf("%s%s", prefix, secretValue), nil
}

func init() {
	err := core.RegisterNodeFactory(secretDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &SecretNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
