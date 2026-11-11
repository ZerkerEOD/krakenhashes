-- Restore the slow = TRUE default for md5crypt (-m 500) set by 20260721120000.
UPDATE hash_types SET slow = TRUE WHERE id = 500 AND slow = FALSE;
