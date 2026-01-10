package nodes

import (
	_ "embed"
	"time"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces" // Assuming node_interfaces are generated

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

//go:embed git-commit@v1.yml
var gitCommitDefinition string

type GitCommitNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *GitCommitNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	repoInfo, err := core.InputValueById[core.GitRepository](c, n, ni.Core_git_commit_v1_Input_repo)
	if err != nil {
		return err
	}

	message, err := core.InputValueById[string](c, n, ni.Core_git_commit_v1_Input_message)
	if err != nil {
		return err
	}

	authorName, err := core.InputValueById[string](c, n, ni.Core_git_commit_v1_Input_author_name)
	if err != nil {
		return err
	}

	authorEmail, err := core.InputValueById[string](c, n, ni.Core_git_commit_v1_Input_author_email)
	if err != nil {
		return err
	}

	stage, err := core.InputValueById[bool](c, n, ni.Core_git_commit_v1_Input_stage)
	if err != nil {
		return err
	}

	repo, err := git.PlainOpen(repoInfo.Path)
	if err != nil {
		err := core.CreateErr(c, err, "failed to open repository at path '%s'", repoInfo.Path)
		return n.Execute(ni.Core_git_commit_v1_Output_exec_err, c, err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		err := core.CreateErr(c, err, "failed to get worktree from repository")
		return n.Execute(ni.Core_git_commit_v1_Output_exec_err, c, err)
	}

	commitOpts := &git.CommitOptions{
		All: stage,
	}

	if authorName != "" || authorEmail == "" {
		commitOpts.Author = &object.Signature{
			Name:  authorName,
			Email: authorEmail,
			When:  time.Now(),
		}
	}

	commitHash, err := worktree.Commit(message, commitOpts)
	if err != nil {
		err := core.CreateErr(c, err, "failed to create commit")
		return n.Execute(ni.Core_git_commit_v1_Output_exec_err, c, err)
	}

	err = n.SetOutputValue(c, ni.Core_git_commit_v1_Output_commit_hash, commitHash.String(), core.SetOutputValueOpts{})
	if err != nil {
		return err
	}

	return n.Execute(ni.Core_git_commit_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(gitCommitDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &GitCommitNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
