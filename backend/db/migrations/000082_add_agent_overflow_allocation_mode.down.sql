-- Remove agent overflow allocation mode setting

DELETE FROM system_settings
WHERE key = 'agent_overflow_allocation_mode';
