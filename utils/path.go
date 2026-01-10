package utils

import (
	"os"
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
