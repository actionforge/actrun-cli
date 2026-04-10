//go:build p4

package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed p4-credentials@v1.yml
var p4CredentialsDefinition string

type P4CredentialsNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *P4CredentialsNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	if outputId != ni.Core_p4_credentials_v1_Output_credential {
		return nil, core.CreateErr(c, nil, "unknown output id '%s'", outputId)
	}

	port, _ := core.InputValueById[core.SecretValue](c, n, ni.Core_p4_credentials_v1_Input_port)
	user, _ := core.InputValueById[core.SecretValue](c, n, ni.Core_p4_credentials_v1_Input_user)
	password, _ := core.InputValueById[core.SecretValue](c, n, ni.Core_p4_credentials_v1_Input_password)
	trust, _ := core.InputValueById[string](c, n, ni.Core_p4_credentials_v1_Input_trust)
	client, _ := core.InputValueById[string](c, n, ni.Core_p4_credentials_v1_Input_client)

	return P4Credentials{
		Port:     port.Secret,
		User:     user.Secret,
		Password: password.Secret,
		Trust:    trust,
		Client:   client,
	}, nil
}

func init() {
	err := core.RegisterNodeFactory(p4CredentialsDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &P4CredentialsNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
