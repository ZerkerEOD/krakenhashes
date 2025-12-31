-- Add system setting for hashlist bulk batch size
INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'hashlist_bulk_batch_size',
    '100000',
    'Number of hashes to process per batch during hashlist uploads. Higher values (500K-1M) may improve performance for large hashlists but use more memory.',
    'integer'
);
