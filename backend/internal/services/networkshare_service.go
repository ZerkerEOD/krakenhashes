package services

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/storagepaths"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// NetworkShareService owns the network-share configuration, server-side mount
// validation, and the health check used by the reconcile guard / degraded
// mode. It holds no secrets: the server mounts the share via docker-compose
// (KH_SHARE_DIR), and this service only reads that mount and records
// non-secret connection info for the agent setup command.
type NetworkShareService struct {
	repo *repository.NetworkShareRepository
}

// NewNetworkShareService creates the service.
func NewNetworkShareService(repo *repository.NetworkShareRepository) *NetworkShareService {
	return &NetworkShareService{repo: repo}
}

// NetworkShareConfigInput is the mutable, non-secret share configuration an
// admin can set. Migration state and storage backend are NOT settable here —
// they change only through a migration.
type NetworkShareConfigInput struct {
	ShareType    string            `json:"share_type"`
	Name         string            `json:"name"`
	Enabled      bool              `json:"enabled"`
	ServerHost   string            `json:"server_host"`
	ShareName    string            `json:"share_name"`
	MountOptions map[string]string `json:"mount_options"`
}

// ValidationResult reports the outcome of a server-side mount validation.
type ValidationResult struct {
	OK          bool    `json:"ok"`
	ShareDir    string  `json:"share_dir"`
	Mounted     bool    `json:"mounted"`
	Writable    bool    `json:"writable"`
	FreeBytes   int64   `json:"free_bytes"`
	WriteMBps   float64 `json:"write_mbps"`
	ReadMBps    float64 `json:"read_mbps"`
	Error       string  `json:"error,omitempty"`
	ValidatedAt string  `json:"validated_at"`
}

// Get returns the current share configuration. When no share has ever been
// configured it returns a zero-value local-backend config so the UI has a
// stable shape to render.
func (s *NetworkShareService) Get(ctx context.Context) (*models.NetworkShare, error) {
	ns, err := s.repo.Get(ctx)
	if err != nil {
		return nil, err
	}
	if ns == nil {
		return &models.NetworkShare{
			ShareType:      models.ShareTypeSMB,
			Name:           "Network Share",
			Enabled:        false,
			MountOptions:   map[string]string{},
			StorageBackend: models.StorageBackendLocal,
			MigrationState: models.MigrationStateIdle,
		}, nil
	}
	return ns, nil
}

// UpsertConfig writes the non-secret config fields, preserving storage backend
// and migration state (a config edit must never disturb an active migration or
// silently change which backend is live).
func (s *NetworkShareService) UpsertConfig(ctx context.Context, in NetworkShareConfigInput) (*models.NetworkShare, error) {
	if in.ShareType != models.ShareTypeSMB && in.ShareType != models.ShareTypeNFS {
		return nil, fmt.Errorf("invalid share_type %q (want smb or nfs)", in.ShareType)
	}
	ns, err := s.repo.Get(ctx)
	if err != nil {
		return nil, err
	}
	if ns == nil {
		ns = &models.NetworkShare{
			StorageBackend: models.StorageBackendLocal,
			MigrationState: models.MigrationStateIdle,
		}
	}
	if ns.MigrationInProgress() {
		return nil, fmt.Errorf("cannot edit share configuration while a migration is in progress")
	}
	ns.ShareType = in.ShareType
	ns.Name = in.Name
	ns.Enabled = in.Enabled
	ns.ServerHost = in.ServerHost
	ns.ShareName = in.ShareName
	if in.MountOptions == nil {
		in.MountOptions = map[string]string{}
	}
	ns.MountOptions = in.MountOptions
	if err := s.repo.Save(ctx, ns); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx)
}

