-- Migration: Add client-specific potfile and wordlist support
-- SIMPLIFIED: Uses existing potfile_staging table with client_id column
-- This enables clients to have their own potfiles alongside the global potfile,
-- with independent enable/disable controls and configurable password cleanup.

-- 1. Add client_id to existing potfile_staging table
-- NULL = global-only entry, non-NULL = associated with a client
ALTER TABLE potfile_staging ADD COLUMN client_id UUID REFERENCES clients(id) ON DELETE CASCADE;

-- Index for efficient per-client queries
CREATE INDEX idx_potfile_staging_client_id ON potfile_staging(client_id) WHERE client_id IS NOT NULL;

COMMENT ON COLUMN potfile_staging.client_id IS 'Client ID for client potfile staging. NULL means global-only.';

-- 2. Client potfiles metadata table (tracks file info, not staging)
-- Each client can have exactly one potfile
CREATE TABLE client_potfiles (
    id SERIAL PRIMARY KEY,
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    file_path TEXT NOT NULL,
    file_size BIGINT DEFAULT 0,
    line_count BIGINT DEFAULT 0,
    md5_hash VARCHAR(32),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(client_id)
);

CREATE INDEX idx_client_potfiles_client_id ON client_potfiles(client_id);

COMMENT ON TABLE client_potfiles IS 'Metadata for client-specific potfiles (file at wordlists/clients/{clientID}/potfile.txt)';
COMMENT ON COLUMN client_potfiles.file_path IS 'Path to the potfile on disk';
COMMENT ON COLUMN client_potfiles.line_count IS 'Number of unique passwords in the potfile';

-- 3. Client-specific wordlists table (separate from association_wordlists)
-- These are general-purpose wordlists tied to a client, visible across all their hashlists
CREATE TABLE client_wordlists (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    file_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    file_size BIGINT,
    line_count BIGINT,
    md5_hash VARCHAR(32),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_client_wordlists_client_id ON client_wordlists(client_id);

COMMENT ON TABLE client_wordlists IS 'Client-specific wordlists available for any attack mode';
COMMENT ON COLUMN client_wordlists.file_name IS 'Original filename as uploaded';

-- 4. Add client potfile settings to clients table
ALTER TABLE clients ADD COLUMN enable_client_potfile BOOLEAN DEFAULT FALSE;
ALTER TABLE clients ADD COLUMN contribute_to_global_potfile BOOLEAN DEFAULT TRUE;
ALTER TABLE clients ADD COLUMN remove_passwords_on_hashlist_delete BOOLEAN DEFAULT NULL;

COMMENT ON COLUMN clients.enable_client_potfile IS 'When true, cracked passwords are added to client-specific potfile';
COMMENT ON COLUMN clients.contribute_to_global_potfile IS 'When true, cracked passwords also go to global potfile';
COMMENT ON COLUMN clients.remove_passwords_on_hashlist_delete IS 'NULL=system default, true=always remove, false=never remove';

-- 5. System settings for client potfile feature
INSERT INTO system_settings (key, value, description, data_type) VALUES
    ('client_potfiles_enabled', 'true', 'Enable client-specific potfiles globally', 'boolean'),
    ('remove_passwords_on_hashlist_delete_default', 'false', 'Default: remove client potfile passwords when hashlist deleted', 'boolean');
