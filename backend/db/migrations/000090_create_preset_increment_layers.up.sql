-- Create preset_increment_layers table for storing pre-calculated increment layers
-- These layers are calculated when a preset job with increment mode is created,
-- and copied to job_increment_layers when a job is created from the preset
CREATE TABLE IF NOT EXISTS preset_increment_layers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    preset_job_id UUID NOT NULL REFERENCES preset_jobs(id) ON DELETE CASCADE,
    layer_index INTEGER NOT NULL,
    mask VARCHAR(512) NOT NULL,
    base_keyspace BIGINT,
    effective_keyspace BIGINT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(preset_job_id, layer_index)
);

-- Index for efficient lookup by preset_job_id
CREATE INDEX IF NOT EXISTS idx_preset_increment_layers_preset_job_id
    ON preset_increment_layers(preset_job_id);

-- Add trigger for updated_at
CREATE TRIGGER update_preset_increment_layers_updated_at
    BEFORE UPDATE ON preset_increment_layers
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
