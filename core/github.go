package core

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/actionforge/actrun-cli/utils"
	"github.com/go-git/go-git/v5"
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

func getRunnerOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	default:
		return runtime.GOOS
	}
}

func getRunnerArch() string {
	switch runtime.GOARCH {
	case "arm64", "aarch64":
		return "ARM64"
	case "amd64":
		return "X64"
	default:
		return runtime.GOARCH
	}
}

// Extracts owner/repo from a git remote URL. Supports http and ssh formats.
func parseRepoFromRemoteURL(remoteURL string) (string, error) {
	remoteURL = strings.TrimSpace(remoteURL)

	// handle ssh format
	if strings.HasPrefix(remoteURL, "git@") {
		// git@github.com:user/repo.git -> user/repo
		colonIdx := strings.Index(remoteURL, ":")
		if colonIdx == -1 {
			return "", fmt.Errorf("invalid SSH remote URL format: %s", remoteURL)
		}
		path := remoteURL[colonIdx+1:]
		path = strings.TrimSuffix(path, ".git")
		return path, nil
	}

	// handle https format
	if strings.HasPrefix(remoteURL, "https://") || strings.HasPrefix(remoteURL, "http://") {
		path := remoteURL
		path = strings.TrimPrefix(path, "https://")
		path = strings.TrimPrefix(path, "http://")

		// remove the host, eg github.com
		slashIdx := strings.Index(path, "/")
		if slashIdx == -1 {
			return "", fmt.Errorf("invalid HTTPS remote URL format: %s", remoteURL)
		}
		path = path[slashIdx+1:]
		path = strings.TrimSuffix(path, ".git")
		return path, nil
	}

	return "", fmt.Errorf("unsupported remote URL format: %s", remoteURL)
}

func SetupGitHubActionsEnv(finalEnv map[string]string) error {
	sourceWorkspace := finalEnv["GITHUB_WORKSPACE"]
	if sourceWorkspace == "" {
		return CreateErr(nil, nil, "GITHUB_WORKSPACE environment variable is required").
			SetHint("Set GITHUB_WORKSPACE to the path of a git repository.")
	}

	eventName := finalEnv["GITHUB_EVENT_NAME"]
	if eventName == "" {
		return CreateErr(nil, nil, "GITHUB_EVENT_NAME environment variable is required").
			SetHint("Set GITHUB_EVENT_NAME to the event that triggered the workflow (e.g., push, pull_request).")
	}

	repo, err := git.PlainOpenWithOptions(sourceWorkspace, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return CreateErr(nil, err, "unable to open git repository at GITHUB_WORKSPACE").
			SetHint("Ensure GITHUB_WORKSPACE points to a valid git repository.")
	}

	remote, err := repo.Remote("origin")
	if err != nil {
		return CreateErr(nil, err, "remote \"origin\" not found in git repository").
			SetHint("Add an origin remote with: git remote add origin <url>")
	}

	remoteURLs := remote.Config().URLs
	if len(remoteURLs) == 0 {
		return CreateErr(nil, nil, "remote \"origin\" has no URLs configured").
			SetHint("Set the origin URL with: git remote set-url origin <url>")
	}

	repoName, err := parseRepoFromRemoteURL(remoteURLs[0])
	if err != nil {
		return CreateErr(nil, err, "unable to parse repository from remote URL").
			SetHint("Ensure the origin remote URL is a valid GitHub repository URL.")
	}

	head, err := repo.Head()
	if err != nil {
		return CreateErr(nil, err, "failed to get git HEAD").
			SetHint("Ensure you have at least one commit in the repository.")
	}

	// here we default to main if we are not in a branch
	branch := "main"
	if head.Name().IsBranch() {
		branch = head.Name().Short()
	}

	sha := head.Hash().String()

	// create RUNNER_WORKSPACE with an empty directory for the actual GITHUB_WORKSPACE
	runnerWorkspace, err := os.MkdirTemp("", "actrun-runner-")
	if err != nil {
		return CreateErr(nil, err, "failed to create runner workspace directory").
			SetHint("Check that you have write permissions to the system temp directory.")
	}

	// extract repo name for the workspace dir name
	repoParts := strings.Split(repoName, "/")
	repoBaseName := repoParts[len(repoParts)-1]

	// here create the actual GITHUB_WORKSPACE inside the runner workspace
	githubWorkspace := filepath.Join(runnerWorkspace, repoBaseName)
	if err := os.MkdirAll(githubWorkspace, 0755); err != nil {
		return CreateErr(nil, err, "failed to create github workspace directory").
			SetHint("Check that you have write permissions to the system temp directory.")
	}

	// create temp dir for runner files
	tempDir, err := os.MkdirTemp("", "actrun-")
	if err != nil {
		return CreateErr(nil, err, "failed to create temp directory").
			SetHint("Check that you have write permissions to the system temp directory.")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return CreateErr(nil, err, "failed to get home directory").
			SetHint("Ensure the HOME environment variable is set correctly.")
	}
	toolCacheDir := filepath.Join(homeDir, ".actrun", "tool-cache")

	setIfNotSet := func(key, value string) {
		if finalEnv[key] == "" {
			finalEnv[key] = value
		}
	}

	setIfNotSet("CI", "true")
	setIfNotSet("GITHUB_ACTIONS", "true")
	setIfNotSet("GITHUB_REPOSITORY", repoName)
	setIfNotSet("GITHUB_REF", "refs/heads/"+branch)
	setIfNotSet("GITHUB_REF_NAME", branch)
	setIfNotSet("GITHUB_SHA", sha)
	setIfNotSet("RUNNER_OS", getRunnerOS())
	setIfNotSet("RUNNER_ARCH", getRunnerArch())
	setIfNotSet("RUNNER_TOOL_CACHE", toolCacheDir)
	setIfNotSet("GITHUB_OUTPUT", filepath.Join(tempDir, "output"))
	setIfNotSet("GITHUB_ENV", filepath.Join(tempDir, "env"))
	setIfNotSet("GITHUB_PATH", filepath.Join(tempDir, "path"))
	setIfNotSet("GITHUB_STATE", filepath.Join(tempDir, "state"))
	setIfNotSet("GITHUB_STEP_SUMMARY", filepath.Join(tempDir, "summary"))
	setIfNotSet("RUNNER_TEMP", tempDir)

	// override a few envs here no matter if they were set or not
	finalEnv["GITHUB_WORKSPACE"] = githubWorkspace
	finalEnv["RUNNER_WORKSPACE"] = runnerWorkspace

	err = os.MkdirAll(toolCacheDir, 0755)
	if err != nil {
		return CreateErr(nil, err, "failed to create tool cache directory").
			SetHint("Check that you have write permissions to %s.", toolCacheDir)
	}

	fileCommandFiles := []string{
		finalEnv["GITHUB_OUTPUT"],
		finalEnv["GITHUB_ENV"],
		finalEnv["GITHUB_PATH"],
		finalEnv["GITHUB_STATE"],
		finalEnv["GITHUB_STEP_SUMMARY"],
	}

	for _, filePath := range fileCommandFiles {
		if filePath != "" {
			f, err := os.Create(filePath)
			if err != nil {
				return CreateErr(nil, err, "failed to create file command file %s", filePath).
					SetHint("Check that you have write permissions to the runner temp directory.")
			}
			f.Close()
		}
	}

	return nil
}
