-- cloud_chunk_duration_seconds is a FLOOR for rented agents, not an override.
--
-- The key was seeded by 20260822090200 and then read by nothing that could act
-- on it. dispatcher.go applied it and then unconditionally overwrote it with
-- job_executions.chunk_size_seconds, on the documented assumption that "NULL or
-- 0 means fall back to the system setting". That column is INT DEFAULT 900
-- (000049) and its preset/workflow source is NOT NULL (000018), and the preset
-- form fills the system default into the field on blur -- so it is never NULL
-- and never 0, the fallback branch is unreachable, and the override fired on
-- 100% of jobs. Every rented GPU ran the on-prem chunk size while an operator
-- who had set 3600 believed otherwise, re-paying hashcat startup, kernel
-- autotune and wordlist load roughly 3x as often at $0.50-$22/hr.
--
-- The fix makes the cloud value a lower bound rather than a precedence
-- question, because the two cases the schema cannot tell apart -- "the operator
-- chose 1200" and "the create form pre-filled 1200" -- have the same right
-- answer on rented hardware. A job asking for MORE than the floor still keeps
-- its own value; only "shorter on a rented GPU" is overruled, which is the one
-- intent this setting exists to overrule.
--
-- Only the description changes. The value is deliberately left alone so a
-- deployment that already tuned it keeps its choice, and no job row is
-- reinterpreted -- there is nothing to backfill and therefore nothing to guess
-- about jobs created before this. The INSERT covers a database that somehow
-- lacks the row: SystemSettingsRepository.SetSetting is UPDATE-only, so a key
-- that is absent here can never be written from the admin UI.

INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'cloud_chunk_duration_seconds',
    '3600',
    'Minimum target chunk duration for cloud agents, in seconds. A rented GPU is billed while hashcat starts, autotunes kernels and loads the wordlist, so cloud chunks are floored at this value even when the job asks for less. A job asking for MORE keeps its own value. Always clamped back down to the instance''s remaining TTL minus cloud_teardown_slack_seconds. Set to 0 to disable the floor entirely and let every job use its own chunk size on rented hardware.',
    'integer'
)
ON CONFLICT (key) DO NOTHING;

UPDATE system_settings
SET description = 'Minimum target chunk duration for cloud agents, in seconds. A rented GPU is billed while hashcat starts, autotunes kernels and loads the wordlist, so cloud chunks are floored at this value even when the job asks for less. A job asking for MORE keeps its own value. Always clamped back down to the instance''s remaining TTL minus cloud_teardown_slack_seconds. Set to 0 to disable the floor entirely and let every job use its own chunk size on rented hardware.'
WHERE key = 'cloud_chunk_duration_seconds';
