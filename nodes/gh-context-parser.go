package nodes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/actionforge/actrun-cli/core"

	"github.com/google/uuid"
)

type GhContextParser struct {
}

func (p *GhContextParser) Init(c *core.ExecutionState, sysRunnerTempDir string) (map[string]string, error) {
	envs := map[string]string{}
	fileCommandUuid := uuid.New()

	for fileCommand, envName := range contextEnvList {
		fname := fmt.Sprintf("%s_%s", fileCommand, fileCommandUuid)
		path := filepath.Join(sysRunnerTempDir, "_runner_file_commands")
		err := os.MkdirAll(path, 0755)
		if err != nil {
			return nil, core.CreateErr(c, err, "unable to create directory")
		}

		path = filepath.Join(sysRunnerTempDir, "_runner_file_commands", fname)
		err = os.WriteFile(path, []byte(""), 0644)
		if err != nil {
			return nil, core.CreateErr(c, err, "unable to create file")
		}
		envs[envName] = path
	}
	return envs, nil
}

func (p *GhContextParser) Parse(c *core.ExecutionState, contextEnvironMap map[string]string) (map[string]string, error) {

	envs := map[string]string{}

	githubPath := contextEnvironMap["GITHUB_PATH"]
	// load all paths from the github path file and append them to the PATH
	if githubPath != "" {
		p, err := os.ReadFile(githubPath)
		if err != nil {
			return nil, core.CreateErr(c, err, "unable to read file set in GITHUB_PATH")
		}

		newPaths := []string{}

		lines := strings.SplitSeq(string(p), "\n")
		for line := range lines {
			line = strings.TrimRight(line, " \t\n\r")
			if line == "" {
				continue
			}
			newPaths = append(newPaths, line)
		}

		if len(newPaths) > 0 {
			envs["PATH"] = strings.Join(newPaths, string(os.PathListSeparator)) + string(os.PathListSeparator) + contextEnvironMap["PATH"]
		}

		err = os.Remove(githubPath)
		if err != nil {
			return nil, core.CreateErr(c, nil, "unable to remove file set in GITHUB_PATH")
		}

		delete(contextEnvironMap, "GITHUB_PATH")
	}

	githubEnv := contextEnvironMap["GITHUB_ENV"]
	if githubEnv != "" {
		b, err := os.ReadFile(githubEnv)
		if err != nil {
			return nil, core.CreateErr(c, nil, "unable to read file set in GITHUB_ENV")
		}
		ghEnvs, err := parseOutputFile(string(b))
		if err != nil {
			return nil, err
		}
		for envName, envValue := range ghEnvs {
			envs[envName] = strings.TrimRight(envValue, " \t\n\r")
		}

		err = os.Remove(githubEnv)
		if err != nil {
			return nil, core.CreateErr(c, err, "unable to remove file set in GITHUB_ENV")
		}

		delete(contextEnvironMap, "GITHUB_ENV")
	}
	return envs, nil
}

func parseOutputFile(input string) (map[string]string, error) {
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
				return nil, core.CreateErr(nil, nil, "invalid format '%s'. Name must not be empty", line)
			}
			key, value = parts[0], parts[1]
		} else if heredocIndex >= 0 && (equalsIndex < 0 || heredocIndex < equalsIndex) {
			// Heredoc style: NAME<<EOF
			parts := strings.SplitN(line, "<<", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return nil, core.CreateErr(nil, nil, "invalid format '%s'. Name must not be empty", line)
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
				return nil, core.CreateErr(nil, nil, "invalid value. Matching delimiter not found '%s'", delimiter)
			}
			value = heredocValue.String()
		} else {
			return nil, core.CreateErr(nil, nil, "invalid format '%s'", line)
		}

		results[key] = value
	}

	return results, nil
}

// https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions#environment-files
var contextEnvList = map[string]string{
	"add_path":     "GITHUB_PATH",
	"save_state":   "GITHUB_STATE",
	"set_env":      "GITHUB_ENV",
	"step_summary": "GITHUB_STEP_SUMMARY",
	"set_output":   "GITHUB_OUTPUT",
}
