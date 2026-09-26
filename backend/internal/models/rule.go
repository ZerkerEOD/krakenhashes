package models

import (
	"time"

	"github.com/google/uuid"
)

// Verification status constants
const (
	VerificationStatusPending  = "pending"
	VerificationStatusVerified = "verified"
	VerificationStatusFailed   = "failed"
)

// RuleType represents the type of rule
type RuleType string

// Rule types
const (
	RuleTypeHashcat RuleType = "hashcat"
	RuleTypeJohn    RuleType = "john"
	RuleTypeCustom  RuleType = "custom"
)

// Rule represents the structure of the 'rules' table.
// Note: Add other fields from migration 000014 if needed for other contexts.
type Rule struct {
	ID                 int        `json:"id"`
	Name               string     `json:"name"`
	Description        string     `json:"description"`
	RuleType           string     `json:"rule_type"` // e.g., "hashcat", "custom"
	FileName           string     `json:"file_name"`
	MD5Hash            string     `json:"md5_hash"`
	FileSize           int64      `json:"file_size"`
	RuleCount          int64      `json:"rule_count"`
	CreatedAt          time.Time  `json:"created_at"`
	CreatedBy          uuid.UUID  `json:"created_by"`
	UpdatedAt          time.Time  `json:"updated_at"`
	UpdatedBy          uuid.UUID  `json:"updated_by,omitempty"`
	LastVerifiedAt     time.Time  `json:"last_verified_at,omitempty"`
	VerificationStatus string     `json:"verification_status"`     // e.g., "pending", "verified", "failed"
	MissingSince       *time.Time `json:"missing_since,omitempty"` // set when verification_status='failed' because the file is gone from disk (GH #93)
	Tags               []string   `json:"tags,omitempty"`
}

// RuleBasic is a subset of Rule used for simple listings (e.g., form data).
type RuleBasic struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// RuleAddRequest represents a request to add a new rule
type RuleAddRequest struct {
	Name        string   `json:"name" validate:"required"`
	Description string   `json:"description"`
	RuleType    string   `json:"rule_type" validate:"required"`
	FileName    string   `json:"file_name" validate:"required"`
	MD5Hash     string   `json:"md5_hash" validate:"required"`
	FileSize    int64    `json:"file_size" validate:"required"`
	RuleCount   int64    `json:"rule_count"`
	Tags        []string `json:"tags"`
}

// RuleUpdateRequest represents a request to update an existing rule
type RuleUpdateRequest struct {
	Name        string   `json:"name" validate:"required"`
	Description string   `json:"description"`
	RuleType    string   `json:"rule_type" validate:"required"`
	Tags        []string `json:"tags"`
}

// RuleVerifyRequest represents a request to verify a rule
type RuleVerifyRequest struct {
	Status    string `json:"status" validate:"required,oneof=pending verified failed"`
	RuleCount *int64 `json:"rule_count,omitempty"`
}

// RuleTagRequest represents a request to add or remove a tag
type RuleTagRequest struct {
	Tag string `json:"tag" validate:"required"`
}

// RuleTag represents a tag associated with a rule
type RuleTag struct {
	ID        int       `json:"id"`
	RuleID    int       `json:"rule_id"`
	Tag       string    `json:"tag"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy uuid.UUID `json:"created_by"`
}

// RuleAuditLog represents an entry in the rule audit log
type RuleAuditLog struct {
	ID          int       `json:"id"`
	RuleID      int       `json:"rule_id"`
	Action      string    `json:"action"`
	PerformedBy uuid.UUID `json:"performed_by"`
	PerformedAt time.Time `json:"performed_at"`
	Details     []byte    `json:"details"`
}

// RuleWordlistCompatibility represents compatibility information between a rule and a wordlist
type RuleWordlistCompatibility struct {
	ID                 int        `json:"id" db:"id"`
	RuleID             int        `json:"rule_id" db:"rule_id"`
	WordlistID         int        `json:"wordlist_id" db:"wordlist_id"`
	CompatibilityScore float64    `json:"compatibility_score" db:"compatibility_score"`
	Notes              string     `json:"notes" db:"notes"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	CreatedBy          uuid.UUID  `json:"created_by" db:"created_by"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
	UpdatedBy          *uuid.UUID `json:"updated_by,omitempty" db:"updated_by"`
}

// RuleUploadResponse represents the response for a rule upload request
type RuleUploadResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Rule      *Rule  `json:"rule,omitempty"`
}

// RuleFilter represents filter criteria for listing rules
type RuleFilter struct {
	Search             string `json:"search"`
	RuleType           string `json:"rule_type"`
	VerificationStatus string `json:"verification_status"`
	SortBy             string `json:"sort_by"`
	SortOrder          string `json:"sort_order"`
}
