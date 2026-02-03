package core

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/actionforge/actrun-cli/utils"
	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type DockerClient struct {
	cli *client.Client
}

func getDockerHost() string {
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		return host
	}

	// On macOS, docker desktop uses a socet in my home folder and not in /var/run/docker.sock.
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err == nil {
			macosSocket := filepath.Join(home, ".docker", "run", "docker.sock")
			if _, err := os.Stat(macosSocket); err == nil {
				return "unix://" + macosSocket
			}
		}
	}

	// On Windows, docker uses the named pipe
	if runtime.GOOS == "windows" {
		return "npipe:////./pipe/docker_engine"
	}

	return "" // whatever Docker decides
}

func NewDockerClient() (*DockerClient, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}

	if host := getDockerHost(); host != "" {
		opts = append(opts, client.WithHost(host))
	} else {
		opts = append(opts, client.FromEnv)
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	return &DockerClient{cli: cli}, nil
}

func (d *DockerClient) Close() error {
	return d.cli.Close()
}

type DockerRunConfig struct {
	Image       string
	Name        string
	Entrypoint  []string
	Cmd         []string
	Env         map[string]string
	WorkingDir  string
	Volumes     []Volume
	Network     string
	AutoRemove  bool
	Labels      map[string]string
	AttachStdio bool
}

func (d *DockerClient) ImageExists(ctx context.Context, imageRef string) (bool, error) {
	_, err := d.cli.ImageInspect(ctx, imageRef)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (d *DockerClient) PullImage(ctx context.Context, imageRef string) error {
	verbose := !IsTestE2eRunning()

	if verbose {
		utils.LogOut.Infof("%sPulling image '%s'\n", utils.LogGhStartGroup, utils.SanitizeImageRef(imageRef))
		defer utils.LogOut.Infof(utils.LogGhEndGroup)
	}

	reader, err := d.cli.ImagePull(ctx, imageRef, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imageRef, err)
	}
	defer reader.Close()

	decoder := json.NewDecoder(reader)
	for {
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		if verbose {
			if status, ok := event["status"].(string); ok {
				if progress, ok := event["progress"].(string); ok {
					utils.LogOut.Infof("  %s %s\n", status, progress)
				} else {
					utils.LogOut.Infof("  %s\n", status)
				}
			}
		}
	}

	return nil
}

func (d *DockerClient) BuildImage(ctx context.Context, dockerfilePath, contextPath, tag string) error {
	verbose := !IsTestE2eRunning()

	if verbose {
		utils.LogOut.Infof("%sBuilding image '%s' from %s\n", utils.LogGhStartGroup, utils.SanitizeImageRef(tag), utils.SanitizeImageRef(dockerfilePath))
		defer utils.LogOut.Infof(utils.LogGhEndGroup)
	}

	// Create a tar archive of the build context
	buildContext, err := createBuildContext(contextPath)
	if err != nil {
		return fmt.Errorf("failed to create build context: %w", err)
	}

	// Get the relative path of the Dockerfile within the context
	dockerfileRelPath, err := filepath.Rel(contextPath, dockerfilePath)
	if err != nil {
		dockerfileRelPath = filepath.Base(dockerfilePath)
	}

	resp, err := d.cli.ImageBuild(ctx, buildContext, build.ImageBuildOptions{
		Dockerfile: dockerfileRelPath,
		Tags:       []string{tag},
		Remove:     true,
	})
	if err != nil {
		return fmt.Errorf("failed to build image: %w", err)
	}
	defer resp.Body.Close()

	// parse and display build output
	decoder := json.NewDecoder(resp.Body)
	for {
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		if verbose {
			if stream, ok := event["stream"].(string); ok {
				utils.LogOut.Infof("%s", stream)
			}
		}
		if errMsg, ok := event["error"].(string); ok {
			return fmt.Errorf("build error: %s", errMsg)
		}
	}

	return nil
}

