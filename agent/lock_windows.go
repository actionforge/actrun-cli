//go:build windows

package agent

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// AcquireInstanceLock uses LockFileEx on Windows to take an exclusive,
// non-blocking lock on a file next to the credential file. If another
// process already holds the lock, ErrAgentAlreadyRunning is returned.
func AcquireInstanceLock(serverURL, agentToken string) (*os.File, error) {
	lockPath, err := instanceLockPath(serverURL, agentToken)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	// LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY
	ol := new(windows.Overlapped)
	err = windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, ol,
	)
	if err != nil {
		f.Close()
		return nil, ErrAgentAlreadyRunning
	}
	return f, nil
}

// ReleaseInstanceLock releases the lock and removes the lockfile.
func ReleaseInstanceLock(f *os.File) {
	if f == nil {
		return
	}
	ol := new(windows.Overlapped)
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
}
