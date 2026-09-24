package sync

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

/*
 * These are the first tests that actually run the extractor. The one that
 * existed was t.Skip'd for want of a real archive, so every behaviour below --
 * prefix stripping, executable permissions, completion marking -- was
 * unverified in a code path that decides whether the agent execs a binary.
 *
 * testdata/hashcat-fixture.7z is 312 bytes and contains hashcat-test/hashcat.bin,
 * hashcat-test/example.rule and hashcat-test/OpenCL/m00000.cl -- a single common
 * top directory that must be stripped, plus a nested directory.
 */

const fixtureArchive = "testdata/hashcat-fixture.7z"

// stageArchive copies the fixture into a fresh binary directory, the way a
// download would leave it.
func stageArchive(t *testing.T) (binaryDir, archivePath string) {
	t.Helper()
	binaryDir = t.TempDir()
	archivePath = filepath.Join(binaryDir, "hashcat-fixture.7z")

	raw, err := os.ReadFile(fixtureArchive)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(archivePath, raw, 0o640); err != nil {
		t.Fatalf("stage fixture: %v", err)
	}
	return binaryDir, archivePath
}

func TestEnsureBinaryExtracted_ExtractsAndMarks(t *testing.T) {
	fs := &FileSync{}
	binaryDir, archivePath := stageArchive(t)

	if IsBinaryExtracted(binaryDir) {
		t.Fatal("a directory holding only an archive must not report as extracted")
	}

	if err := fs.EnsureBinaryExtracted(archivePath, binaryDir); err != nil {
		t.Fatalf("EnsureBinaryExtracted: %v", err)
	}

	// The common "hashcat-test/" prefix must be stripped, not preserved.
	bin := filepath.Join(binaryDir, "hashcat.bin")
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("hashcat.bin was not published to the top level: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("hashcat.bin mode = %v, want the executable bit set", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(binaryDir, "OpenCL", "m00000.cl")); err != nil {
		t.Errorf("nested OpenCL kernel not extracted: %v", err)
	}

	// The archive must survive: the backend sizes cloud disks assuming archive
	// and tree coexist, and ScanDirectory reports it as the agent's inventory.
	if _, err := os.Stat(archivePath); err != nil {
		t.Errorf("the .7z was removed; every sync would re-push it: %v", err)
	}

	if !IsBinaryExtracted(binaryDir) {
		t.Error("after a successful extraction the directory must report as extracted")
	}
	// No staging or set-aside directories left behind.
	for _, prefix := range []string{extractTempPrefix, extractOldPrefix} {
		if m, _ := filepath.Glob(filepath.Join(binaryDir, prefix+"*")); len(m) > 0 {
			t.Errorf("left %d %s* leftovers behind: %v", len(m), prefix, m)
		}
	}
}

/*
 * A truncated executable is the failure this whole change exists to prevent.
 * The old probes answered on the NAME, which a partially written hashcat.bin
 * satisfies, so the agent would exec it.
 */
func TestIsBinaryExtracted_RejectsTruncatedTree(t *testing.T) {
	fs := &FileSync{}
	binaryDir, archivePath := stageArchive(t)
	if err := fs.EnsureBinaryExtracted(archivePath, binaryDir); err != nil {
		t.Fatalf("initial extraction: %v", err)
	}

	// Simulate a crash mid-copy: right name, wrong contents, marker gone.
	if err := os.WriteFile(filepath.Join(binaryDir, "hashcat.bin"), []byte("x"), 0o755); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := os.Remove(filepath.Join(binaryDir, ExtractionMarkerName)); err != nil {
		t.Fatalf("remove marker: %v", err)
	}

	if IsBinaryExtracted(binaryDir) {
		t.Fatal("a truncated hashcat.bin with no marker reported as extracted; " +
			"this is exactly the state that gets exec'd")
	}

	// And it must repair itself rather than staying broken.
	if err := fs.EnsureBinaryExtracted(archivePath, binaryDir); err != nil {
		t.Fatalf("repair extraction: %v", err)
	}
	if !IsBinaryExtracted(binaryDir) {
		t.Error("re-running EnsureBinaryExtracted did not repair the tree")
	}
	if data, _ := os.ReadFile(filepath.Join(binaryDir, "hashcat.bin")); len(data) <= 1 {
		t.Errorf("hashcat.bin is still truncated (%d bytes)", len(data))
	}
}

/*
 * Adoption. An agent upgrading into this code has a complete tree and no
 * marker; re-extracting every binary would cost ~467 MB of work per agent for
 * nothing. The mtime assertion is the real check -- it proves no file was
 * rewritten.
 */
