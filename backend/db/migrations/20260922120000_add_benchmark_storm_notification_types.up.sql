-- Add the two notification types the Go code dispatches but the enum never had.
--
--   benchmark_storm     repeated benchmark failures on one job inside the storm
--                       window (benchmark_storm_threshold /
--                       benchmark_storm_window_minutes)
--   hashlist_malformed  hashcat rejected every line of a hashlist, so the job
--                       is fast-failed instead of cycling the retry cap
--
-- Both are declared in models/notification.go and both were unusable. Every
-- dispatch died at the first INSERT:
--
--   pq: invalid input value for enum notification_type: "benchmark_storm"
--
-- The failure is worse than a missing message. benchmark_storm exists precisely
-- to warn that an agent is failing benchmarks repeatedly — the condition that,
-- ten failures later, marks a whole job "per-tuple hard cap reached". On the
-- deployment that prompted this, three jobs died that way at 21%, 22% and 40%
-- complete while the advisory designed to warn about it could not be delivered
-- to anyone. Two WARNING lines per attempt were the only trace.
--
-- Separate migration from anything that USES these values: ALTER TYPE ... ADD
-- VALUE is only safe inside migrate's transaction when the value is added but
-- not used in the same migration. Same reasoning as 20260822090100.
--
-- Both are registered as auditable events on the Go side, so dispatching one
-- also writes an audit_log row (audit_log.event_type shares this enum).

ALTER TYPE notification_type ADD VALUE IF NOT EXISTS 'benchmark_storm';
ALTER TYPE notification_type ADD VALUE IF NOT EXISTS 'hashlist_malformed';
