//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFileExclusive takes a non-blocking exclusive lock that Windows releases
// when the handle closes or the process exits.
func lockFileExclusive(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
}
