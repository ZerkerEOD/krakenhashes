-- Admin-configurable default and enforced ceiling for a job's max_agents (GH #126).
--
-- Mirrors how default_chunk_duration (a default) and max_job_priority (an
-- enforced ceiling) already work. Both default to 0 so behaviour is unchanged
-- until an admin sets them:
--   default_max_agents = 0  -> new custom jobs default to 0 (unlimited), as today.
--   max_agents_limit   = 0  -> no ceiling is enforced, as today.
-- When max_agents_limit > 0, creation/edit paths clamp or reject a requested
-- value above it, and a requested 0 (unlimited) is treated as the limit.
INSERT INTO system_settings (key, value, description, data_type)
VALUES
    ('default_max_agents', '0', 'Default max_agents for new custom jobs (0 = unlimited). Pre-fills the create-job form.', 'integer'),
    ('max_agents_limit', '0', 'Maximum max_agents a non-admin user may request for a job (0 = no limit). Enforced server-side on create and edit.', 'integer')
ON CONFLICT (key) DO NOTHING;
