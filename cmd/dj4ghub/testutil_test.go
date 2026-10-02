package main

import (
	"os"
	"runtime"
)

// privateFileMode reports whether a file is private to the user. Windows has
// no POSIX mode bits; files under the profile inherit a user-only ACL.
func privateFileMode(info os.FileInfo) bool {
	return runtime.GOOS == "windows" || info.Mode().Perm() == 0600
}