// Validate exercises the server-side mount: confirms KH_SHARE_DIR is set and
// mounted, ensures the wordlists/ and rules/ subdirs exist, measures a small
// write+read throughput sample, reports free space, and persists the result.
// It does NOT require the share to be enabled — validating before enabling is
// the normal flow.
func (s *NetworkShareService) Validate(ctx context.Context) (*ValidationResult, error) {
	res := &ValidationResult{
		ShareDir:    storagepaths.ShareDir(),
		ValidatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	shareDir := res.ShareDir
	if shareDir == "" {
		res.Error = "KH_SHARE_DIR is not set — mount the network share in docker-compose and set KH_SHARE_DIR to its container path"
		s.persistValidation(ctx, res)
		return res, nil
	}

	info, err := os.Stat(shareDir)
	if err != nil || !info.IsDir() {
		res.Error = fmt.Sprintf("share mount %q is not accessible: %v", shareDir, err)
		s.persistValidation(ctx, res)
		return res, nil
	}
	res.Mounted = true

	// Ensure the wordlists/ and rules/ subdirs exist on the share (the server
	// is the sole writer, so creating them here is safe and idempotent).
	for _, sub := range []string{"wordlists", "rules"} {
		if mkErr := os.MkdirAll(filepath.Join(shareDir, sub), 0o750); mkErr != nil {
			res.Error = fmt.Sprintf("cannot create %s/ on the share (is it mounted read-only?): %v", sub, mkErr)
			s.persistValidation(ctx, res)
			return res, nil
		}
	}

	// Write + read throughput probe on a small sample (16 MiB), then clean up.
	writeMBps, readMBps, probeErr := probeThroughput(shareDir)
	if probeErr != nil {
		res.Error = fmt.Sprintf("write/read probe failed: %v", probeErr)
		s.persistValidation(ctx, res)
		return res, nil
	}
	res.Writable = true
	res.WriteMBps = writeMBps
	res.ReadMBps = readMBps

	if free, ferr := freeBytes(shareDir); ferr == nil {
		res.FreeBytes = free
	}

	res.OK = true
	s.persistValidation(ctx, res)
	return res, nil
}

// ShareDir returns the server's compose-mounted share path (KH_SHARE_DIR), or ""
// when no share is mounted. This is what the server actually reads from — the
// admin UI shows it read-only so operators see the real mount instead of
// mistaking the agent-command coordinate fields for server-connection config.
func (s *NetworkShareService) ShareDir() string {
	return storagepaths.ShareDir()
}

// Health is the cheap liveness check used by the degraded-mode surface: is the
// share currently reachable? Mirrors storagepaths.ShareHealthy but is scoped to
// the configured share regardless of the active backend, for the admin UI.
func (s *NetworkShareService) Health(ctx context.Context) bool {
	shareDir := storagepaths.ShareDir()
	if shareDir == "" {
		return false
	}
	info, err := os.Stat(shareDir)
	return err == nil && info.IsDir()
}

func (s *NetworkShareService) persistValidation(ctx context.Context, res *ValidationResult) {
	ns, err := s.repo.Get(ctx)
	if err != nil {
		debug.Warning("network share: could not load config to persist validation: %v", err)
		return
	}
	if ns == nil {
		ns = &models.NetworkShare{
			ShareType:      models.ShareTypeSMB,
			Name:           "Network Share",
			MountOptions:   map[string]string{},
			StorageBackend: models.StorageBackendLocal,
			MigrationState: models.MigrationStateIdle,
		}
	}
	now := time.Now().UTC()
	ns.LastValidatedAt = &now
	if res.OK {
		ns.LastValidationError = nil
	} else {
		msg := res.Error
		ns.LastValidationError = &msg
	}
	if err := s.repo.Save(ctx, ns); err != nil {
		debug.Warning("network share: could not persist validation result: %v", err)
	}
}

// probeThroughput writes then reads a 16 MiB sample under shareDir and returns
// the observed write and read throughput in MB/s. The sample is always removed.
func probeThroughput(shareDir string) (writeMBps, readMBps float64, err error) {
	const sampleBytes = 16 << 20 // 16 MiB
	buf := make([]byte, sampleBytes)
	if _, err = rand.Read(buf); err != nil {
		return 0, 0, fmt.Errorf("generate sample: %w", err)
	}
	probePath := filepath.Join(shareDir, fmt.Sprintf(".kh-probe-%d", time.Now().UnixNano()))
	defer os.Remove(probePath)

	start := time.Now()
	if err = os.WriteFile(probePath, buf, 0o600); err != nil {
		return 0, 0, fmt.Errorf("write probe: %w", err)
	}
	writeSecs := time.Since(start).Seconds()
	if writeSecs > 0 {
		writeMBps = float64(sampleBytes) / (1 << 20) / writeSecs
	}

	start = time.Now()
	if _, err = os.ReadFile(probePath); err != nil {
		return writeMBps, 0, fmt.Errorf("read probe: %w", err)
	}
	readSecs := time.Since(start).Seconds()
	if readSecs > 0 {
		readMBps = float64(sampleBytes) / (1 << 20) / readSecs
	}
	return writeMBps, readMBps, nil
}

// freeBytes returns the free space available at path (linux/unix; the backend
// runs in a linux container).
func freeBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
