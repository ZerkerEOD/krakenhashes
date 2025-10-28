-- Update potfile batch settings to more aggressive defaults based on production experience
-- Only update if still at original default values (prevents overwriting user customizations)

UPDATE system_settings
SET value = '5',
    description = 'Seconds between pot-file batch processing (default: 5, original: 60)'
WHERE key = 'potfile_batch_interval'
  AND value = '60';

UPDATE system_settings
SET value = '100000',
    description = 'Maximum entries to process in one batch (default: 100000, original: 1000)'
WHERE key = 'potfile_max_batch_size'
  AND value = '1000';
