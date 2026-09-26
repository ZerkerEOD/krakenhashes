package jobs

import (
	"os"
	"path/filepath"
	"testing"
)

const testNetNTLMv1 = "u4-netntlm::kNS:338d08f8e26de93300000000000000000000000000000000:9526fb8c23a90751cdd619b6cea564742e1e4bf33006ba41:cb8086049ec4736c"

// GH #90: hash:plain splitting must use the known hashlist entries, both when
// the hash contains colons and when the plain does.
func TestParseCrackedHash_ColonSplitting(t *testing.T) {
	executor := NewHashcatExecutor(t.TempDir())

	tests := []struct {
		name      string
		line      string
		hashlist  []string
		hashType  int
		wantHash  string
		wantPlain string
		wantNil   bool
	}{
		{
			name:      "NetNTLMv1 hash full of colons",
			line:      testNetNTLMv1 + ":hashcat",
			hashlist:  []string{testNetNTLMv1},
			hashType:  5500,
			wantHash:  testNetNTLMv1,
			wantPlain: "hashcat",
		},
		{
			name:      "NetNTLMv1 with a colon in the password",
			line:      testNetNTLMv1 + ":pass:word",
			hashlist:  []string{testNetNTLMv1},
			hashType:  5500,
			wantHash:  testNetNTLMv1,
			wantPlain: "pass:word",
		},
		{
			name:      "hash:salt mode with colons in the password",
			line:      "5f4dcc3b5aa765d61d8327deb882cf99:NaCl:a:b:c",
			hashlist:  []string{"5f4dcc3b5aa765d61d8327deb882cf99:NaCl"},
			hashType:  10,
			wantHash:  "5f4dcc3b5aa765d61d8327deb882cf99:NaCl",
			wantPlain: "a:b:c",
		},
		{
			name:      "longer entry wins when one entry is a colon-prefix of another",
			line:      "5f4dcc3b5aa765d61d8327deb882cf99:s1:x:pw",
			hashlist:  []string{"5f4dcc3b5aa765d61d8327deb882cf99:s1", "5f4dcc3b5aa765d61d8327deb882cf99:s1:x"},
			hashType:  10,
			wantHash:  "5f4dcc3b5aa765d61d8327deb882cf99:s1:x",
			wantPlain: "pw",
		},
		{
			name:      "autohex plain is passed through for the backend to decode",
			line:      "cb136a448767792bae25563a498a86e6:$HEX[506173733a776f7264]",
			hashlist:  []string{"cb136a448767792bae25563a498a86e6"},
			hashType:  1000,
			wantHash:  "cb136a448767792bae25563a498a86e6",
			wantPlain: "$HEX[506173733a776f7264]",
		},
		{
			name:      "leading and trailing spaces in the password are kept",
			line:      "cb136a448767792bae25563a498a86e6: pass word ",
			hashlist:  []string{"cb136a448767792bae25563a498a86e6"},
			hashType:  1000,
			wantHash:  "cb136a448767792bae25563a498a86e6",
			wantPlain: " pass word ",
		},
		{
			name:      "hash case in output differs from the hashlist",
			line:      "CB136A448767792BAE25563A498A86E6:pw",
			hashlist:  []string{"cb136a448767792bae25563a498a86e6"},
			hashType:  1000,
			wantHash:  "cb136a448767792bae25563a498a86e6",
			wantPlain: "pw",
		},
		{
			name:     "hash not in the hashlist is dropped, not invented",
			line:     "ffffffffffffffffffffffffffffffff:pw",
			hashlist: []string{"cb136a448767792bae25563a498a86e6"},
			hashType: 1000,
			wantNil:  true,
		},
		{
			name:      "no hashlist loaded keeps the last-colon split",
			line:      "cb136a448767792bae25563a498a86e6:pw",
			hashlist:  nil,
			hashType:  1000,
			wantHash:  "cb136a448767792bae25563a498a86e6",
			wantPlain: "pw",
		},
		{
			name:     "no hashlist loaded still rejects short prefixes",
			line:     "short:pw",
			hashlist: nil,
			hashType: 1000,
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executor.parseCrackedHash(tt.line, tt.hashlist, buildHashlistMap(tt.hashlist), tt.hashType)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("got %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("got nil, want hash=%q plain=%q", tt.wantHash, tt.wantPlain)
			}
			if got.Hash != tt.wantHash {
				t.Errorf("hash = %q, want %q", got.Hash, tt.wantHash)
			}
			if got.Plain != tt.wantPlain {
				t.Errorf("plain = %q, want %q", got.Plain, tt.wantPlain)
			}
		})
	}
}

