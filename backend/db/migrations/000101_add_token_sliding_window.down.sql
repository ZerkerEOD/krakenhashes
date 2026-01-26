-- Remove token sliding window columns

DROP INDEX IF EXISTS idx_tokens_superseded_at;
ALTER TABLE tokens DROP COLUMN IF EXISTS superseded_by;
ALTER TABLE tokens DROP COLUMN IF EXISTS superseded_at;
