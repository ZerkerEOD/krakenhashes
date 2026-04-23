-- Reverse hex charset support

-- Restore --hex-charset into additional_args for any job_executions that had it
-- Note: preset_jobs does not have additional_args column
UPDATE job_executions SET
  additional_args = CASE
    WHEN additional_args IS NULL OR additional_args = '' THEN '--hex-charset'
    ELSE additional_args || ' --hex-charset'
  END
  WHERE hex_charset = true;

-- Drop columns
ALTER TABLE job_executions DROP COLUMN hex_charset;
ALTER TABLE preset_jobs DROP COLUMN hex_charset;
ALTER TABLE custom_charsets DROP CONSTRAINT IF EXISTS chk_hex_inline_only;
ALTER TABLE custom_charsets DROP COLUMN is_hex;
