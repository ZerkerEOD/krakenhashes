-- Add file-based charset support to custom_charsets table
-- Supports both inline text definitions and binary .hcchr charset files

-- Add charset type discriminator
ALTER TABLE custom_charsets ADD COLUMN charset_type TEXT NOT NULL DEFAULT 'inline'
    CHECK (charset_type IN ('inline', 'file'));

-- For file charsets: path relative to data_dir (e.g., charsets/{uuid}.hcchr)
ALTER TABLE custom_charsets ADD COLUMN file_path TEXT DEFAULT NULL;

-- MD5 hash of the file for agent sync verification
ALTER TABLE custom_charsets ADD COLUMN file_md5 TEXT DEFAULT NULL;

-- File size in bytes
ALTER TABLE custom_charsets ADD COLUMN file_size BIGINT DEFAULT NULL;

-- Number of unique bytes in the file (1-256) - used for keyspace calculation
ALTER TABLE custom_charsets ADD COLUMN byte_count INT DEFAULT NULL;

-- Make definition nullable (file charsets have no inline definition)
ALTER TABLE custom_charsets ALTER COLUMN definition DROP NOT NULL;

-- Constraint: inline charsets must have definition, file charsets must have file_path + byte_count
ALTER TABLE custom_charsets ADD CONSTRAINT chk_charset_type_fields CHECK (
    (charset_type = 'inline' AND definition IS NOT NULL AND definition != '') OR
    (charset_type = 'file' AND file_path IS NOT NULL AND byte_count IS NOT NULL)
);

-- Add custom_charset_files JSONB to preset_jobs and job_executions
ALTER TABLE preset_jobs ADD COLUMN custom_charset_files JSONB DEFAULT NULL;
ALTER TABLE job_executions ADD COLUMN custom_charset_files JSONB DEFAULT NULL;

-- Seed DES_full.hcchr as a global charset
-- The actual file (256 bytes: 0x00-0xFF) is written by the backend on startup
INSERT INTO custom_charsets (name, description, charset_type, definition, file_path, file_md5, file_size, byte_count, scope)
VALUES (
    'DES Full Charset',
    'All 256 byte values (0x00-0xFF) for DES and NTLMv1 cracking. Standard hashcat DES_full.hcchr charset.',
    'file',
    NULL,
    'charsets/des_full.hcchr',
    'e2c865db4162bed963bfaa9ef6ac18f0',
    256,
    256,
    'global'
);
