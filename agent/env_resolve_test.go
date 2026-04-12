package agent

import (
	"testing"
)

func TestResolveTemplate(t *testing.T) {
	tests := []struct {
		name     string
		tmpl     string
		matrix   map[string]string
		want     string
		wantErr  bool
	}{
		{
			"simple substitution",
			"{{os}}-{{arch}}",
			map[string]string{"os": "linux", "arch": "amd64"},
			"linux-amd64",
			false,
		},
		{
			"no placeholders",
			"static-value",
			map[string]string{"os": "linux"},
			"static-value",
			false,
		},
		{
			"unresolved placeholder",
			"{{os}}-{{missing}}",
			map[string]string{"os": "linux"},
			"",
			true,
		},
		{
			"single var",
			"prefix-{{version}}",
			map[string]string{"version": "3.12"},
			"prefix-3.12",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveTemplate(tt.tmpl, tt.matrix)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func FuzzResolveTemplate(f *testing.F) {
	f.Add("{{os}}-{{arch}}", "os", "linux", "arch", "amd64")
	f.Add("static-value", "", "", "", "")
	f.Add("{{a}}", "a", "hello", "", "")
	f.Add("no-placeholders", "x", "y", "", "")
	f.Fuzz(func(t *testing.T, tmpl, k1, v1, k2, v2 string) {
		matrix := make(map[string]string)
		if k1 != "" {
			matrix[k1] = v1
		}
		if k2 != "" {
			matrix[k2] = v2
		}
		// Must not panic
		resolveTemplate(tmpl, matrix)
	})
}

func TestResolveEnvMappings_Template(t *testing.T) {
	mappings := map[string]interface{}{
		"RUNNER_OS": "{{os}}",
		"LABEL":     "{{os}}-{{arch}}",
	}
	matrix := map[string]string{"os": "linux", "arch": "arm64"}

	result, err := resolveEnvMappings(mappings, matrix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["RUNNER_OS"] != "linux" {
		t.Fatalf("RUNNER_OS = %q, want 'linux'", result["RUNNER_OS"])
	}
	if result["LABEL"] != "linux-arm64" {
		t.Fatalf("LABEL = %q, want 'linux-arm64'", result["LABEL"])
	}
}

func TestResolveEnvMappings_ValueMap(t *testing.T) {
	mappings := map[string]interface{}{
		"RUNNER_IMAGE": map[string]interface{}{
			"macos":  "macos-14",
			"linux":  "ubuntu-24.04",
			"windows": "windows-2022",
		},
	}
	matrix := map[string]string{"os": "linux"}

	result, err := resolveEnvMappings(mappings, matrix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["RUNNER_IMAGE"] != "ubuntu-24.04" {
		t.Fatalf("RUNNER_IMAGE = %q, want 'ubuntu-24.04'", result["RUNNER_IMAGE"])
	}
}

func TestResolveEnvMappings_ValueMap_NoMatch(t *testing.T) {
	mappings := map[string]interface{}{
		"IMAGE": map[string]interface{}{
			"macos": "macos-14",
		},
	}
	matrix := map[string]string{"os": "freebsd"}

	_, err := resolveEnvMappings(mappings, matrix)
	if err == nil {
		t.Fatal("expected error for unmatched value map")
	}
}

func TestResolveEnvMappings_NonStringRule(t *testing.T) {
	mappings := map[string]interface{}{
		"COUNT": 42,
	}
	matrix := map[string]string{}

	result, err := resolveEnvMappings(mappings, matrix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["COUNT"] != "42" {
		t.Fatalf("COUNT = %q, want '42'", result["COUNT"])
	}
}
