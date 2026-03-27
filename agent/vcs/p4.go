//go:build p4

package vcs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	p4go "github.com/perforce/p4go"
)

func init() {
	p4Available = true
}

// P4Provider checks out from Perforce using the p4go library.
// Credentials are handled via the runner's environment (P4USER, P4PASSWD, P4TICKETS, etc.).
type P4Provider struct {
	p4          *p4go.P4
	clientName  string
	reuseClient string // when set, reuse this existing workspace for incremental syncs
	tempClient  bool   // true if we created the workspace and should delete it on cleanup
}

func (p *P4Provider) Checkout(ctx context.Context, url, ref, pipeline, destDir string) (CheckoutResult, error) {
	p.p4 = p4go.New()
	p.p4.SetPort(url)

	// Pick up credentials from environment
	if user := os.Getenv("P4USER"); user != "" {
		p.p4.SetUser(user)
	}
	if passwd := os.Getenv("P4PASSWD"); passwd != "" {
		p.p4.SetPassword(passwd)
	}

	if _, err := p.p4.Connect(); err != nil {
		return CheckoutResult{}, fmt.Errorf("p4 connect failed: %w", err)
	}

	// Normalize ref: ensure it ends with /...
	depotPath := strings.TrimRight(ref, "/") + "/..."

	if p.reuseClient != "" {
		// Reuse existing workspace — incremental sync
		p.clientName = p.reuseClient
		p.tempClient = false
		p.p4.SetClient(p.clientName)

		// Get the workspace root from the client spec
		clientSpec, err := p.p4.RunFetch("client")
		if err != nil {
			return CheckoutResult{}, fmt.Errorf("p4 fetch client spec failed: %w", err)
		}
		root, _ := clientSpec["Root"].(string)
		if root == "" {
			return CheckoutResult{}, fmt.Errorf("p4 client %s has no root", p.clientName)
		}

		// Ensure workspace root exists
		if err := os.MkdirAll(root, 0755); err != nil {
			return CheckoutResult{}, fmt.Errorf("failed to create workspace root: %w", err)
		}

		// Sync to latest; use -f (force) to handle cases where the
		// workspace directory was wiped but P4 still thinks files are synced.
		entries, _ := os.ReadDir(root)
		if len(entries) == 0 {
			if _, err := p.p4.Run("sync", "-f"); err != nil {
				return CheckoutResult{}, fmt.Errorf("p4 force sync failed: %w", err)
			}
		} else {
			if _, err := p.p4.Run("sync"); err != nil {
				return CheckoutResult{}, fmt.Errorf("p4 sync failed: %w", err)
			}
		}

		return CheckoutResult{Dir: root, Persistent: true}, nil
	}

	// Create temporary workspace
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return CheckoutResult{}, fmt.Errorf("failed to create dest dir: %w", err)
	}

	absDir, err := filepath.Abs(destDir)
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("failed to resolve dest dir: %w", err)
	}

	p.clientName = fmt.Sprintf("actrun-%s", filepath.Base(absDir))
	p.tempClient = true
	p.p4.SetClient(p.clientName)

	clientSpec, err := p.p4.RunFetch("client")
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("p4 fetch client spec failed: %w", err)
	}

	clientSpec["Root"] = absDir
	clientSpec["Host"] = ""
	clientSpec["Options"] = "noallwrite noclobber nocompress unlocked nomodtime rmdir"
	clientSpec["View"] = []string{
		depotPath + " //" + p.clientName + "/...",
	}

	if _, err := p.p4.RunSave("client", clientSpec); err != nil {
		return CheckoutResult{}, fmt.Errorf("p4 save client spec failed: %w", err)
	}

	// Force sync all files into the new workspace
	if _, err := p.p4.Run("sync", "-f", depotPath); err != nil {
		return CheckoutResult{}, fmt.Errorf("p4 sync failed: %w", err)
	}

	return CheckoutResult{Dir: absDir}, nil
}

func (p *P4Provider) Cleanup(ctx context.Context) error {
	if p.p4 == nil {
		return nil
	}
	if p.tempClient {
		// Delete the temp client workspace
		p.p4.Run("client", "-d", p.clientName)
	}
	p.p4.Disconnect()
	p.p4.Close()
	return nil
}
