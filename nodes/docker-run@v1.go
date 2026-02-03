package nodes

import (
	_ "embed"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/actionforge/actrun-cli/core"
	ni "github.com/actionforge/actrun-cli/node_interfaces"
	"github.com/actionforge/actrun-cli/utils"
	"github.com/google/uuid"
)

//go:embed docker-run@v1.yml
var dockerDefinition string

type DockerNode struct {
	core.NodeBaseComponent
	core.Inputs
	core.Outputs
	core.Executions
}

func (n *DockerNode) ExecuteImpl(c *core.ExecutionState, inputId core.InputId, prevError error) error {
	image, err := core.InputValueById[string](c, n, ni.Core_docker_run_v1_Input_image)
	if err != nil {
		return err
	}

	if image == "" {
		return core.CreateErr(c, nil, "image is required")
	}

	args, err := core.InputValueById[[]string](c, n, ni.Core_docker_run_v1_Input_args)
	if err != nil {
		return err
	}

	entrypoint, err := core.InputValueById[[]string](c, n, ni.Core_docker_run_v1_Input_entrypoint)
	if err != nil {
		return err
	}

	workdir, err := core.InputValueById[string](c, n, ni.Core_docker_run_v1_Input_workdir)
	if err != nil {
		return err
	}

	envs, err := core.InputValueById[[]string](c, n, ni.Core_docker_run_v1_Input_env)
	if err != nil {
		return err
	}

	volumes, err := core.InputValueById[[]string](c, n, ni.Core_docker_run_v1_Input_volumes)
	if err != nil {
		return err
	}

	network, err := core.InputValueById[string](c, n, ni.Core_docker_run_v1_Input_network)
	if err != nil {
		return err
	}

	pullPolicy, err := core.InputValueById[string](c, n, ni.Core_docker_run_v1_Input_pull)
	if err != nil {
		return err
	}

	dockerSocket, err := core.InputValueById[bool](c, n, ni.Core_docker_run_v1_Input_docker_socket)
	if err != nil {
		return err
	}

	// Build container environment following GitHub Actions approach:
	// Don't inherit host OS environment wholesale. Instead, build explicitly from:
	// 1. Allowlisted GITHUB_* variables (when in GitHub workflow mode)
	// 2. Hardcoded CI=true, GITHUB_ACTIONS=true
	// 3. User-defined env vars from node inputs
	// 4. Proxy variables (HTTP_PROXY, HTTPS_PROXY, NO_PROXY)
	//
	// References:
	// - GitHubContext.cs allowlist: https://github.com/actions/runner/blob/main/src/Runner.Worker/GitHubContext.cs#L106-L146
	// - DockerCommandManager.cs CI/GITHUB_ACTIONS: https://github.com/actions/runner/blob/main/src/Runner.Worker/Container/DockerCommandManager.cs#L329-L336
	// - ContainerActionHandler.cs HOME: https://github.com/actions/runner/blob/main/src/Runner.Worker/Handlers/ContainerActionHandler.cs#L176
	currentEnvMap := buildDockerEnvironment(c, envs)

	// parse image reference. docker:// prefix means registry, otherwise Dockerfile
	isRegistry, imageRef := parseImageReference(image)

	var containerImage string
	cwd, _ := os.Getwd()

	dockerClient, err := core.NewDockerClient()
	if err != nil {
		return core.CreateErr(c, err, "failed to create Docker client")
	}
	defer dockerClient.Close()

	if isRegistry {
		// pull from registry
		containerImage = imageRef

		shouldPull := false
		switch pullPolicy {
		case "always":
			shouldPull = true
		case "missing":
			exists, err := dockerClient.ImageExists(c.Ctx, containerImage)
			if err != nil {
				return core.CreateErr(c, err, "failed to check if image exists")
			}
			shouldPull = !exists
		case "never":
			shouldPull = false
		}

		if shouldPull {
			err = dockerClient.PullImage(c.Ctx, containerImage)
			if err != nil {
				return core.CreateErr(c, err, "failed to pull Docker image: %s", containerImage)
			}
		}
	} else {
		// build from Dockerfile
		dockerfilePath := imageRef
		if !filepath.IsAbs(dockerfilePath) {
			dockerfilePath = filepath.Join(cwd, dockerfilePath)
		}

		cleanPath, pathErr := utils.ValidatePath(dockerfilePath)
		if pathErr != nil {
			return core.CreateErr(c, pathErr, "invalid Dockerfile path")
		}

		if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
			// check if this looks like an image reference (user forgot docker:// prefix)
			if looksLikeImageReference(imageRef) {
				return core.CreateErr(c, nil, "Dockerfile not found: %s. Did you mean 'docker://%s' to pull from a registry?", cleanPath, imageRef)
			}
			return core.CreateErr(c, nil, "Dockerfile not found: %s", cleanPath)
		}
		dockerfilePath = cleanPath

		var containerIdSuffix string
		if core.IsTestE2eRunning() {
			containerIdSuffix = "e2e"
		} else {
			containerIdSuffix = uuid.New().String()[:8]
		}

		buildTag := fmt.Sprintf("actrun-docker-%s", containerIdSuffix)
		buildContext := filepath.Dir(dockerfilePath)

		err = dockerClient.BuildImage(c.Ctx, dockerfilePath, buildContext, buildTag)
		if err != nil {
			return core.CreateErr(c, err, "failed to build Docker image from %s", dockerfilePath)
		}

		containerImage = buildTag
	}

	// parse volume mounts
	var mountVolumes []core.Volume
	for _, vol := range volumes {
		if vol == "" {
			continue
		}
		v, err := parseVolume(vol)
		if err != nil {
			return core.CreateErr(c, err, "invalid volume format: %s", vol)
		}
		mountVolumes = append(mountVolumes, v)
	}

	// mount docker socket on Linux/macOS if enabled and not already mounted
	// reminder here, windows uses named pipes (//./pipe/docker_engine) not Unix sockets
	// I haven't tried this on Windows yet
	if dockerSocket && runtime.GOOS != "windows" {
		socketAlreadyMounted := false
		for _, v := range mountVolumes {
			if v.TargetVolumePath == "/var/run/docker.sock" {
				socketAlreadyMounted = true
				break
			}
		}
		if !socketAlreadyMounted {
			mountVolumes = append(mountVolumes, core.Volume{
				SourceVolumePath: "/var/run/docker.sock",
				TargetVolumePath: "/var/run/docker.sock",
				ReadOnly:         false,
			})
		}
	}

	// If in GitHub, auto-mount workspace and set default workdir
	// See here https://docs.github.com/en/actions/creating-actions/creating-a-docker-container-action
	containerWorkdir := workdir
	if c.IsGitHubWorkflow {
		// Mount current working directory to /github/workspace
		mountVolumes = append(mountVolumes, core.Volume{
			SourceVolumePath: cwd,
			TargetVolumePath: "/github/workspace",
			ReadOnly:         false,
		})
		// Set default workdir to /github/workspace if not specified
		if containerWorkdir == "" {
			containerWorkdir = "/github/workspace"
		}
	}

	config := core.DockerRunConfig{
		Image:       containerImage,
		Name:        fmt.Sprintf("actrun_docker_%s", uuid.New().String()[:8]),
		Entrypoint:  entrypoint,
		Cmd:         args,
		Env:         currentEnvMap,
		WorkingDir:  containerWorkdir,
		Volumes:     mountVolumes,
		Network:     network,
		AutoRemove:  true,
		AttachStdio: true,
		Labels:      map[string]string{"actrun": "docker-node"},
	}

	exitCode, err := dockerClient.RunContainer(c.Ctx, config)

	setErr := n.SetOutputValue(c, ni.Core_docker_run_v1_Output_exit_code, int(exitCode), core.SetOutputValueOpts{})
	if setErr != nil {
		return setErr
	}

	if err != nil {
		execErr := n.Execute(ni.Core_docker_run_v1_Output_exec_err, c, err)
		if execErr != nil {
			return execErr
		}
		return nil
	}

	if exitCode != 0 {
		execErr := n.Execute(ni.Core_docker_run_v1_Output_exec_err, c, core.CreateErr(c, nil, "docker run failed with exit code %d", exitCode))
		if execErr != nil {
			return execErr
		}
		return nil
	}

	// Handle GITHUB_ENV and GITHUB_OUTPUT for GitHub workflows
	if c.IsGitHubWorkflow {
		ghContextParser := GhContextParser{}
		ghEnvs, ghOutputs, err := ghContextParser.Parse(c, currentEnvMap)
		if err != nil {
			return err
		}

		nextEnvMap := c.GetContextEnvironMapCopy()
		maps.Copy(nextEnvMap, ghEnvs)
		c.SetContextEnvironMap(nextEnvMap)

		for key, value := range ghOutputs {
			err = n.SetOutputValue(c, core.OutputId(key), value, core.SetOutputValueOpts{
				NotExistsIsNoError: true,
				ForceSet:           true,
				StringTypeHint:     true,
			})
			if err != nil {
				return err
			}
		}
	}

	err = n.Execute(ni.Core_docker_run_v1_Output_exec_success, c, nil)
	if err != nil {
		return err
	}

	return nil
}

