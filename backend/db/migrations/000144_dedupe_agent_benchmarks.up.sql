-- Dedupe agent_benchmarks rows that accumulated under the old
-- NULLS-DISTINCT unique constraint.
--
-- Background: migration 000109 added a regular UNIQUE constraint on
-- (agent_id, attack_mode, hash_type, salt_count). Under Postgres' default
-- NULLS-DISTINCT semantics, two rows whose salt_count is NULL count as
-- distinct, so every successful benchmark for a non-salted hash type
-- (NTLM, MD5, etc.) silently inserted a fresh duplicate row. Production
-- agents had >5 duplicates per (agent, mode, type) tuple within a few days.
--
-- This migration:
--   1. Collapses each (agent_id, attack_mode, hash_type, salt_count) group
--      to a single row, keeping the entry with the newest `updated_at`.
--   2. Replaces the unique constraint with one that uses NULLS NOT DISTINCT
--      so the database itself prevents the regression going forward.
--
-- Requires PostgreSQL 15+ for the `NULLS NOT DISTINCT` syntax. The project
-- already runs 15.x (production confirmed at 15.17).

WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY agent_id, attack_mode, hash_type, COALESCE(salt_count, -1)
               ORDER BY updated_at DESC, created_at DESC, id DESC
           ) AS rn
    FROM agent_benchmarks
)
DELETE FROM agent_benchmarks
WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

ALTER TABLE agent_benchmarks
    DROP CONSTRAINT IF EXISTS agent_benchmarks_agent_attack_hash_salt_key;

ALTER TABLE agent_benchmarks
    ADD CONSTRAINT agent_benchmarks_agent_attack_hash_salt_key
    UNIQUE NULLS NOT DISTINCT (agent_id, attack_mode, hash_type, salt_count);
