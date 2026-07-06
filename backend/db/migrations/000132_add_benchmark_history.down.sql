-- Remove benchmark history table and setting
DROP TABLE IF EXISTS agent_benchmark_history;

DELETE FROM system_settings WHERE key = 'benchmark_history_retention_days';
