// This file defines the structure of a GitHub Action workflow file (workflow.yml).
//
// After trying to understand various docs and sources, I assume that there isn't actually
// a single "source of truth" for this schema. so here are some info that might be useful:
//
// 1. github.com/actions/runner is the cloest what I've found but its mostly programmatic
//    and types aren't defined in one place. But its the best start.
//
// 3. https://www.schemastore.org/github-workflow.json The official schema for GitHub
//    Workflows. This is a good addition but too strict in some places and too loose
//    in others. Eg timeout can be an expression but not defined in the schema.
//
// So below is an effort to combine all sources using Gemini 3 Pro. **So far** I haven't
// come across any workflow that violates this schema, but if you find one, please open an issue, thx!

package gh_workflow_yml

import (
	"go.yaml.in/yaml/v4"
)

// GhWorkflow represents a GitHub Actions workflow file.
type GhWorkflow struct {
	Name        string            `yaml:"name,omitempty"`
	RunName     string            `yaml:"run-name,omitempty"`
	On          WorkflowTriggers  `yaml:"on,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	Defaults    Defaults          `yaml:"defaults,omitempty"`
	Concurrency Concurrency       `yaml:"concurrency,omitempty"`
	Permissions Permissions       `yaml:"permissions,omitempty"`
	Jobs        map[string]Job    `yaml:"jobs"`
}

// ----------------------------------------------------------------------------
// 1. Triggers (on: push | [push, pull_request] | { push: ... })
// ----------------------------------------------------------------------------

type WorkflowTriggers struct {
	// Events captures all configurations. If "on" was a string or list,
	// they are converted to map keys with empty values.
	Events map[string]interface{} `yaml:"-"`
}

func (w *WorkflowTriggers) UnmarshalYAML(value *yaml.Node) error {
	w.Events = make(map[string]interface{})

	// Case 1: Single string "on: workflow_dispatch"
	if value.Kind == yaml.ScalarNode {
		w.Events[value.Value] = map[string]interface{}{}
		return nil
	}

	// Case 2: Sequence "on: [push, pull_request]"
	if value.Kind == yaml.SequenceNode {
		for _, node := range value.Content {
			w.Events[node.Value] = map[string]interface{}{}
		}
		return nil
	}

	// Case 3: Map "on: { push: { branches: ... } }"
	return value.Decode(&w.Events)
}

// ----------------------------------------------------------------------------
// 2. Job Definitions
// ----------------------------------------------------------------------------

type Job struct {
	Name            string               `yaml:"name,omitempty"`
	Needs           StringOrSlice        `yaml:"needs,omitempty"`
	Permissions     Permissions          `yaml:"permissions,omitempty"`
	If              string               `yaml:"if,omitempty"`
	RunsOn          RunsOn               `yaml:"runs-on,omitempty"`
	Environment     Environment          `yaml:"environment,omitempty"`
	Concurrency     Concurrency          `yaml:"concurrency,omitempty"`
	Outputs         map[string]string    `yaml:"outputs,omitempty"`
	Env             map[string]string    `yaml:"env,omitempty"`
	Defaults        Defaults             `yaml:"defaults,omitempty"`
	Steps           []Step               `yaml:"steps,omitempty"`
	TimeoutMinutes  int                  `yaml:"timeout-minutes,omitempty"`
	ContinueOnError BoolOrString         `yaml:"continue-on-error,omitempty"`
	Strategy        *Strategy            `yaml:"strategy,omitempty"`
	Container       Container            `yaml:"container,omitempty"`
	Services        map[string]Container `yaml:"services,omitempty"`

	// Reusable workflow specific
	Uses    string                 `yaml:"uses,omitempty"`
	With    map[string]interface{} `yaml:"with,omitempty"`
	Secrets Secrets                `yaml:"secrets,omitempty"`
}

type Step struct {
	ID               string                 `yaml:"id,omitempty"`
	If               string                 `yaml:"if,omitempty"`
	Name             string                 `yaml:"name,omitempty"`
	Uses             string                 `yaml:"uses,omitempty"`
	Run              string                 `yaml:"run,omitempty"`
	Shell            string                 `yaml:"shell,omitempty"`
	WorkingDirectory string                 `yaml:"working-directory,omitempty"`
	With             map[string]interface{} `yaml:"with,omitempty"`
	Env              map[string]string      `yaml:"env,omitempty"`
	ContinueOnError  BoolOrString           `yaml:"continue-on-error,omitempty"`
	TimeoutMinutes   int                    `yaml:"timeout-minutes,omitempty"`
}

// ----------------------------------------------------------------------------
// 3. Polymorphic Types (Fixing the Unmarshal Errors)
// ----------------------------------------------------------------------------

// RunsOn handles:
// - String: "ubuntu-latest"
// - List: ["self-hosted", "linux"]
// - Object: { group: "...", labels: ... }
type RunsOn struct {
	Target string   // For simple "ubuntu-latest" or expression strings
	Labels []string // For ["self-hosted", "linux"]
	Group  string   // For object syntax
}

func (r *RunsOn) UnmarshalYAML(value *yaml.Node) error {
	// Case 1: Scalar String (e.g., "ubuntu-latest" or "${{ inputs.runs-on }}")
	if value.Kind == yaml.ScalarNode {
		r.Target = value.Value
		return nil
	}

	// Case 2: Sequence of strings (e.g., ["self-hosted", "linux"])
	if value.Kind == yaml.SequenceNode {
		return value.Decode(&r.Labels)
	}

	// Case 3: Map/Object (e.g., { group: "ubuntu-runners" })
	var temp struct {
		Group  string      `yaml:"group"`
		Labels interface{} `yaml:"labels"`
	}
	if err := value.Decode(&temp); err != nil {
		return err
	}
	r.Group = temp.Group

	// Handle labels inside the object which usually implies 'self-hosted' logic,
	// but strictly we just need to not error out.
	// We can try to decode labels as string slice if present.
	return nil
}

// Secrets handles:
// - String: "inherit"
// - Map: { key: val }
type Secrets struct {
	Inherit bool
	Map     map[string]string
}

func (s *Secrets) UnmarshalYAML(value *yaml.Node) error {
	// Case 1: "secrets: inherit"
	if value.Kind == yaml.ScalarNode && value.Value == "inherit" {
		s.Inherit = true
		return nil
	}
	// Case 2: Map of secrets
	return value.Decode(&s.Map)
}

// Container handles:
// - String: "node:14" or "${{ fromJSON(...) }}"
// - Object: { image: "node:14", options: "..." }
type Container struct {
	Image       string            `yaml:"image,omitempty"`
	Credentials map[string]string `yaml:"credentials,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	Ports       []string          `yaml:"ports,omitempty"`
	Volumes     []string          `yaml:"volumes,omitempty"`
	Options     string            `yaml:"options,omitempty"`
	IsString    bool              `yaml:"-"` // Helper to identify scalar containers
}

