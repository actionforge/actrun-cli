package vcs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// GitProvider fetches individual pipeline files from git repositories
// using sparse checkout with partial clone filters. Only the requested
// file is downloaded from the remote.
// Auth: SSH keys via SSH_AUTH_SOCK/GIT_SSH_KEY_FILE, HTTPS tokens via
// GIT_USERNAME/GIT_PASSWORD or git credential helpers.
type GitProvider struct{}

func (g *GitProvider) Checkout(ctx context.Context, url, ref, pipeline, destDir string) (CheckoutResult, error) {
	env := gitEnv()

	if shaRe.MatchString(ref) {
		// SHA ref: init + fetch the exact commit
		if err := gitRun(ctx, env, "", "init", destDir); err != nil {
			return CheckoutResult{}, err
		}
		if err := gitRun(ctx, env, destDir, "remote", "add", "origin", url); err != nil {
			return CheckoutResult{}, err
		}
		if err := gitRun(ctx, env, destDir, "fetch", "--filter=blob:none", "--depth=1", "origin", ref); err != nil {
			return CheckoutResult{}, err
		}
		// Set sparse checkout before checkout so only the pipeline blob is fetched
		if err := gitRun(ctx, env, destDir, "sparse-checkout", "set", "--no-cone", pipeline); err != nil {
			return CheckoutResult{}, err
		}
		if err := gitRun(ctx, env, destDir, "checkout", "FETCH_HEAD"); err != nil {
			return CheckoutResult{}, err
		}
	} else {
		// Branch/tag ref: shallow clone (no files checked out yet)
		clone := exec.CommandContext(ctx, "git", "clone",
			"--filter=blob:none", "--no-checkout", "--depth=1",
			"-b", ref, url, destDir,
		)
		clone.Env = env
		if out, err := clone.CombinedOutput(); err != nil {
			return CheckoutResult{}, fmt.Errorf("git clone failed: %s: %w", out, err)
		}
		// Sparse checkout: tell git we only need the pipeline file
		if err := gitRun(ctx, env, destDir, "sparse-checkout", "set", "--no-cone", pipeline); err != nil {
			return CheckoutResult{}, err
		}
		// Checkout: fetches only the blob for the pipeline file
		if err := gitRun(ctx, env, destDir, "checkout"); err != nil {
			return CheckoutResult{}, err
		}
	}

	// Extract the commit SHA of HEAD
	var sha string
	revParse := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	revParse.Dir = destDir
	if shaOut, err := revParse.Output(); err == nil {
		sha = strings.TrimSpace(string(shaOut))
	}

	return CheckoutResult{Dir: destDir, SHA: sha}, nil
}

// gitRun executes a git command and returns a formatted error on failure.
func gitRun(ctx context.Context, env []string, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = env
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s failed: %s: %w", args[0], out, err)
	}
	return nil
}

func (g *GitProvider) Cleanup(ctx context.Context) error {
	return nil
}

// gitEnv builds the environment for git commands, forwarding auth-related
// variables from the runner environment.
func gitEnv() []string {
	env := os.Environ()
	// GIT_USERNAME/GIT_PASSWORD are picked up by credential helpers or
	// can be used with https://<user>:<pass>@host URLs. SSH auth flows
	// through SSH_AUTH_SOCK or GIT_SSH_KEY_FILE automatically.
	return env
}

