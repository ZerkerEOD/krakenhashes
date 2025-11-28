-- Add increment_layer_id to job_tasks table
-- When set, this task belongs to a specific increment layer
-- When NULL, this is a regular (non-increment) job task
ALTER TABLE job_tasks
    ADD COLUMN increment_layer_id UUID REFERENCES job_increment_layers(id) ON DELETE CASCADE;

-- Create index for layer-based task queries
CREATE INDEX idx_job_tasks_increment_layer ON job_tasks(increment_layer_id) WHERE increment_layer_id IS NOT NULL;

-- Add helpful comment
COMMENT ON COLUMN job_tasks.increment_layer_id IS 'References job_increment_layers if task belongs to an increment layer (NULL for regular jobs)';
