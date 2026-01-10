package nodes

import (
	"github.com/actionforge/actrun-cli/core"

	_ "embed"
	"os"

	ni "github.com/actionforge/actrun-cli/node_interfaces"
)

//go:embed credentials-ssh@v1.yml
var sshCredentialDefinition string

type SshCredentialNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
}

func (n *SshCredentialNode) OutputValueById(c *core.ExecutionState, outputId core.OutputId) (any, error) {
	if outputId != ni.Core_credentials_ssh_v1_Output_credential {
		return nil, core.CreateErr(c, nil, "unknown output id '%s'", outputId)
	}

	username, err := core.InputValueById[string](c, n, ni.Core_credentials_ssh_v1_Input_username)
	if err != nil {
		return nil, err
	}

	privateKeyInput, err := core.InputValueById[string](c, n, ni.Core_credentials_ssh_v1_Input_private_key)
	if err != nil {
		return nil, err
	}

	password, err := core.InputValueById[string](c, n, ni.Core_credentials_ssh_v1_Input_private_key_password)
	if err != nil {
		return nil, err
	}

	var keyContent string

	expandedPath := os.ExpandEnv(privateKeyInput)
	if len(expandedPath) > 0 && expandedPath[0] == '~' {
		homeDir, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return nil, core.CreateErr(c, homeErr, "failed to get user home directory")
		}
		expandedPath = homeDir + expandedPath[1:]
	}
	_, err = os.Stat(expandedPath)
	privateKeyInput = expandedPath
	if err == nil {
		keyBytes, readErr := os.ReadFile(privateKeyInput)
		if readErr != nil {
			return nil, core.CreateErr(c, readErr, "failed to read private key from path '%s'", privateKeyInput)
		}
		keyContent = string(keyBytes)
		if keyContent == "" {
			return nil, core.CreateErr(c, nil, "private key content is empty")
		}
	} else {
		return nil, core.CreateErr(c, err, "error checking path for private key '%s'", privateKeyInput)
	}

	credential := SshCredentials{
		Username:           username,
		PrivateKey:         keyContent,
		PrivateKeyPassword: password,
	}

	return credential, nil
}

func init() {
	err := core.RegisterNodeFactory(sshCredentialDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &SshCredentialNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}

type SshCredentials struct {
	Username           string // optional, e.g. "git" or "ubuntu"
	PrivateKey         string
	PrivateKeyPassword string
}

func (c SshCredentials) Type() core.CredentialType {
	return core.CredentialTypeSSH
}

type UserPassCredentials struct {
	Username string
	Password string
}

func (c *UserPassCredentials) Type() core.CredentialType {
	return core.CredentialTypeUsernamePassword
}

type AccessKeyCredentials struct {
	AccessKey      string
	AccessPassword string
}

func (c AccessKeyCredentials) Type() core.CredentialType {
	return core.CredentialTypeAccessKey
}
