-- Drop linked_hashlists table and its indexes
DROP INDEX IF EXISTS idx_linked_hashlists_type;
DROP INDEX IF EXISTS idx_linked_hashlists_id2;
DROP TABLE IF EXISTS linked_hashlists;
