-- Migration 000121: Fix missing system_settings rows for split potfile deletion settings
-- Migration 000120 split the clients table columns but forgot to add the corresponding
-- system_settings rows for the new split keys.

-- Remove the stale pre-split setting
DELETE FROM system_settings WHERE key = 'remove_passwords_on_hashlist_delete_default';

-- Insert the two new split settings
INSERT INTO system_settings (key, value, description, data_type) VALUES
    ('remove_from_global_potfile_on_hashlist_delete_default', 'false', 'Default: remove from global potfile when hashlist is deleted', 'boolean'),
    ('remove_from_client_potfile_on_hashlist_delete_default', 'false', 'Default: remove from client potfile when hashlist is deleted', 'boolean')
ON CONFLICT (key) DO NOTHING;
