-- Remove is_salted column from hash_types table
DROP INDEX IF EXISTS idx_hash_types_is_salted;
ALTER TABLE hash_types DROP COLUMN IF EXISTS is_salted;
