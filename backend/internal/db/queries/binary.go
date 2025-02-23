package queries

// Binary version queries
const (
	CreateBinaryVersion = `
		INSERT INTO binary_versions (
			binary_type, compression_type, source_url, 
			file_name, md5_hash, file_size, created_by, is_active, 
			verification_status
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		) RETURNING id, created_at`

	GetBinaryVersion = `
		SELECT 
			id, binary_type, compression_type, source_url,
			file_name, md5_hash, file_size, created_at, created_by,
			is_active, last_verified_at, verification_status
		FROM binary_versions 
		WHERE id = $1`

	ListBinaryVersionsBase = `
		SELECT 
			id, binary_type, compression_type, source_url,
			file_name, md5_hash, file_size, created_at, created_by,
			is_active, last_verified_at, verification_status
		FROM binary_versions 
		WHERE 1=1`

	UpdateBinaryVersion = `
		UPDATE binary_versions SET
			binary_type = $1,
			compression_type = $2,
			source_url = $3,
			file_name = $4,
			md5_hash = $5,
			file_size = $6,
			is_active = $7,
			last_verified_at = $8,
			verification_status = $9
		WHERE id = $10`

	DeleteBinaryVersion = `
		UPDATE binary_versions SET
			is_active = false
		WHERE id = $1`

	GetLatestActiveBinaryVersion = `
		SELECT 
			id, binary_type, compression_type, source_url,
			file_name, md5_hash, file_size, created_at, created_by,
			is_active, last_verified_at, verification_status
		FROM binary_versions 
		WHERE binary_type = $1 
		AND is_active = true 
		AND verification_status = 'verified'
		ORDER BY created_at DESC 
		LIMIT 1`

	CreateBinaryAuditLog = `
		INSERT INTO binary_version_audit_log (
			binary_version_id, action, performed_by, details
		) VALUES (
			$1, $2, $3, $4
		) RETURNING id, performed_at`
)
