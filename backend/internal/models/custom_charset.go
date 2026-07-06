package models

import (
	"time"

	"github.com/google/uuid"
)

// CustomCharsetScope represents the visibility scope of a saved charset
type CustomCharsetScope string

const (
	CustomCharsetScopeGlobal CustomCharsetScope = "global"
	CustomCharsetScopeUser   CustomCharsetScope = "user"
	CustomCharsetScopeTeam   CustomCharsetScope = "team"
)

// CustomCharsetType represents whether a charset is an inline definition or a binary file
type CustomCharsetType string

const (
	CustomCharsetTypeInline CustomCharsetType = "inline"
	CustomCharsetTypeFile   CustomCharsetType = "file"
)

// CustomCharset represents a saved/reusable charset definition.
// Supports both inline text definitions (e.g., "?u?d") and binary .hcchr charset files.
type CustomCharset struct {
	ID          uuid.UUID          `json:"id" db:"id"`
	Name        string             `json:"name" db:"name"`
	Description string             `json:"description" db:"description"`
	CharsetType CustomCharsetType  `json:"charset_type" db:"charset_type"`
	Definition  *string            `json:"definition,omitempty" db:"definition"` // Inline charsets only; nil for file charsets
	FilePath    *string            `json:"file_path,omitempty" db:"file_path"`   // Relative path from data_dir (e.g., charsets/{uuid}.hcchr)
	FileMD5     *string            `json:"file_md5,omitempty" db:"file_md5"`     // MD5 hash for agent sync verification
	FileSize    *int64             `json:"file_size,omitempty" db:"file_size"`   // File size in bytes
	ByteCount   *int               `json:"byte_count,omitempty" db:"byte_count"` // Number of unique bytes (1-256) for keyspace calculation
	IsHex       bool               `json:"is_hex" db:"is_hex"`                   // True if inline definition is hex-encoded byte pairs
	Scope       CustomCharsetScope `json:"scope" db:"scope"`
	OwnerID     *uuid.UUID         `json:"owner_id,omitempty" db:"owner_id"`
	CreatedBy   *uuid.UUID         `json:"created_by,omitempty" db:"created_by"`
	CreatedAt   time.Time          `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at" db:"updated_at"`
}
