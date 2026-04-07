-- Add archived_at column to hashlists table for the archive feature
ALTER TABLE hashlists ADD COLUMN archived_at TIMESTAMPTZ DEFAULT NULL;

-- Index for efficient filtering of non-archived hashlists
CREATE INDEX idx_hashlists_archived_at ON hashlists(archived_at);
