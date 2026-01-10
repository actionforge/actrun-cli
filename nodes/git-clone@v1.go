package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces" // Assuming node_interfaces are generated

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
)

//go:embed git-clone@v1.yml
var gitCloneDefinition string

type GitCloneNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *GitCloneNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	repoURL, err := core.InputValueById[string](c, n, ni.Core_git_clone_v1_Input_url)
	if err != nil {
		return err
	}

	directory, err := core.InputValueById[string](c, n, ni.Core_git_clone_v1_Input_directory)
	if err != nil {
		return err
	}

	ref, err := core.InputValueById[string](c, n, ni.Core_git_clone_v1_Input_ref)
	if err != nil {
		return err
	}

	onlyRef, err := core.InputValueById[bool](c, n, ni.Core_git_clone_v1_Input_only_ref)
	if err != nil {
		return err
	}

	checkout, err := core.InputValueById[bool](c, n, ni.Core_git_clone_v1_Input_checkout)
	if err != nil {
		return err
	}

	credentials, err := core.InputValueById[core.Credentials](c, n, ni.Core_git_clone_v1_Input_auth)
	if err != nil {
		return err
	}

	depth, err := core.InputValueById[int](c, n, ni.Core_git_clone_v1_Input_depth)
	if err != nil {
		return err
	}

	authMethod, err := convertCredentialToGitAuth(credentials)
	if err != nil {
		return core.CreateErr(c, err, "failed to convert credentials to git auth method")
	}

	var refName plumbing.ReferenceName
	// if a specific ref is provided, we need to check if it exists in the remote repo
	if ref != "" {
		remoteConfig := &config.RemoteConfig{
			Name: "origin",
			URLs: []string{repoURL},
		}
		rem := git.NewRemote(memory.NewStorage(), remoteConfig)
		refs, err := rem.List(&git.ListOptions{
			Auth: authMethod,
		})
		if err != nil {
			return core.CreateErr(c, err, "failed to list remote references durig clone operation")
		}

		for _, r := range refs {
			if r.Name().Short() == ref || r.Name().String() == ref {
				refName = r.Name()
				break
			}
		}

		if refName == "" {
			return core.CreateErr(c, nil, "specified ref '%s' does not exist in the remote repository", ref)
		}
	}

	cloneOptions := &git.CloneOptions{
		URL:           repoURL,
		Auth:          authMethod,
		ReferenceName: refName,
		SingleBranch:  onlyRef,
		NoCheckout:    !checkout,
		Depth:         depth,
	}

	_, cloneErr := git.PlainClone(directory, false, cloneOptions)
	if cloneErr != nil {
		err := core.CreateErr(c, cloneErr, "failed to clone repository")
		return n.Execute(ni.Core_git_clone_v1_Output_exec_err, c, err)
	}

	repo := core.GitRepository{
		Path: directory,
	}
	err = n.SetOutputValue(c, ni.Core_git_clone_v1_Output_repo, repo, core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	return n.Execute(ni.Core_git_clone_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(gitCloneDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &GitCloneNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}

func convertCredentialToGitAuth(cred core.Credentials) (transport.AuthMethod, error) {
	if upCred, ok := cred.(*UserPassCredentials); ok {
		username := upCred.Username
		password := upCred.Password

		if password == "" {
			return nil, nil
		}

		return &http.BasicAuth{
			Username: username,
			Password: password,
		}, nil
	} else if sshCred, ok := cred.(SshCredentials); ok {
		username := sshCred.Username
		privateKey := []byte(sshCred.PrivateKey)
		password := sshCred.PrivateKeyPassword

		credentials, err := ssh.NewPublicKeys(username, privateKey, password)
		if err != nil {
			return nil, err
		}
		return credentials, nil
	} else if akCred, ok := cred.(AccessKeyCredentials); ok {
		username := akCred.AccessKey
		password := akCred.AccessPassword

		if password == "" {
			return nil, nil
		}

		return &http.BasicAuth{
			Username: username,
			Password: password,
		}, nil
	}

	return nil, nil
}
