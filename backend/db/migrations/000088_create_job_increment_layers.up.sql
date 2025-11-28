-- Create job_increment_layers table to track sub-layers for increment mode jobs
CREATE TABLE IF NOT EXISTS job_increment_layers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_execution_id UUID NOT NULL REFERENCES job_executions(id) ON DELETE CASCADE,
    layer_index INT NOT NULL,  -- Ordering: increment (1=shortest), increment_inverse (1=longest)
    mask VARCHAR(255) NOT NULL,  -- Specific mask for this layer (e.g., ?l?l)
    status VARCHAR(50) NOT NULL DEFAULT 'pending',

    -- Keyspace tracking (similar to job_executions)
    base_keyspace BIGINT,  -- From hashcat --keyspace command (set during job creation)
    effective_keyspace BIGINT,  -- From forced benchmark progress[1] (set after benchmark)
    processed_keyspace BIGINT DEFAULT 0,  -- Sum from tasks' effective_keyspace_processed
    dispatched_keyspace BIGINT DEFAULT 0,  -- Total keyspace dispatched to tasks
    is_accurate_keyspace BOOLEAN DEFAULT FALSE,  -- TRUE after forced benchmark captures effective_keyspace

    -- Progress tracking
    overall_progress_percent NUMERIC(5,2) DEFAULT 0.00,  -- Layer completion percentage (0-100)
    last_progress_update TIMESTAMP WITH TIME ZONE,

    -- Timing
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    -- Error handling
    error_message TEXT,

    -- Constraints
    UNIQUE(job_execution_id, layer_index),
    CHECK (status IN ('pending', 'running', 'paused', 'completed', 'failed', 'cancelled')),
    CHECK (layer_index > 0)
);

-- Create indexes for performance
CREATE INDEX idx_job_increment_layers_job_id ON job_increment_layers(job_execution_id);
CREATE INDEX idx_job_increment_layers_status ON job_increment_layers(status);
CREATE INDEX idx_job_increment_layers_accurate_keyspace ON job_increment_layers(is_accurate_keyspace) WHERE is_accurate_keyspace = FALSE;

-- Add helpful comments
COMMENT ON TABLE job_increment_layers IS 'Sub-layers for increment mode jobs - each layer represents one mask length';
COMMENT ON COLUMN job_increment_layers.layer_index IS 'Layer ordering: increment mode (1=shortest→longest), increment_inverse mode (1=longest→shortest)';
COMMENT ON COLUMN job_increment_layers.mask IS 'Specific mask for this layer (e.g., ?l?l for length 2)';
COMMENT ON COLUMN job_increment_layers.base_keyspace IS 'Keyspace from --keyspace command (calculated during job creation)';
COMMENT ON COLUMN job_increment_layers.effective_keyspace IS 'Actual keyspace from hashcat progress[1] (set after forced benchmark)';
COMMENT ON COLUMN job_increment_layers.status IS 'Layer status matches job_execution: pending (waiting for work or benchmark), running (has tasks), paused, completed, failed, cancelled. Use is_accurate_keyspace to check if benchmark is needed.';
