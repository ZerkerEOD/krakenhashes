package services

import (
	"context"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// SettingVoucherRetentionDays is how long an expired, never-redeemed claim
// voucher is kept before the sweep removes it. 0 means keep forever.
const SettingVoucherRetentionDays = "voucher_retention_days"

// defaultVoucherRetentionDays is deliberately generous. These rows are tiny and
// the only cost of keeping them is table size, whereas deleting one an operator
// was still investigating cannot be undone.
const defaultVoucherRetentionDays = 30

/*
 * VoucherCleanupService removes claim vouchers that expired long ago and were
 * never redeemed.
 *
 * WHY IT EXISTS. Cloud provisioning mints a claim voucher per CANDIDATE OFFER,
 * before the provider is called, and nothing ever removed one — the repository
 * had no delete method at all. On the reference deployment that left 695
 * vouchers, 673 of them belonging to cloud instances that failed to launch.
 *
 * A previous migration (20260822090000_add_claim_voucher_expiry) already built
 * `idx_claim_vouchers_expires_at` as a partial index, with a comment saying it
 * exists "for cleanup". This is the sweep that comment was anticipating.
 *
 * NOT a security control — deactivation on failure is. By the time a row
 * reaches this sweep it has been unusable for weeks; this is hygiene, so that
 * a long-lived install does not carry every launch attempt it ever made.
 *
 * Modelled on MetricsCleanupService: settings-driven retention with 0 meaning
 * unlimited, an immediate run then a daily ticker, logs and continues, never
 * returns an error upward and never blocks startup.
 */
type VoucherCleanupService struct {
	vouchers           *repository.ClaimVoucherRepository
	systemSettingsRepo *repository.SystemSettingsRepository
}

func NewVoucherCleanupService(
	vouchers *repository.ClaimVoucherRepository,
	systemSettingsRepo *repository.SystemSettingsRepository,
) *VoucherCleanupService {
	return &VoucherCleanupService{vouchers: vouchers, systemSettingsRepo: systemSettingsRepo}
}

// StartCleanupScheduler runs the sweep now and then daily until ctx is done.
func (s *VoucherCleanupService) StartCleanupScheduler(ctx context.Context) {
	s.runCleanup(ctx)

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			debug.Info("Claim voucher cleanup scheduler stopped")
			return
		case <-ticker.C:
			s.runCleanup(ctx)
		}
	}
}

func (s *VoucherCleanupService) runCleanup(ctx context.Context) {
	retentionDays := s.retentionDays(ctx)
	if retentionDays <= 0 {
		debug.Info("Claim voucher retention is unlimited (%s = 0), skipping cleanup",
			SettingVoucherRetentionDays)
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	removed, err := s.vouchers.PurgeExpired(ctx, cutoff)
	if err != nil {
		debug.Error("Claim voucher cleanup failed: %v", err)
		return
	}
	if removed > 0 {
		// Says "never redeemed" explicitly: a redeemed voucher is the audit link
		// between an agent and the credential it joined with, and this sweep
		// must never be read as having removed those.
		debug.Info("Claim voucher cleanup removed %d expired, never-redeemed voucher(s) older than %s (%d days)",
			removed, cutoff.Format("2006-01-02"), retentionDays)
	}
}

// retentionDays reads the setting, falling back to the default when it is
// absent or unparseable. A malformed value must not silently mean "delete
// everything", so anything it cannot read becomes the conservative default.
func (s *VoucherCleanupService) retentionDays(ctx context.Context) int {
	setting, err := s.systemSettingsRepo.GetSetting(ctx, SettingVoucherRetentionDays)
	if err != nil || setting == nil || setting.Value == nil {
		return defaultVoucherRetentionDays
	}
	var days int
	if _, err := fmt.Sscanf(*setting.Value, "%d", &days); err != nil {
		debug.Warning("Invalid %s value %q; using the %d-day default",
			SettingVoucherRetentionDays, *setting.Value, defaultVoucherRetentionDays)
		return defaultVoucherRetentionDays
	}
	return days
}