func (d *DockerClient) RunContainer(ctx context.Context, config DockerRunConfig) (int64, error) {
	var envSlice []string
	for k, v := range config.Env {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, v))
	}

	var mounts []mount.Mount
	for _, v := range config.Volumes {
		m := mount.Mount{
			Type:   mount.TypeBind,
			Source: v.SourceVolumePath,
			Target: v.TargetVolumePath,
		}
		if v.ReadOnly {
			m.ReadOnly = true
		}
		mounts = append(mounts, m)
	}

	containerConfig := &container.Config{
		Image:      config.Image,
		Env:        envSlice,
		WorkingDir: config.WorkingDir,
		Labels:     config.Labels,
		Tty:        false,
	}

	if len(config.Entrypoint) > 0 {
		containerConfig.Entrypoint = config.Entrypoint
	}

	if len(config.Cmd) > 0 {
		containerConfig.Cmd = config.Cmd
	}

	if config.AttachStdio {
		containerConfig.AttachStdout = true
		containerConfig.AttachStderr = true
	}

	hostConfig := &container.HostConfig{
		Mounts:     mounts,
		AutoRemove: config.AutoRemove,
	}

	if config.Network != "" {
		hostConfig.NetworkMode = container.NetworkMode(config.Network)
	}

	networkConfig := &network.NetworkingConfig{}

	resp, err := d.cli.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, config.Name)
	if err != nil {
		return -1, fmt.Errorf("failed to create container: %w", err)
	}

	containerID := resp.ID

	var attachResp types.HijackedResponse
	if config.AttachStdio {
		attachResp, err = d.cli.ContainerAttach(ctx, containerID, container.AttachOptions{
			Stream: true,
			Stdout: true,
			Stderr: true,
		})
		if err != nil {
			return -1, fmt.Errorf("failed to attach to container: %w", err)
		}
		defer attachResp.Close()
	}

	// set up wait BEFORE starting, needed when AutoRemove is true,
	// otherwise the container may be removed before we can wait for it
	// Use WaitConditionRemoved when AutoRemove is true, as the container will be deleted immediately
	waitCondition := container.WaitConditionNotRunning
	if config.AutoRemove {
		waitCondition = container.WaitConditionRemoved
	}
	statusCh, errCh := d.cli.ContainerWait(ctx, containerID, waitCondition)

	if err := d.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return -1, fmt.Errorf("failed to start container: %w", err)
	}

	var outputDone chan struct{}
	if config.AttachStdio {
		outputDone = make(chan struct{})
		go func() {
			_, _ = stdcopy.StdCopy(utils.LogOut.Out, utils.LogErr.Out, attachResp.Reader)
			close(outputDone)
		}()
	}

	// Wait for output to be copied first (this blocks until container finishes)
	if outputDone != nil {
		<-outputDone
	}

	// Now wait for the container to finish and get exit code
	var exitCode int64
	select {
	case err := <-errCh:
		if err != nil {
			return -1, fmt.Errorf("error waiting for container: %w", err)
		}
		// errCh received nil, still need to get exit code from statusCh
		status := <-statusCh
		exitCode = status.StatusCode
	case status := <-statusCh:
		exitCode = status.StatusCode
	}

	return exitCode, nil
}

func (d *DockerClient) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	return d.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: force})
}

// createBuildContext creates a tar archive for the Docker build context
func createBuildContext(contextPath string) (io.Reader, error) {
	pr, pw := io.Pipe()

	go func() {
		tw := tar.NewWriter(pw)
		defer tw.Close()
		defer pw.Close()

		err := filepath.Walk(contextPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			// Get relative path
			relPath, err := filepath.Rel(contextPath, path)
			if err != nil {
				return err
			}

			// Create tar header
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = relPath

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			// Write file content
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(tw, file)
			return err
		})

		if err != nil {
			pw.CloseWithError(err)
		}
	}()

	return pr, nil
}

// Helper function to parse command string into args
func ParseCommandArgs(cmdStr string) []string {
	if cmdStr == "" {
		return nil
	}

	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range cmdStr {
		switch {
		case r == '"' || r == '\'':
			if !inQuote {
				inQuote = true
				quoteChar = r
			} else if r == quoteChar {
				inQuote = false
				quoteChar = 0
			} else {
				current.WriteRune(r)
			}
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}

// StreamLogs streams container logs to stdout/stderr
func (d *DockerClient) StreamLogs(ctx context.Context, containerID string) error {
	out, err := d.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = stdcopy.StdCopy(utils.LogOut.Out, utils.LogErr.Out, out)
	return err
}

// ContainerInfo2DockerRunConfig converts ContainerInfo to DockerRunConfig for backward compatibility
func ContainerInfo2DockerRunConfig(ci ContainerInfo) DockerRunConfig {
	var entrypoint []string
	if ci.ContainerEntryPoint != "" {
		entrypoint = []string{ci.ContainerEntryPoint}
	}

	var cmd []string
	if ci.ContainerEntryPointArgs != "" {
		cmd = ParseCommandArgs(ci.ContainerEntryPointArgs)
	}

	return DockerRunConfig{
		Image:       ci.ContainerImage,
		Name:        ci.ContainerDisplayName,
		Entrypoint:  entrypoint,
		Cmd:         cmd,
		Env:         ci.ContainerEnvironmentVariables,
		WorkingDir:  ci.ContainerWorkDirectory,
		Volumes:     ci.MountVolumes,
		Network:     ci.ContainerNetwork,
		AutoRemove:  true,
		AttachStdio: true,
		Labels:      map[string]string{"actionforge": "true"},
	}
}

// SDKDockerRun runs a container using the Docker SDK (replacement for CLI-based DockerRun)
func SDKDockerRun(ctx context.Context, config DockerRunConfig) (int64, error) {
	dockerClient, err := NewDockerClient()
	if err != nil {
		return -1, err
	}
	defer dockerClient.Close()

	return dockerClient.RunContainer(ctx, config)
}

// SDKDockerPull pulls an image using the Docker SDK
func SDKDockerPull(ctx context.Context, imageRef string) error {
	dockerClient, err := NewDockerClient()
	if err != nil {
		return err
	}
	defer dockerClient.Close()

	return dockerClient.PullImage(ctx, imageRef)
}

// SDKDockerBuild builds an image using the Docker SDK
func SDKDockerBuild(ctx context.Context, dockerfilePath, contextPath, tag string) error {
	dockerClient, err := NewDockerClient()
	if err != nil {
		return err
	}
	defer dockerClient.Close()

	return dockerClient.BuildImage(ctx, dockerfilePath, contextPath, tag)
}

// SDKDockerImageExists checks if an image exists using the Docker SDK
func SDKDockerImageExists(ctx context.Context, imageRef string) (bool, error) {
	dockerClient, err := NewDockerClient()
	if err != nil {
		return false, err
	}
	defer dockerClient.Close()

	return dockerClient.ImageExists(ctx, imageRef)
}
