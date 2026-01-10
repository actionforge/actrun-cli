package core

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"strings"

	"github.com/actionforge/actrun-cli/utils"
	"github.com/google/shlex"
)

type ContainerInfo struct {
	ContainerDisplayName          string
	ContainerWorkDirectory        string
	ContainerEnvironmentVariables map[string]string
	ContainerEntryPoint           string
	ContainerNetwork              string
	MountVolumes                  []Volume
	ContainerImage                string
	ContainerEntryPointArgs       string
}

type Volume struct {
	SourceVolumePath string
	TargetVolumePath string
	ReadOnly         bool
}

func SplitAtCommas(s string) []string {
	var res []string
	var beg int
	var inString bool

	for i, char := range s {
		switch {
		case char == ',' && !inString:
			res = append(res, s[beg:i])
			beg = i + 1
		case char == '"':
			inString = !inString || (i > 0 && s[i-1] != '\\')
		}
	}

	return append(res, s[beg:])
}

func ExecuteDockerCommand(ctx context.Context, command string, optionsString string, workdir string, stdoutDataReceived chan string, stderrDataReceived chan string) (int, error) {
	args, err := shlex.Split(optionsString)
	if err != nil {
		return 1, err
	}
	cmdArgs := append([]string{command}, args...)

	cmd := exec.Command("docker", cmdArgs...)
	cmd.Stdout = utils.LogOut.Out
	cmd.Stderr = utils.LogErr.Out
	cmd.Dir = workdir
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if ok {
			exitCode = exitError.ExitCode()
		}
	}

	return exitCode, err
}

func CreateEscapedOption(flag, key, value string) string {
	if key == "" {
		return ""
	}
	escapedString := SanitizeOptionKeyValue(key + "=" + value)
	return flag + " " + escapedString
}

func SanitizeOptionKeyValue(value string) string {
	if value == "" {
		return ""
	}

	pair := strings.SplitN(value, "=", 2)
	if len(pair) == 1 {
		return fmt.Sprintf("%q=", pair[0])
	}

	// If the value contains spaces or quotes, wrap it in quotes
	if strings.ContainsAny(value, " \t\"") {
		return fmt.Sprintf("%s=%q", pair[0], strings.ReplaceAll(pair[1], "\"", "\\\""))
	}
	return value
}

func DockerRun(ctx context.Context, label string, container ContainerInfo, workingDirectory string, stdoutDataReceived, stderrDataReceived chan string) (int, error) {
	var dockerOptions []string

	dockerOptions = append(dockerOptions,
		fmt.Sprintf("--name %s", container.ContainerDisplayName),
		fmt.Sprintf("--label %s", "actionforge"),
		fmt.Sprintf("--workdir %s", container.ContainerWorkDirectory),
		"--rm",
	)

	for key, value := range container.ContainerEnvironmentVariables {
		dockerOptions = append(dockerOptions, CreateEscapedOption("-e", key, value))
	}

	dockerOptions = append(dockerOptions, "-e GITHUB_ACTIONS=true")

	if _, exists := container.ContainerEnvironmentVariables["CI"]; !exists {
		dockerOptions = append(dockerOptions, "-e CI=true")
	}

	if container.ContainerEntryPoint != "" {
		dockerOptions = append(dockerOptions, fmt.Sprintf("--entrypoint \"%s\"", container.ContainerEntryPoint))
	}

	if container.ContainerNetwork != "" {
		dockerOptions = append(dockerOptions, fmt.Sprintf("--network %s", container.ContainerNetwork))
	}

	for _, volume := range container.MountVolumes {
		mountArg := formatMountArg(volume)
		dockerOptions = append(dockerOptions, mountArg)
	}

	dockerOptions = append(dockerOptions, container.ContainerImage)
	dockerOptions = append(dockerOptions, container.ContainerEntryPointArgs)

	optionsString := strings.Join(dockerOptions, " ")
	return ExecuteDockerCommand(ctx, "run", optionsString, workingDirectory, stdoutDataReceived, stderrDataReceived)
}

