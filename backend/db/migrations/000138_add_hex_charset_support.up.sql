-- Add hex charset support: per-charset is_hex flag and per-job hex_charset boolean

-- Saved charsets: per-charset hex flag (only valid for inline type)
ALTER TABLE custom_charsets ADD COLUMN is_hex BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE custom_charsets ADD CONSTRAINT chk_hex_inline_only
  CHECK (is_hex = false OR charset_type = 'inline');

-- Job-level hex charset flag (system auto-injects --hex-charset when true)
ALTER TABLE preset_jobs ADD COLUMN hex_charset BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE job_executions ADD COLUMN hex_charset BOOLEAN NOT NULL DEFAULT false;

-- Data migration: move --hex-charset from additional_args to the new boolean column
-- This ensures backward compatibility with any existing job_executions that used the flag manually
-- Note: preset_jobs does not have additional_args column, only job_executions does
UPDATE job_executions SET hex_charset = true,
  additional_args = TRIM(REPLACE(additional_args, '--hex-charset', ''))
  WHERE additional_args LIKE '%--hex-charset%';
UPDATE job_executions SET additional_args = NULL
  WHERE additional_args = '';
