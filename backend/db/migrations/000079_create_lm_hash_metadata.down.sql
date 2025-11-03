-- Drop LM hash metadata table and indexes
DROP INDEX IF EXISTS idx_lm_metadata_hash_id;
DROP INDEX IF EXISTS idx_lm_metadata_crack_status;
DROP TABLE IF EXISTS lm_hash_metadata;
