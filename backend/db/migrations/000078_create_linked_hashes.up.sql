-- Create linked_hashes table for managing relationships between individual hashes
-- This allows connecting LM and NTLM hashes for the same user
CREATE TABLE linked_hashes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hash_id_1 UUID NOT NULL REFERENCES hashes(id) ON DELETE CASCADE,
    hash_id_2 UUID NOT NULL REFERENCES hashes(id) ON DELETE CASCADE,
    link_type VARCHAR(50) NOT NULL, -- e.g., 'lm_ntlm', future: 'other_type'
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Ensure bidirectional uniqueness: (A,B) and (B,A) should not both exist
    CONSTRAINT unique_hash_link UNIQUE (hash_id_1, hash_id_2),
    CONSTRAINT no_self_link CHECK (hash_id_1 != hash_id_2)
);

-- Index for reverse lookups
CREATE INDEX idx_linked_hashes_id2 ON linked_hashes(hash_id_2);

-- Index for link type filtering
CREATE INDEX idx_linked_hashes_type ON linked_hashes(link_type);
