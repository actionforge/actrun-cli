package nodes

import (
	_ "embed"

	"github.com/actionforge/actrun-cli/core" // Assuming node_interfaces are generated
	ni "github.com/actionforge/actrun-cli/node_interfaces"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

//go:embed git-checkout@v1.yml
var gitCheckoutDefinition string

type GitCheckoutNode struct {
	core.NodeBaseComponent
	core.Executions
	core.Inputs
	core.Outputs
}

func (n *GitCheckoutNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	repoInfo, err := core.InputValueById[*core.GitRepository](c, n, ni.Core_git_checkout_v1_Input_repo)
	if err != nil {
		return err
	}

	reference, err := core.InputValueById[string](c, n, ni.Core_git_checkout_v1_Input_reference)
	if err != nil {
		return err
	}

	create, _ := core.InputValueById[bool](c, n, ni.Core_git_checkout_v1_Input_create)
	force, _ := core.InputValueById[bool](c, n, ni.Core_git_checkout_v1_Input_force)

	repo, err := git.PlainOpen(repoInfo.Path)
	if err != nil {
		err := core.CreateErr(c, err, "failed to open repository at path '%s'", repoInfo.Path)
		return n.Execute(ni.Core_git_checkout_v1_Output_exec_err, c, err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		err := core.CreateErr(c, err, "failed to get worktree from repository")
		return n.Execute(ni.Core_git_checkout_v1_Output_exec_err, c, err)
	}

	checkoutOpts := &git.CheckoutOptions{
		Create: create,
		Force:  force,
	}

	// go-git's Checkout function can handle branch names and commit hashes.
	// We first try to resolve the reference as a hash.
	hash, err := repo.ResolveRevision(plumbing.Revision(reference))
	if err != nil {
		// If it's not a hash, it might be a branch name.
		checkoutOpts.Branch = plumbing.NewBranchReferenceName(reference)
	} else {
		// If it resolves to a hash, we check out that specific commit.
		checkoutOpts.Hash = *hash
	}

	err = worktree.Checkout(checkoutOpts)
	if err != nil {
		err := core.CreateErr(c, err, "failed to perform checkout for reference '%s'", reference)
		return n.Execute(ni.Core_git_checkout_v1_Output_exec_err, c, err)
	}

	return n.Execute(ni.Core_git_checkout_v1_Output_exec_success, c, nil)
}

func init() {
	err := core.RegisterNodeFactory(gitCheckoutDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool) (core.NodeBaseInterface, []error) {
		return &GitCheckoutNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
