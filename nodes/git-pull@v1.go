package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

//go:embed git-pull@v1.yml
var gitPullDefinition string

type GitPullNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

// Assumes convertCredentialToGitAuth is available (as seen in core/git-clone@v1.go)
// and ni.Core_git_pull_v1_Input_repo, ni.Core_git_pull_v1_Input_auth, etc. are generated.
func (n *GitPullNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	repoInfo, err := core.InputValueById[core.GitRepository](c, n, ni.Core_git_pull_v1_Input_repo)
	if err != nil {
		return err
	}

	credentials, err := core.InputValueById[core.Credentials](c, n, ni.Core_git_pull_v1_Input_auth)
	if err != nil {
		return err
	}

	repo, err := git.PlainOpen(repoInfo.Path)
	if err != nil {
		err := core.CreateErr(c, err, "failed to open repository at path '%s'", repoInfo.Path)
		return n.Execute(ni.Core_git_pull_v1_Output_exec_err, c, err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		err := core.CreateErr(c, err, "failed to get worktree from repository")
		return n.Execute(ni.Core_git_pull_v1_Output_exec_err, c, err)
	}

	var authMethod transport.AuthMethod
	if credentials != nil {
		authMethod, err = convertCredentialToGitAuth(credentials)
		if err != nil {
			return core.CreateErr(c, err, "failed to convert credentials to git auth method")
		}
	}

	pullOptions := &git.PullOptions{
		Auth: authMethod,
		// Assuming we want to use the repository's configured remote and branch.
		// For simplicity, we omit setting RemoteName or ReferenceName here,
		// relying on the default go-git behavior which mirrors 'git pull'.
	}

	err = worktree.Pull(pullOptions)
	if err != nil && err != git.NoErrAlreadyUpToDate {
		err := core.CreateErr(c, err, "failed to pull changes from remote")
		return n.Execute(ni.Core_git_pull_v1_Output_exec_err, c, err)
	}

	return n.Execute(ni.Core_git_pull_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(gitPullDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &GitPullNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
