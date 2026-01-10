// This file defines the structure of a GitHub Action metadata file (action.yml).
//
// After trying to understand various docs and sources, I assume that there isn't actually
// a single "source of truth" for this schema. so here are some info that might be useful:
//
// 1. github.com/actions/runner is the cloest what I've found but also ignores (ofc)
//    marketplace metadata (like 'branding' and 'author'). But its the best start.
//
// 2. https://www.schemastore.org/github-action.json This is a good addition but too strict.
//    Eg it claims things like input `defaults` MUST be strings, but the runner actually
//    allows it to be a non-string and it converts it due to type coercion rules.
//
// 3. https://github.com/actions/languageservices The official language services for
//    GitHub Actions workflows and expressions but seems stale and also no source of truth.
//    From the readme: "[...] we are allocating resources towards other areas [...]"
//
// So below is an effort to combine all sources using Gemini 3 Pro. **So far** I haven't
// come across any action that violates this schema, but if you find one, please open an issue, thx!

package gh_action_yml

// GhAction represents the structure of a GitHub Action metadata file (action.yml).
// It synthesizes the requirements of the GitHub Runner (execution) and the
// GitHub Marketplace (display).
type GhAction struct {
	// Name is the name of your action. GitHub displays the `name` in the Actions tab.
	Name string `yaml:"name"`

	// Author is the name of the action's author (Required for Marketplace).
	Author string `yaml:"author,omitempty"`

	// Description is a short description of the action.
	Description string `yaml:"description"`

	// Branding contains the color and icon for the Marketplace badge.
	Branding *Branding `yaml:"branding,omitempty"`

	// Inputs is a map of input parameters. Key is the input ID.
	Inputs map[string]Input `yaml:"inputs,omitempty"`

	// Outputs is a map of output parameters. Key is the output ID.
	Outputs map[string]Output `yaml:"outputs,omitempty"`

	// Runs describes how the action is executed.
	Runs Runs `yaml:"runs"`
}

// Input represents a single input parameter.
type Input struct {
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`

	// Default is the default value.
	// NOTE: We use any because real-world actions use booleans (false),
	// numbers (1), and strings. The Runner converts them all to strings.
	Default any `yaml:"default,omitempty"`

	DeprecationMessage string `yaml:"deprecationMessage,omitempty"`
}

// Output represents a single output parameter.
type Output struct {
	Description string `yaml:"description"`
	Value       string `yaml:"value"` // Used for composite actions to map values
}

// Branding defines the visual appearance in the GitHub Marketplace.
type Branding struct {
	Color string `yaml:"color,omitempty"` // white, yellow, blue, green, orange, red, purple, gray-dark
	Icon  string `yaml:"icon,omitempty"`  // feather icon name (e.g., 'activity', 'cast', 'rotate-cw')
}

// Runs configures the path to the action's code and the executor (Node, Docker, or Composite).
// This struct is a union of all possible runner types.
type Runs struct {
	// Using is the execution runner.
	// Values: 'composite', 'docker', 'node12', 'node16', 'node20', 'node24'
	Using string `yaml:"using"`

	// --- Node.js Action Fields ---
	Main string `yaml:"main,omitempty"` // Entry file (e.g., 'dist/index.js')
	Pre  string `yaml:"pre,omitempty"`  // Pre-entry file
	Post string `yaml:"post,omitempty"` // Post-entry file

	// --- Docker Action Fields ---
	Image          string            `yaml:"image,omitempty"`           // Docker image or 'Dockerfile'
	Args           []string          `yaml:"args,omitempty"`            // Arguments for the container
	Env            map[string]string `yaml:"env,omitempty"`             // Environment variables
	Entrypoint     string            `yaml:"entrypoint,omitempty"`      // Overrides ENTRYPOINT
	PreEntrypoint  string            `yaml:"pre-entrypoint,omitempty"`  // Script before entrypoint
	PostEntrypoint string            `yaml:"post-entrypoint,omitempty"` // Script after entrypoint

	// --- Composite Action Fields ---
	Steps []Step `yaId:"steps,omitempty"`

	// --- Common Conditional Execution ---
	PreIf  string `yaml:"pre-if,omitempty"`  // Condition for running 'pre' steps
	PostIf string `yaml:"post-if,omitempty"` // Condition for running 'post' steps
}

// Step represents a single step in a Composite Action.
type Step struct {
	// Id is the unique identifier for the step.
	Id string `yaml:"id,omitempty"`

	// If is the conditional expression.
	If string `yaml:"if,omitempty"`

	// Name is the display name of the step.
	Name string `yaml:"name,omitempty"`

	// Uses selects an action to run.
	Uses string `yaml:"uses,omitempty"`

	// Run is the command to run (for shell steps).
	Run string `yaml:"run,omitempty"`

	// Shell is the shell to use (required if Run is specified).
	Shell string `yaml:"shell,omitempty"`

	// With maps input parameters to the action.
	// NOTE: Values here are typically strings or expressions like ${{ ... }}
	With map[string]string `yaml:"with,omitempty"`

	// Env sets environment variables for the step.
	Env map[string]string `yaml:"env,omitempty"`

	// WorkingDirectory specifies where the command is run.
	WorkingDirectory string `yaml:"working-directory,omitempty"`

	// ContinueOnError prevents the job from failing when this step fails.
	// Can be a boolean or an expression string.
	ContinueOnError any `yaml:"continue-on-error,omitempty"`
}
