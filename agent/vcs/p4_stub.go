//go:build !p4

package vcs

import (
	"context"
	"fmt"
)

// P4Provider is a stub when built without the p4 build tag.
// Build with -tags p4 to enable Perforce support (requires P4 C++ API SDK).
type P4Provider struct {
	reuseClient string
}

func (p *P4Provider) Checkout(ctx context.Context, url, ref, pipeline, destDir string) (CheckoutResult, error) {
	return CheckoutResult{}, fmt.Errorf("Perforce support not compiled in. Rebuild with: go build -tags p4")
}

func (p *P4Provider) Cleanup(ctx context.Context) error {
	return nil
}
