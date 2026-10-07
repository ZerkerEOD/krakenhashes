//go:build linux

package jobs

import "syscall"

// Network filesystem magic numbers (statfs f_type) on Linux.
const (
	nfsSuperMagic = 0x6969
	smbSuperMagic = 0x517B     // old smbfs
	cifsMagic     = 0xFF534D42 // cifs / smb1
	smb2Magic     = 0xFE534D42 // smb2/3 (cifs vfs)
)

// IsNetworkFS reports whether path lives on a network filesystem (NFS or
// SMB/CIFS), and ok=true when it could be determined. Used to (a) refuse to run
// the agent binary from a network mount and (b) recognise a network_direct data
// mount. Only NFS/SMB/CIFS are flagged — local FUSE and other exotic types are
// deliberately treated as local to avoid false positives.
func IsNetworkFS(path string) (isNetwork bool, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false, false
	}
	switch int64(st.Type) {
	case nfsSuperMagic, smbSuperMagic, cifsMagic, smb2Magic:
		return true, true
	default:
		return false, true
	}
}
