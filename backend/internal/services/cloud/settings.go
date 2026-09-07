package cloud

import (
	"context"
	"os"
	"strconv"
	"strings"
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

// Setting keys, matching 20260822090200_add_cloud_provisioning.up.sql.
const (
	SettingGlobalMonthlyCapCents = "cloud_global_monthly_cap_cents"
	SettingGlobalInstanceCap     = "cloud_global_concurrent_instance_cap"
	SettingChunkDurationSeconds  = "cloud_chunk_duration_seconds"
	SettingTeardownSlackSeconds  = "cloud_teardown_slack_seconds"
	SettingIdleDrainMinutes      = "cloud_idle_drain_minutes"
	SettingReaperIntervalSeconds = "cloud_reaper_interval_seconds"
	SettingOrphanGraceMinutes    = "cloud_orphan_grace_minutes"

	// SettingAgentImage is seeded by 20260907130000_add_cloud_agent_image.
	SettingAgentImage = "cloud_agent_image"

	// Client defaults, seeded by 20260907140000_add_cloud_budget_defaults.
	// A client field left unset inherits the matching value here.
	SettingDefaultClientBudgetCents = "cloud_default_client_budget_cents"
	SettingDefaultBudgetPeriod      = "cloud_default_budget_period"
	SettingDefaultCloudEnabled      = "cloud_default_cloud_enabled"
	SettingDefaultProviderAllowlist = "cloud_default_provider_allowlist"
)

/*
 * BudgetPeriod is the window a client's spend ceiling applies to.
 *
 * Calendar-aligned and computed on read rather than rolled over by a scheduled
 * job, matching the existing monthly behaviour: there is no cron infrastructure
 * in this codebase to own a period boundary, and a boundary that only exists
 * when a job runs is a boundary that silently stops existing.
 */
type BudgetPeriod string

const (
	BudgetPeriodMonthly    BudgetPeriod = "monthly"
	BudgetPeriodQuarterly  BudgetPeriod = "quarterly"
	BudgetPeriodSemiannual BudgetPeriod = "semiannual"
)

// IsValid reports whether p is a period this code knows how to bound.
func (p BudgetPeriod) IsValid() bool {
	switch p {
	case BudgetPeriodMonthly, BudgetPeriodQuarterly, BudgetPeriodSemiannual:
		return true
	}
	return false
}

// AllBudgetPeriods is the set the API and UI offer, shortest window first.
var AllBudgetPeriods = []BudgetPeriod{
	BudgetPeriodMonthly, BudgetPeriodQuarterly, BudgetPeriodSemiannual,
}

/*
 * ClientDefaults are the server-side values a client inherits when it has not
 * set its own.
 *
 * BudgetCents is a pointer because "no default configured" and "a default of
 * zero" must stay distinguishable. Nil leaves an inheriting client UNFUNDED,
 * which is the fail-closed position this feature must not quietly give up;
 * zero would produce the same outcome by accident and be indistinguishable
 * from a misconfiguration.
 */
type ClientDefaults struct {
	BudgetCents       *int64
	Period            BudgetPeriod
	Enabled           bool
	ProviderAllowlist []string
}

// DefaultClientDefaults is the cold-start shape: nothing funded, nothing
// enabled, monthly windows.
func DefaultClientDefaults() ClientDefaults {
	return ClientDefaults{
		BudgetCents:       nil,
		Period:            BudgetPeriodMonthly,
		Enabled:           false,
		ProviderAllowlist: []string{},
	}
}

/*
 * LoadClientDefaults reads the server-side client defaults.
 *
 * Every read failure falls back to the cold-start value rather than to
 * something permissive: an unreadable default must not be the reason a client
 * becomes fundable.
 */
func LoadClientDefaults(ctx context.Context, repo *repository.SystemSettingsRepository) ClientDefaults {
	d := DefaultClientDefaults()
	if repo == nil {
		return d
	}

	if v, ok := readInt(ctx, repo, SettingDefaultClientBudgetCents); ok && v >= 0 {
		cents := int64(v)
		d.BudgetCents = &cents
	}
	if v, ok := readString(ctx, repo, SettingDefaultBudgetPeriod); ok {
		if p := BudgetPeriod(strings.TrimSpace(v)); p.IsValid() {
			d.Period = p
		} else if v != "" {
			debug.Warning("Cloud setting %s = %q is not a known budget period; using %s",
				SettingDefaultBudgetPeriod, v, d.Period)
		}
	}
	if v, ok := readString(ctx, repo, SettingDefaultCloudEnabled); ok {
		d.Enabled = strings.EqualFold(strings.TrimSpace(v), "true")
	}
	if v, ok := readString(ctx, repo, SettingDefaultProviderAllowlist); ok {
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				d.ProviderAllowlist = append(d.ProviderAllowlist, p)
			}
		}
	}
	return d
}

