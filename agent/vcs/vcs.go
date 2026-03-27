package vcs

import (
	"context"
	"fmt"
)

// p4Available is set to true by p4.go init() when built with the p4 tag.
var p4Available bool

// Options configures VCS provider behavior.
type Options struct {
	// P4Client reuses an existing Perforce workspace instead of creating a
	// temporary one per run. When empty, a temp workspace is created and
	// cleaned up after each run.
	P4Client string
}

// CheckoutResult contains the result of a VCS checkout operation.
type CheckoutResult struct {
	// Dir is the directory where files were placed.
	Dir string
	// Persistent is true when the checkout uses a permanent workspace
	// (e.g. a reused P4 client). The worker should run scripts directly
	// in this directory instead of copying them to a temp location.
	Persistent bool
	// SHA is the resolved commit SHA (or changelist number for P4) after checkout.
	SHA string
}

// Provider handles VCS checkout operations.
type Provider interface {
	// Checkout fetches files from the VCS. pipeline is the relative path to
	// the script file needed. destDir is the preferred checkout location,
	// but providers may use a different root (e.g. an existing P4 workspace).
	Checkout(ctx context.Context, url, ref, pipeline, destDir string) (CheckoutResult, error)
	Cleanup(ctx context.Context) error
}

// New creates a VCS provider for the given type.
func New(vcsType string, opts Options) (Provider, error) {
	switch vcsType {
	case "git", "github":
		return &GitProvider{}, nil
	case "p4":
		if !p4Available {
			return nil, fmt.Errorf("Perforce support not compiled in. Rebuild with: go build -tags p4")
		}
		return &P4Provider{reuseClient: opts.P4Client}, nil
	case "", "local", "orchestrator":
		return &GitProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported VCS type: %s", vcsType)
	}
}
