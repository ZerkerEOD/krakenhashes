-- The teardown ladder had no idea what an in-flight task costs.
--
-- Hashcat reports status code 6 (all hashes cracked) WHILE STILL RUNNING. The
-- agent forwards it as status:"running", HandleJobProgress puts the task in
-- 'processing', and HashlistCompletionService.completeJob completes the JOB
-- with that task still mid-handshake -- deliberately, so the handshake can
-- finish. The cloud reaper then read job=completed and destroyed the instance
-- with ZERO grace, on the ordinary successful path, while the agent still held
-- cracked passwords in a 500ms flush buffer.
--
-- Those cracks are not delayed, they are gone. RetransmitOutfile and
-- request_crack_retransmit both read <dataDirectory>/outfile/<taskID>.txt --
-- on the disk that was just destroyed -- and applyRecovery books the truncated
-- range as covered, so the keyspace is never re-issued either. The job reads
-- 'completed' and looks perfect.
--
-- The idle rung had the same hole for a different reason, fixed alongside this
-- in cloud_instance_repository.go: its activity signal was GREATEST over
-- assigned_at/started_at/completed_at, none of which move during a chunk, while
-- cloud_chunk_duration_seconds is a 3600s FLOOR for rented agents and
-- cloud_idle_drain_minutes defaults to 5. That destroyed every rented instance
-- five minutes into its first chunk -- observed on real AWS hardware ("no work
-- for 5m41s (idle drain)") and reproduced on the mock provider. The query now
-- also reads job_tasks.last_activity_at; this key covers what that still cannot
-- see: the window after hashcat exits, when only crack batches are moving.
--
-- QUIET TIME, NOT TOTAL WAIT. A healthy crack upload writes every ~500ms and a
-- healthy chunk reports every few seconds, so ten minutes of silence means
-- something is stuck, not busy. That is why this is a freshness test rather
-- than a plain "is anything in flight" boolean: a wedged row, or a stale row
-- from a different job on the same agent (the reaper's in-flight query is
-- agent-scoped on purpose, so Retarget cannot orphan a live handshake), stops
-- suppressing teardown after this long instead of holding a GPU to its TTL.
--
-- 10 minutes sits deliberately between two existing numbers, and tests assert
-- both relationships:
--   > cloud_idle_drain_minutes (5)  -- holding work deserves more patience
--                                      than holding nothing;
--   < services.StaleProcessingTimeout (30 min) -- or this rung waits for a row
--     job_cleanup_service.go is about to abandon anyway.
--
-- Worst case is one grace period of billing per teardown decision: $0.08 at
-- $0.50/hr, $3.67 at $22/hr, and only on an instance that went silent. It
-- cannot compound -- ttl_epoch is written once and ExtendTTL has no callers,
-- and the same epoch is armed in-guest.
--
-- Zero disables the hold and restores the old unconditional teardown. It is the
-- only setting in this group whose zero value can lose data rather than merely
-- waste money.
--
-- Seeded rather than left absent because SystemSettingsRepository.SetSetting is
-- UPDATE-only: a key that does not exist here can never be written by an admin.

INSERT INTO system_settings (key, value, description, data_type)
VALUES (
    'cloud_crack_drain_grace_minutes',
    '10',
    'Before destroying a rented instance, wait up to this many minutes for a task it still owns to finish uploading cracked passwords. Measured from the last time that task was written to, not from when it started, so a working agent is never destroyed and a stuck one is not waited on forever. Must be larger than the idle drain and smaller than 30 minutes. Set to 0 to destroy immediately, which can permanently lose cracks the agent has already found.',
    'integer'
)
ON CONFLICT (key) DO NOTHING;
