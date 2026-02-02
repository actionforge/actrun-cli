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

	// build env map from context and input env vars.
	// In contrary to Docker, env vars in GitHub are automatically included, see code.
	// See: github.com/actions/runner/blob/main/src/Runner.Worker/Handlers/ContainerActionHandler.cs
	// I just take this behaviour for the entire system.
	// TODO: (Seb) Add an option to override this
	currentEnvMap := c.GetContextEnvironMapCopy()

	// filter out env variables that would break Linux containers when running on Windows:
	// 1. Empty keys or keys starting with '=' - Windows per-drive CWD tracking variables
	//    (eg =C:=, =D:=, =::=) are parsed by strings.Cut as empty-key entries
	// 2. PATH - Windows PATH contains Windows paths that break Linux container commands
	for key := range currentEnvMap {
		if key == "" || strings.HasPrefix(key, "=") || key == "PATH" {
			delete(currentEnvMap, key)
		}
	}
	for _, env := range envs {
		if idx := strings.Index(env, "="); idx > 0 {
			currentEnvMap[env[:idx]] = env[idx+1:]
		}
		// KEY without `=` is a Docker feature, but here a no-op since
		// we pass the full env to Docker anyway
	}

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

		if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
			// check if this looks like an image reference (user forgot docker:// prefix)
			if looksLikeImageReference(imageRef) {
				return core.CreateErr(c, nil, "Dockerfile not found: %s. Did you mean 'docker://%s' to pull from a registry?", dockerfilePath, imageRef)
			}
			return core.CreateErr(c, nil, "Dockerfile not found: %s", dockerfilePath)
		}

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
		ghEnvs, err := ghContextParser.Parse(c, currentEnvMap)
		if err != nil {
			return err
		}

		nextEnvMap := c.GetContextEnvironMapCopy()
		maps.Copy(nextEnvMap, ghEnvs)
		c.SetContextEnvironMap(nextEnvMap)

		// Parse GITHUB_OUTPUT file if it exists
		githubOutput := currentEnvMap["GITHUB_OUTPUT"]
		if githubOutput != "" {
			b, err := os.ReadFile(githubOutput)
			if err != nil {
				return core.CreateErr(c, err, "unable to read github output file")
			}

			outputs, err := parseOutputFile(string(b))
			if err != nil {
				return err
			}
			for key, value := range outputs {
				err = n.SetOutputValue(c, core.OutputId(key), strings.TrimRight(value, "\t\n"), core.SetOutputValueOpts{
					NotExistsIsNoError: true,
				})
				if err != nil {
					return err
				}
			}

			_ = os.Remove(githubOutput)
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

func init() {
	err := core.RegisterNodeFactory(dockerDefinition, func(ctx any, parent core.NodeBaseInterface, parentId string, nodeDef map[string]any, validate bool, opts core.RunOpts) (core.NodeBaseInterface, []error) {
		return &DockerNode{}, nil
	})
	if err != nil {
		panic(err)
	}
}
