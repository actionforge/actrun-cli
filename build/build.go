package build

import (
	"fmt"
	"runtime/debug"
)

var (
	// To set version number, build with:
	// $ go build -ldflags "-X actionforge/actrun-cli/build.Version=v1.2.3"
	Version string

	Production string
	License    string
)

func GetBuildSettings() (map[string]string, bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil, false
	}

	settings := map[string]string{}

	for _, s := range bi.Settings {
		settings[s.Key] = s.Value
	}
	return settings, true
}

func IsProVersion() bool {
	return License == "" || License == "pro"
}

func IsProduction() bool {
	return Production == "true"
}

func GetAppVersion() string {
	if Version != "" {
		return Version
	} else {
		return "development build"
	}
}

func GetFulllVersionInfo() string {

	bi, ok := GetBuildSettings()
	if !ok {
		return "invalid build info"
	}

	if Version == "" {
		Version = "unknown"
	}

	// if git status returned no changes
	modified := ""
	if bi["vcs.modified"] == "true" {
		modified = ", workdir modified"
	}

	revision := bi["vcs.revision"]
	if len(revision) > 8 {
		revision = revision[:8]
	}

	var production string
	if IsProduction() {
		production = "prod"
	} else {
		production = "dev"
	}

	return fmt.Sprintf("%s (%s, %s %s, %s, %s%s)", Version, production, bi["GOOS"], bi["GOARCH"], bi["vcs.time"], revision, modified)
}

func GetBuildTime() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.time" {
			return setting.Value
		}
	}
	return ""
}
