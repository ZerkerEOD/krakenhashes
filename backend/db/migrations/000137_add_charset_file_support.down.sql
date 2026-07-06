-- Remove file charset support

-- Remove custom_charset_files from job tables
ALTER TABLE job_executions DROP COLUMN IF EXISTS custom_charset_files;
ALTER TABLE preset_jobs DROP COLUMN IF EXISTS custom_charset_files;

-- Remove file-type charsets before dropping columns
DELETE FROM custom_charsets WHERE charset_type = 'file';

-- Drop the type constraint
ALTER TABLE custom_charsets DROP CONSTRAINT IF EXISTS chk_charset_type_fields;

-- Remove file-related columns
ALTER TABLE custom_charsets DROP COLUMN IF EXISTS byte_count;
ALTER TABLE custom_charsets DROP COLUMN IF EXISTS file_size;
ALTER TABLE custom_charsets DROP COLUMN IF EXISTS file_md5;
ALTER TABLE custom_charsets DROP COLUMN IF EXISTS file_path;
ALTER TABLE custom_charsets DROP COLUMN IF EXISTS charset_type;

-- Restore definition NOT NULL
ALTER TABLE custom_charsets ALTER COLUMN definition SET NOT NULL;
