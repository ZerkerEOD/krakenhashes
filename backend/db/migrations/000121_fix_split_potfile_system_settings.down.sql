-- Revert migration 000121: Restore the original single potfile deletion setting

DELETE FROM system_settings WHERE key IN (
    'remove_from_global_potfile_on_hashlist_delete_default',
    'remove_from_client_potfile_on_hashlist_delete_default'
);

INSERT INTO system_settings (key, value, description, data_type) VALUES
    ('remove_passwords_on_hashlist_delete_default', 'false', 'Default: remove client potfile passwords when hashlist deleted', 'boolean')
ON CONFLICT (key) DO NOTHING;
