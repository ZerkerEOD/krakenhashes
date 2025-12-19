-- Remove index
DROP INDEX IF EXISTS idx_binary_versions_source_type;

-- Remove constraint
ALTER TABLE binary_versions DROP CONSTRAINT IF EXISTS check_source_type;

-- Remove columns
ALTER TABLE binary_versions DROP COLUMN IF EXISTS version;
ALTER TABLE binary_versions DROP COLUMN IF EXISTS description;
ALTER TABLE binary_versions DROP COLUMN IF EXISTS source_type;

-- Restore source_url NOT NULL constraint (after ensuring no NULL values exist)
UPDATE binary_versions SET source_url = '' WHERE source_url IS NULL;
ALTER TABLE binary_versions ALTER COLUMN source_url SET NOT NULL;
