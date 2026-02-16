//go:build tests_unit

package tests_unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v4"
)

func compileActfileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()

	schemaPath := filepath.Join("..", "actfile-schema.json")
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("failed to read schema file: %v", err)
	}

	var schemaObj any
	if err := json.Unmarshal(schemaData, &schemaObj); err != nil {
		t.Fatalf("failed to parse schema JSON: %v", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("actfile-schema.json", schemaObj); err != nil {
		t.Fatalf("failed to add schema resource: %v", err)
	}

	schema, err := compiler.Compile("actfile-schema.json")
	if err != nil {
		t.Fatalf("failed to compile schema: %v", err)
	}

	return schema
}

// parseAndValidate parses a YAML string and validates it against the schema.
func parseAndValidate(schema *jsonschema.Schema, yamlStr string) error {
	var data any
	if err := yaml.Unmarshal([]byte(yamlStr), &data); err != nil {
		return err
	}
	return schema.Validate(convertToJSONCompatible(data))
}

// TestActFilesMatchSchema validates all .act files in the e2e test directory
// against the JSON schema.
func TestActFilesMatchSchema(t *testing.T) {
	schema := compileActfileSchema(t)

	actDir := filepath.Join("..", "tests_e2e", "scripts")
	actFiles, err := filepath.Glob(filepath.Join(actDir, "*.act"))
	if err != nil {
		t.Fatalf("failed to glob .act files: %v", err)
	}

	if len(actFiles) == 0 {
		t.Fatal("no .act files found in tests_e2e/scripts/")
	}

	for _, actFile := range actFiles {
		name := filepath.Base(actFile)
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(actFile)
			if err != nil {
				t.Fatalf("failed to read file: %v", err)
			}

			var yamlData any
			if err := yaml.Unmarshal(data, &yamlData); err != nil {
				t.Fatalf("failed to parse YAML: %v", err)
			}

			if err := schema.Validate(convertToJSONCompatible(yamlData)); err != nil {
				t.Errorf("schema validation failed:\n%v", err)
			}
		})
	}
}

