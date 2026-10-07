package services

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/migrationgate"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/storagepaths"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// drainReconnectFloor is how long the drain phase waits before it is willing to
// proceed, sized to the agent reconnect exponential-backoff ceiling so a
// briefly-disconnected agent has time to check back in and report/finish its
// task before we lock. The migration proceeds only after this floor AND the
// fleet is idle.
//
// Defaults to 12 minutes. KH_MIGRATION_DRAIN_FLOOR_SECONDS overrides it, mainly
// so local migration testing isn't gated on the full 12-minute wait; set e.g.
// KH_MIGRATION_DRAIN_FLOOR_SECONDS=10. A missing, non-numeric, or <=0 value
// falls back to the 12-minute production default.
func drainReconnectFloor() time.Duration {
	const def = 12 * time.Minute
	if v := os.Getenv("KH_MIGRATION_DRAIN_FLOOR_SECONDS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return def
}

// drainHardCap bounds the total drain wait so a permanently stuck task can't
// hang the migration forever; hitting it fails the migration (leaving the
// server on its original backend, fully operational) rather than locking with
// work still running.
const drainHardCap = 3 * time.Hour

// wordlistDirsExcludedFromShare are wordlist subdirectories that never move to
// the share (per-client and association wordlists are engagement data kept
// local). They are skipped by the migration copy/validate walk.
var wordlistDirsExcludedFromShare = map[string]bool{
	"clients":     true,
	"association": true,
}

// MigrationEngine runs the in-process, maintenance-mode storage migration
// (local <-> network share) as a background goroutine, coordinating with the
// scheduler and upload handlers through migrationgate.
type MigrationEngine struct {
	db      *db.DB
	repo    *repository.NetworkShareRepository
	running atomic.Bool

	// cancel aborts the in-flight migration goroutine. Set under mu when a run
	// starts, cleared when it returns; Cancel() invokes it so an admin can abort
	// a run stuck on a slow/hung share. nil whenever nothing is running.
	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewMigrationEngine creates the engine.
func NewMigrationEngine(database *db.DB, repo *repository.NetworkShareRepository) *MigrationEngine {
	return &MigrationEngine{db: database, repo: repo}
}

// Status returns the live migration progress snapshot.
func (e *MigrationEngine) Status() migrationgate.Progress {
	return migrationgate.Get()
}

// Start validates preconditions and launches a migration in the given
// direction ("to_share" or "to_local"). It returns immediately; progress is
// tracked via Status(). Returns an error if a migration is already running or
// preconditions fail.
func (e *MigrationEngine) Start(ctx context.Context, direction string) error {
	if direction != models.MigrationDirectionToShare && direction != models.MigrationDirectionToLocal {
		return fmt.Errorf("invalid migration direction %q", direction)
	}
	if !storagepaths.ShareConfigured() {
		return fmt.Errorf("no network share is configured (KH_SHARE_DIR is not set)")
	}

	// Require the share to be configured and enabled in the admin panel before
	// migrating either direction, so there is a row to carry migration state
	// and the operator has explicitly turned the feature on.
	ns, err := e.repo.Get(ctx)
	if err != nil {
		return fmt.Errorf("load network share config: %w", err)
	}
	if ns == nil || !ns.Enabled {
		return fmt.Errorf("configure and enable the network share before migrating")
	}

	current := storagepaths.Backend()
	if direction == models.MigrationDirectionToShare && current == models.StorageBackendShare {
		return fmt.Errorf("storage is already on the network share")
	}
	if direction == models.MigrationDirectionToLocal && current == models.StorageBackendLocal {
		return fmt.Errorf("storage is already local")
	}

	// The share mount must be present for either direction (we read from or
	// write to it).
	shareDir := storagepaths.ShareDir()
	if info, err := os.Stat(shareDir); err != nil || !info.IsDir() {
		return fmt.Errorf("network share mount %q is not accessible: %v", shareDir, err)
	}

	if !e.running.CompareAndSwap(false, true) {
		return fmt.Errorf("a migration is already in progress")
	}

	// Derive the migration's context from Background (NOT the request) so it
	// survives the HTTP request that started it, but make it cancelable and stash
	// the cancel func so Cancel() can abort a run wedged on a slow/hung share.
	runCtx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.cancel = cancel
	e.mu.Unlock()

	// Seed the gate immediately so callers/UI see 'draining' before the
	// goroutine ticks.
	now := time.Now().UTC()
	deadline := now.Add(drainReconnectFloor())
	migrationgate.Set(func(p *migrationgate.Progress) {
		*p = migrationgate.Progress{
			Phase:         migrationgate.PhaseDraining,
			Direction:     direction,
			Message:       "Waiting for agents to finish and reconnect before locking storage",
			StartedAt:     &now,
			DrainDeadline: &deadline,
		}
	})
	e.persistState(ctx, direction, models.MigrationStateDraining, &now, nil, nil)

	go e.run(runCtx, direction, now, deadline)
	return nil
}

// Cancel aborts an in-progress migration, if any. The copy and validate loops
// check the context between files and the drain loop selects on it, so the run
// goroutine unwinds through fail() at the next checkpoint — leaving storage on
// its original backend and resuming dispatch/writes (a canceled migration is
// handled exactly like a failed one). Returns false when nothing is running.
// Safe to call repeatedly; context cancellation is idempotent.
func (e *MigrationEngine) Cancel() bool {
	e.mu.Lock()
	cancel := e.cancel
	e.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// clearCancel drops the stored cancel func once the run goroutine exits, so a
// later Cancel() against a finished run is a no-op rather than firing a stale
// (already-canceled or reused) function.
func (e *MigrationEngine) clearCancel() {
	e.mu.Lock()
	e.cancel = nil
	e.mu.Unlock()
}

// run is the migration goroutine. ctx is a cancelable context derived from
// Background (set up in Start) so the run survives the HTTP request that started
// it yet can be aborted by Cancel(); drain/copy/validate all observe it and
// unwind through fail() on cancellation.
func (e *MigrationEngine) run(ctx context.Context, direction string, startedAt, floorDeadline time.Time) {
	defer e.running.Store(false)
	defer e.clearCancel()

	// Phase 1: drain.
	if err := e.drain(ctx, floorDeadline); err != nil {
		e.fail(direction, fmt.Sprintf("drain failed: %v", err))
		return
	}

	// Phase 2: lock + copy.
	migrationgate.Set(func(p *migrationgate.Progress) {
		p.Phase = migrationgate.PhaseMigrating
		p.Message = "Copying wordlists and rules"
		p.DrainDeadline = nil
	})
	e.persistState(ctx, direction, models.MigrationStateMigrating, &startedAt, nil, nil)

	srcRoot, dstRoot := e.roots(direction)
	if err := e.copyAll(ctx, srcRoot, dstRoot); err != nil {
		e.fail(direction, fmt.Sprintf("copy failed: %v", err))
		return
	}

	// Phase 3: validate.
	migrationgate.Set(func(p *migrationgate.Progress) {
		p.Phase = migrationgate.PhaseValidating
		p.Message = "Verifying copied files (MD5)"
	})
	e.persistState(ctx, direction, models.MigrationStateValidating, &startedAt, nil, nil)

	if err := e.validateAll(ctx, srcRoot, dstRoot); err != nil {
		e.fail(direction, fmt.Sprintf("validation failed: %v", err))
		return
	}

	// Phase 4: flip. From here the resolver serves from the target backend.
	target := models.StorageBackendShare
	if direction == models.MigrationDirectionToLocal {
		target = models.StorageBackendLocal
	}
	storagepaths.SetBackend(target)
	finished := time.Now().UTC()
	if err := e.flip(ctx, target, direction, &startedAt, &finished); err != nil {
		// The backend is already flipped in memory; log but still mark
		// completed — the data is validated and the resolver is pointed
		// correctly, a DB write hiccup must not present as a failed migration.
		debug.Error("network share migration: flip DB write failed (backend already switched in memory): %v", err)
	}

	migrationgate.Set(func(p *migrationgate.Progress) {
		p.Phase = migrationgate.PhaseCompleted
		p.Message = fmt.Sprintf("Migration complete — storage is now %s. Local copies were left in place; reclaim disk by clearing them once you've confirmed the share.", target)
	})
	debug.Info("network share migration complete: backend=%s (%s)", target, direction)
}

// roots returns the (source, destination) parent directories for the given
// direction. Each has wordlists/ and rules/ subdirectories.
func (e *MigrationEngine) roots(direction string) (src, dst string) {
	local := storagepaths.DataDir()
	share := storagepaths.ShareDir()
	if direction == models.MigrationDirectionToShare {
		return local, share
	}
	return share, local
}

// drain waits until the fleet is idle (no assigned/running tasks) AND the
// reconnect floor has passed. Dispatch is already paused by migrationgate.
func (e *MigrationEngine) drain(ctx context.Context, floorDeadline time.Time) error {
	hardDeadline := time.Now().Add(drainHardCap)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		busy, err := e.busyTaskCount(ctx)
		if err != nil {
			return err
		}
		migrationgate.Set(func(p *migrationgate.Progress) { p.AgentsBusy = busy })

		if time.Now().After(floorDeadline) && busy == 0 {
			return nil
		}
		if time.Now().After(hardDeadline) {
			return fmt.Errorf("timed out after %s with %d task(s) still running", drainHardCap, busy)
		}
		select {
		case <-ctx.Done():
			// Admin aborted (or the process is shutting down): stop draining and
			// let run() unwind through fail() — storage is untouched this phase.
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// busyTaskCount counts tasks currently occupying an agent.
func (e *MigrationEngine) busyTaskCount(ctx context.Context) (int, error) {
	var n int
	err := e.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM job_tasks
		WHERE status IN ('assigned', 'running', 'processing', 'reconnect_pending')
	`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count busy tasks: %w", err)
	}
	return n, nil
}

// migratableFile is one file to copy/verify, relative to the wordlists/ or
// rules/ subtree.
type migratableFile struct {
	rel  string // path relative to srcRoot (e.g. "wordlists/general/x.txt")
	size int64
}

// enumerate walks the shareable wordlists/rules under root, skipping the
// always-local wordlist subdirs.
func (e *MigrationEngine) enumerate(root string) ([]migratableFile, int64, error) {
	var files []migratableFile
	var total int64
	for _, top := range []string{"wordlists", "rules"} {
		base := filepath.Join(root, top)
		walkErr := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil // subtree not present yet — nothing to copy
				}
				return err
			}
			if info.IsDir() {
				// Skip the always-local wordlist subdirs at the top level.
				if top == "wordlists" {
					rel, _ := filepath.Rel(base, path)
					first := firstSegment(rel)
					if wordlistDirsExcludedFromShare[first] {
						return filepath.SkipDir
					}
				}
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			files = append(files, migratableFile{rel: filepath.ToSlash(rel), size: info.Size()})
			total += info.Size()
			return nil
		})
		if walkErr != nil {
			return nil, 0, fmt.Errorf("walk %s: %w", base, walkErr)
		}
	}
	return files, total, nil
}

// firstSegment returns the first path segment of a slash/OS-separated relative
// path (e.g. "clients/uuid/x" -> "clients").
func firstSegment(rel string) string {
	rel = filepath.ToSlash(rel)
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' {
			return rel[:i]
		}
	}
	return rel
}

// copyAll copies every shareable file from srcRoot to dstRoot, idempotently
// (skips a destination whose MD5 already matches the source, so an interrupted
// migration resumes). Progress is reported to the gate.
func (e *MigrationEngine) copyAll(ctx context.Context, srcRoot, dstRoot string) error {
	files, total, err := e.enumerate(srcRoot)
	if err != nil {
		return err
	}
	migrationgate.Set(func(p *migrationgate.Progress) {
		p.FilesTotal = len(files)
		p.BytesTotal = total
		p.FilesDone = 0
		p.BytesDone = 0
	})

	var doneBytes int64
	for i, f := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		srcPath := filepath.Join(srcRoot, filepath.FromSlash(f.rel))
		dstPath := filepath.Join(dstRoot, filepath.FromSlash(f.rel))

		migrationgate.Set(func(p *migrationgate.Progress) { p.CurrentFile = f.rel })

		// Resume: skip if the destination already matches.
		if same, _ := sameFileMD5(srcPath, dstPath); same {
			doneBytes += f.size
			idx := i + 1
			migrationgate.Set(func(p *migrationgate.Progress) {
				p.FilesDone = idx
				p.BytesDone = doneBytes
			})
			continue
		}

		start := time.Now()
		n, cErr := copyFileAtomic(srcPath, dstPath)
		if cErr != nil {
			return fmt.Errorf("copy %s: %w", f.rel, cErr)
		}
		elapsed := time.Since(start).Seconds()
		doneBytes += n
		idx := i + 1
		var mbps float64
		if elapsed > 0 {
			mbps = float64(n) / (1 << 20) / elapsed
		}
		migrationgate.Set(func(p *migrationgate.Progress) {
			p.FilesDone = idx
			p.BytesDone = doneBytes
			if mbps > 0 {
				p.ThroughputMBps = mbps
			}
		})
	}
	return nil
}

// validateAll re-verifies that every shareable source file has an identical
// MD5 at the destination.
func (e *MigrationEngine) validateAll(ctx context.Context, srcRoot, dstRoot string) error {
	files, _, err := e.enumerate(srcRoot)
	if err != nil {
		return err
	}
	for _, f := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		srcPath := filepath.Join(srcRoot, filepath.FromSlash(f.rel))
		dstPath := filepath.Join(dstRoot, filepath.FromSlash(f.rel))
		same, mErr := sameFileMD5(srcPath, dstPath)
		if mErr != nil {
			return fmt.Errorf("verify %s: %w", f.rel, mErr)
		}
		if !same {
			return fmt.Errorf("checksum mismatch for %s after copy", f.rel)
		}
	}
	return nil
}

// fail records a failed migration: DB + gate go to 'failed', the backend is
// left UNCHANGED (still the source), and dispatch/writes resume because
// 'failed' is not a gated phase.
func (e *MigrationEngine) fail(direction, msg string) {
	debug.Error("network share migration failed (%s): %s", direction, msg)
	migrationgate.Set(func(p *migrationgate.Progress) {
		p.Phase = migrationgate.PhaseFailed
		p.Error = msg
		p.Message = "Migration failed — storage is unchanged and the server is operational"
	})
	finished := time.Now().UTC()
	// Persist on a FRESH context: the run ctx is frequently the reason we are
	// here (an admin Cancel() cancels it), and reusing it would make the DB write
	// that records the 'failed' state fail too, stranding the migration_state row
	// mid-flight even though the in-memory gate is already correct.
	persistCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	e.persistState(persistCtx, direction, models.MigrationStateFailed, nil, &finished, &msg)
}

// persistState writes the migration lifecycle fields onto the singleton row.
func (e *MigrationEngine) persistState(ctx context.Context, direction, state string, startedAt, finishedAt *time.Time, errMsg *string) {
	ns, err := e.repo.Get(ctx)
	if err != nil {
		debug.Warning("network share migration: load config to persist state: %v", err)
		return
	}
	if ns == nil {
		debug.Warning("network share migration: no config row to persist state onto")
		return
	}
	ns.MigrationState = state
	dir := direction
	ns.MigrationDirection = &dir
	if startedAt != nil {
		ns.MigrationStartedAt = startedAt
	}
	if finishedAt != nil {
		ns.MigrationFinishedAt = finishedAt
	}
	ns.MigrationError = errMsg
	if err := e.repo.Save(ctx, ns); err != nil {
		debug.Warning("network share migration: persist state %s: %v", state, err)
	}
}

// flip persists the completed migration: storage_backend switched, state
// completed, finished timestamp.
func (e *MigrationEngine) flip(ctx context.Context, target, direction string, startedAt, finishedAt *time.Time) error {
	ns, err := e.repo.Get(ctx)
	if err != nil {
		return err
	}
	if ns == nil {
		return fmt.Errorf("no config row to flip")
	}
	ns.StorageBackend = target
	ns.MigrationState = models.MigrationStateCompleted
	dir := direction
	ns.MigrationDirection = &dir
	ns.MigrationStartedAt = startedAt
	ns.MigrationFinishedAt = finishedAt
	ns.MigrationError = nil
	return e.repo.Save(ctx, ns)
}

// --- file helpers ---

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// sameFileMD5 reports whether both files exist and have equal MD5. A missing
// destination returns (false, nil) — the normal "needs copying" case.
func sameFileMD5(src, dst string) (bool, error) {
	if _, err := os.Stat(dst); err != nil {
		return false, nil
	}
	srcSum, err := fileMD5(src)
	if err != nil {
		return false, err
	}
	dstSum, err := fileMD5(dst)
	if err != nil {
		return false, err
	}
	return srcSum == dstSum, nil
}

// copyFileAtomic copies src to dst via a temp file + rename, creating parent
// dirs. Returns bytes written.
func copyFileAtomic(src, dst string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return 0, err
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	tmp := dst + ".khmig.tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if err != nil {
		out.Close()
		os.Remove(tmp)
		return 0, err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return 0, err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	return n, nil
}
