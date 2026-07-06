package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// WordlistType represents the type of wordlist
type WordlistType string

// Wordlist types
const (
	WordlistTypeGeneral     WordlistType = "general"
	WordlistTypeSpecialized WordlistType = "specialized"
	WordlistTypeTargeted    WordlistType = "targeted"
	WordlistTypeCustom      WordlistType = "custom"
)

// WordlistFormat represents the format of a wordlist
type WordlistFormat string

// Wordlist formats
const (
	WordlistFormatPlaintext  WordlistFormat = "plaintext"
	WordlistFormatCompressed WordlistFormat = "compressed"
)

// Wordlist represents the structure of the 'wordlists' table.
// Note: Add other fields from migration 000013 if needed for other contexts.
type Wordlist struct {
	ID                 int       `json:"id" db:"id"`
	Name               string    `json:"name" db:"name"`
	Description        string    `json:"description"`
	WordlistType       string    `json:"wordlist_type"` // e.g., "dictionary", "password", "custom"
	Format             string    `json:"format"`        // e.g., "txt", "gz", "zip"
	FileName           string    `json:"file_name"`
	MD5Hash            string    `json:"md5_hash"`
	FileSize           int64     `json:"file_size" db:"file_size"`
	WordCount          int64     `json:"word_count"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
	CreatedBy          uuid.UUID `json:"created_by" db:"created_by"`
	UpdatedAt          time.Time `json:"updated_at"`
	UpdatedBy          uuid.UUID `json:"updated_by,omitempty"`
	LastVerifiedAt     time.Time `json:"last_verified_at,omitempty"`
	VerificationStatus string    `json:"verification_status"` // e.g., "pending", "verified", "failed"
	IsPotfile          bool      `json:"is_potfile" db:"is_potfile"`
	Tags               []string  `json:"tags,omitempty"`

	// Filtering (GH #40). Populated only for derived/filtered wordlists.
	ParentWordlistID *int            `json:"parent_wordlist_id,omitempty"`
	FilterSpec       *WordlistFilter `json:"filter_spec,omitempty"`
	ParentMD5        string          `json:"parent_md5,omitempty"`
	IsEphemeral      bool            `json:"is_ephemeral"`
	OwnerJobID       *uuid.UUID      `json:"owner_job_id,omitempty"`
	IsStale          bool            `json:"is_stale"`

	// Incremental-regeneration index (GH #40 follow-up). Populated only for
	// derived/filtered wordlists, and only when an incremental append is possible
	// (NULL for compressed parents / before the first generation). See migration
	// 000160 for semantics.
	ParentOffset    *int64  `json:"parent_offset,omitempty"`
	ParentAnchorMD5 *string `json:"parent_anchor_md5,omitempty"`
}

// WordlistFilter describes the criteria used to derive a filtered wordlist from
// a parent wordlist (GH #40). A candidate line is kept only if it satisfies
// every non-empty criterion. Length is measured as a UTF-8 rune count
// (character-based password policy), not bytes.
type WordlistFilter struct {
	MinLength      *int   `json:"min_length,omitempty"`
	MaxLength      *int   `json:"max_length,omitempty"`
	RequireUpper   bool   `json:"require_upper,omitempty"`
	RequireLower   bool   `json:"require_lower,omitempty"`
	RequireDigit   bool   `json:"require_digit,omitempty"`
	RequireSpecial bool   `json:"require_special,omitempty"` // printable ASCII non-alphanumeric
	MinClasses     *int   `json:"min_classes,omitempty"`     // require at least N of the 4 classes
	Regex          string `json:"regex,omitempty"`           // Go RE2 (linear-time, ReDoS-safe)
}

// Validate ensures the filter is well-formed and non-empty. It compiles the
// regex (if any) so callers reject invalid input before generation begins.
func (f *WordlistFilter) Validate() error {
	if f == nil {
		return fmt.Errorf("filter is required")
	}
	if f.MinLength != nil && *f.MinLength < 0 {
		return fmt.Errorf("min_length must be >= 0")
	}
	if f.MaxLength != nil && *f.MaxLength < 0 {
		return fmt.Errorf("max_length must be >= 0")
	}
	if f.MinLength != nil && f.MaxLength != nil && *f.MinLength > *f.MaxLength {
		return fmt.Errorf("min_length cannot be greater than max_length")
	}
	if f.MinClasses != nil && (*f.MinClasses < 1 || *f.MinClasses > 4) {
		return fmt.Errorf("min_classes must be between 1 and 4")
	}
	if f.Regex != "" {
		if _, err := regexp.Compile(f.Regex); err != nil {
			return fmt.Errorf("invalid regex: %w", err)
		}
	}
	if f.IsEmpty() {
		return fmt.Errorf("filter must specify at least one criterion")
	}
	return nil
}

// IsEmpty reports whether the filter would keep every line (no-op filter).
func (f *WordlistFilter) IsEmpty() bool {
	if f == nil {
		return true
	}
	return f.MinLength == nil && f.MaxLength == nil &&
		!f.RequireUpper && !f.RequireLower && !f.RequireDigit && !f.RequireSpecial &&
		f.MinClasses == nil && f.Regex == ""
}

// Value implements driver.Valuer so a filter can be stored directly in a JSONB column.
func (f WordlistFilter) Value() (driver.Value, error) {
	return json.Marshal(f)
}

// Scan implements sql.Scanner so a JSONB column can populate a WordlistFilter.
func (f *WordlistFilter) Scan(src interface{}) error {
	if src == nil {
		return nil
	}
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, f)
	case string:
		return json.Unmarshal([]byte(v), f)
	default:
		return fmt.Errorf("unsupported type for WordlistFilter: %T", src)
	}
}

// CreateFilteredWordlistRequest is the request body for creating a permanent
// filtered wordlist via Wordlist Management.
type CreateFilteredWordlistRequest struct {
	ParentWordlistID int            `json:"parent_wordlist_id" validate:"required"`
	Name             string         `json:"name" validate:"required"`
	Description      string         `json:"description"`
	Filter           WordlistFilter `json:"filter" validate:"required"`
}

// FilterPreviewRequest asks the backend to estimate how many candidates a
// filter would keep by sampling the start of the parent wordlist.
type FilterPreviewRequest struct {
	ParentWordlistID int            `json:"parent_wordlist_id" validate:"required"`
	Filter           WordlistFilter `json:"filter" validate:"required"`
}

// FilterPreviewResponse reports the sampled estimate.
type FilterPreviewResponse struct {
	EstimatedCount  int64   `json:"estimated_count"` // extrapolated to the full parent
	SampledLines    int64   `json:"sampled_lines"`   // how many lines were inspected
	MatchedInSample int64   `json:"matched_in_sample"`
	MatchRate       float64 `json:"match_rate"` // matched/sampled
	ParentCount     int64   `json:"parent_count"`
}

// WordlistBasic is a subset of Wordlist used for simple listings (e.g., form data).
type WordlistBasic struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// WordlistAddRequest represents a request to add a new wordlist
type WordlistAddRequest struct {
	Name         string   `json:"name" validate:"required"`
	Description  string   `json:"description"`
	WordlistType string   `json:"wordlist_type" validate:"required"`
	Format       string   `json:"format" validate:"required"`
	FileName     string   `json:"file_name" validate:"required"`
	MD5Hash      string   `json:"md5_hash" validate:"required"`
	FileSize     int64    `json:"file_size" validate:"required"`
	WordCount    int64    `json:"word_count"`
	Tags         []string `json:"tags"`
}

// WordlistUpdateRequest represents a request to update an existing wordlist
type WordlistUpdateRequest struct {
	Name         string   `json:"name" validate:"required"`
	Description  string   `json:"description"`
	WordlistType string   `json:"wordlist_type" validate:"required"`
	Format       string   `json:"format"`
	Tags         []string `json:"tags"`
}

// WordlistVerifyRequest represents a request to verify a wordlist
type WordlistVerifyRequest struct {
	Status    string `json:"status" validate:"required,oneof=pending verified failed"`
	WordCount *int64 `json:"word_count,omitempty"`
}

// WordlistTagRequest represents a request to add or remove a tag
type WordlistTagRequest struct {
	Tag string `json:"tag" validate:"required"`
}

// WordlistTag represents a tag associated with a wordlist
type WordlistTag struct {
	ID         int       `json:"id" db:"id"`
	WordlistID int       `json:"wordlist_id" db:"wordlist_id"`
	Tag        string    `json:"tag" db:"tag"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	CreatedBy  uuid.UUID `json:"created_by" db:"created_by"`
}

