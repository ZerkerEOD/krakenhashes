-- Restore avg_rule_multiplier (mirrors 000063). The column is recreated empty;
-- no data is backfilled since it is no longer produced by any code path. The
-- multiplication_factor backfill from the up migration is not reverted (the
-- rounded values are strictly more correct than the previously-truncated ones).

ALTER TABLE job_executions
    ADD COLUMN IF NOT EXISTS avg_rule_multiplier NUMERIC(20,10);

COMMENT ON COLUMN job_executions.avg_rule_multiplier IS 'Actual multiplier: effective_keyspace / base_keyspace / rule_count. Used for estimating future tasks.';
