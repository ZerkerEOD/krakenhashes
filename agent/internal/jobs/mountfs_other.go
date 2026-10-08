//go:build !linux && !darwin

package jobs

// IsNetworkFS cannot be reliably determined without platform-specific syscalls
// on this OS (notably Windows, where a mapped/UNC drive would need
// GetDriveType). Returns ok=false so callers fall back to explicit config: the
// binary-on-mount guard does not block, and network_direct relies on the
// admin-set tier rather than auto-detection. A Windows-native implementation
// (GetDriveTypeW == DRIVE_REMOTE) is a follow-up.
func IsNetworkFS(path string) (isNetwork bool, ok bool) {
	return false, false
}
