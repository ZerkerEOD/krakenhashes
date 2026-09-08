package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/bodgit/sevenzip"

	"github.com/ZerkerEOD/krakenhashes/agent/pkg/console"
	"github.com/ZerkerEOD/krakenhashes/agent/pkg/debug"
)

/*
 * Serialized, atomic binary extraction.
 *
 * Extraction used to run in place, with no lock, from four different goroutine
 * families: readPump's file-sync handler, the async file-sync handler, the
 * download workers, and the per-task/benchmark ensure* calls. Two of them could
 * write the same file at the same time, and because os.OpenFile was called
 * without O_TRUNC a second writer overwrote from offset zero and left any
 * trailing bytes of a longer previous file behind.
 *
 * The worse half was the readers. Every "is it already extracted?" probe in the
 * agent answered by looking for an executable NAME, and ExtractBinary7z creates
 * hashcat.bin with O_CREATE before copying 200+ MB into it. So a half-written
 * tree looked complete: device detection would exec a zero-byte hashcat -I, and
 * a task could launch a truncated binary.
 *
 * The fix is two things that only work together:
 *
 *   1. One extraction at a time per binary directory (extractLockFor).
 *   2. Extraction into a temp dir, published by rename, with a marker written
 *      LAST. "Extracted" now means "the marker is there", which cannot be true
 *      of a partial tree.
 *
 * Publishing by rename also removes ETXTBSY on Unix for free: new bytes go to a
 * different inode and rename only swaps a directory entry, so a running hashcat
 * keeps executing the old one. That is how package managers replace live
 * binaries, and it is why there is no retry-on-ETXTBSY loop here -- one would
 * stall for the length of a job.
 */

/*
 * extractLocks is package-global rather than a FileSync field, for three
 * independent reasons:
 *
 *   - FileSync really is constructed more than once. main.go retries
 *     NewConnection up to three times, and each Connection lazily builds its
 *     own FileSync; goroutines from an abandoned one are not torn down.
 *   - The tests construct bare &FileSync{} literals that never run NewFileSync,
 *     so a lock initialised in the constructor would be nil for them.
 *   - The resource being protected is a filesystem path, which is process-wide.
 *     Per-instance scope would be wrong even if the first two were not true.
 *
 * Entries are never evicted. This deliberately does NOT copy the shape of
 * registration.go's getFileLock, which drops a mutex from its map after five
 * minutes of no lookups -- INCLUDING while it is held. A 467 MB extraction
 * routinely exceeds that, and the next caller would get a fresh mutex and no
 * mutual exclusion at all. The key space here is one entry per binary ID, a
 * handful for the life of the process, so retaining them costs nothing.
 *
 * Cross-PROCESS exclusion is deliberately out of scope. Two agents sharing a
 * data directory waste work; they cannot corrupt each other, because each
 * extracts into its own PID-stamped temp dir and publishes by rename.
 */
var (
	extractLocksMu sync.Mutex
	extractLocks   = map[string]*sync.Mutex{}
)

func extractLockFor(binaryDir string) *sync.Mutex {
	key := binaryDir
	if abs, err := filepath.Abs(binaryDir); err == nil {
		key = abs
	}
	key = filepath.Clean(key)

	extractLocksMu.Lock()
	defer extractLocksMu.Unlock()
	if m, ok := extractLocks[key]; ok {
		return m
	}
	m := &sync.Mutex{}
	extractLocks[key] = m
	return m
}

// ExtractionMarkerName is written into a binary directory only after every file
// has been published. Its presence is the definition of "extracted".
const ExtractionMarkerName = ".khextracted.json"

const (
	extractTempPrefix = ".khextract-"
	extractOldPrefix  = ".khold-"
)

// ExtractionMarker records which archive produced the tree beside it.
type ExtractionMarker struct {
	Archive        string    `json:"archive"`
	ArchiveSize    int64     `json:"archive_size"`
	ArchiveModTime int64     `json:"archive_modtime"`
	Files          int       `json:"files"`
	CompletedAt    time.Time `json:"completed_at"`
}

// hashcatExecutableNames mirrors the names resolveHashcatBinary probes for, so
// the two cannot disagree about what counts as a usable binary.
func hashcatExecutableNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"hashcat.exe", "hashcat.bin", "hashcat"}
	}
	return []string{"hashcat.bin", "hashcat"}
}

