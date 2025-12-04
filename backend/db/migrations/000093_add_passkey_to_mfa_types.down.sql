-- Rollback: Remove passkey from MFA type constraints
-- Note: This will fail if any users have passkey as their MFA type

-- Drop updated constraints
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_mfa_type_check;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_preferred_mfa_method_check;

-- Recreate original mfa_type constraint without passkey
ALTER TABLE users ADD CONSTRAINT users_mfa_type_check CHECK (
    (array_length(mfa_type, 1) > 0) AND
    ('email'::text = ANY (mfa_type)) AND
    (mfa_type <@ ARRAY['email'::text, 'authenticator'::text, 'backup'::text])
);

-- Recreate original preferred_mfa_method constraint without passkey
ALTER TABLE users ADD CONSTRAINT users_preferred_mfa_method_check CHECK (
    ((preferred_mfa_method)::text = ANY (ARRAY['email'::character varying, 'authenticator'::character varying]::text[])) AND
    ((preferred_mfa_method)::text <> 'backup'::text)
);
