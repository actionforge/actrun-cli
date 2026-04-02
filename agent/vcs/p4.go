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

// P4Provider fetches only the pipeline file from Perforce.
// The full workspace sync is the graph's responsibility.
// Credentials are handled via the runner's environment (P4USER, P4PASSWD, P4TICKETS, etc.).
type P4Provider struct {
	p4          *p4go.P4
	clientName  string
	reuseClient string // when set, reuse this existing workspace
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

	// For SSL servers, set up a trust file so the API can persist fingerprints.
	if strings.HasPrefix(url, "ssl:") {
		trustFile := os.Getenv("P4TRUST")
		if trustFile == "" {
			trustFile = filepath.Join(os.TempDir(), ".p4trust")
			os.Setenv("P4TRUST", trustFile)
		}
		p.p4.SetTrustFile(trustFile)
	}

	if connected, err := p.p4.Connect(); !connected {
		return CheckoutResult{}, fmt.Errorf("p4 connect failed: %w", err)
	}

	// Accept the server fingerprint for SSL connections.
	if strings.HasPrefix(url, "ssl:") {
		p.p4.Run("trust", "-y")
	}

	// Authenticate using RunLogin() which feeds the password via SetInput.
	if os.Getenv("P4PASSWD") != "" {
		if _, err := p.p4.RunLogin(); err != nil {
			return CheckoutResult{}, fmt.Errorf("p4 login failed: %w", err)
		}
	}

	// Normalize ref: ensure it ends with /
	depotBase := strings.TrimRight(ref, "/")

	if p.reuseClient != "" {
		// Reuse existing workspace — sync only the pipeline file
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

		if err := os.MkdirAll(root, 0755); err != nil {
			return CheckoutResult{}, fmt.Errorf("failed to create workspace root: %w", err)
		}

		// Sync only the pipeline file
		pipelineDepotPath := depotBase + "/" + pipeline
		if _, err := p.p4.Run("sync", "-f", pipelineDepotPath); err != nil {
			return CheckoutResult{}, fmt.Errorf("p4 sync pipeline file failed: %w", err)
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
		depotBase + "/... //" + p.clientName + "/...",
	}

	if _, err := p.p4.RunSave("client", clientSpec); err != nil {
		return CheckoutResult{}, fmt.Errorf("p4 save client spec failed: %w", err)
	}

	// Sync only the pipeline file
	pipelineDepotPath := depotBase + "/" + pipeline
	if _, err := p.p4.Run("sync", "-f", pipelineDepotPath); err != nil {
		return CheckoutResult{}, fmt.Errorf("p4 sync pipeline file failed: %w", err)
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
