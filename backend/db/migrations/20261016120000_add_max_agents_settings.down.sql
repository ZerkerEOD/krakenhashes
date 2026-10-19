-- Remove the max_agents default/limit settings (GH #126).
DELETE FROM system_settings
WHERE key IN ('default_max_agents', 'max_agents_limit');