func TestEnsureBinaryExtracted_AdoptsLegacyTreeWithoutReExtracting(t *testing.T) {
	fs := &FileSync{}
	binaryDir, archivePath := stageArchive(t)
	if err := fs.EnsureBinaryExtracted(archivePath, binaryDir); err != nil {
		t.Fatalf("initial extraction: %v", err)
	}

	bin := filepath.Join(binaryDir, "hashcat.bin")
	before, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	// Back to the pre-marker world: complete tree, no marker.
	if err := os.Remove(filepath.Join(binaryDir, ExtractionMarkerName)); err != nil {
		t.Fatalf("remove marker: %v", err)
	}

	if err := fs.EnsureBinaryExtracted(archivePath, binaryDir); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	after, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("stat after adopt: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("hashcat.bin was rewritten during adoption (mtime %v -> %v); every upgrading "+
			"agent would re-extract its binaries for no reason", before.ModTime(), after.ModTime())
	}
	if !IsBinaryExtracted(binaryDir) {
		t.Error("adoption did not record a marker")
	}
}

// An operator who pruned the .7z but kept the tree must not trigger a fresh
// download. Nothing can be verified without the archive, so the executable's
// presence is the best available answer -- and is what the old code did.
func TestIsBinaryExtracted_NoArchiveFallsBackToExecutable(t *testing.T) {
	fs := &FileSync{}
	binaryDir, archivePath := stageArchive(t)
	if err := fs.EnsureBinaryExtracted(archivePath, binaryDir); err != nil {
		t.Fatalf("initial extraction: %v", err)
	}
	if err := os.Remove(archivePath); err != nil {
		t.Fatalf("remove archive: %v", err)
	}
	if err := os.Remove(filepath.Join(binaryDir, ExtractionMarkerName)); err != nil {
		t.Fatalf("remove marker: %v", err)
	}

	if !IsBinaryExtracted(binaryDir) {
		t.Error("a complete tree whose archive was pruned reported as not extracted; " +
			"the agent would re-download hundreds of megabytes it already has")
	}
}

/*
 * Concurrency. Four goroutine families could previously extract the same
 * directory at once, interleaving writes into the same files.
 */
func TestEnsureBinaryExtracted_ConcurrentCallersProduceOneGoodTree(t *testing.T) {
	fs := &FileSync{}
	binaryDir, archivePath := stageArchive(t)

	const goroutines = 8
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = fs.EnsureBinaryExtracted(archivePath, binaryDir)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	if !IsBinaryExtracted(binaryDir) {
		t.Fatal("concurrent extraction did not leave a usable tree")
	}

	// Every file must be whole. An interleaved write shows up as a short file.
	entries, err := readArchiveEntries(archivePath)
	if err != nil {
		t.Fatalf("read archive entries: %v", err)
	}
	for _, e := range entries {
		info, err := os.Stat(filepath.Join(binaryDir, e.relPath))
		if err != nil {
			t.Errorf("%s missing after concurrent extraction: %v", e.relPath, err)
			continue
		}
		if info.Size() != e.size {
			t.Errorf("%s is %d bytes, want %d — concurrent writers interleaved",
				e.relPath, info.Size(), e.size)
		}
	}
}

// Staging directories from a crashed run must be swept, and must never be
// reported as the agent's executables while they exist.
func TestExtraction_StaleTempIsSweptAndNeverReported(t *testing.T) {
	fs := &FileSync{}
	binaryDir, archivePath := stageArchive(t)

	stale := filepath.Join(binaryDir, extractTempPrefix+"999-1")
	if err := os.MkdirAll(stale, 0o750); err != nil {
		t.Fatalf("create stale temp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stale, "hashcat.bin"), []byte("partial"), 0o755); err != nil {
		t.Fatalf("write stale binary: %v", err)
	}

	// Before anything else: the walker must not report the staged file.
	found, err := fs.FindExtractedExecutables(binaryDir)
	if err != nil {
		t.Fatalf("FindExtractedExecutables: %v", err)
	}
	for _, f := range found {
		if strings.Contains(f, extractTempPrefix) {
			t.Errorf("reported %s from an in-flight extraction as an available executable", f)
		}
	}

	if err := fs.EnsureBinaryExtracted(archivePath, binaryDir); err != nil {
		t.Fatalf("EnsureBinaryExtracted: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale staging directory %s was not swept", stale)
	}
}

// A marker naming a different archive must not certify this one — otherwise
// swapping in a new hashcat build would be silently ignored.
func TestEnsureBinaryExtracted_StaleMarkerIsRejected(t *testing.T) {
	fs := &FileSync{}
	binaryDir, archivePath := stageArchive(t)
	if err := fs.EnsureBinaryExtracted(archivePath, binaryDir); err != nil {
		t.Fatalf("initial extraction: %v", err)
	}

	m, ok := readMarker(binaryDir)
	if !ok {
		t.Fatal("expected a marker")
	}
	if m.Archive != "hashcat-fixture.7z" {
		t.Errorf("marker names %q, want the archive it was extracted from", m.Archive)
	}
	if m.Files == 0 {
		t.Error("marker recorded zero files")
	}

	// A marker for a different archive must not match.
	if markerMatchesArchive(&ExtractionMarker{Archive: "something-else.7z"}, archivePath, mustStat(t, archivePath)) {
		t.Error("a marker naming a different archive was accepted as current")
	}
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info
}
