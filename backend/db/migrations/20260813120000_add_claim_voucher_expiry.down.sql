-- Reverse 20260813120000_add_claim_voucher_expiry.
--
-- Dropping the column restores the previous "vouchers never expire" behavior.
-- Any expiry data is lost, which is the intended semantics of rolling back.

DROP INDEX IF EXISTS idx_claim_vouchers_expires_at;

ALTER TABLE claim_vouchers
    DROP COLUMN IF EXISTS expires_at;
