-- Rollback: Remove teams_enabled setting
DELETE FROM system_settings WHERE key = 'teams_enabled';
