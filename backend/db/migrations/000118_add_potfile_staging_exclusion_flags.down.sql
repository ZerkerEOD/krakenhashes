-- Rollback: Remove exclusion flags from potfile_staging

ALTER TABLE potfile_staging DROP COLUMN IF EXISTS exclude_from_global;
ALTER TABLE potfile_staging DROP COLUMN IF EXISTS exclude_from_client;
