package vcs

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// GitProvider fetches only the pipeline file from git repositories.
// The full repository clone is the graph's responsibility via git-clone nodes.
type GitProvider struct{}

func (g *GitProvider) Checkout(ctx context.Context, repoURL, ref, pipeline, destDir string) (CheckoutResult, error) {
	auth, cleanURL := gitAuth(repoURL)

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return CheckoutResult{}, fmt.Errorf("create checkout dir: %w", err)
	}

	// Clone into a temporary directory with no-checkout, then read just the
	// pipeline file from the git tree. This avoids writing the entire worktree
	// to disk while still resolving the correct commit SHA.
	tmpClone, err := os.MkdirTemp("", "git-sparse-*")
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpClone)

	var repo *git.Repository
	var commitSHA string

	if shaRe.MatchString(ref) {
		repo, commitSHA, err = g.fetchSHA(ctx, tmpClone, cleanURL, ref, auth)
	} else {
		repo, commitSHA, err = g.fetchRef(ctx, tmpClone, cleanURL, ref, auth)
	}
	if err != nil {
		return CheckoutResult{}, err
	}

	// Read only the pipeline file from the commit tree.
	data, err := readFileFromRepo(repo, commitSHA, pipeline)
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("read pipeline file from git: %w", err)
	}

	// Write the pipeline file at the expected relative path inside destDir.
	filePath := filepath.Join(destDir, pipeline)
	if dir := filepath.Dir(filePath); dir != destDir {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return CheckoutResult{}, fmt.Errorf("create pipeline dir: %w", err)
		}
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return CheckoutResult{}, fmt.Errorf("write pipeline file: %w", err)
	}

	return CheckoutResult{Dir: destDir, SHA: commitSHA}, nil
}

// fetchSHA fetches a single commit by SHA into a bare-ish repo (no checkout).
func (g *GitProvider) fetchSHA(ctx context.Context, dir, repoURL, sha string, auth transport.AuthMethod) (*git.Repository, string, error) {
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		return nil, "", fmt.Errorf("git init failed: %w", err)
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{repoURL},
	})
	if err != nil {
		return nil, "", fmt.Errorf("git remote add failed: %w", err)
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
		return nil, "", fmt.Errorf("git fetch failed: %w", err)
	}

	return repo, sha, nil
}

// fetchRef clones a single branch/tag at depth 1 with NoCheckout.
func (g *GitProvider) fetchRef(ctx context.Context, dir, repoURL, ref string, auth transport.AuthMethod) (*git.Repository, string, error) {
	cloneOpts := &git.CloneOptions{
		URL:           repoURL,
		Depth:         1,
		SingleBranch:  true,
		NoCheckout:    true,
		Auth:          auth,
		ReferenceName: plumbing.NewBranchReferenceName(ref),
	}

	repo, err := git.PlainCloneContext(ctx, dir, false, cloneOpts)
	if err != nil {
		// Branch ref failed — retry as tag.
		os.RemoveAll(dir)
		os.MkdirAll(dir, 0755)
		cloneOpts.ReferenceName = plumbing.NewTagReferenceName(ref)
		repo, err = git.PlainCloneContext(ctx, dir, false, cloneOpts)
		if err != nil {
			return nil, "", fmt.Errorf("git clone failed: %w", err)
		}
	}

	head, err := repo.Head()
	if err != nil {
		return nil, "", fmt.Errorf("resolve HEAD failed: %w", err)
	}

	return repo, head.Hash().String(), nil
}

// readFileFromRepo reads a single file from the commit tree without checking
// out the entire worktree.
func readFileFromRepo(repo *git.Repository, sha, path string) ([]byte, error) {
	hash := plumbing.NewHash(sha)
	commit, err := repo.CommitObject(hash)
	if err != nil {
		return nil, fmt.Errorf("resolve commit %s: %w", sha, err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	file, err := tree.File(path)
	if err != nil {
		return nil, fmt.Errorf("file %s not found in commit %s: %w", path, sha, err)
	}

	content, err := file.Contents()
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}

	return []byte(content), nil
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
