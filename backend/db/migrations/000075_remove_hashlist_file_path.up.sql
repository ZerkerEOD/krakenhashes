-- Remove file_path column from hashlists table
-- Hashlists are now generated on-demand from the database (single source of truth)
-- No longer storing static files on disk - agents receive fresh uncracked hashes each download
ALTER TABLE hashlists DROP COLUMN IF EXISTS file_path;
