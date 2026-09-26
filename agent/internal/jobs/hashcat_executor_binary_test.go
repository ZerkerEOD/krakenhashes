package jobs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// GH #91: an empty binary path used to resolve to the data directory itself,
// because os.Stat accepts directories, and then fail at exec time.
func TestResolveHashcatBinary_RejectsEmptyPath(t *testing.T) {
	executor := NewHashcatExecutor(t.TempDir())
	for _, p := range []string{"", "   "} {
		got, err := executor.resolveHashcatBinary(p)
		if !errors.Is(err, errNoBinaryAssigned) {
			t.Errorf("resolveHashcatBinary(%q) = %q, %v; want errNoBinaryAssigned", p, got, err)
		}
	}
}

func TestResolveHashcatBinary_RejectsDirectory(t *testing.T) {
	dataDir := t.TempDir()
	executor := NewHashcatExecutor(dataDir)
	if err := os.MkdirAll(filepath.Join(dataDir, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dataDir, "tools"} {
		if got, err := executor.resolveHashcatBinary(p); err == nil {
			t.Errorf("resolveHashcatBinary(%q) resolved a directory to %q", p, got)
		}
	}
}

func TestResolveHashcatBinary_AcceptsExecutableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bit check is Unix-specific")
	}
	dataDir := t.TempDir()
	executor := NewHashcatExecutor(dataDir)
	bin := filepath.Join(dataDir, "hashcat-direct")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := executor.resolveHashcatBinary(bin)
	if err != nil || got != bin {
		t.Fatalf("resolveHashcatBinary(%q) = %q, %v; want the file itself", bin, got, err)
	}

	// Same file without the executable bit is refused.
	if err := os.Chmod(bin, 0644); err != nil {
		t.Fatal(err)
	}
	if got, err := executor.resolveHashcatBinary(bin); err == nil {
		t.Errorf("non-executable file resolved to %q", got)
	}
}
