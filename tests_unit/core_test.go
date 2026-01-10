//go:build tests_unit

package tests_unit

import (
	"testing"

	"github.com/actionforge/actrun-cli/core"

	// initialize all nodes
	_ "github.com/actionforge/actrun-cli/nodes"
)

func TestPortValidation(t *testing.T) {
	testCases := []struct {
		name      string
		portId    string
		portDef   core.PortDefinition
		shouldErr bool
	}{
		{"ValidExecPort1", "exec-valid", core.PortDefinition{Exec: true}, false},
		{"ValidExecPort2", "exec-success", core.PortDefinition{Exec: true}, false},
		{"ValidExecPort3", "exec", core.PortDefinition{Exec: true}, false},
		{"ValidExecPort4", "exec-success_foo", core.PortDefinition{Exec: true}, false},

		{"InvalidExecPort1", "", core.PortDefinition{Exec: true}, true},
		{"InvalidExecPort2", "-", core.PortDefinition{Exec: true}, true},
		{"InvalidExecPort3", "exec_success_foo", core.PortDefinition{Exec: true}, true},
		{"InvalidExecPort5", "exec-success-foo", core.PortDefinition{Exec: true}, true},
		{"InvalidExecPort6", "invalid-exec", core.PortDefinition{Exec: true}, true},
		{"InvalidExecPort7", "invalid", core.PortDefinition{Exec: true}, true},

		{"ValidDataPort1", "foo", core.PortDefinition{Exec: false}, false},
		{"ValidDataPort2", "foo_abc", core.PortDefinition{Exec: false}, false},

		{"InvalidDataPort1", "", core.PortDefinition{Exec: false}, true},
		{"InvalidDataPort2", "-", core.PortDefinition{Exec: false}, true},
		{"InvalidDataPort3", "invalid-port", core.PortDefinition{Exec: false}, true},
		{"InvalidDataPort3", "exec", core.PortDefinition{Exec: false}, true},
		{"InvalidDataPort3", "exec-123", core.PortDefinition{Exec: false}, true},
	}

	// Run each test case
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := core.PortDefValidation(tc.portId, tc.portDef)
			if (err != nil) != tc.shouldErr {
				t.Errorf("unexpected error: %v, expected error: %v", err, tc.shouldErr)
			}
		})
	}
}

// Run a simple script/program and check that the output is correct.
func Test_NewNodeInstance(t *testing.T) {
	n, err := core.NewNodeInstance("core/run@v1", nil, "", nil, false)
	if err != nil {
		t.Fatal(err)
	}

	if n.GetNodeTypeId() != "core/run@v1" {
		t.Errorf("Expected node name to be 'core/run@v1', got '%s'", n.GetNodeTypeId())
	}
}
