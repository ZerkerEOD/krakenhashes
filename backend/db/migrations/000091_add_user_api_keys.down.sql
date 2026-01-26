-- Remove API key index
DROP INDEX IF EXISTS idx_users_api_key;

-- Remove API key columns from users table
ALTER TABLE users DROP COLUMN IF EXISTS api_key;
ALTER TABLE users DROP COLUMN IF EXISTS api_key_created_at;
ALTER TABLE users DROP COLUMN IF EXISTS api_key_last_used;
