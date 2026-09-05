-- Add an expiry timestamp to claim vouchers.
--
-- Why: ClaimVoucherService.CreateTempVoucher has always accepted an
-- `expiresIn` duration (both API callers pass it from the request body) and
-- silently discarded it. Every voucher ever issued is therefore valid
-- forever, bounded only by is_active and, for single-use codes, by
-- used_by_agent_id.
--
-- That is tolerable on a trusted LAN and not tolerable for cloud agents: a
-- claim code is injected into a rented instance's environment, where the
-- machine's operator can read it. A short-lived, single-use voucher limits
-- that exposure to the provisioning window.
--
-- NULL means "never expires", so every existing voucher keeps its current
-- behavior and this migration is a no-op for running deployments.

ALTER TABLE claim_vouchers
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ DEFAULT NULL;

COMMENT ON COLUMN claim_vouchers.expires_at IS
    'Absolute expiry for the voucher. NULL means the voucher never expires (legacy behavior). Enforced by ClaimVoucher.IsValid().';

-- Partial index: only expiring vouchers are worth scanning for cleanup, and
-- active ones are the only ones a sweep would act on.
CREATE INDEX IF NOT EXISTS idx_claim_vouchers_expires_at
    ON claim_vouchers (expires_at)
    WHERE expires_at IS NOT NULL AND is_active = true;
