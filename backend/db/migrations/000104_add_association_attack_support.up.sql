-- Migration: Add Association Attack Support
-- This migration adds support for hashcat's association attack mode (-a 9)

-- Hashlists table: Track original file path + work factor warning
ALTER TABLE hashlists ADD COLUMN original_file_path TEXT;
ALTER TABLE hashlists ADD COLUMN has_mixed_work_factors BOOLEAN DEFAULT FALSE;

-- Association wordlists table: Tied to HASHLIST (reusable across jobs)
CREATE TABLE association_wordlists (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hashlist_id BIGINT NOT NULL REFERENCES hashlists(id) ON DELETE CASCADE,
    file_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    file_size BIGINT,
    line_count BIGINT NOT NULL,
    md5_hash VARCHAR(32),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Job executions table: Reference association wordlist (not path)
ALTER TABLE job_executions ADD COLUMN association_wordlist_id UUID REFERENCES association_wordlists(id);

-- Agent file tracking (for cleanup)
CREATE TABLE agent_association_files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id INTEGER NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    job_execution_id UUID NOT NULL REFERENCES job_executions(id) ON DELETE CASCADE,
    file_type VARCHAR(20) NOT NULL, -- 'wordlist' or 'hashlist'
    file_path TEXT NOT NULL,
    downloaded_at TIMESTAMPTZ DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    UNIQUE(agent_id, job_execution_id, file_type)
);

-- Index for finding wordlists by hashlist
CREATE INDEX idx_association_wordlists_hashlist ON association_wordlists(hashlist_id);

-- Index for finding agent files by job execution
CREATE INDEX idx_agent_association_files_job ON agent_association_files(job_execution_id);

-- Index for finding agent files by agent
CREATE INDEX idx_agent_association_files_agent ON agent_association_files(agent_id);

COMMENT ON COLUMN hashlists.original_file_path IS 'Path to the original uploaded hashlist file, used for association attacks';
COMMENT ON COLUMN hashlists.has_mixed_work_factors IS 'Warning flag: true if hashes have different work factors (e.g., mixed bcrypt costs)';
COMMENT ON TABLE association_wordlists IS 'Wordlists for association attacks, tied to hashlists for reuse across jobs';
COMMENT ON TABLE agent_association_files IS 'Tracks association files downloaded to agents for cleanup';