// looksLikeImageReference checks if a string looks like a Docker image reference
// rather than a Dockerfile path. Used to provide helpful error messages.
func looksLikeImageReference(s string) bool {
	// Image references typically have patterns like:
	// - "nginx" (short name)
	// - "nginx:latest" (with tag)
	// - "library/nginx" (with namespace)
	// - "ghcr.io/owner/image:tag" (full registry path)
	//
	// Dockerfile paths typically have patterns like:
	// - "Dockerfile"
	// - "./Dockerfile"
	// - "./build/Dockerfile.prod"

	// If it starts with "./" or "/" (or ".\" on Windows) it's likely a path
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, ".\\") || strings.HasPrefix(s, "/") {
		return false
	}

	// If it contains "Dockerfile" (case-insensitive), it's likely a path
	if strings.Contains(strings.ToLower(s), "dockerfile") {
		return false
	}

	// If it has a tag separator ":" followed by something that looks like a tag
	// (not a Windows drive letter), it's likely an image
	if idx := strings.LastIndex(s, ":"); idx > 0 {
		tag := s[idx+1:]
		// Tags are typically alphanumeric with dots, dashes, underscores
		if len(tag) > 0 && !strings.ContainsAny(tag, "/\\") {
			return true
		}
	}

	// If it contains a registry path (e.g., "gcr.io/", "ghcr.io/", "docker.io/")
	if strings.Contains(s, ".io/") || strings.Contains(s, ".com/") {
		return true
	}

	// If it's a simple name without path separators and no file extension
	if !strings.ContainsAny(s, "/\\") && !strings.Contains(s, ".") {
		return true
	}

	return false
}

