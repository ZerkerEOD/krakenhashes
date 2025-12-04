-- Migration: Add WebAuthn/Passkey support for MFA
-- This migration adds tables for storing passkey credentials and challenges,
-- as well as WebAuthn configuration settings.

-- Store registered passkey credentials
CREATE TABLE user_passkeys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BYTEA NOT NULL UNIQUE,
    public_key BYTEA NOT NULL,
    aaguid BYTEA,
    sign_count BIGINT NOT NULL DEFAULT 0,
    transports TEXT[] DEFAULT '{}',
    name VARCHAR(255) NOT NULL DEFAULT 'Passkey',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT unique_user_credential UNIQUE (user_id, credential_id)
);

CREATE INDEX idx_user_passkeys_user_id ON user_passkeys(user_id);
CREATE INDEX idx_user_passkeys_credential_id ON user_passkeys(credential_id);

-- Store registration challenges (5-min expiry)
CREATE TABLE pending_passkey_registration (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    challenge BYTEA NOT NULL,
    session_data BYTEA NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Store authentication challenges during MFA flow
CREATE TABLE pending_passkey_authentication (
    session_token TEXT PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    challenge BYTEA NOT NULL,
    session_data BYTEA NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_pending_passkey_auth_user_id ON pending_passkey_authentication(user_id);

-- Cleanup trigger for expired challenges
CREATE OR REPLACE FUNCTION cleanup_expired_passkey_challenges()
RETURNS trigger AS $$
BEGIN
    DELETE FROM pending_passkey_registration WHERE created_at < NOW() - INTERVAL '5 minutes';
    DELETE FROM pending_passkey_authentication WHERE created_at < NOW() - INTERVAL '5 minutes';
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_cleanup_passkey_challenges
    AFTER INSERT ON pending_passkey_registration
    EXECUTE FUNCTION cleanup_expired_passkey_challenges();

-- Add WebAuthn settings to auth_settings (hot-reloadable)
ALTER TABLE auth_settings ADD COLUMN webauthn_rp_id VARCHAR(255);
ALTER TABLE auth_settings ADD COLUMN webauthn_rp_origins TEXT[] DEFAULT '{}';
ALTER TABLE auth_settings ADD COLUMN webauthn_rp_display_name VARCHAR(255) DEFAULT 'KrakenHashes';
