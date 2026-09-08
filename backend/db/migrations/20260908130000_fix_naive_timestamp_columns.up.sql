-- Convert the timestamp columns that are compared against wall-clock windows
-- from `timestamp without time zone` to `timestamptz`.
--
-- WHY
--
-- These columns are written from Go as time.Time values carrying a location,
-- but a tz-naive column stores only the wall clock. A backend running in, say,
-- BST therefore writes 13:50 where the database's NOW() reads 12:50 UTC, and
-- every comparison against NOW() is wrong by the UTC offset -- in the direction
-- that makes a fresh row look like it is in the future.
--
-- It has been invisible because the shipped container sets TZ=UTC, so the wall
-- clock and UTC coincide. Any deployment that does not (a bare-metal install, a
-- compose file without the TZ line, a developer running the backend directly)
-- silently gets:
--
--   * agents.sync_started_at        -- the benchmark readiness gate and
--                                      AgentSyncRecovery's stuck-sync sweep
--   * benchmark_requests.*          -- the 5-minute in-flight window that stops
--                                      the dispatcher re-issuing a benchmark
--   * job_executions.last_progress_update -- progress staleness
--
-- The neighbouring columns on the same tables (agents.last_heartbeat,
-- agents.created_at) are already timestamptz, which is what makes this an
-- inconsistency rather than a deliberate choice.
--
-- SAFETY
--
-- `AT TIME ZONE 'UTC'` reinterprets each stored wall clock AS UTC, which is
-- exactly right for existing rows: every one of them was written by a container
-- running TZ=UTC. Rows written by a non-UTC backend were already wrong and are
-- left no worse.
--
-- Scoped to three small tables. Deliberately NOT applied to linked_hashes,
-- lm_hash_metadata or users, whose tz-naive columns are audit timestamps never
-- compared against a moving window -- and linked_hashes in particular can be
-- large enough that a full table rewrite would be a long lock for no benefit.

ALTER TABLE agents
    ALTER COLUMN sync_started_at   TYPE TIMESTAMPTZ USING sync_started_at   AT TIME ZONE 'UTC',
    ALTER COLUMN sync_completed_at TYPE TIMESTAMPTZ USING sync_completed_at AT TIME ZONE 'UTC';

ALTER TABLE benchmark_requests
    ALTER COLUMN requested_at TYPE TIMESTAMPTZ USING requested_at AT TIME ZONE 'UTC',
    ALTER COLUMN completed_at TYPE TIMESTAMPTZ USING completed_at AT TIME ZONE 'UTC';

ALTER TABLE job_executions
    ALTER COLUMN last_progress_update     TYPE TIMESTAMPTZ USING last_progress_update     AT TIME ZONE 'UTC',
    ALTER COLUMN completion_email_sent_at TYPE TIMESTAMPTZ USING completion_email_sent_at AT TIME ZONE 'UTC';
