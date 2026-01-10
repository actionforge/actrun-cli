//go:build !windows

package utils

func getLongPath(shortPath string) (string, error) {
	return shortPath, nil
}

func ConvertToPosixPath(windowsPath string) string {
	return windowsPath
}
