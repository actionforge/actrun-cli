package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DockerConfig controls Docker execution behavior for the runner.
type DockerConfig struct {
	Disabled     bool   // Always run natively, ignore script directives
	DefaultImage string // Force this image for all scripts
}

// ParseDockerImage scans the first 10 lines of a script for a
// "# DOCKER_IMAGE: <image>" directive and returns the image name.
// Returns empty string if no directive is found.
func ParseDockerImage(script string) string {
	scanner := bufio.NewScanner(strings.NewReader(script))
	for i := 0; i < 10 && scanner.Scan(); i++ {
		line := strings.TrimSpace(scanner.Text())
		if after, ok := strings.CutPrefix(line, "#"); ok {
			after = strings.TrimSpace(after)
			if image, ok := strings.CutPrefix(after, "DOCKER_IMAGE:"); ok {
				image = strings.TrimSpace(image)
				if image != "" {
					return image
				}
			}
		}
	}
	return ""
}

// ResolveDockerImage determines which Docker image (if any) should be used.
// Returns empty string for native execution.
func ResolveDockerImage(cfg DockerConfig, script string) string {
	if cfg.Disabled {
		return ""
	}
	if cfg.DefaultImage != "" {
		return cfg.DefaultImage
	}
	return ParseDockerImage(script)
}

const containerWorkDir = "/build"

// parseShebang extracts the interpreter from the script's shebang line.
// Returns "sh" if no shebang is found.
func parseShebang(script string) string {
	if shell, ok := strings.CutPrefix(strings.SplitN(script, "\n", 2)[0], "#!"); ok {
		shell = strings.TrimSpace(shell)
		// Handle "#!/usr/bin/env bash" style
		if after, ok := strings.CutPrefix(shell, "/usr/bin/env "); ok {
			return strings.TrimSpace(after)
		}
		// Handle "#!/bin/bash" style — extract basename
		if i := strings.LastIndex(shell, "/"); i >= 0 {
			shell = shell[i+1:]
		}
		if shell != "" {
			return shell
		}
	}
	return "sh"
}

// buildDockerCommand creates an exec.Cmd that runs the script inside a Docker container.
func buildDockerCommand(ctx context.Context, image string, runID string, tmpDir, scriptRelPath, scriptContent string, env []string) *exec.Cmd {
	containerName := fmt.Sprintf("runner-build-%s", runID)

	args := []string{
		"run", "--rm",
		"--name", containerName,
		"-v", tmpDir + ":" + containerWorkDir,
		"-w", containerWorkDir,
	}

	// Pass environment variables
	for _, e := range env {
		// Remap BUILD_TMPDIR to the container path
		if strings.HasPrefix(e, "BUILD_TMPDIR=") {
			args = append(args, "-e", "BUILD_TMPDIR="+containerWorkDir)
			continue
		}
		// Skip host-specific vars that don't make sense in container
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "PATH=") ||
			strings.HasPrefix(e, "TMPDIR=") || strings.HasPrefix(e, "SHELL=") {
			continue
		}
		args = append(args, "-e", e)
	}

	shell := parseShebang(scriptContent)
	args = append(args, "--entrypoint", shell, image, containerWorkDir+"/"+scriptRelPath)

	return exec.CommandContext(ctx, "docker", args...)
}

// buildNativeCommand creates an exec.Cmd that runs the script directly on the host.
func buildNativeCommand(ctx context.Context, workDir, scriptPath string, env []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	cmd.Dir = workDir
	cmd.Env = env
	return cmd
}

// cleanupDockerContainer does a best-effort removal of the named container.
func cleanupDockerContainer(runID string) {
	containerName := fmt.Sprintf("runner-build-%s", runID)
	cmd := exec.Command("docker", "rm", "-f", containerName)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}
