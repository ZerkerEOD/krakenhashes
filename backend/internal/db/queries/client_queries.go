package queries

// --- Client Query Constants ---

const CreateClientQuery = `
INSERT INTO clients (id, name, description, contact_info, data_retention_months, exclude_from_potfile, enable_client_potfile, contribute_to_global_potfile, remove_passwords_on_hashlist_delete, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
`

const GetClientByIDQuery = `
SELECT id, name, description, contact_info, data_retention_months, exclude_from_potfile, enable_client_potfile, contribute_to_global_potfile, remove_passwords_on_hashlist_delete, created_at, updated_at
FROM clients
WHERE id = $1
`

const ListClientsQuery = `
SELECT id, name, description, contact_info, data_retention_months, exclude_from_potfile, enable_client_potfile, contribute_to_global_potfile, remove_passwords_on_hashlist_delete, created_at, updated_at
FROM clients
ORDER BY name ASC
`

const UpdateClientQuery = `
UPDATE clients
SET name = $1, description = $2, contact_info = $3, data_retention_months = $4, exclude_from_potfile = $5, enable_client_potfile = $6, contribute_to_global_potfile = $7, remove_passwords_on_hashlist_delete = $8, updated_at = $9
WHERE id = $10
`

const DeleteClientQuery = `DELETE FROM clients WHERE id = $1`

const GetClientByNameQuery = `
SELECT id, name, description, contact_info, data_retention_months, exclude_from_potfile, enable_client_potfile, contribute_to_global_potfile, remove_passwords_on_hashlist_delete, created_at, updated_at
FROM clients
WHERE name = $1
`

const SearchClientsQuery = `
SELECT id, name, description, contact_info, data_retention_months, exclude_from_potfile, enable_client_potfile, contribute_to_global_potfile, remove_passwords_on_hashlist_delete, created_at, updated_at
FROM clients
WHERE name ILIKE $1 OR description ILIKE $1
ORDER BY name ASC
LIMIT 50
`

// ListClientsWithCrackedCountsQuery retrieves all clients with their cracked hash counts
// Uses a subquery for efficiency - avoids scanning entire hashes table for each client
const ListClientsWithCrackedCountsQuery = `
SELECT
    c.id,
    c.name,
    c.description,
    c.contact_info,
    c.data_retention_months,
    c.exclude_from_potfile,
    c.enable_client_potfile,
    c.contribute_to_global_potfile,
    c.remove_passwords_on_hashlist_delete,
    c.created_at,
    c.updated_at,
    COALESCE(cc.cracked_count, 0) as cracked_count
FROM clients c
LEFT JOIN (
    SELECT hl.client_id, COUNT(DISTINCT h.id) as cracked_count
    FROM hashlists hl
    JOIN hashlist_hashes hh ON hh.hashlist_id = hl.id
    JOIN hashes h ON h.id = hh.hash_id AND h.is_cracked = true
    WHERE hl.client_id IS NOT NULL
    GROUP BY hl.client_id
) cc ON cc.client_id = c.id
ORDER BY c.name ASC
`
