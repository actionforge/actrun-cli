package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type WriteOptions int32

const (
	Normalize_LineEndings WriteOptions = 1
)

func CreateAndWriteTempFile(script, tmpfileName string, opts WriteOptions) (string, error) {
	tmpfile, err := os.CreateTemp("", tmpfileName)
	if err != nil {
		return "", err
	}

	defer func() {
		_ = tmpfile.Close()
	}()

	tmpfilePath := tmpfile.Name()

	if runtime.GOOS == "windows" {
		tmpfilePath, err = getLongPath(tmpfilePath)
		if err != nil {
			return "", err
		}
	}

	if opts&Normalize_LineEndings != 0 {
		script = strings.ReplaceAll(script, "\r\n", "\n")
	}

	_, err = tmpfile.WriteString(script)
	if err != nil {
		return "", err
	}

	return tmpfilePath, nil
}

// SafeJoinPath safely joins path elements and validates the result stays within the base directory.
// It prevents path traversal attacks by cleaning the path and verifying containment.
// Returns the joined path and an error if the path would escape the base directory.
func SafeJoinPath(base string, elem ...string) (string, error) {
	absBase, err := filepath.Abs(filepath.Clean(base))
	if err != nil {
		return "", fmt.Errorf("invalid base path: %w", err)
	}

	allElems := append([]string{absBase}, elem...)
	joined := filepath.Join(allElems...)

	absJoined, err := filepath.Abs(filepath.Clean(joined))
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// check for null bytes
	if strings.ContainsRune(absJoined, 0) {
		return "", fmt.Errorf("path contains null byte")
	}

	rel, err := filepath.Rel(absBase, absJoined)
	if err != nil {
		return "", fmt.Errorf("path validation failed: %w", err)
	}

	// If relative path starts with "..", it escapes the base directory
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("path escapes base directory")
	}

	return absJoined, nil
}

// ValidatePath validates that a path is safe to use.
// It cleans the path and checks for dangerous patterns
func ValidatePath(path string) (string, error) {
	cleaned := filepath.Clean(path)

	if strings.ContainsRune(cleaned, 0) {
		return "", fmt.Errorf("path contains null byte")
	}

	return cleaned, nil
}
