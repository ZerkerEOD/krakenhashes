-- Disable needs_processing for LM hash type (3000)
UPDATE hashcat_hash_types
SET needs_processing = FALSE
WHERE id = 3000;
