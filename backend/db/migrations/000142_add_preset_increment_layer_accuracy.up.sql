-- Track whether each preset_increment_layer's effective_keyspace was set from hashcat
-- --total-candidates (accurate, true) vs the mask-math fallback estimator (false).
--
-- Existing rows were populated by the pre-fix estimator and may be wrong (e.g. file-charset
-- slots evaluated as 26). They default to FALSE here so copyPresetIncrementLayers treats
-- them as stale and triggers a background refresh.
ALTER TABLE preset_increment_layers
  ADD COLUMN is_accurate_keyspace BOOLEAN NOT NULL DEFAULT FALSE;
