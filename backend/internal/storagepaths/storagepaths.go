// Package storagepaths resolves the on-disk roots for the migratable storage
// resources (general wordlists and rules, including the global potfile) based
// on the server's active storage backend.
//
// When the backend is 'local' (the default, and today's behavior) the roots
// are under the local data directory. When an admin has migrated onto a
// network share, the roots move under the compose-mounted share path
// (KH_SHARE_DIR) — WITHOUT a restart: a successful migration flips the backend
// via SetBackend and every subsequent path resolution follows.
//
// Only the SHAREABLE resources move. Per-client and association wordlists,
// hashlists, binaries, charsets and hash uploads always stay local — callers
// for those use LocalWordlistsDir / the data dir directly, never WordlistsRoot.
//
// This is a process-global singleton (mirroring internal/crypto) so the many
// services that only hold a data-dir string don't each need a new dependency
// threaded through their constructors.
package storagepaths

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// ErrStatTimeout is returned by StatWithTimeout when the stat did not complete
// within the bound — a hung or high-latency mount. Callers (HTTP serve handlers)
// treat it as a transient share-unavailable condition (503), never as "file
// deleted", so a blip must not flag the DB row missing.
var ErrStatTimeout = errors.New("stat timed out")

// shareStatTimeout bounds the liveness stat of the share mount so a hung/
// high-latency mount cannot block the health check (and its callers — the
// directory monitor's reconcile and the admin panel) indefinitely. A slow or
// hung mount is reported unhealthy (degraded), which is the correct signal.
const shareStatTimeout = 5 * time.Second

// statDirWithTimeout stats path on a goroutine and gives up after d, returning
// true only if it resolves to a directory within the bound. On a truly hung
// hard mount the inner goroutine stays blocked on the syscall (one leaked per
// timeout); soft mounts return on their own timeo.
func statDirWithTimeout(path string, d time.Duration) bool {
	ch := make(chan bool, 1)
	go func() {
		info, err := os.Stat(path)
		ch <- (err == nil && info.IsDir())
	}()
	select {
	case ok := <-ch:
		return ok
	case <-time.After(d):
		return false
	}
}

// StatWithTimeout stats path on a goroutine and gives up after shareStatTimeout,
// returning ErrStatTimeout rather than blocking the caller — an HTTP request
// goroutine serving a share-backed file — indefinitely on a hung mount. On a
// truly hung hard mount the inner goroutine stays blocked on the syscall (one
// leaked per timeout) until the OS returns; soft mounts return on their own
// timeo. On success it returns the real (os.FileInfo, error) from os.Stat.
func StatWithTimeout(path string) (os.FileInfo, error) {
	type result struct {
		fi  os.FileInfo
		err error
	}
	ch := make(chan result, 1)
	go func() {
		fi, err := os.Stat(path)
		ch <- result{fi, err}
	}()
	select {
	case r := <-ch:
		return r.fi, r.err
	case <-time.After(shareStatTimeout):
		return nil, ErrStatTimeout
	}
}

type resolver struct {
	dataDir  string
	shareDir string       // KH_SHARE_DIR mount target; "" when no share is mounted
	backend  atomic.Value // string: models.StorageBackendLocal | StorageBackendShare
}

var (
	mu     sync.RWMutex
	global *resolver
)

// Initialize sets up the singleton at startup. dataDir is KH_DATA_DIR;
// shareDir is the compose-mounted share path (KH_SHARE_DIR), empty when no
// share is mounted; initialBackend is the value read from the network_shares
// row (defaults to local).
func Initialize(dataDir, shareDir, initialBackend string) {
	r := &resolver{dataDir: dataDir, shareDir: shareDir}
	if initialBackend == "" {
		initialBackend = models.StorageBackendLocal
	}
	// A share backend with no mounted share path is a misconfiguration; fall
	// back to local so the server still serves files (degraded, logged) rather
	// than resolving everything under an empty/for-real-missing path.
	if initialBackend == models.StorageBackendShare && shareDir == "" {
		debug.Error("storagepaths: storage_backend=share but KH_SHARE_DIR is empty; falling back to local. Mount the share and set KH_SHARE_DIR.")
		initialBackend = models.StorageBackendLocal
	}
	r.backend.Store(initialBackend)

	mu.Lock()
	global = r
	mu.Unlock()
	debug.Info("storagepaths initialized: dataDir=%s shareDir=%q backend=%s", dataDir, shareDir, initialBackend)
}

