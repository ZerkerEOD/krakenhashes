DROP INDEX IF EXISTS idx_claim_vouchers_cloud_instance;

ALTER TABLE claim_vouchers
    DROP COLUMN IF EXISTS cloud_instance_id;
