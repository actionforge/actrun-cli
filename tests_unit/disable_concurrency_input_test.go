//go:build tests_unit

package tests_unit

import (
	"testing"

	"github.com/actionforge/actrun-cli/core"

	// initialize all nodes
	_ "github.com/actionforge/actrun-cli/nodes"
)

func TestDisableConcurrencyInputInjection(t *testing.T) {
	registries := core.GetRegistries()
	if len(registries) == 0 {
		t.Fatal("no node types registered; node factories not loaded")
	}

	for id, nodeDef := range registries {
		// Count execution inputs to determine if this is an execution node.
		hasExecInput := false
		for _, inputDef := range nodeDef.Inputs {
			if inputDef.Exec {
				hasExecInput = true
				break
			}
		}

		dcDef, hasDC := nodeDef.Inputs[core.InputId("_disable_concurrency")]

		if hasExecInput {
			if !hasDC {
				t.Errorf("node %s has execution inputs but is missing _disable_concurrency input", id)
				continue
			}
			if dcDef.Type != "bool" {
				t.Errorf("node %s: _disable_concurrency type = %q, want \"bool\"", id, dcDef.Type)
			}
			if dcDef.Default != false {
				t.Errorf("node %s: _disable_concurrency default = %v, want false", id, dcDef.Default)
			}
			if !dcDef.HideSocket {
				t.Errorf("node %s: _disable_concurrency HideSocket = false, want true", id)
			}
		} else {
			if hasDC {
				t.Errorf("node %s has no execution inputs but has _disable_concurrency input", id)
			}
		}
	}
}
