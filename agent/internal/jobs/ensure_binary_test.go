package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
)

/*
 * ensureBinary is the function whose ABSENCE cost a rented GPU eight minutes of
 * billing and a 24-hour blocklist. Nothing anywhere in the agent fetched the
 * hashcat binary on demand — not the benchmark path, not the task path — so an
 * agent asked to work before a backend-pushed sync happened to deliver it
 * failed outright instead of downloading what it lacked.
 *
 * These tests pin the decisions it makes WITHOUT a network: whether it
 * short-circuits, and whether it refuses to short-circuit when it must not.
 */

func newBinaryTestManager(t *testing.T) (*JobManager, string) {
	t.Helper()
	dataDir := t.TempDir()
	return NewJobManager(&config.Config{DataDirectory: dataDir}, nil, nil), dataDir
}

// TestEnsureBinary_SkipsWhenExecutablePresent: the common case must not touch
// the network. A ~467 MB re-download per task would be its own outage.
func TestEnsureBinary_SkipsWhenExecutablePresent(t *testing.T) {
	jm, dataDir := newBinaryTestManager(t)

	binDir := filepath.Join(dataDir, "binaries", "5")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "hashcat.bin"), []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	// fileSync is nil: reaching the download path would nil-panic or error, so
	// a clean return proves the presence check short-circuited first.
	err := jm.ensureBinary(context.Background(), &JobTaskAssignment{
		BinaryPath: "binaries/5",
		BinaryName: "hashcat-7.1.2+338.7z",
		BinaryMD5:  "deadbeef",
	})
	if err != nil {
		t.Fatalf("expected no-op when the executable is already present, got %v", err)
	}
}

/*
 * TestEnsureBinary_ArchivePresentButUnextractedIsNotReady.
 *
 * Presence is tested by looking for an extracted EXECUTABLE, never for the
 * archive. A half-finished extraction leaves the .7z sitting there, and
 * treating that as "present" reproduces the original production failure with
 * extra steps: hashcat still cannot be resolved and the benchmark still fails.
 */
func TestEnsureBinary_ArchivePresentButUnextractedIsNotReady(t *testing.T) {
	jm, dataDir := newBinaryTestManager(t)

	binDir := filepath.Join(dataDir, "binaries", "5")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "hashcat-7.1.2+338.7z"), []byte("not really 7z"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := jm.ensureBinary(context.Background(), &JobTaskAssignment{
		BinaryPath: "binaries/5",
		BinaryName: "hashcat-7.1.2+338.7z",
		BinaryMD5:  "deadbeef",
	})
	if err == nil {
		t.Fatal("an unextracted archive must NOT count as present — hashcat cannot be resolved from it")
	}
}

/*
 * TestEnsureBinary_NoNameFallsBackToOldBehaviour: an older backend sends
 * binary_path without binary_name. Guessing a filename would download something
 * and still not resolve, so the correct move is the pre-existing
 * present-or-absent behaviour rather than a wrong request.
 */
func TestEnsureBinary_NoNameFallsBackToOldBehaviour(t *testing.T) {
	jm, _ := newBinaryTestManager(t)

	if err := jm.ensureBinary(context.Background(), &JobTaskAssignment{
		BinaryPath: "binaries/5",
	}); err != nil {
		t.Fatalf("missing binary_name must degrade to a no-op, not an error: %v", err)
	}
}

// TestEnsureBinary_NoBinaryPathIsNoOp: benchmarks and tasks without an assigned
// binary (older payloads) must not be blocked by this check.
func TestEnsureBinary_NoBinaryPathIsNoOp(t *testing.T) {
	jm, _ := newBinaryTestManager(t)
	if err := jm.ensureBinary(context.Background(), &JobTaskAssignment{}); err != nil {
		t.Fatalf("empty BinaryPath must be a no-op, got %v", err)
	}
}
