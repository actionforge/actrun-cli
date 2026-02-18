package core

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/actionforge/actrun-cli/utils"
)

// PostStepRunner is implemented by node types that support post-step execution.
type PostStepRunner interface {
	RunPost(c *ExecutionState, env map[string]string) error
}

// PostStep holds the information needed to execute a post step.
type PostStep struct {
	ActionName    string
	NodeID        string
	PostIf        string
	Runner        PostStepRunner
	StateFilePath string
	EnvSnapshot   map[string]string
}

// PostStepQueue is a thread-safe queue for post steps.
type PostStepQueue struct {
	mu    sync.Mutex
	steps []PostStep
}

// NewPostStepQueue creates a new empty PostStepQueue.
func NewPostStepQueue() *PostStepQueue {
	return &PostStepQueue{}
}

// Register adds a post step to the queue.
func (q *PostStepQueue) Register(step PostStep) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.steps = append(q.steps, step)
}

// Len returns the number of registered post steps.
func (q *PostStepQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.steps)
}

// DrainLIFO returns all post steps in LIFO order and clears the queue.
func (q *PostStepQueue) DrainLIFO() []PostStep {
	q.mu.Lock()
	defer q.mu.Unlock()

	result := make([]PostStep, len(q.steps))
	for i, step := range q.steps {
		result[len(q.steps)-1-i] = step
	}
	q.steps = nil
	return result
}

// executePostSteps runs post steps in order, logging errors but continuing.
func executePostSteps(c *ExecutionState, steps []PostStep) {
	for _, step := range steps {
		utils.LogOut.Infof("Running post step: %s (%s)\n", step.ActionName, step.NodeID)

		if !evaluatePostIf(c, step) {
			utils.LogOut.Infof("Post step skipped (post-if condition not met): %s\n", step.ActionName)
			continue
		}

		env := step.EnvSnapshot
		if step.StateFilePath != "" {
			injectStateVars(env, step.StateFilePath)
		}

		if err := step.Runner.RunPost(c, env); err != nil {
			utils.LogErr.Errorf("Post step failed: %s: %v\n", step.ActionName, err)
		}
	}
}

// evaluatePostIf evaluates the post-if condition for a post step.
// Returns true if the step should run.
func evaluatePostIf(c *ExecutionState, step PostStep) bool {
	condition := step.PostIf
	if condition == "" {
		// Default: always()
		return true
	}

	// Wrap in ${{ }} if not already wrapped
	if !strings.Contains(condition, "${{") {
		condition = "${{ " + condition + " }}"
	}

	evaluator := NewEvaluator(c)
	result, err := evaluator.Evaluate(condition)
	if err != nil {
		utils.LogErr.Errorf("Failed to evaluate post-if condition for %s: %v\n", step.ActionName, err)
		return false
	}

	return isTruthy(result)
}

// injectStateVars reads the GITHUB_STATE file and injects STATE_* env vars.
func injectStateVars(env map[string]string, stateFilePath string) {
	if stateFilePath == "" {
		return
	}

	stateVars, err := ParseKeyValueFile(stateFilePath)
	if err != nil {
		utils.LogErr.Errorf("Failed to read state file %s: %v\n", stateFilePath, err)
		return
	}

	for key, value := range stateVars {
		env[fmt.Sprintf("STATE_%s", key)] = value
	}
}

// ParseKeyValueFile parses a GitHub Actions file command output file.
// Supports both NAME=VALUE and NAME<<DELIMITER heredoc styles.
func ParseKeyValueFile(filePath string) (map[string]string, error) {
	cleanPath, err := utils.ValidatePath(filePath)
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}

	return ParseKeyValueString(string(b))
}

// ParseKeyValueString parses a GitHub Actions key-value string.
// Supports both NAME=VALUE and NAME<<DELIMITER heredoc styles.
func ParseKeyValueString(input string) (map[string]string, error) {
	results := make(map[string]string)
	lines := strings.Split(input, "\n")

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}

		var key, value string
		equalsIndex := strings.Index(line, "=")
		heredocIndex := strings.Index(line, "<<")

		// Normal style: NAME=VALUE
		if equalsIndex >= 0 && (heredocIndex < 0 || equalsIndex < heredocIndex) {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 || parts[0] == "" {
				return nil, CreateErr(nil, nil, "invalid format '%s'. Name must not be empty", line)
			}
			key, value = parts[0], parts[1]
		} else if heredocIndex >= 0 && (equalsIndex < 0 || heredocIndex < equalsIndex) {
			// Heredoc style: NAME<<EOF
			parts := strings.SplitN(line, "<<", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return nil, CreateErr(nil, nil, "invalid format '%s'. Name must not be empty", line)
			}
			key = parts[0]
			delimiter := strings.TrimRight(parts[1], " \t\n\r")

			var heredocValue strings.Builder
			for i++; i < len(lines); i++ {
				if strings.TrimRight(lines[i], " \t\n\r") == delimiter {
					break
				}
				heredocValue.WriteString(lines[i])
				if i < len(lines)-1 {
					heredocValue.WriteString("\n")
				}
			}
			if i >= len(lines) {
				return nil, CreateErr(nil, nil, "invalid value. Matching delimiter not found '%s'", delimiter)
			}
			value = heredocValue.String()
		} else {
			return nil, CreateErr(nil, nil, "invalid format '%s'", line)
		}

		results[key] = value
	}

	return results, nil
}
