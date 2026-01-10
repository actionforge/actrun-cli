package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed credentials-accesskey@v1.yml
var accessKeyCredentialDefinition string

type AccessKeyCredentialsNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *AccessKeyCredentialsNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	if outputId != ni.Core_credentials_accesskey_v1_Output_credential {
		return nil, core.CreateErr(c, nil, "unknown output id '%s'", outputId)
	}

	accessKey, err := core.InputValueById[core.SecretValue](c, n, ni.Core_credentials_accesskey_v1_Input_access_key)
	if err != nil {
		return nil, err
	}

	accessPassword, err := core.InputValueById[core.SecretValue](c, n, ni.Core_credentials_accesskey_v1_Input_access_password)
	if err != nil {
		return nil, err
	}

	credential := AccessKeyCredentials{
		AccessKey:      accessKey.Secret,
		AccessPassword: accessPassword.Secret,
	}

	return credential, nil
}

func init() {
	err := core.RegisterNodeFactory(accessKeyCredentialDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &AccessKeyCredentialsNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