func formatMountArg(volume Volume) string {
	var volumeArg string
	if volume.SourceVolumePath == "" {
		volumeArg = fmt.Sprintf("-v \"%s\"", escapePath(volume.TargetVolumePath))
	} else {
		volumeArg = fmt.Sprintf("-v \"%s\":\"%s\"", escapePath(volume.SourceVolumePath), escapePath(volume.TargetVolumePath))
	}
	if volume.ReadOnly {
		volumeArg += ":ro"
	}
	return volumeArg
}

func escapePath(path string) string {
	return strings.ReplaceAll(path, "\"", "\\\"")
}

func DockerPull(ctx context.Context, image string, workingDirectory string) (int, error) {

	utils.LogOut.Infof("%sPull down action image '%s'.\n",
		utils.LogGhStartGroup,
		image,
	)

	defer utils.LogOut.Infof(utils.LogGhEndGroup)

	return ExecuteDockerCommand(ctx, "pull", image, workingDirectory, nil, nil)
}

func DockerBuild(ctx context.Context, workingDirectory string, dockerFile string, dockerContext string, tag string) (int, error) {
	buildOptions := fmt.Sprintf("-t %s -f \"%s\" \"%s\"", tag, dockerFile, dockerContext)
	return ExecuteDockerCommand(ctx, "build", buildOptions, workingDirectory, nil, nil)
}

func LoadGitHubContext(env map[string]string, inputs map[string]any, secrets map[string]string) (map[string]any, error) {
	gh := make(map[string]any)

	mapping := map[string]string{
		"action":           "GITHUB_ACTION",
		"actor":            "GITHUB_ACTOR",
		"actor_id":         "GITHUB_ACTOR_ID",
		"api_url":          "GITHUB_API_URL",
		"base_ref":         "GITHUB_BASE_REF",
		"event_name":       "GITHUB_EVENT_NAME",
		"event_path":       "GITHUB_EVENT_PATH",
		"graphql_url":      "GITHUB_GRAPHQL_URL",
		"head_ref":         "GITHUB_HEAD_REF",
		"job":              "GITHUB_JOB",
		"ref":              "GITHUB_REF",
		"ref_name":         "GITHUB_REF_NAME",
		"ref_protected":    "GITHUB_REF_PROTECTED",
		"ref_type":         "GITHUB_REF_TYPE",
		"repository":       "GITHUB_REPOSITORY",
		"repository_id":    "GITHUB_REPOSITORY_ID",
		"repository_owner": "GITHUB_REPOSITORY_OWNER",
		"run_attempt":      "GITHUB_RUN_ATTEMPT",
		"run_id":           "GITHUB_RUN_ID",
		"run_number":       "GITHUB_RUN_NUMBER",
		"server_url":       "GITHUB_SERVER_URL",
		"sha":              "GITHUB_SHA",
		"workflow":         "GITHUB_WORKFLOW",
		"workflow_ref":     "GITHUB_WORKFLOW_REF",
		"workspace":        "GITHUB_WORKSPACE",
	}

	for ghKey, envKey := range mapping {
		if val, ok := env[envKey]; ok {
			gh[ghKey] = val
		}
	}

	// Support for github.event.pull_request, github.event.commits, etc.
	eventData := make(map[string]any)
	if eventPath, ok := env["GITHUB_EVENT_PATH"]; ok && eventPath != "" {
		fileContent, err := os.ReadFile(eventPath)
		if err == nil {
			_ = json.Unmarshal(fileContent, &eventData)
		}
	}

	// Inputs are part of the event payload usually, but meant to be
	// usually accessed via top-level `inputs` context. However,
	// `github.event.inputs` IS still valid legacy syntax
	if len(inputs) > 0 {
		if _, ok := eventData["inputs"]; !ok {
			eventData["inputs"] = make(map[string]any)
		}

		inputsMap := eventData["inputs"].(map[string]any)
		maps.Copy(inputsMap, inputs)
	}

	gh["event"] = eventData

	if ghToken, ok := secrets["GITHUB_TOKEN"]; ok {
		gh["token"] = ghToken
	}

	return gh, nil
}

func decodeJsonFromEnvValue[T any](envValue string) (map[string]T, error) {
	envMap := map[string]T{}
	if envValue != "" {
		tmp := map[string]T{}
		err := json.NewDecoder(strings.NewReader(envValue)).Decode(&tmp)
		if err != nil {
			return nil, err
		}
		maps.Copy(envMap, tmp)
	}
	return envMap, nil
}
