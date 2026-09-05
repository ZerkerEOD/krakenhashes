-- Add the 'keyspace_estimate_used' notification type.
--
-- Emitted when a job is created but hashcat --keyspace did not finish within
-- keyspace_calculation_timeout_minutes, so base_keyspace fell back to the
-- wordlist's stored word count. The job runs normally — this tells the operator
-- their timeout is too low for that wordlist, which otherwise only shows up in
-- backend logs that are silent whenever DEBUG=false.
--
-- ALTER TYPE ... ADD VALUE is safe inside migrate's transaction here because the
-- new value is only ADDED (not used) in this migration.

ALTER TYPE notification_type ADD VALUE IF NOT EXISTS 'keyspace_estimate_used';
