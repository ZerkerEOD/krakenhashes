-- Restore the original three-kind constraint.
--
-- Any RunPod rows must go first: the ADD CONSTRAINT below validates existing
-- rows and would fail outright with them present, leaving the table with NO
-- provider check at all -- strictly worse than either state. Deleting them is
-- the honest reading of this rollback, since a provider kind the schema no
-- longer admits cannot be configured, enabled or provisioned from.
--
-- If any cloud_instances row references a RunPod config this DELETE FAILS, and
-- the whole rollback fails with it. That is intentional, not an oversight:
-- cloud_instances.provider_config_id is ON DELETE RESTRICT, and those rows are
-- the billing and teardown record for machines that may still be running. A
-- rollback that quietly discarded them would leave rented GPUs with nothing in
-- the database pointing at them and no way for the reaper to find them. Destroy
-- the instances first, then roll back.

DELETE FROM cloud_provider_configs WHERE provider IN ('runpod', 'runpod_community');

ALTER TABLE cloud_provider_configs
    DROP CONSTRAINT IF EXISTS cloud_provider_configs_provider_check;

ALTER TABLE cloud_provider_configs
    ADD CONSTRAINT cloud_provider_configs_provider_check
    CHECK (provider IN ('vastai', 'aws', 'mock'));
