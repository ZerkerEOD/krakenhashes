-- Enable needs_processing for LM hash type (3000)
-- This allows the processLM function to extract LM hashes from pwdump format
UPDATE hash_types
SET needs_processing = TRUE
WHERE id = 3000;
