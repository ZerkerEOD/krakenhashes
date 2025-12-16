-- Migration: Add backup flags to user_passkeys table
-- These flags track the WebAuthn credential's backup eligibility and state
-- Required for proper validation during passkey authentication

ALTER TABLE user_passkeys ADD COLUMN IF NOT EXISTS backup_eligible BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE user_passkeys ADD COLUMN IF NOT EXISTS backup_state BOOLEAN NOT NULL DEFAULT false;

-- For existing passkeys, set backup_eligible=true since synced passkeys (Bitwarden, etc.)
-- typically have this flag set
UPDATE user_passkeys SET backup_eligible = true, backup_state = true WHERE backup_eligible = false;
