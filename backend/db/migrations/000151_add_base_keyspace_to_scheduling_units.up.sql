-- Scheduler-v2 chunking model fix: scheduling_units needs base_keyspace to
-- size chunks correctly. The previous v2 model stored intervals in effective
-- keyspace units (base × rules × salts) and sized chunks as speed × seconds,
-- but hashcat's --skip/--limit for -a 0 operate on base wordlist units. The
-- mismatch caused chunks 2700× larger than the wordlist; hashcat ignored
-- --limit and processed the entire keyspace per "chunk."
--
-- After this migration, intervals are stored in BASE units, dispatcher
-- sizes chunks via the v1 multiplier formula
-- (basePerSec = speed / multiplier; chunkBase = duration × basePerSec),
-- and the agent receives --skip/--limit values that hashcat actually honors.
--
-- base_keyspace lives on job_executions and job_increment_layers already;
-- we denormalize onto scheduling_units so the dispatcher reads from one row
-- without branching on layer vs non-layer at every chunk decision (matches
-- the user's reconciliation preference: "tracking inside the scheduling
-- units makes it easier to calculate per task").

ALTER TABLE scheduling_units ADD COLUMN base_keyspace BIGINT;

-- Backfill non-layer units (layer_index = 0) from job_executions.base_keyspace
UPDATE scheduling_units su
   SET base_keyspace = je.base_keyspace
  FROM job_executions je
 WHERE su.parent_job_id = je.id
   AND su.layer_index   = 0
   AND su.base_keyspace IS NULL;

-- Backfill layer units (layer_index > 0) from job_increment_layers.base_keyspace
UPDATE scheduling_units su
   SET base_keyspace = il.base_keyspace
  FROM job_increment_layers il
 WHERE il.job_execution_id = su.parent_job_id
   AND il.layer_index      = su.layer_index
   AND su.layer_index > 0
   AND su.base_keyspace IS NULL;

-- Sanity check (informational; column stays NULLABLE since rows can predate
-- their parent having base_keyspace populated): how many are still NULL?
-- SELECT count(*) FROM scheduling_units WHERE base_keyspace IS NULL;
