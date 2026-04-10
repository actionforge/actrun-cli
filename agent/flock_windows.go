//go:build windows

package agent

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock   = 0x00000002
	lockfileFailImmediately = 0x00000001
)

func lockFileExclusive(f *os.File) error {
	var overlapped syscall.Overlapped
	r1, _, err := procLockFileEx.Call(
		uintptr(f.Fd()),
		uintptr(lockfileExclusiveLock|lockfileFailImmediately),
		0,
		1, 0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		return err
	}
	return nil
}

func unlockFile(f *os.File) {
	var overlapped syscall.Overlapped
	procUnlockFileEx.Call(
		uintptr(f.Fd()),
		0,
		1, 0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
}
