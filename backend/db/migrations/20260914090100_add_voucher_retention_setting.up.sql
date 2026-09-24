-- Retention for expired, never-redeemed claim vouchers.
--
-- Cloud provisioning mints a claim voucher per CANDIDATE OFFER, before the
-- provider is called, and until now nothing ever removed one -- the repository
-- had no delete method at all. On the reference deployment that left 695
-- vouchers, 673 of them belonging to instances that failed to launch.
--
-- 0 means keep forever, matching the metrics_retention_* keys above it.
--
-- Only affects vouchers that are BOTH expired and never redeemed. A redeemed
-- voucher is the audit link between an agent and the credential it joined with
-- and is never swept, whatever this is set to.
INSERT INTO system_settings (key, value, description, data_type)
VALUES ('voucher_retention_days', '30',
        'Days to retain expired, never-redeemed agent claim vouchers before deletion (0 = keep forever). Redeemed vouchers are always kept as an audit record.',
        'integer')
ON CONFLICT (key) DO NOTHING;
