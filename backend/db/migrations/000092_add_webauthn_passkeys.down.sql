-- Rollback: Remove WebAuthn/Passkey support

-- Remove WebAuthn settings from auth_settings
ALTER TABLE auth_settings DROP COLUMN IF EXISTS webauthn_rp_display_name;
ALTER TABLE auth_settings DROP COLUMN IF EXISTS webauthn_rp_origins;
ALTER TABLE auth_settings DROP COLUMN IF EXISTS webauthn_rp_id;

-- Remove cleanup trigger and function
DROP TRIGGER IF EXISTS trigger_cleanup_passkey_challenges ON pending_passkey_registration;
DROP FUNCTION IF EXISTS cleanup_expired_passkey_challenges();

-- Remove passkey tables
DROP TABLE IF EXISTS pending_passkey_authentication;
DROP TABLE IF EXISTS pending_passkey_registration;
DROP TABLE IF EXISTS user_passkeys;