// EnvAgentImage is the legacy environment variable SettingAgentImage replaces.
// Still read as the bootstrap source and imported once, never authoritative
// after that.
const EnvAgentImage = "KH_CLOUD_AGENT_IMAGE"

/*
 * DefaultAgentImage is the compiled fallback.
 *
 * Note that :latest does not exist until a release is tagged — the branch
 * builds publish :dev and :cloud-gpu. That is deliberate (publishing :latest
 * from a branch would push unreleased code to every deployment that never set
 * this), but it means a pre-release deployment MUST override this, and the
 * failure if it does not is a billed minute rather than a startup error.
 */
const DefaultAgentImage = "zerkereod/krakenhashes-agent-cloud:latest"

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
	// AgentImage is the container image rented instances pull. Empty means the
	// setting is unconfigured and the caller should fall back to the
	// environment variable, then to DefaultAgentImage.
	AgentImage string
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
	if v, ok := readString(ctx, repo, SettingAgentImage); ok {
		s.AgentImage = strings.TrimSpace(v)
	}
	return s
}

/*
 * ImportEnvAgentImageIfUnset copies KH_CLOUD_AGENT_IMAGE into system_settings
 * the first time the backend boots after this setting lands, and never again.
 *
 * A NULL row means "never configured". Once the row holds any string the
 * database is authoritative and the environment variable is dead — including
 * when it holds the empty string, which is how an administrator says "use the
 * compiled default".
 *
 * Mirrors certs.ImportEnvSANsIfUnset deliberately: an operator who has already
 * moved one of these settings into the UI should not have to learn a second set
 * of rules for the next one.
 */
func ImportEnvAgentImageIfUnset(ctx context.Context, repo *repository.SystemSettingsRepository) bool {
	if repo == nil {
		return false
	}
	setting, err := repo.GetSetting(ctx, SettingAgentImage)
	if err != nil {
		debug.Warning("Could not read %s; leaving it alone: %v", SettingAgentImage, err)
		return false
	}

	envValue := strings.TrimSpace(os.Getenv(EnvAgentImage))

	if setting != nil && setting.Value != nil {
		// Already configured. Warn if the environment variable still disagrees:
		// a stale variable that looks effective is how an operator ends up sure
		// they pinned an image they did not pin.
		if envValue != "" && envValue != *setting.Value {
			debug.Warning("%s is set to %q but is NO LONGER READ. The cloud agent image is managed in "+
				"Admin -> Cloud Provisioning -> Limits & operations (%s = %q). Remove the environment variable.",
				EnvAgentImage, envValue, SettingAgentImage, *setting.Value)
		}
		return false
	}

	if err := repo.UpdateSetting(ctx, SettingAgentImage, envValue); err != nil {
		debug.Warning("Could not import %s into %s: %v", EnvAgentImage, SettingAgentImage, err)
		return false
	}
	debug.Info("Imported %s=%q into %s; the database is authoritative from now on",
		EnvAgentImage, envValue, SettingAgentImage)
	return true
}

func readString(ctx context.Context, repo *repository.SystemSettingsRepository, key string) (string, bool) {
	setting, err := repo.GetSetting(ctx, key)
	if err != nil || setting == nil || setting.Value == nil {
		return "", false
	}
	return *setting.Value, true
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
