//go:build windows

package utils

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// getLongPath converts a short path to a long path.
// e.g. "C:\Users\sebas~1\file.txt" to "C:\Users\sebastian\file.txt".
func getLongPath(shortPath string) (string, error) {
	utf16Path, err := windows.UTF16PtrFromString(shortPath)
	if err != nil {
		return "", err
	}

	buffer := make([]uint16, windows.MAX_PATH)
	length, err := windows.GetLongPathName(utf16Path, &buffer[0], uint32(len(buffer)))
	if err != nil {
		return "", err
	}
	if length == 0 {
		return shortPath, nil // Return original path if it couldn't be converted
	}

	return windows.UTF16ToString(buffer[:length]), nil
}

// ConvertToPosixPath converts a Windows file path to a WSL.
// e.g. "C:\Users\user\file.txt" to "/c/Users/user/file.txt".
func ConvertToPosixPath(windowsPath string) string {
	unixPath := filepath.ToSlash(windowsPath)

	if len(unixPath) > 1 && unixPath[1] == ':' {
		driveLetter := strings.ToLower(string(unixPath[0]))
		restOfPath := unixPath[2:]

		wslPath := fmt.Sprintf("/%s%s", driveLetter, restOfPath)
		return wslPath
	}

	// if invalid format then return original path
	return windowsPath
}
