-- Restore rule-splitting columns, indexes and settings (mirrors migrations
-- 000034 / 000102 / 000021 / 000039). The columns are recreated empty/default;
-- no rule-split data is restored since it is no longer produced.

ALTER TABLE job_executions
    ADD COLUMN IF NOT EXISTS uses_rule_splitting BOOLEAN DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS rule_split_count INT DEFAULT 0;

ALTER TABLE job_tasks
    ADD COLUMN IF NOT EXISTS rule_start_index INT,
    ADD COLUMN IF NOT EXISTS rule_end_index INT,
    ADD COLUMN IF NOT EXISTS rule_chunk_path TEXT,
    ADD COLUMN IF NOT EXISTS is_rule_split_task BOOLEAN DEFAULT FALSE;

ALTER TABLE preset_jobs
    ADD COLUMN IF NOT EXISTS use_rule_splitting BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_job_executions_rule_splitting ON job_executions(uses_rule_splitting);
CREATE INDEX IF NOT EXISTS idx_job_tasks_rule_split ON job_tasks(is_rule_split_task);

INSERT INTO system_settings (key, value, description) VALUES
    ('rule_split_enabled', 'true', 'Enable/disable rule splitting feature'),
    ('rule_split_threshold', '2.0', 'Multiplier for triggering rule split (job time > threshold × chunk time)'),
    ('rule_split_min_rules', '100', 'Minimum number of rules to consider splitting'),
    ('rule_split_max_chunks', '1000', 'Maximum number of rule chunks to create'),
    ('rule_chunk_temp_dir', '/data/krakenhashes/temp/rule_chunks', 'Directory for temporary rule chunk files')
ON CONFLICT (key) DO NOTHING;

INSERT INTO system_settings (key, value, description, data_type) VALUES
    ('chunk_fluctuation_percentage', '20', 'Percentage fluctuation allowed for final job chunks to avoid small remainder chunks', 'integer'),
    ('speedtest_timeout_seconds', '180', 'Maximum time to wait for speedtest completion (in seconds)', 'integer')
ON CONFLICT (key) DO NOTHING;