// WordlistAuditLog represents an entry in the wordlist audit log
type WordlistAuditLog struct {
	ID          int       `json:"id" db:"id"`
	WordlistID  int       `json:"wordlist_id" db:"wordlist_id"`
	Action      string    `json:"action" db:"action"`
	PerformedBy uuid.UUID `json:"performed_by" db:"performed_by"`
	PerformedAt time.Time `json:"performed_at" db:"performed_at"`
	Details     []byte    `json:"details" db:"details"`
}

// DeletionImpact represents the impact of deleting a resource (wordlist or rule)
type DeletionImpact struct {
	ResourceID         int                   `json:"resource_id"`
	ResourceType       string                `json:"resource_type"` // "wordlist" or "rule"
	CanDelete          bool                  `json:"can_delete"`
	HasCascadingImpact bool                  `json:"has_cascading_impact"`
	Impact             DeletionImpactDetails `json:"impact"`
	Summary            DeletionImpactSummary `json:"summary"`
}

// DeletionImpactDetails contains the detailed lists of affected entities
type DeletionImpactDetails struct {
	Jobs              []DeletionImpactJob          `json:"jobs"`
	PresetJobs        []DeletionImpactPresetJob    `json:"preset_jobs"`
	WorkflowSteps     []DeletionImpactWorkflowStep `json:"workflow_steps"`
	WorkflowsToDelete []DeletionImpactWorkflow     `json:"workflows_to_delete"`
}

// DeletionImpactSummary contains counts of affected entities
type DeletionImpactSummary struct {
	TotalJobs              int `json:"total_jobs"`
	TotalPresetJobs        int `json:"total_preset_jobs"`
	TotalWorkflowSteps     int `json:"total_workflow_steps"`
	TotalWorkflowsToDelete int `json:"total_workflows_to_delete"`
}

// DeletionImpactJob represents a job that would be deleted
type DeletionImpactJob struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	HashlistName string    `json:"hashlist_name"`
}

// DeletionImpactPresetJob represents a preset job that would be deleted
type DeletionImpactPresetJob struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	AttackMode string    `json:"attack_mode"`
}

// DeletionImpactWorkflowStep represents a workflow step that would be deleted
type DeletionImpactWorkflowStep struct {
	WorkflowID    uuid.UUID `json:"workflow_id"`
	WorkflowName  string    `json:"workflow_name"`
	StepOrder     int       `json:"step_order"`
	PresetJobID   uuid.UUID `json:"preset_job_id"`
	PresetJobName string    `json:"preset_job_name"`
}

// DeletionImpactWorkflow represents a workflow that would be deleted
type DeletionImpactWorkflow struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	StepCount   int       `json:"step_count"`
}

// DeleteResourceRequest represents a request to delete a resource with optional confirmation
type DeleteResourceRequest struct {
	ConfirmID *int `json:"confirm_id,omitempty"`
}
