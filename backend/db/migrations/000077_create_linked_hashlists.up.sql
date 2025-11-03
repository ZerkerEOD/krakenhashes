-- Create linked_hashlists table for managing relationships between hashlists
-- Generic design allows linking any hash types, not just LM/NTLM
CREATE TABLE linked_hashlists (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hashlist_id_1 BIGINT NOT NULL REFERENCES hashlists(id) ON DELETE CASCADE,
    hashlist_id_2 BIGINT NOT NULL REFERENCES hashlists(id) ON DELETE CASCADE,
    link_type VARCHAR(50) NOT NULL, -- e.g., 'lm_ntlm', future: 'other_type'
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Ensure bidirectional uniqueness: (A,B) and (B,A) should not both exist
    CONSTRAINT unique_hashlist_link UNIQUE (hashlist_id_1, hashlist_id_2),
    CONSTRAINT no_self_link CHECK (hashlist_id_1 != hashlist_id_2)
);

-- Index for reverse lookups
CREATE INDEX idx_linked_hashlists_id2 ON linked_hashlists(hashlist_id_2);

-- Index for link type filtering
CREATE INDEX idx_linked_hashlists_type ON linked_hashlists(link_type);
