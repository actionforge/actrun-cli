package agent

import (
	"testing"
)

func TestParseDockerImage(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{"with directive", "#!/bin/bash\n# DOCKER_IMAGE: ubuntu:22.04\necho hello", "ubuntu:22.04"},
		{"no directive", "#!/bin/bash\necho hello", ""},
		{"directive on first line", "# DOCKER_IMAGE: alpine:3.18\necho hello", "alpine:3.18"},
		{"extra spaces", "#   DOCKER_IMAGE:   golang:1.21   \necho hello", "golang:1.21"},
		{"past line 10", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n# DOCKER_IMAGE: late:tag", ""},
		{"empty image", "# DOCKER_IMAGE: \necho hello", ""},
		{"comment but not directive", "# This is a comment\necho hello", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseDockerImage(tt.script)
			if got != tt.want {
				t.Fatalf("ParseDockerImage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveDockerImage(t *testing.T) {
	script := "#!/bin/bash\n# DOCKER_IMAGE: ubuntu:22.04\necho hello"

	tests := []struct {
		name   string
		cfg    DockerConfig
		want   string
	}{
		{"disabled", DockerConfig{Disabled: true}, ""},
		{"default image overrides", DockerConfig{DefaultImage: "custom:latest"}, "custom:latest"},
		{"from script", DockerConfig{}, "ubuntu:22.04"},
		{"no directive no default", DockerConfig{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := script
			if tt.name == "no directive no default" {
				s = "#!/bin/bash\necho hello"
			}
			got := ResolveDockerImage(tt.cfg, s)
			if got != tt.want {
				t.Fatalf("ResolveDockerImage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func FuzzParseDockerImage(f *testing.F) {
	f.Add("#!/bin/bash\n# DOCKER_IMAGE: ubuntu:22.04\necho hello")
	f.Add("# DOCKER_IMAGE: alpine:3.18")
	f.Add("#!/bin/bash\necho hello")
	f.Add("")
	f.Fuzz(func(t *testing.T, script string) {
		// Must not panic on any input
		ParseDockerImage(script)
	})
}

func FuzzParseShebang(f *testing.F) {
	f.Add("#!/bin/bash\necho hello")
	f.Add("#!/usr/bin/env python3\necho hello")
	f.Add("echo hello")
	f.Add("")
	f.Fuzz(func(t *testing.T, script string) {
		result := parseShebang(script)
		if result == "" {
			t.Fatal("parseShebang must never return empty string")
		}
	})
}

func TestParseShebang(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{"bash", "#!/bin/bash\necho hello", "bash"},
		{"env bash", "#!/usr/bin/env bash\necho hello", "bash"},
		{"sh", "#!/bin/sh\necho hello", "sh"},
		{"env python3", "#!/usr/bin/env python3\necho hello", "python3"},
		{"no shebang", "echo hello", "sh"},
		{"empty", "", "sh"},
		{"zsh", "#!/usr/bin/zsh\necho hello", "zsh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseShebang(tt.script)
			if got != tt.want {
				t.Fatalf("parseShebang() = %q, want %q", got, tt.want)
			}
		})
	}
}
