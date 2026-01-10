package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

//go:embed git-push@v1.yml
var gitPushDefinition string

type GitPushNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

// Assumes convertCredentialToGitAuth is available (as seen in core/git-clone@v1.go)
// and ni.Core_git_push_v1_Input_repo, etc. are generated.
func (n *GitPushNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	repoInfo, err := core.InputValueById[core.GitRepository](c, n, ni.Core_git_push_v1_Input_repo)
	if err != nil {
		return err
	}

	credentials, err := core.InputValueById[core.Credentials](c, n, ni.Core_git_push_v1_Input_auth)
	if err != nil {
		return err
	}

	remoteName, err := core.InputValueById[string](c, n, ni.Core_git_push_v1_Input_remote)
	if err != nil {
		return err
	}

	branch, err := core.InputValueById[string](c, n, ni.Core_git_push_v1_Input_branch)
	if err != nil {
		return err
	}

	pushTags, err := core.InputValueById[bool](c, n, ni.Core_git_push_v1_Input_tags)
	if err != nil {
		return err
	}

	force, err := core.InputValueById[bool](c, n, ni.Core_git_push_v1_Input_force)
	if err != nil {
		return err
	}

	repo, err := git.PlainOpen(repoInfo.Path)
	if err != nil {
		err := core.CreateErr(c, err, "failed to open repository at path '%s'", repoInfo.Path)
		return n.Execute(ni.Core_git_push_v1_Output_exec_err, c, err)
	}

	var authMethod transport.AuthMethod
	if credentials != nil {
		authMethod, err = convertCredentialToGitAuth(credentials)
		if err != nil {
			return core.CreateErr(c, err, "failed to convert credentials to git auth method")
		}
	}

	pushOptions := &git.PushOptions{
		Auth:       authMethod,
		RemoteName: remoteName,
		Force:      force,
		// Default RefSpecs for current branch or specific branch
	}

	// Determine which references to push
	var refSpecs []config.RefSpec
	if pushTags {
		refSpecs = append(refSpecs, config.RefSpec("+refs/tags/*:refs/tags/*"))
	} else if branch != "" {
		ref := plumbing.NewBranchReferenceName(branch)
		refSpecs = append(refSpecs, config.RefSpec(ref+":"+ref))
	} else {
		// Push current branch if no specific branch is provided and not pushing all tags
		head, err := repo.Head()
		if err == nil {
			refSpecs = append(refSpecs, config.RefSpec(head.Name()+":"+head.Name()))
		}
	}
	pushOptions.RefSpecs = refSpecs

	err = repo.Push(pushOptions)
	if err != nil && err != git.NoErrAlreadyUpToDate {
		err := core.CreateErr(c, err, "failed to push changes to remote '%s'", remoteName)
		return n.Execute(ni.Core_git_push_v1_Output_exec_err, c, err)
	}

	return n.Execute(ni.Core_git_push_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(gitPushDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &GitPushNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
