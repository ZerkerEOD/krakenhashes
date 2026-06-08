-- name: CreatePresetJob :one
INSERT INTO preset_jobs (
    name, wordlist_ids, rule_ids, attack_mode, priority, 
    chunk_size_seconds, status_updates_enabled, 
    allow_high_priority_override, binary_version_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: GetPresetJobByID :one
SELECT * FROM preset_jobs
WHERE id = $1 LIMIT 1;

-- name: GetPresetJobByName :one
SELECT * FROM preset_jobs
WHERE name = $1 LIMIT 1;

-- name: ListPresetJobs :many
-- TODO: Add pagination (LIMIT/OFFSET) and sorting parameters
SELECT 
    pj.*,
    bv.file_name as binary_version_name -- Example: Join to get binary name
FROM preset_jobs pj
LEFT JOIN binary_versions bv ON pj.binary_version_id = bv.id
ORDER BY pj.name; -- Default sort, make configurable

-- name: UpdatePresetJob :one
UPDATE preset_jobs
SET 
    name = $2,
    wordlist_ids = $3,
    rule_ids = $4,
    attack_mode = $5,
    priority = $6,
    chunk_size_seconds = $7,
    status_updates_enabled = $8,
    allow_high_priority_override = $9,
    binary_version_id = $10,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeletePresetJob :exec
DELETE FROM preset_jobs
WHERE id = $1;

-- name: ListWordlistsForForm :many
-- Exclude ephemeral __eph__ job-scoped filtered wordlists (GH #40).
SELECT id, name FROM wordlists WHERE is_ephemeral = false ORDER BY name;

-- name: ListRulesForForm :many
SELECT id, name FROM rules ORDER BY name;

-- name: ListBinaryVersionsForForm :many
-- Assuming file_name is a user-friendly identifier
SELECT id, file_name as name FROM binary_versions WHERE is_active = true AND verification_status = 'verified' ORDER BY file_name; 