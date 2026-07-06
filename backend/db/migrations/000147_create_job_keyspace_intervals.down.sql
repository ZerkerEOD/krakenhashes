-- Roll back job_keyspace_intervals. The btree_gist extension is left in
-- place because other migrations may want it; dropping an extension is
-- noisy and rarely the right thing on a rollback.

DROP TRIGGER IF EXISTS update_intervals_updated_at ON job_keyspace_intervals;
DROP INDEX IF EXISTS idx_intervals_task;
DROP INDEX IF EXISTS idx_intervals_unit_range;
DROP INDEX IF EXISTS idx_intervals_unit_status;
DROP TABLE IF EXISTS job_keyspace_intervals;
