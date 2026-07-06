-- Reverse migration 000145_add_hash_validation.

DROP TABLE IF EXISTS invalid_hashes;

ALTER TABLE hashlists
    DROP COLUMN IF EXISTS invalid_count,
    DROP COLUMN IF EXISTS total_input_lines,
    DROP COLUMN IF EXISTS validation_notice;

-- Restore the legacy CHECK constraint. Any rows whose status falls outside
-- this set will block the rollback — operators must reconcile those rows
-- manually before reverting (e.g. by moving 'awaiting_validation_decision'
-- rows to 'error' or 'cancelled'-to-'error').
ALTER TABLE hashlists DROP CONSTRAINT IF EXISTS hashlists_status_check;
ALTER TABLE hashlists ADD CONSTRAINT hashlists_status_check
    CHECK (status IN ('uploading', 'processing', 'ready', 'error'));
