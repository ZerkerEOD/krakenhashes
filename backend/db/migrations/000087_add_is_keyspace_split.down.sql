-- Remove is_keyspace_split flag from job_tasks table
ALTER TABLE job_tasks
    DROP COLUMN is_keyspace_split;
