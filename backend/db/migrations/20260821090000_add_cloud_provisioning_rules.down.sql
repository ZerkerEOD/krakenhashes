ALTER TABLE preset_jobs
    DROP COLUMN IF EXISTS cloud_allow_community_hosts;
ALTER TABLE job_executions
    DROP COLUMN IF EXISTS cloud_allow_community_hosts;

DROP INDEX IF EXISTS idx_cloud_provisioning_rules_system_default;
DROP TABLE IF EXISTS cloud_provisioning_rules;
