//go:build !windows

package jobs

import "syscall"

// availableDiskBytes returns the bytes available to an unprivileged process on
// the filesystem containing path, and ok=true when it could be determined.
// Used by the task disk pre-flight. Unix implementation via statfs.
func availableDiskBytes(path string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	// Bavail = blocks free for unprivileged users; Bsize = block size. Bsize is
	// int64 on Linux and uint32 on Darwin — uint64() normalizes both.
	return st.Bavail * uint64(st.Bsize), true
}