func (c *Container) UnmarshalYAML(value *yaml.Node) error {
	// Case 1: String scalar (Image name or Expression)
	if value.Kind == yaml.ScalarNode {
		c.Image = value.Value
		c.IsString = true
		return nil
	}
	// Case 2: Object definition
	// We use a type alias to avoid recursive infinite loop
	type plain Container
	return value.Decode((*plain)(c))
}

// StringOrSlice handles:
// - String: "job-name"
// - List: ["job-a", "job-b"]
type StringOrSlice []string

func (s *StringOrSlice) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*s = []string{value.Value}
		return nil
	}
	var slice []string
	if err := value.Decode(&slice); err != nil {
		return err
	}
	*s = slice
	return nil
}

// Concurrency handles:
// - String: "group-name"
// - Object: { group: "group-name", cancel-in-progress: true }
type Concurrency struct {
	Group            string       `yaml:"group,omitempty"`
	CancelInProgress BoolOrString `yaml:"cancel-in-progress,omitempty"`
}

func (c *Concurrency) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		c.Group = value.Value
		return nil
	}
	type plain Concurrency
	return value.Decode((*plain)(c))
}

// BoolOrString handles fields that can be a boolean or a generic expression string.
// Example: cancel-in-progress: ${{ inputs.cancel }}
type BoolOrString struct {
	Value      bool
	Expression string
	IsBool     bool
}

func (b *BoolOrString) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		// Try to decode as bool
		var boolVal bool
		if err := value.Decode(&boolVal); err == nil {
			b.Value = boolVal
			b.IsBool = true
			return nil
		}
		// Fallback to string
		b.Expression = value.Value
		b.IsBool = false
		return nil
	}
	return nil
}

// Permissions handles:
// - String: "read-all", "write-all"
// - Map: { contents: read, ... }
type Permissions struct {
	Scope  string
	Access map[string]string
}

func (p *Permissions) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		p.Scope = value.Value
		return nil
	}
	return value.Decode(&p.Access)
}

// Environment handles:
// - String: "production"
// - Object: { name: "production", url: "..." }
type Environment struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url,omitempty"`
}

func (e *Environment) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		e.Name = value.Value
		return nil
	}
	type plain Environment
	return value.Decode((*plain)(e))
}

// Strategy / Matrix
type Strategy struct {
	Matrix      Matrix      `yaml:"matrix"`
	FailFast    interface{} `yaml:"fail-fast,omitempty"`
	MaxParallel int         `yaml:"max-parallel,omitempty"`
}

// Matrix can be an expression string or a map of configs
type Matrix struct {
	Expression string
	Config     map[string]interface{}
}

func (m *Matrix) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		m.Expression = value.Value
		return nil
	}
	return value.Decode(&m.Config)
}

type Defaults struct {
	Run RunDefaults `yaml:"run,omitempty"`
}

type RunDefaults struct {
	Shell            string `yaml:"shell,omitempty"`
	WorkingDirectory string `yaml:"working-directory,omitempty"`
}
