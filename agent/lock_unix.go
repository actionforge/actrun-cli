//go:build !windows

package agent

import (
	"os"
	"path/filepath"
	"syscall"
)

// AcquireInstanceLock takes an exclusive flock on a file next to the
// credential file. If another process on this machine already holds the
// lock, ErrAgentAlreadyRunning is returned immediately (non-blocking).
// The returned *os.File must be kept open for the lifetime of the process
// to maintain the lock. On process crash the OS releases the flock
// automatically, so no stale lockfiles.
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
	// LOCK_EX | LOCK_NB: exclusive, non-blocking. Fails immediately if held.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, ErrAgentAlreadyRunning
	}
	return f, nil
}

// ReleaseInstanceLock releases the flock and removes the lockfile.
func ReleaseInstanceLock(f *os.File) {
	if f == nil {
		return
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
}
