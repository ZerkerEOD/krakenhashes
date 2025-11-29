-- Drop indexes
DROP INDEX IF EXISTS idx_job_increment_layers_accurate_keyspace;
DROP INDEX IF EXISTS idx_job_increment_layers_status;
DROP INDEX IF EXISTS idx_job_increment_layers_job_id;

-- Drop table
DROP TABLE IF EXISTS job_increment_layers;
