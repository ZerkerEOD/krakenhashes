//go:build darwin

package jobs

import (
	"strings"
	"syscall"
)

// networkFstypes are the Darwin f_fstypename values that indicate a network
// mount.
var networkFstypes = []string{"nfs", "smbfs", "afpfs", "webdav", "ftp"}

// IsNetworkFS reports whether path lives on a network filesystem, and ok=true
// when it could be determined. Darwin exposes the filesystem type name in
// statfs f_fstypename rather than a magic number.
func IsNetworkFS(path string) (isNetwork bool, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false, false
	}
	// Fstypename is a fixed [16]int8; convert to a Go string up to the NUL.
	buf := make([]byte, 0, len(st.Fstypename))
	for _, c := range st.Fstypename {
		if c == 0 {
			break
		}
		buf = append(buf, byte(c))
	}
	name := strings.ToLower(string(buf))
	for _, nf := range networkFstypes {
		if name == nf {
			return true, true
		}
	}
	return false, true
}
