-- Rollback: Remove backup flags from user_passkeys table

ALTER TABLE user_passkeys DROP COLUMN IF EXISTS backup_eligible;
ALTER TABLE user_passkeys DROP COLUMN IF EXISTS backup_state;
