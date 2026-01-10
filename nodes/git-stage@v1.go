package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core"

	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"github.com/go-git/go-git/v5"
)

//go:embed git-stage@v1.yml
var gitStageDefinition string

type GitStageNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *GitStageNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	repoInfo, err := core.InputValueById[core.GitRepository](c, n, ni.Core_git_stage_v1_Input_repo)
	if err != nil {
		return err
	}

	paths, err := core.InputValueById[[]string](c, n, ni.Core_git_stage_v1_Input_paths)
	if err != nil {
		return err
	} else if len(paths) == 0 {
		return core.CreateErr(c, err, "at least one path must be provided to stage")
	}

	repo, err := git.PlainOpen(repoInfo.Path)
	if err != nil {
		err := core.CreateErr(c, err, "failed to open repository at path '%s'", repoInfo.Path)
		return n.Execute(ni.Core_git_stage_v1_Output_exec_err, c, err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		err := core.CreateErr(c, err, "failed to get worktree from repository")
		return n.Execute(ni.Core_git_stage_v1_Output_exec_err, c, err)
	}

	for _, path := range paths {
		if path == "" {
			continue
		}
		_, err := worktree.Add(path)
		if err != nil {
			err := core.CreateErr(c, err, "failed to stage path '%s'", path)
			return n.Execute(ni.Core_git_stage_v1_Output_exec_err, c, err)
		}
	}

	return n.Execute(ni.Core_git_stage_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(gitStageDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &GitStageNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
