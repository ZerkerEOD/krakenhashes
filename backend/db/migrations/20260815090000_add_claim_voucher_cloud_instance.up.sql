-- Bind a claim voucher to the cloud instance it was minted for.
--
-- Separate from 20260813120200 rather than folded into it: that migration has
-- already run on development databases, and an in-place edit to an applied
-- migration is silently skipped — leaving a schema that does not match the code
-- that expects it.
--
-- WHY THE COLUMN EXISTS
--
-- agents.cloud_instance_id decides three things about an agent: which single
-- job it may be dispatched work from, whether max_agents applies to it, and
-- whether the offline monitor pages someone when it disappears. All three are
-- privileges, so the value must be assigned by the backend rather than reported
-- by the agent — an agent that could name its own instance could hand itself a
-- lock on any client's job.
--
-- The voucher is where that assignment lives. The backend mints one voucher per
-- instance it rents; at registration it reads the instance id off the redeemed
-- voucher and copies it onto the agent inside the same INSERT.
--
-- ON DELETE SET NULL rather than CASCADE: deleting an instance row must not
-- delete voucher history, which is the audit trail of what registered when.

ALTER TABLE claim_vouchers
    ADD COLUMN IF NOT EXISTS cloud_instance_id UUID REFERENCES cloud_instances(id) ON DELETE SET NULL;

COMMENT ON COLUMN claim_vouchers.cloud_instance_id IS
    'Binds a single-use voucher to the instance it was minted for. Registration reads the agent''s cloud identity from here rather than from anything the agent reports, because that identity confers a job lock, a max_agents exemption and an offline-monitor exemption. NULL for every ordinary voucher.';

CREATE INDEX IF NOT EXISTS idx_claim_vouchers_cloud_instance
    ON claim_vouchers (cloud_instance_id) WHERE cloud_instance_id IS NOT NULL;
