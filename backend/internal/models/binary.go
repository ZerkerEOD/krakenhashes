package models

import (
	"time"

	"github.com/google/uuid"
)

// BinaryType represents the type of binary (e.g., hashcat, john).
type BinaryType string

const (
	BinaryTypeHashcat BinaryType = "hashcat"
	BinaryTypeJohn    BinaryType = "john"
)

// CompressionType represents the archive compression type.
type CompressionType string

const (
	CompressionType7z    CompressionType = "7z"
	CompressionTypeZip   CompressionType = "zip"
	CompressionTypeTarGz CompressionType = "tar.gz"
	CompressionTypeTarXz CompressionType = "tar.xz"
)

// BinaryVersion mirrors the binary_versions table structure.
type BinaryVersion struct {
	ID                 int             `json:"id" db:"id"` // SERIAL PRIMARY KEY
	BinaryType         BinaryType      `json:"binary_type" db:"binary_type"`
	CompressionType    CompressionType `json:"compression_type" db:"compression_type"`
	SourceURL          string          `json:"source_url" db:"source_url"`
	FileName           string          `json:"file_name" db:"file_name"`
	MD5Hash            string          `json:"md5_hash" db:"md5_hash"`
	FileSize           int64           `json:"file_size" db:"file_size"` // BIGINT
	CreatedAt          time.Time       `json:"created_at" db:"created_at"`
	CreatedBy          uuid.UUID       `json:"created_by" db:"created_by"`
	IsActive           bool            `json:"is_active" db:"is_active"`
	LastVerifiedAt     time.Time       `json:"last_verified_at,omitempty" db:"last_verified_at"`       // Nullable
	VerificationStatus string          `json:"verification_status,omitempty" db:"verification_status"` // Nullable, VARCHAR(50)
}

// BinaryVersionBasic is a subset used for form data lists.
type BinaryVersionBasic struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`       // Using FileName as Name for display
	IsDefault bool   `json:"is_default"` // Whether this is the default binary version
}
