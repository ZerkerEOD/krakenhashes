-- Add API key fields to users table for User API authentication
ALTER TABLE users ADD COLUMN api_key VARCHAR(255);
ALTER TABLE users ADD COLUMN api_key_created_at TIMESTAMP;
ALTER TABLE users ADD COLUMN api_key_last_used TIMESTAMP;

-- Create index for fast API key lookups
CREATE INDEX idx_users_api_key ON users(api_key) WHERE api_key IS NOT NULL;

-- Add comment for documentation
COMMENT ON COLUMN users.api_key IS 'Bcrypt hash of user API key for external API authentication';
COMMENT ON COLUMN users.api_key_created_at IS 'Timestamp when the API key was generated';
COMMENT ON COLUMN users.api_key_last_used IS 'Timestamp of last successful API key usage';