// parseImageReference parses the image input and returns:
//   - isRegistry: true if it's a registry reference (docker:// prefix)
//   - imageRef: the actual image reference or Dockerfile path
//
// Convention (matches GitHub Actions):
//   - docker://alpine:latest      → Pull from registry
//   - docker://ghcr.io/owner/img  → Pull from registry
//   - Dockerfile                  → Build from local file
//   - ./path/to/Dockerfile        → Build from local file
func parseImageReference(image string) (isRegistry bool, imageRef string) {
	if after, ok := strings.CutPrefix(image, "docker://"); ok {
		return true, after
	}
	return false, image
}

// parseVolume parses a volume string in the format "host:container" or "host:container:ro"
func parseVolume(vol string) (core.Volume, error) {
	parts := strings.Split(vol, ":")

	// Handle Windows paths like C:\path
	// Examples:
	//   C:\host:/container       -> ["C", "\host", "/container"]
	//   C:\host:/container:ro    -> ["C", "\host", "/container", "ro"]
	//   C:\host:D:\container     -> ["C", "\host", "D", "\container"]
	//   C:\host:D:\container:ro  -> ["C", "\host", "D", "\container", "ro"]
	if runtime.GOOS == "windows" && len(parts) >= 2 && len(parts[0]) == 1 {
		hostPath := parts[0] + ":" + parts[1]
		remaining := parts[2:]

		if len(remaining) >= 2 && len(remaining[0]) == 1 {
			// container path also has a Windows drive letter
			containerPath := remaining[0] + ":" + remaining[1]
			if len(remaining) >= 3 {
				parts = []string{hostPath, containerPath, remaining[2]}
			} else {
				parts = []string{hostPath, containerPath}
			}
		} else if len(remaining) >= 1 {
			// container path is Unix-style (e.g., /container)
			if len(remaining) >= 2 {
				parts = []string{hostPath, remaining[0], remaining[1]}
			} else {
				parts = []string{hostPath, remaining[0]}
			}
		}
	}

	if len(parts) < 2 {
		return core.Volume{}, fmt.Errorf("volume must be in format 'host:container' or 'host:container:ro'")
	}

	v := core.Volume{
		SourceVolumePath: parts[0],
		TargetVolumePath: parts[1],
		ReadOnly:         false,
	}

	if len(parts) >= 3 && parts[2] == "ro" {
		v.ReadOnly = true
	}

	return v, nil
}

