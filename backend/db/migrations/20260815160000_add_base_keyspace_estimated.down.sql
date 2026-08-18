ALTER TABLE scheduling_units
    DROP COLUMN IF EXISTS base_keyspace_estimated;

ALTER TABLE job_executions
    DROP COLUMN IF EXISTS base_keyspace_estimated;
