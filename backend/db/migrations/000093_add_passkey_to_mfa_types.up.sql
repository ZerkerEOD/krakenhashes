-- Migration: Add passkey to MFA type constraints
-- Updates the users table constraints to allow 'passkey' as a valid MFA type

-- Drop existing constraints
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_mfa_type_check;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_preferred_mfa_method_check;

-- Recreate mfa_type constraint with passkey included
ALTER TABLE users ADD CONSTRAINT users_mfa_type_check CHECK (
    (array_length(mfa_type, 1) > 0) AND
    ('email'::text = ANY (mfa_type)) AND
    (mfa_type <@ ARRAY['email'::text, 'authenticator'::text, 'backup'::text, 'passkey'::text])
);

-- Recreate preferred_mfa_method constraint with passkey included
ALTER TABLE users ADD CONSTRAINT users_preferred_mfa_method_check CHECK (
    ((preferred_mfa_method)::text = ANY (ARRAY['email'::character varying, 'authenticator'::character varying, 'passkey'::character varying]::text[])) AND
    ((preferred_mfa_method)::text <> 'backup'::text)
);
