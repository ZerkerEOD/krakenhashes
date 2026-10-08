package services

import (
	"testing"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/storagepaths"
)

// TestRelPathShareVsDataPrefix locks the fix for the share-path corruption:
// the data dir is a plain STRING prefix of the share dir
// (/var/lib/krakenhashes vs /var/lib/krakenhashes-share), so a naive
// TrimPrefix(dataDir) mangled a share file into "-share/rules/...". relPath must
// match on a path-component boundary and strip the share root for share-backed
// files while still stripping the data dir for always-local (client) files.
func TestRelPathShareVsDataPrefix(t *testing.T) {
	const dataDir = "/var/lib/krakenhashes"
	const shareDir = "/var/lib/krakenhashes-share" // dataDir is a string prefix of this

	// Configure a share root so relPath can recognize share-backed paths.
	storagepaths.Initialize(dataDir, shareDir, "local")
	t.Cleanup(func() { storagepaths.Initialize(dataDir, "", "local") })

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"share rule not mangled to -share", shareDir + "/rules/hashcat/best64.rule", "rules/hashcat/best64.rule"},
		{"share wordlist", shareDir + "/wordlists/general/x.txt", "wordlists/general/x.txt"},
		{"local client wordlist strips data dir", dataDir + "/wordlists/clients/abc/x.txt", "wordlists/clients/abc/x.txt"},
		{"local general wordlist strips data dir", dataDir + "/wordlists/general/y.txt", "wordlists/general/y.txt"},
		{"path equal to share root", shareDir, ""},
		{"unrelated path unchanged", "/opt/other/x.txt", "/opt/other/x.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := relPath(dataDir, c.in); got != c.want {
				t.Fatalf("relPath(%q, %q) = %q; want %q", dataDir, c.in, got, c.want)
			}
		})
	}
}
