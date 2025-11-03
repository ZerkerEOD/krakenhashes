-- Drop linked_hashes table and its indexes
DROP INDEX IF EXISTS idx_linked_hashes_type;
DROP INDEX IF EXISTS idx_linked_hashes_id2;
DROP TABLE IF EXISTS linked_hashes;
