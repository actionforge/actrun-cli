//go:build tests_unit

package tests_unit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	// initialize all nodes

	gh_action_yml "github.com/actionforge/actrun-cli/github/action.yml"
	gh_workflow_yml "github.com/actionforge/actrun-cli/github/workflow.yml"

	_ "github.com/actionforge/actrun-cli/nodes"
	"go.yaml.in/yaml/v4"
)

func findGoModFile() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		path := filepath.Join(cwd, "go.mod")
		_, err := os.Stat(path)
		if err == nil {
			return cwd, nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(cwd)
		if parent == cwd {
			return "", errors.New("go.mod file not found")
		}

		cwd = parent
	}
}

func _testFunc[T any](t *testing.T, testDir string) error {
	projectRoot, err := findGoModFile()
	if err != nil {
		t.Error(err)
	}

	err = filepath.Walk(filepath.Join(projectRoot, testDir), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		stat, err := os.Stat(path)
		if err != nil {
			return err
		}

		if stat.IsDir() || !strings.HasSuffix(path, "action.yml") {
			return nil // just skip
		}

		actionYaml, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var a T
		err = yaml.Unmarshal(actionYaml, &a)
		if err != nil {
			t.Errorf("Failed to unmarshal action YAML %s: %v", path, err)
		}
		return nil
	})
	return err
}

func TestGhWorkflowYml(t *testing.T) {
	err := _testFunc[gh_action_yml.GhAction](t, filepath.Join("github", "action.yml"))
	if err != nil {
		t.Error(err)
	}
}

func TestGhActionYml(t *testing.T) {
	err := _testFunc[gh_workflow_yml.GhWorkflow](t, filepath.Join("github", "workflow.yml"))
	if err != nil {
		t.Error(err)
	}
}