// writeOutfile writes an outfile for taskID in the executor's data directory.
func writeOutfile(t *testing.T, dataDir, taskID, content string) {
	t.Helper()
	dir := filepath.Join(dataDir, "outfile")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, taskID+".txt"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// GH #90: retransmit used to cut every line at its first colon, corrupting any
// hash that contains one. With the task's hashlist remembered it must parse
// exactly like the live path.
func TestRetransmitOutfile_UsesHashlistContext(t *testing.T) {
	dataDir := t.TempDir()
	executor := NewHashcatExecutor(dataDir)
	const taskID = "task-with-context"

	hashlist := []string{testNetNTLMv1, "cb136a448767792bae25563a498a86e6"}
	executor.mutex.Lock()
	executor.rememberParseContextLocked(taskID, &crackParseContext{
		content:  hashlist,
		lookup:   buildHashlistMap(hashlist),
		hashType: 5500,
	})
	executor.mutex.Unlock()

	writeOutfile(t, dataDir, taskID,
		testNetNTLMv1+":pass:word\r\n"+
			"cb136a448767792bae25563a498a86e6:trailing \n"+
			"ffffffffffffffffffffffffffffffff:not-in-list\n")

	cracks, err := executor.RetransmitOutfile(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cracks) != 2 {
		t.Fatalf("got %d cracks (%+v), want 2", len(cracks), cracks)
	}
	if cracks[0].Hash != testNetNTLMv1 || cracks[0].Plain != "pass:word" {
		t.Errorf("crack 0 = %+v, want the full NetNTLMv1 hash with plain pass:word", cracks[0])
	}
	if cracks[1].Plain != "trailing " {
		t.Errorf("crack 1 plain = %q, want %q", cracks[1].Plain, "trailing ")
	}

	// Deleting the outfile forgets the context.
	if err := executor.DeleteOutfile(taskID); err != nil {
		t.Fatal(err)
	}
	executor.mutex.RLock()
	_, kept := executor.parseContexts[taskID]
	executor.mutex.RUnlock()
	if kept {
		t.Error("parse context kept after DeleteOutfile")
	}
}

// After an agent restart there is no context; retransmit keeps the old
// first-colon behaviour rather than failing.
func TestRetransmitOutfile_NoContextFallsBack(t *testing.T) {
	dataDir := t.TempDir()
	executor := NewHashcatExecutor(dataDir)
	writeOutfile(t, dataDir, "orphan", "cb136a448767792bae25563a498a86e6:pw\n")

	cracks, err := executor.RetransmitOutfile("orphan")
	if err != nil {
		t.Fatal(err)
	}
	if len(cracks) != 1 || cracks[0].Hash != "cb136a448767792bae25563a498a86e6" || cracks[0].Plain != "pw" {
		t.Fatalf("got %+v", cracks)
	}
}

// Contexts for finished tasks whose outfile is gone are pruned when a new task
// starts, so a missing delete approval cannot grow memory forever.
func TestRememberParseContext_PrunesStaleEntries(t *testing.T) {
	dataDir := t.TempDir()
	executor := NewHashcatExecutor(dataDir)
	writeOutfile(t, dataDir, "pending", "x:y\n")

	executor.mutex.Lock()
	executor.parseContexts["gone"] = &crackParseContext{}
	executor.parseContexts["pending"] = &crackParseContext{}
	executor.rememberParseContextLocked("new", &crackParseContext{})
	_, gone := executor.parseContexts["gone"]
	_, pending := executor.parseContexts["pending"]
	_, added := executor.parseContexts["new"]
	executor.mutex.Unlock()

	if gone {
		t.Error("context without an outfile should be pruned")
	}
	if !pending || !added {
		t.Error("context with a pending outfile, and the new one, must be kept")
	}
}
