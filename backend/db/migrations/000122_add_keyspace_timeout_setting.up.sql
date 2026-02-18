INSERT INTO system_settings (key, value, description, data_type, updated_at)
VALUES (
    'keyspace_calculation_timeout_minutes',
    '4',
    'Timeout in minutes for hashcat --keyspace and --total-candidates commands. Increase for large wordlists/rules.',
    'integer',
    NOW()
)
ON CONFLICT (key) DO NOTHING;
