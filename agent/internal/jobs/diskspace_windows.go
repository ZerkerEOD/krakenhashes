//go:build windows

package jobs

// availableDiskBytes is a no-op on Windows: the standard library has no
// cross-platform statfs and we avoid pulling extra syscalls into the agent. The
// pre-flight is simply skipped (ok=false); a genuine disk-full condition is
// still caught at run time via the "no space left on device" stderr classifier
// (AGENT_DISK_FULL).
func availableDiskBytes(path string) (uint64, bool) {
	return 0, false
}