func get() *resolver {
	mu.RLock()
	defer mu.RUnlock()
	return global
}

// Initialized reports whether the resolver has been set up. Components that
// hold a constructor-time fallback directory (e.g. the wordlist/rule managers
// in tests) use this to decide whether to trust the resolver or their own
// fallback.
func Initialized() bool {
	return get() != nil
}

// Backend returns the active storage backend, or local before Initialize.
func Backend() string {
	r := get()
	if r == nil {
		return models.StorageBackendLocal
	}
	if b, ok := r.backend.Load().(string); ok && b != "" {
		return b
	}
	return models.StorageBackendLocal
}

// SetBackend flips the active backend. Called by the migration engine on a
// successful switch and at startup after reading the DB. A switch to 'share'
// with no configured share path is refused (kept local, logged).
func SetBackend(backend string) {
	r := get()
	if r == nil {
		return
	}
	if backend == models.StorageBackendShare && r.shareDir == "" {
		debug.Error("storagepaths: refusing to switch to share backend with no KH_SHARE_DIR mounted; staying local")
		return
	}
	r.backend.Store(backend)
	debug.Info("storagepaths: active backend set to %s", backend)
}

// ShareConfigured reports whether a server-side share mount path is set.
func ShareConfigured() bool {
	r := get()
	return r != nil && r.shareDir != ""
}

// ShareDir returns the configured server-side share mount path, or "".
func ShareDir() string {
	r := get()
	if r == nil {
		return ""
	}
	return r.shareDir
}

// DataDir returns the local data directory root.
func DataDir() string {
	r := get()
	if r == nil {
		return ""
	}
	return r.dataDir
}

// onShare reports whether the shareable resources currently resolve to the
// share (backend is share AND a share path is mounted).
func (r *resolver) onShare() bool {
	if r == nil || r.shareDir == "" {
		return false
	}
	b, _ := r.backend.Load().(string)
	return b == models.StorageBackendShare
}

// WordlistsRoot returns the absolute root for the SHAREABLE wordlist
// categories (general/specialized/targeted/custom, including the global
// potfile at custom/potfile.txt). Under the share when migrated, else local.
// NOT for client or association wordlists — those use LocalWordlistsDir.
func WordlistsRoot() string {
	r := get()
	if r == nil {
		return "wordlists"
	}
	if r.onShare() {
		return filepath.Join(r.shareDir, "wordlists")
	}
	return filepath.Join(r.dataDir, "wordlists")
}

// RulesRoot returns the absolute root for rules. Under the share when
// migrated, else local.
func RulesRoot() string {
	r := get()
	if r == nil {
		return "rules"
	}
	if r.onShare() {
		return filepath.Join(r.shareDir, "rules")
	}
	return filepath.Join(r.dataDir, "rules")
}

// LocalWordlistsDir always returns the LOCAL wordlists root, regardless of
// backend. For resources that never move to the share: per-client wordlists
// (wordlists/clients) and association wordlists (wordlists/association).
func LocalWordlistsDir() string {
	r := get()
	if r == nil {
		return "wordlists"
	}
	return filepath.Join(r.dataDir, "wordlists")
}

// LocalRulesDir always returns the LOCAL rules root, regardless of backend.
func LocalRulesDir() string {
	r := get()
	if r == nil {
		return "rules"
	}
	return filepath.Join(r.dataDir, "rules")
}

// ShareHealthy reports whether the shareable resources are currently
// reachable. When the backend is local it is always true (there is no share to
// depend on). When the backend is share it stats the mount directory: a false
// result means the share is offline, and callers MUST NOT treat a file that
// fails to open as deleted — the file is on a mount that has gone away, not
// gone. This is the reconcile guard that stops the missing-file logic from
// mass-flagging every wordlist/rule the instant a share blips.
func ShareHealthy() bool {
	r := get()
	if r == nil {
		return true
	}
	b, _ := r.backend.Load().(string)
	if b != models.StorageBackendShare || r.shareDir == "" {
		return true
	}
	return statDirWithTimeout(r.shareDir, shareStatTimeout)
}