// githubContextAllowlist defines the GitHub context keys that are exposed as GITHUB_* env vars.
// This matches the allowlist in the GitHub Actions runner:
// https://github.com/actions/runner/blob/main/src/Runner.Worker/GitHubContext.cs#L106-L146
var githubContextAllowlist = map[string]bool{
	"action": true, "action_path": true, "action_ref": true, "action_repository": true,
	"actor": true, "actor_id": true, "api_url": true, "base_ref": true,
	"env": true, "event_name": true, "event_path": true, "graphql_url": true,
	"head_ref": true, "job": true, "output": true, "path": true,
	"ref": true, "ref_name": true, "ref_protected": true, "ref_type": true,
	"repository": true, "repository_id": true, "repository_owner": true, "repository_owner_id": true,
	"retention_days": true, "run_attempt": true, "run_id": true, "run_number": true,
	"server_url": true, "sha": true, "state": true, "step_summary": true,
	"triggering_actor": true, "workflow": true, "workflow_ref": true, "workflow_sha": true,
	"workspace": true,
}

// buildDockerEnvironment builds the container environment based on the GitHub Actions impl
// so I just borrow tha, *partially* even for non-gh graphs. See also here:
// - GitHubContext.cs: https://github.com/actions/runner/blob/main/src/Runner.Worker/GitHubContext.cs#L106-L146
// - DockerCommandManager.cs: https://github.com/actions/runner/blob/main/src/Runner.Worker/Container/DockerCommandManager.cs#L329-L336
// - ContainerActionHandler.cs: https://github.com/actions/runner/blob/main/src/Runner.Worker/Handlers/ContainerActionHandler.cs#L176
func buildDockerEnvironment(c *core.ExecutionState, userEnvs []string) map[string]string {
	env := make(map[string]string)
	contextEnv := c.GetContextEnvironMapCopy()

	// Add allowlisted GITHUB_* vars from context
	// https://github.com/actions/runner/blob/main/src/Runner.Worker/GitHubContext.cs#L148-L160
	if c.IsGitHubWorkflow {
		for key := range githubContextAllowlist {
			envKey := "GITHUB_" + strings.ToUpper(key)
			if val, ok := contextEnv[envKey]; ok {
				env[envKey] = val
			}
		}

		// also add RUNNER_* vars if present
		for key, val := range contextEnv {
			if strings.HasPrefix(key, "RUNNER_") {
				env[key] = val
			}
		}
	}

	// add hardcoded variables (always set for Docker containers)
	// https://github.com/actions/runner/blob/main/src/Runner.Worker/Container/DockerCommandManager.cs#L329-L336
	env["GITHUB_ACTIONS"] = "true"
	if _, exists := env["CI"]; !exists {
		env["CI"] = "true"
	}

	// set HOME to container path (GitHub Actions sets this to /github/home)
	// https://github.com/actions/runner/blob/main/src/Runner.Worker/Handlers/ContainerActionHandler.cs#L176
	if c.IsGitHubWorkflow {
		env["HOME"] = "/github/home"
	}

	// 4. add proxy variables if set in host environment
	// https://github.com/actions/runner/blob/main/src/Runner.Worker/Container/ContainerInfo.cs#L105-L130
	proxyVars := []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"}
	for _, proxyVar := range proxyVars {
		if val, ok := contextEnv[proxyVar]; ok && val != "" {
			env[proxyVar] = val
		}
	}

	// 5. add user-defined env vars from node inputs. Highest prio, can override above
	for _, e := range userEnvs {
		if idx := strings.Index(e, "="); idx > 0 {
			env[e[:idx]] = e[idx+1:]
		}
	}

	return env
}

func init() {
	err := core.RegisterNodeFactory(dockerDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &DockerNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
