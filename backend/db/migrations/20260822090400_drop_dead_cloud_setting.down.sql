INSERT INTO system_settings (key, value, description, data_type) VALUES
    ('cloud_ttl_extension_max_pct', '25',
     'Maximum TTL extension when an instance is mid-chunk and would finish the job. Requires a fresh budget reservation and never crosses hard_stop_pct.', 'integer')
ON CONFLICT (key) DO NOTHING;
