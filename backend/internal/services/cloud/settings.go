package cloud

import (
	"context"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

/*
 * Cloud system settings.
 *
 * These keys were seeded by the provisioning migration and then read by
 * nothing, which is a worse state than not existing. An operator who sets
 * cloud_global_monthly_cap_cents believes they have capped what this deployment
 * can spend; if nothing reads it, they have capped nothing and will find out
 * from an invoice.
 *
 * Every key below is read here and nowhere else, so "is this setting live?" has
 * exactly one answer to check.
 */

// Setting keys, matching 20260813120200_add_cloud_provisioning.up.sql.
const (
	SettingGlobalMonthlyCapCents = "cloud_global_monthly_cap_cents"
	SettingGlobalInstanceCap     = "cloud_global_concurrent_instance_cap"
	SettingChunkDurationSeconds  = "cloud_chunk_duration_seconds"
	SettingTeardownSlackSeconds  = "cloud_teardown_slack_seconds"
	SettingIdleDrainMinutes      = "cloud_idle_drain_minutes"
	SettingReaperIntervalSeconds = "cloud_reaper_interval_seconds"
	SettingOrphanGraceMinutes    = "cloud_orphan_grace_minutes"
)

// Settings is the resolved cloud configuration.
type Settings struct {
	// GlobalMonthlyCapCents is a system-wide ceiling across every client, on
	// top of each client's own budget. Zero means cloud provisioning is
	// DISABLED, not unlimited — a paid feature should require someone to state
	// a ceiling before it can spend anything, and that is what the shipped
	// description of this key promises.
	GlobalMonthlyCapCents int64
	// GlobalInstanceCap bounds live instances across all providers and clients.
	// Zero means unlimited (budget still applies).
	GlobalInstanceCap int
	// ReaperInterval is how often the database is reconciled against provider
	// inventory.
	ReaperInterval time.Duration
	// OrphanGrace is how long a provider-side instance with no database row is
	// tolerated before destruction, so a launch in flight is not reaped by the
	// pass racing it.
	OrphanGrace time.Duration
	// IdleDrain is how long an instance may sit with no task before teardown.
	// Zero disables idle drain.
	IdleDrain time.Duration
}

// DefaultSettings mirrors the values seeded by the migration, so a deployment
// whose settings table cannot be read behaves like a fresh install rather than
// like one with every limit set to zero.
func DefaultSettings() Settings {
	return Settings{
		GlobalMonthlyCapCents: 0,
		GlobalInstanceCap:     0,
		ReaperInterval:        60 * time.Second,
		OrphanGrace:           10 * time.Minute,
		IdleDrain:             5 * time.Minute,
	}
}

/*
 * LoadSettings reads the cloud settings.
 *
 * Individual read failures fall back to the default for that key rather than
 * failing the whole load: a single missing row must not take out the reaper
 * interval and leave instances unreconciled. The one place this is NOT
 * forgiving is the monthly cap, which is re-read per provisioning decision
 * rather than cached — see GlobalCapRemaining.
 */
func LoadSettings(ctx context.Context, repo *repository.SystemSettingsRepository) Settings {
	s := DefaultSettings()
	if repo == nil {
		return s
	}

	if v, ok := readInt(ctx, repo, SettingGlobalMonthlyCapCents); ok {
		s.GlobalMonthlyCapCents = int64(v)
	}
	if v, ok := readInt(ctx, repo, SettingGlobalInstanceCap); ok {
		s.GlobalInstanceCap = v
	}
	if v, ok := readInt(ctx, repo, SettingReaperIntervalSeconds); ok && v > 0 {
		s.ReaperInterval = time.Duration(v) * time.Second
	}
	if v, ok := readInt(ctx, repo, SettingOrphanGraceMinutes); ok && v > 0 {
		s.OrphanGrace = time.Duration(v) * time.Minute
	}
	if v, ok := readInt(ctx, repo, SettingIdleDrainMinutes); ok {
		// Zero is meaningful here: it disables idle drain.
		s.IdleDrain = time.Duration(v) * time.Minute
	}
	return s
}

func readInt(ctx context.Context, repo *repository.SystemSettingsRepository, key string) (int, bool) {
	setting, err := repo.GetSetting(ctx, key)
	if err != nil || setting == nil || setting.Value == nil {
		return 0, false
	}
	v, err := strconv.Atoi(*setting.Value)
	if err != nil {
		debug.Warning("Cloud setting %s = %q is not an integer; using the default", key, *setting.Value)
		return 0, false
	}
	return v, true
}
