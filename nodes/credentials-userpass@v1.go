package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed credentials-userpass@v1.yml
var userPassCredentialDefinition string

type UserPassCredentialsNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *UserPassCredentialsNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	if outputId != ni.Core_credentials_userpass_v1_Output_credential {
		return nil, core.CreateErr(c, nil, "unknown output id '%s'", outputId)
	}

	username, err := core.InputValueById[core.SecretValue](c, n, ni.Core_credentials_userpass_v1_Input_username)
	if err != nil {
		return nil, err
	}

	password, err := core.InputValueById[core.SecretValue](c, n, ni.Core_credentials_userpass_v1_Input_password)
	if err != nil {
		return nil, err
	}

	credential := UserPassCredentials{
		Username: username.Secret,
		Password: password.Secret,
	}

	return credential, nil
}

func init() {
	err := core.RegisterNodeFactory(userPassCredentialDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &UserPassCredentialsNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
