-- Remove hashlist bulk batch size setting
DELETE FROM system_settings WHERE key = 'hashlist_bulk_batch_size';
