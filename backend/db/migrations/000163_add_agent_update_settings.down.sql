DELETE FROM system_settings WHERE key IN (
    'agent_auto_update_enabled',
    'agent_update_max_concurrent',
    'agent_update_health_timeout_seconds',
    'agent_update_max_attempts'
);
