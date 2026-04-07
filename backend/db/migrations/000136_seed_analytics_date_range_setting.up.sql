INSERT INTO system_settings (key, value, description, data_type)
VALUES ('analytics_default_date_range_months', '12', 'Default date range in months for analytics reports', 'integer')
ON CONFLICT (key) DO NOTHING;