// hasHashcatExecutable reports whether an OS-appropriate hashcat executable is
// present directly in binaryDir.
func hasHashcatExecutable(binaryDir string) bool {
	for _, name := range hashcatExecutableNames() {
		if info, err := os.Stat(filepath.Join(binaryDir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func readMarker(binaryDir string) (*ExtractionMarker, bool) {
	raw, err := os.ReadFile(filepath.Join(binaryDir, ExtractionMarkerName))
	if err != nil {
		return nil, false
	}
	var m ExtractionMarker
	// A truncated or malformed marker must read as ABSENT, never as valid --
	// otherwise a crash during the marker write would certify a partial tree.
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	return &m, true
}

func writeMarkerAtomic(binaryDir string, m ExtractionMarker) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal extraction marker: %w", err)
	}
	final := filepath.Join(binaryDir, ExtractionMarkerName)
	tmp := final + ".tmp"

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return fmt.Errorf("create extraction marker: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write extraction marker: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sync extraction marker: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close extraction marker: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("publish extraction marker: %w", err)
	}
	return nil
}

/*
 * IsBinaryExtracted is the single answer to "can this binary be used?".
 *
 * Every probe in the agent must route through here. Before this existed there
 * were five separate implementations -- in sync, jobs, hardware and the
 * executor -- and all five answered by looking for a file NAME, which a
 * half-written extraction satisfies.
 *
 * The no-archive fallback is the one concession. If an operator has pruned the
 * .7z but kept the tree there is nothing left to verify against, so trusting an
 * executable's presence is the best available answer and is exactly what the
 * old code did. Without it, pruning the archive would silently trigger a fresh
 * 467 MB download.
 */
func IsBinaryExtracted(binaryDir string) bool {
	if !hasHashcatExecutable(binaryDir) {
		return false
	}
	if _, ok := readMarker(binaryDir); ok {
		return true
	}

	archives, _ := filepath.Glob(filepath.Join(binaryDir, "*.7z"))
	if len(archives) == 0 {
		debug.Warning("Binary directory %s has an executable and no archive to verify against; "+
			"treating it as extracted", binaryDir)
		return true
	}
	return false
}

// archiveEntries returns the files an archive would produce, with the same
// common-prefix stripping the extractor applies, so verification and extraction
// cannot disagree about where a file lands.
type archiveEntry struct {
	relPath string
	size    int64
	mode    os.FileMode
}

func readArchiveEntries(archivePath string) ([]archiveEntry, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat archive: %w", err)
	}

	// Header parse only -- this does not decompress the payload, which is what
	// makes verifying a 467 MB tree cheap enough to do on every call.
	sz, err := sevenzip.NewReader(f, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}

	commonPrefix, hasCommonPrefix := archiveCommonPrefix(sz)

	out := make([]archiveEntry, 0, len(sz.File))
	for _, file := range sz.File {
		if file.FileInfo().IsDir() {
			continue
		}
		out = append(out, archiveEntry{
			relPath: archiveRelPath(file.Name, commonPrefix, hasCommonPrefix),
			size:    file.FileInfo().Size(),
			mode:    file.Mode(),
		})
	}
	return out, nil
}

// archiveRelPath applies the extractor's prefix-stripping rule to one name.
func archiveRelPath(name, commonPrefix string, hasCommonPrefix bool) string {
	if !hasCommonPrefix {
		return filepath.FromSlash(name)
	}
	normalized := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(normalized, commonPrefix+"/") {
		normalized = normalized[len(commonPrefix)+1:]
	}
	return filepath.FromSlash(normalized)
}

/*
 * verifyTreeAgainstArchive is the adoption path, and it is what stops this
 * change forcing every existing agent to re-extract ~467 MB.
 *
 * An agent upgrading into this code has a complete tree and no marker. Rather
 * than re-extracting, every archive entry is stat'd against its recorded size.
 * All present at the right size means the tree is genuinely complete, so the
 * marker is written and nothing is extracted.
 *
 * Sizes, not just names, because a partial tree left by a crash under the old
 * code is otherwise indistinguishable from a complete one -- a truncated
 * hashcat.bin has the right name.
 */
func verifyTreeAgainstArchive(binaryDir string, entries []archiveEntry) bool {
	for _, e := range entries {
		info, err := os.Stat(filepath.Join(binaryDir, e.relPath))
		if err != nil || info.IsDir() || info.Size() != e.size {
			return false
		}
	}
	return len(entries) > 0
}

// markerMatchesArchive reports whether the recorded marker describes this exact
// archive file. Compared on name, size and mtime -- all stat-only, so the hot
// path stays two syscalls rather than hashing hundreds of megabytes.
func markerMatchesArchive(m *ExtractionMarker, archivePath string, fi os.FileInfo) bool {
	return m.Archive == filepath.Base(archivePath) &&
		m.ArchiveSize == fi.Size() &&
		m.ArchiveModTime == fi.ModTime().UnixNano()
}

/*
 * EnsureBinaryExtracted guarantees that binaryDir holds the complete, usable
 * contents of archivePath by the time it returns nil.
 *
 * Safe to call concurrently from any goroutine and cheap when the work is
 * already done, so callers should simply call it rather than trying to decide
 * for themselves whether extraction is needed -- deciding is exactly what the
 * five racy probes used to do.
 */
func (fs *FileSync) EnsureBinaryExtracted(archivePath, binaryDir string) error {
	lk := extractLockFor(binaryDir)
	lk.Lock()
	defer lk.Unlock()

	archiveInfo, err := os.Stat(archivePath)
	if err != nil {
		return fmt.Errorf("archive %s is not readable: %w", archivePath, err)
	}

	// Fast path: a marker that names this exact archive, plus an executable.
	if m, ok := readMarker(binaryDir); ok && markerMatchesArchive(m, archivePath, archiveInfo) {
		if hasHashcatExecutable(binaryDir) {
			return nil
		}
		debug.Warning("Binary directory %s has a current marker but no executable; re-extracting", binaryDir)
	}

	entries, err := readArchiveEntries(archivePath)
	if err != nil {
		return fmt.Errorf("failed to read archive %s: %w", archivePath, err)
	}

	// Adoption: a complete tree from before markers existed, or from an earlier
	// run of this code whose marker write was lost.
	if verifyTreeAgainstArchive(binaryDir, entries) && hasHashcatExecutable(binaryDir) {
		debug.Info("Binary directory %s already matches %s; recording marker without re-extracting",
			binaryDir, filepath.Base(archivePath))
		return writeMarkerAtomic(binaryDir, ExtractionMarker{
			Archive:        filepath.Base(archivePath),
			ArchiveSize:    archiveInfo.Size(),
			ArchiveModTime: archiveInfo.ModTime().UnixNano(),
			Files:          len(entries),
			CompletedAt:    time.Now(),
		})
	}

	// Leftovers from a crashed run. Inert -- nothing reads them -- but each can
	// be over a gigabyte, so sweep before sizing the disk check.
	sweepExtractionLeftovers(binaryDir)

	var needed int64
	for _, e := range entries {
		needed += e.size
	}

	/*
	 * Extracting beside the old tree peaks at archive + old tree + new tree,
	 * roughly five times the archive, while the backend sizes cloud disks at
	 * three (BinaryExtractionMultiplier). Raising that multiplier would bill on
	 * every rental, and a Vast.ai disk cannot be grown after creation.
	 *
	 * So when the disk is tight, prune first. That is safe precisely here: the
	 * marker is absent or stale and verification has already failed, so the
	 * existing tree is known-invalid and nothing may treat it as usable.
	 */
	if free, ok := freeDiskSpace(binaryDir); ok && needed > 0 && free < uint64(needed) {
		debug.Warning("Only %d bytes free in %s for a %d byte extraction; removing the previous "+
			"(already invalid) tree before extracting", free, binaryDir, needed)
		pruneExtractedTree(binaryDir)
		if free, ok := freeDiskSpace(binaryDir); ok && free < uint64(needed) {
			return fmt.Errorf("not enough disk space to extract %s: need %d bytes, %d free",
				filepath.Base(archivePath), needed, free)
		}
	}

	tmpDir := filepath.Join(binaryDir,
		fmt.Sprintf("%s%d-%d", extractTempPrefix, os.Getpid(), time.Now().UnixNano()))
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		return fmt.Errorf("failed to create extraction staging directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	console.Status("Extracting binary archive %s...", filepath.Base(archivePath))
	if err := fs.extractTo(archivePath, tmpDir); err != nil {
		return err
	}

	if err := publishExtraction(tmpDir, binaryDir); err != nil {
		return err
	}

	if err := writeMarkerAtomic(binaryDir, ExtractionMarker{
		Archive:        filepath.Base(archivePath),
		ArchiveSize:    archiveInfo.Size(),
		ArchiveModTime: archiveInfo.ModTime().UnixNano(),
		Files:          len(entries),
		CompletedAt:    time.Now(),
	}); err != nil {
		return err
	}

	sweepExtractionLeftovers(binaryDir)
	console.Success("Binary archive %s extracted successfully", filepath.Base(archivePath))
	return nil
}

// sweepExtractionLeftovers removes staging and set-aside directories from
// earlier runs. Best effort: a failure here wastes disk, it does not break
// anything, so it must never fail an extraction.
func sweepExtractionLeftovers(binaryDir string) {
	for _, prefix := range []string{extractTempPrefix, extractOldPrefix} {
		matches, err := filepath.Glob(filepath.Join(binaryDir, prefix+"*"))
		if err != nil {
			continue
		}
		for _, m := range matches {
			if err := os.RemoveAll(m); err != nil {
				debug.Warning("Could not remove stale extraction leftover %s: %v", m, err)
			}
		}
	}
}

// pruneExtractedTree removes everything in binaryDir except archives, staging
// directories and the marker. Only called when the tree has already been shown
// to be invalid.
func pruneExtractedTree(binaryDir string) {
	entries, err := os.ReadDir(binaryDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".7z") ||
			strings.HasPrefix(name, extractTempPrefix) ||
			name == ExtractionMarkerName {
			continue
		}
		if err := os.RemoveAll(filepath.Join(binaryDir, name)); err != nil {
			debug.Warning("Could not prune %s from %s: %v", name, binaryDir, err)
		}
	}
}

/*
 * publishExtraction moves a staged tree into place one top-level entry at a
 * time, each move a single rename.
 *
 * The directory itself cannot be renamed: binaryDir holds the .7z, which must
 * stay (the backend's disk sizing assumes archive and tree coexist, and
 * ScanDirectory reports the archive as the agent's binary inventory -- delete
 * it and every sync re-pushes 467 MB). os.Rename onto a non-empty directory
 * fails with ENOTEMPTY in any case.
 */
func publishExtraction(tmpDir, binaryDir string) error {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("failed to read staged extraction: %w", err)
	}

	stamp := time.Now().UnixNano()
	for _, e := range entries {
		src := filepath.Join(tmpDir, e.Name())
		dst := filepath.Join(binaryDir, e.Name())

		_, statErr := os.Lstat(dst)
		if statErr == nil {
			/*
			 * Something is already there. On Unix os.Rename replaces a file
			 * atomically and a running binary keeps its old inode, so the
			 * straight rename below is enough. Moving the old entry aside
			 * first is for Windows, where a live .exe can be renamed but not
			 * replaced or deleted.
			 */
			aside := filepath.Join(binaryDir,
				fmt.Sprintf("%s%d-%s", extractOldPrefix, stamp, e.Name()))
			if e.IsDir() {
				// Never merge directories: a stale file from a previous build
				// left inside OpenCL/ would be indistinguishable from a
				// current one.
				if err := os.Rename(dst, aside); err != nil {
					return fmt.Errorf("failed to set aside existing %s: %w", dst, err)
				}
			} else if err := os.Rename(dst, aside); err != nil {
				// Non-fatal on Unix: the plain rename below still replaces it.
				debug.Debug("Could not set aside %s (%v); relying on rename to replace it", dst, err)
			}
		}

		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("failed to publish %s: %w", dst, err)
		}
	}

	// Durability of the directory entries themselves, so a crash cannot leave a
	// marker that outlived the files it certifies.
	if runtime.GOOS != "windows" {
		if d, err := os.Open(binaryDir); err == nil {
			_ = d.Sync()
			d.Close()
		}
	}
	return nil
}
