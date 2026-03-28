package vcs

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// GitProvider fetches pipeline files from git repositories using go-git.
// No external git binary is required.
type GitProvider struct{}

func (g *GitProvider) Checkout(ctx context.Context, repoURL, ref, pipeline, destDir string) (CheckoutResult, error) {
	auth, cleanURL := gitAuth(repoURL)

	if shaRe.MatchString(ref) {
		return g.checkoutSHA(ctx, cleanURL, ref, destDir, auth)
	}
	return g.checkoutRef(ctx, cleanURL, ref, destDir, auth)
}

func (g *GitProvider) checkoutSHA(ctx context.Context, repoURL, sha, destDir string, auth transport.AuthMethod) (CheckoutResult, error) {
	repo, err := git.PlainInit(destDir, false)
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("git init failed: %w", err)
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{repoURL},
	})
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("git remote add failed: %w", err)
	}

	hash := plumbing.NewHash(sha)
	err = repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		Depth:      1,
		Auth:       auth,
		RefSpecs: []config.RefSpec{
			config.RefSpec(hash.String() + ":refs/heads/fetched"),
		},
	})
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("git fetch failed: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("worktree failed: %w", err)
	}

	if err = wt.Checkout(&git.CheckoutOptions{Hash: hash}); err != nil {
		return CheckoutResult{}, fmt.Errorf("git checkout failed: %w", err)
	}

	return CheckoutResult{Dir: destDir, SHA: sha}, nil
}

func (g *GitProvider) checkoutRef(ctx context.Context, repoURL, ref, destDir string, auth transport.AuthMethod) (CheckoutResult, error) {
	cloneOpts := &git.CloneOptions{
		URL:           repoURL,
		Depth:         1,
		SingleBranch:  true,
		Auth:          auth,
		ReferenceName: plumbing.NewBranchReferenceName(ref),
	}

	repo, err := git.PlainCloneContext(ctx, destDir, false, cloneOpts)
	if err != nil {
		// Branch ref failed — retry as tag.
		os.RemoveAll(destDir)
		cloneOpts.ReferenceName = plumbing.NewTagReferenceName(ref)
		repo, err = git.PlainCloneContext(ctx, destDir, false, cloneOpts)
		if err != nil {
			return CheckoutResult{}, fmt.Errorf("git clone failed: %w", err)
		}
	}

	head, err := repo.Head()
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("resolve HEAD failed: %w", err)
	}

	return CheckoutResult{Dir: destDir, SHA: head.Hash().String()}, nil
}

func (g *GitProvider) Cleanup(ctx context.Context) error {
	return nil
}

// gitAuth extracts authentication from the repository URL or the
// process environment. Returns the auth method and a URL with
// userinfo stripped.
func gitAuth(repoURL string) (transport.AuthMethod, string) {
	u, err := url.Parse(repoURL)
	if err == nil && u.User != nil {
		password, _ := u.User.Password()
		auth := &http.BasicAuth{
			Username: u.User.Username(),
			Password: password,
		}
		u.User = nil
		return auth, u.String()
	}

	if user := os.Getenv("GIT_USERNAME"); user != "" {
		return &http.BasicAuth{
			Username: user,
			Password: os.Getenv("GIT_PASSWORD"),
		}, repoURL
	}

	if os.Getenv("SSH_AUTH_SOCK") != "" {
		if sshAuth, err := gitssh.NewSSHAgentAuth("git"); err == nil {
			return sshAuth, repoURL
		}
	}

	if keyFile := os.Getenv("GIT_SSH_KEY_FILE"); keyFile != "" {
		if sshAuth, err := gitssh.NewPublicKeysFromFile("git", keyFile, ""); err == nil {
			return sshAuth, repoURL
		}
	}

	return nil, repoURL
}
