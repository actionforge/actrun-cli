//go:build tests_unit

package tests_unit

import (
	"testing"

	"github.com/actionforge/actrun-cli/core"
	"go.yaml.in/yaml/v4"

	// initialize all nodes
	_ "github.com/actionforge/actrun-cli/nodes"
)

func loadTestGraph(t *testing.T, graphStr string) core.ActionGraph {
	t.Helper()
	var graphYaml map[string]any
	if err := yaml.Unmarshal([]byte(graphStr), &graphYaml); err != nil {
		t.Fatalf("unmarshal YAML: %v", err)
	}
	ag, errs := core.LoadGraph(graphYaml, nil, "", nil, core.RunOpts{})
	if len(errs) > 0 {
		t.Fatalf("LoadGraph: %v", errs[0])
	}
	return ag
}

func TestDisableConcurrencyNotSetByDefault(t *testing.T) {
	ag := loadTestGraph(t, `
entry: start
nodes:
  - id: start
    type: core/start@v1
    position: {x: 0, y: 0}
  - id: run1
    type: core/run@v1
    position: {x: 100, y: 0}
    inputs:
      shell: bash
      script: echo hello
connections: []
executions:
  - src: {node: start, port: exec}
    dst: {node: run1, port: exec}
`)

	node := ag.Nodes["run1"]
	if node == nil {
		t.Fatal("node run1 not found")
	}
	if node.DisableConcurrency() {
		t.Error("expected DisableConcurrency to be false by default")
	}
}

func TestDisableConcurrencySetFromYaml(t *testing.T) {
	ag := loadTestGraph(t, `
entry: start
nodes:
  - id: start
    type: core/start@v1
    position: {x: 0, y: 0}
  - id: run1
    type: core/run@v1
    position: {x: 100, y: 0}
    inputs:
      _disable_concurrency: true
      shell: bash
      script: echo hello
connections: []
executions:
  - src: {node: start, port: exec}
    dst: {node: run1, port: exec}
`)

	node := ag.Nodes["run1"]
	if node == nil {
		t.Fatal("node run1 not found")
	}
	if !node.DisableConcurrency() {
		t.Error("expected DisableConcurrency to be true when set in YAML")
	}
}