// TestActfileSchemaRejectsInvalid verifies that the schema actually catches
// structurally broken .act files. Each sub-test provides a YAML document that
// violates a specific schema constraint and must be rejected.
func TestActfileSchemaRejectsInvalid(t *testing.T) {
	schema := compileActfileSchema(t)

	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "missing entry",
			yaml: `
nodes:
  - id: n1
    type: core/start@v1
    position: {x: 0, y: 0}
`,
		},
		{
			name: "missing nodes",
			yaml: `
entry: n1
`,
		},
		{
			name: "entry wrong type",
			yaml: `
entry: 42
nodes: []
`,
		},
		{
			name: "nodes not an array",
			yaml: `
entry: n1
nodes:
  id: n1
  type: core/start@v1
`,
		},
		{
			name: "node missing id",
			yaml: `
entry: n1
nodes:
  - type: core/start@v1
    position: {x: 0, y: 0}
`,
		},
		{
			name: "node missing type",
			yaml: `
entry: n1
nodes:
  - id: n1
    position: {x: 0, y: 0}
`,
		},
		{
			name: "unknown top-level property",
			yaml: `
entry: n1
nodes: []
foobar: true
`,
		},
		{
			name: "unknown node property",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
    position: {x: 0, y: 0}
    foobar: true
`,
		},
		{
			name: "connection missing src",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
connections:
  - dst: {node: n1, port: exec}
`,
		},
		{
			name: "connection missing dst",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
connections:
  - src: {node: n1, port: exec}
`,
		},
		{
			name: "connection src missing node",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
connections:
  - src: {port: value}
    dst: {node: n1, port: value}
`,
		},
		{
			name: "connection src missing port",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
connections:
  - src: {node: n1}
    dst: {node: n1, port: value}
`,
		},
		{
			name: "connection unknown property",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
connections:
  - src: {node: n1, port: value}
    dst: {node: n1, port: value}
    foo: bar
`,
		},
		{
			name: "execution missing src",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
executions:
  - dst: {node: n1, port: exec}
`,
		},
		{
			name: "execution missing dst",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
executions:
  - src: {node: n1, port: exec}
`,
		},
		{
			name: "execution unknown property",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
executions:
  - src: {node: n1, port: exec}
    dst: {node: n1, port: exec}
    weight: 5
`,
		},
		{
			name: "isLoop wrong type in connection",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
connections:
  - src: {node: n1, port: value}
    dst: {node: n1, port: value}
    isLoop: "yes"
`,
		},
		{
			name: "isLoop wrong type in execution",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
executions:
  - src: {node: n1, port: exec}
    dst: {node: n1, port: exec}
    isLoop: 1
`,
		},
		{
			name: "top-level type invalid enum",
			yaml: `
entry: n1
type: invalid
nodes: []
`,
		},
		{
			name: "nested graph missing entry",
			yaml: `
entry: g1
nodes:
  - id: g1
    type: core/group@v1
    graph:
      nodes: []
`,
		},
		{
			name: "nested graph missing nodes",
			yaml: `
entry: g1
nodes:
  - id: g1
    type: core/group@v1
    graph:
      entry: gi
`,
		},
		{
			name: "nested graph unknown property",
			yaml: `
entry: g1
nodes:
  - id: g1
    type: core/group@v1
    graph:
      entry: gi
      nodes: []
      foobar: true
`,
		},
		{
			name: "nested graph type not group",
			yaml: `
entry: g1
nodes:
  - id: g1
    type: core/group@v1
    graph:
      entry: gi
      type: generic
      nodes: []
`,
		},
		{
			name: "input definition missing type",
			yaml: `
entry: n1
nodes: []
inputs:
  my-input:
    name: My Input
    index: 0
`,
		},
		{
			name: "input definition missing index",
			yaml: `
entry: n1
nodes: []
inputs:
  my-input:
    name: My Input
    type: string
`,
		},
		{
			name: "input definition unknown property",
			yaml: `
entry: n1
nodes: []
inputs:
  my-input:
    name: My Input
    type: string
    index: 0
    foobar: true
`,
		},
		{
			name: "output definition missing type",
			yaml: `
entry: n1
nodes: []
outputs:
  my-output:
    name: My Output
    index: 0
`,
		},
		{
			name: "output definition missing index",
			yaml: `
entry: n1
nodes: []
outputs:
  my-output:
    name: My Output
    type: string
`,
		},
		{
			name: "output definition unknown property",
			yaml: `
entry: n1
nodes: []
outputs:
  my-output:
    name: My Output
    type: string
    index: 0
    foobar: true
`,
		},
		{
			name: "position unknown property",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
    position: {x: 0, y: 0, z: 0}
`,
		},
		{
			name: "editor unknown property",
			yaml: `
editor:
  version:
    created: v1.0.0
  theme: dark
entry: n1
nodes: []
`,
		},
		{
			name: "settings unknown property",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
    settings:
      folded: false
      color: red
`,
		},
		{
			name: "port reference unknown property",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
connections:
  - src: {node: n1, port: value, index: 0}
    dst: {node: n1, port: value}
`,
		},
		{
			name: "info unknown property",
			yaml: `
entry: n1
nodes: []
info:
  author: test
  version: "1.0"
  contact: test@test.com
  description: Test
  license: MIT
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := parseAndValidate(schema, tc.yaml)
			if err == nil {
				t.Errorf("expected schema validation to fail for %q, but it passed", tc.name)
			}
		})
	}
}

// TestActfileSchemaAcceptsValid verifies that the schema accepts well-formed
// .act documents covering all major structural features.
func TestActfileSchemaAcceptsValid(t *testing.T) {
	schema := compileActfileSchema(t)

	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "minimal graph",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
`,
		},
		{
			name: "full graph with connections and executions",
			yaml: `
entry: start
type: generic
desc: A test graph
nodes:
  - id: start
    type: core/start@v1
    position: {x: 0, y: 0}
  - id: print
    type: core/print@v1
    position: {x: 200, y: 0}
    inputs:
      "values[0]": hello
connections:
  - src: {node: start, port: args}
    dst: {node: print, port: "values[0]"}
    isLoop: false
executions:
  - src: {node: start, port: exec}
    dst: {node: print, port: exec}
`,
		},
		{
			name: "graph with editor metadata",
			yaml: `
editor:
  version:
    created: v1.26.5
    updated: v1.34.0
entry: n1
nodes:
  - id: n1
    type: core/start@v1
`,
		},
		{
			name: "node with all optional fields",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
    label: My Start Node
    comment: This is a comment
    position: {x: 100, y: -50}
    dimensions: {width: 300, height: 200}
    settings:
      folded: true
    inputs:
      value: test
    outputs:
      exec[0]: null
`,
		},
		{
			name: "group node with nested graph",
			yaml: `
entry: g1
nodes:
  - id: g1
    type: core/group@v1
    position: {x: 0, y: 0}
    graph:
      entry: gi
      type: group
      nodes:
        - id: gi
          type: core/group-inputs@v1
          position: {x: 0, y: 0}
        - id: go
          type: core/group-outputs@v1
          position: {x: 200, y: 0}
      connections: []
      executions:
        - src: {node: gi, port: exec}
          dst: {node: go, port: exec}
      inputs:
        exec-in:
          type: ""
          index: 0
          exec: true
      outputs:
        exec-out:
          type: ""
          index: 0
          exec: true
`,
		},
		{
			name: "graph-level inputs and outputs",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/group-inputs@v1
inputs:
  my-string:
    name: My String
    type: string
    index: 0
    desc: A string input
    required: true
    default: hello
    multiline: true
    hint: Enter a value
  my-number:
    type: number
    index: 1
    step: 0.5
  my-option:
    name: Choice
    type: option
    index: 2
    options:
      - name: Option A
        value: a
      - name: Option B
        value: b
  my-array:
    type: string
    index: 3
    array: true
    array_initial_count: 2
    array_hints:
      - First item
      - Second item
  my-exec:
    type: ""
    index: 4
    exec: true
  my-hidden:
    type: bool
    index: 5
    hide_socket: true
    initial: true
outputs:
  result:
    name: Result
    type: string
    index: 0
    desc: The output
  exec-done:
    type: ""
    index: 1
    exec: true
`,
		},
		{
			name: "loop-back connections",
			yaml: `
entry: start
nodes:
  - id: start
    type: core/start@v1
  - id: loop
    type: core/array-add@v1
connections:
  - src: {node: loop, port: array}
    dst: {node: loop, port: array}
    isLoop: true
executions:
  - src: {node: start, port: exec}
    dst: {node: loop, port: exec}
  - src: {node: loop, port: exec}
    dst: {node: loop, port: exec}
    isLoop: true
`,
		},
		{
			name: "github action node type",
			yaml: `
entry: start
nodes:
  - id: start
    type: core/gh-start@v1
  - id: checkout
    type: github.com/actions/checkout@v4
    inputs:
      fetch-depth: 0
`,
		},
		{
			name: "graph with info block",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
info:
  author: Test Author
  version: "1.0"
  contact: author@example.com
  description: A test graph
`,
		},
		{
			name: "all port types in inputs",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/group-inputs@v1
inputs:
  p-string: {type: string, index: 0}
  p-number: {type: number, index: 1}
  p-bool: {type: bool, index: 2}
  p-secret: {type: secret, index: 3}
  p-option: {type: option, index: 4}
  p-stream: {type: stream, index: 5}
  p-any: {type: any, index: 6}
  p-iterable: {type: iterable, index: 7}
  p-indexable: {type: indexable, index: 8}
  p-arr-any: {type: "[]any", index: 9}
  p-arr-string: {type: "[]string", index: 10}
  p-arr-number: {type: "[]number", index: 11}
  p-arr-bool: {type: "[]bool", index: 12}
  p-exec: {type: "", index: 13, exec: true}
  p-unknown: {type: unknown, index: 14}
`,
		},
		{
			name: "empty optional sections",
			yaml: `
entry: n1
nodes:
  - id: n1
    type: core/start@v1
connections: []
executions: []
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := parseAndValidate(schema, tc.yaml)
			if err != nil {
				t.Errorf("expected valid document to pass, got:\n%v", err)
			}
		})
	}
}

// convertToJSONCompatible recursively converts YAML-unmarshalled data into
// types that the JSON schema validator accepts. In particular, it converts
// integer types to float64 (JSON's number type).
func convertToJSONCompatible(v any) any {
	switch val := v.(type) {
	case map[string]any:
		result := make(map[string]any, len(val))
		for k, v := range val {
			result[k] = convertToJSONCompatible(v)
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, v := range val {
			result[i] = convertToJSONCompatible(v)
		}
		return result
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case float32:
		return float64(val)
	default:
		return val
	}
}
