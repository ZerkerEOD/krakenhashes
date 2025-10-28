-- Revert potfile batch settings to original conservative defaults

UPDATE system_settings
SET value = '60',
    description = 'Seconds between pot-file batch processing'
WHERE key = 'potfile_batch_interval';

UPDATE system_settings
SET value = '1000',
    description = 'Maximum entries to process in one batch'
WHERE key = 'potfile_max_batch_size';
