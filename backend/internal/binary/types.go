package binary

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// BinaryType represents the type of binary (hashcat, john, etc)
type BinaryType string

const (
	BinaryTypeHashcat BinaryType = "hashcat"
	BinaryTypeJohn    BinaryType = "john"
)

// CompressionType represents the compression format of the binary
type CompressionType string

const (
	CompressionType7z    CompressionType = "7z"
	CompressionTypeZip   CompressionType = "zip"
	CompressionTypeTarGz CompressionType = "tar.gz"
	CompressionTypeTarXz CompressionType = "tar.xz"
)

// VerificationStatus represents the current verification state of a binary
type VerificationStatus string

const (
	VerificationStatusPending  VerificationStatus = "pending"
	VerificationStatusVerified VerificationStatus = "verified"
	VerificationStatusFailed   VerificationStatus = "failed"
	VerificationStatusDeleted  VerificationStatus = "deleted"
)

// BinaryVersion represents a stored binary version
type BinaryVersion struct {
	ID                 int64              `json:"id" db:"id"`
	BinaryType         BinaryType         `json:"binary_type" db:"binary_type"`
	CompressionType    CompressionType    `json:"compression_type" db:"compression_type"`
	SourceURL          string             `json:"source_url" db:"source_url"`
	FileName           string             `json:"file_name" db:"file_name"`
	MD5Hash            string             `json:"md5_hash" db:"md5_hash"`
	FileSize           int64              `json:"file_size" db:"file_size"`
	CreatedAt          time.Time          `json:"created_at" db:"created_at"`
	CreatedBy          uuid.UUID          `json:"created_by" db:"created_by"`
	IsActive           bool               `json:"is_active" db:"is_active"`
	IsDefault          bool               `json:"is_default" db:"is_default"`
	LastVerifiedAt     *time.Time         `json:"last_verified_at" db:"last_verified_at"`
	VerificationStatus VerificationStatus `json:"verification_status" db:"verification_status"`
}

// BinaryAuditLog represents an audit log entry for binary operations
type BinaryAuditLog struct {
	ID              int64          `json:"id" db:"id"`
	BinaryVersionID int64          `json:"binary_version_id" db:"binary_version_id"`
	Action          string         `json:"action" db:"action"`
	PerformedBy     uuid.UUID      `json:"performed_by" db:"performed_by"`
	PerformedAt     time.Time      `json:"performed_at" db:"performed_at"`
	Details         map[string]any `json:"details" db:"details"`
}

// Manager defines the interface for binary version management
type Manager interface {
	// AddVersion adds a new binary version
	AddVersion(ctx context.Context, version *BinaryVersion) error

	// GetVersion retrieves a specific binary version
	GetVersion(ctx context.Context, id int64) (*BinaryVersion, error)

	// ListVersions retrieves all binary versions with optional filters
	ListVersions(ctx context.Context, filters map[string]interface{}) ([]*BinaryVersion, error)

	// VerifyVersion verifies the integrity of a binary version
	VerifyVersion(ctx context.Context, id int64) error

	// DownloadBinary downloads a binary from its source URL
	DownloadBinary(ctx context.Context, version *BinaryVersion) error

	// DeleteVersion marks a binary version as inactive with fallback protection
	DeleteVersion(ctx context.Context, id int64) error

	// SetDefaultVersion sets a binary version as the default for its type
	SetDefaultVersion(ctx context.Context, id int64) error

	// GetLatestActive returns the latest active version for a binary type
	GetLatestActive(ctx context.Context, binaryType BinaryType) (*BinaryVersion, error)

	// GetDefault returns the default binary version for a type
	GetDefault(ctx context.Context, binaryType BinaryType) (*BinaryVersion, error)

	// ExtractBinary extracts the binary archive to a local directory for server use
	ExtractBinary(ctx context.Context, id int64) error

	// GetLocalBinaryPath returns the path to the extracted binary for server-side execution
	GetLocalBinaryPath(ctx context.Context, id int64) (string, error)
}

// Store defines the interface for binary version storage operations
type Store interface {
	// CreateVersion creates a new binary version record
	CreateVersion(ctx context.Context, version *BinaryVersion) error

	// GetVersion retrieves a binary version by ID
	GetVersion(ctx context.Context, id int64) (*BinaryVersion, error)

	// ListVersions retrieves binary versions with optional filters
	ListVersions(ctx context.Context, filters map[string]interface{}) ([]*BinaryVersion, error)

	// UpdateVersion updates a binary version record
	UpdateVersion(ctx context.Context, version *BinaryVersion) error

	// DeleteVersion soft deletes a binary version
	DeleteVersion(ctx context.Context, id int64) error

	// GetLatestActive returns the latest active version for a binary type
	GetLatestActive(ctx context.Context, binaryType BinaryType) (*BinaryVersion, error)

	// SetDefault sets a binary version as the default for its type
	SetDefault(ctx context.Context, id int64) error

	// GetDefault returns the default binary version for a type
	GetDefault(ctx context.Context, binaryType BinaryType) (*BinaryVersion, error)

	// CountActiveBinaries returns the count of active binaries for a type
	CountActiveBinaries(ctx context.Context, binaryType BinaryType) (int, error)

	// UpdateReferencesToDefault updates all references from oldID to newID in preset_jobs and job_executions
	UpdateReferencesToDefault(ctx context.Context, oldID, newID int64) error

	// CreateAuditLog creates an audit log entry
	CreateAuditLog(ctx context.Context, log *BinaryAuditLog) error
}

// Config holds configuration for the binary manager
type Config struct {
	DataDir string // Base directory for storing binaries
}
