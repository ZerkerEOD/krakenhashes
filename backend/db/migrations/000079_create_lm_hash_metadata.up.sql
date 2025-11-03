-- Create LM hash metadata table to track partial crack status
-- Only contains rows for LM hashes (hash_type_id = 3000)
-- This isolates LM-specific data and has zero impact on other hash types

CREATE TABLE lm_hash_metadata (
    hash_id UUID PRIMARY KEY REFERENCES hashes(id) ON DELETE CASCADE,
    first_half_cracked BOOLEAN NOT NULL DEFAULT FALSE,
    second_half_cracked BOOLEAN NOT NULL DEFAULT FALSE,
    first_half_password VARCHAR(7),
    second_half_password VARCHAR(7),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index for queries checking partial crack status
CREATE INDEX idx_lm_metadata_crack_status
ON lm_hash_metadata(first_half_cracked, second_half_cracked);

-- Index for foreign key lookups
CREATE INDEX idx_lm_metadata_hash_id ON lm_hash_metadata(hash_id);

COMMENT ON TABLE lm_hash_metadata IS 'Tracks partial crack status for LM hashes (mode 3000). Each LM hash has two 16-char halves that crack independently.';
COMMENT ON COLUMN lm_hash_metadata.first_half_cracked IS 'True if the first 16 characters of the LM hash have been cracked';
COMMENT ON COLUMN lm_hash_metadata.second_half_cracked IS 'True if the last 16 characters of the LM hash have been cracked';
COMMENT ON COLUMN lm_hash_metadata.first_half_password IS 'Password for first half (max 7 chars, uppercase)';
COMMENT ON COLUMN lm_hash_metadata.second_half_password IS 'Password for second half (max 7 chars, uppercase)';
