package models

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// HashListStatus represents the processing status of a hashlist.
const (
	HashListStatusUploading       = "uploading"  // Initial state upon upload start
	HashListStatusProcessing      = "processing" // State while hashes are being processed and added to DB
	HashListStatusReady           = "ready"      // State when processing is complete and list is usable
	HashListStatusError           = "error"      // State if an error occurred during processing
	HashListStatusDeleting        = "deleting"
	HashListStatusReadyWithErrors = "ready_with_errors" // Processing finished, but some lines had errors
)

// HashList represents a collection of hashes uploaded by a user.
type HashList struct {
	ID                  int64          `json:"id"`                             // Primary key (Changed from UUID)
	Name                string         `json:"name"`                           // User-defined name for the list
	UserID              uuid.UUID      `json:"user_id"`                        // FK to users table
	ClientID            uuid.UUID      `json:"client_id"`                      // Optional FK to clients table (nullable in DB)
	ClientName          *string        `json:"clientName,omitempty"`           // Optional Client Name (from JOIN)
	HashTypeID          int            `json:"hash_type_id"`                   // FK to hash_types table
	TotalHashes         int            `json:"total_hashes"`                   // Total number of hashes in the list
	CrackedHashes       int            `json:"cracked_hashes"`                 // Number of hashes found cracked
	Status              string         `json:"status"`                         // Processing status (uploading, processing, ready, error)
	ErrorMessage        sql.NullString `json:"error_message"`                  // Use sql.NullString to handle NULL
	ExcludeFromPotfile  bool           `json:"exclude_from_potfile"`           // Flag to exclude cracked passwords from potfile
	OriginalFilePath    *string        `json:"original_file_path,omitempty"`   // Path to original uploaded file for association attacks
	HasMixedWorkFactors bool           `json:"has_mixed_work_factors"`         // Warning flag if hashes have different work factors
	CreatedAt           time.Time      `json:"createdAt"`                      // Timestamp of creation - Use camelCase
	UpdatedAt           time.Time      `json:"updatedAt"`                      // Timestamp of last update - Use camelCase
}

// Hash represents a single hash entry in the system.
type Hash struct {
	ID           uuid.UUID `json:"id"`                 // Primary key
	HashValue    string    `json:"hash_value"`         // The actual hash string to be cracked
	OriginalHash string    `json:"original_hash"`      // The original hash string from the input file
	Username     *string   `json:"username,omitempty"` // Optional username extracted from the original hash
	Domain       *string   `json:"domain,omitempty"`   // Optional domain extracted from formats like DOMAIN\user or user@domain
	HashTypeID   int       `json:"hash_type_id"`          // FK to hash_types table
	IsCracked    bool      `json:"is_cracked"`            // Flag indicating if the hash is cracked
	Password     *string   `json:"password,omitempty"`    // The cracked password (if is_cracked is true)
	LastUpdated  time.Time `json:"last_updated"`          // Timestamp of the last update (e.g., when cracked)

	// LM hash partial crack status (only populated for hash_type_id 3000)
	IsPartiallyLMCracked bool    `json:"is_partially_lm_cracked"` // True if only one half of the LM hash is cracked
	LMFirstHalfPassword  *string `json:"lm_first_half_password,omitempty"`  // Password for first 7 chars (if first half cracked)
	LMSecondHalfPassword *string `json:"lm_second_half_password,omitempty"` // Password for second 7 chars (if second half cracked)
}

// HashType represents a type of hash algorithm recognized by the system.
type HashType struct {
	ID              int     `json:"id"`                         // Primary key (e.g., hashcat mode number)
	Name            string  `json:"name"`                       // Common name (e.g., "MD5", "NTLM")
	Description     *string `json:"description,omitempty"`      // Description of the hash type (pointer to handle NULL)
	Example         *string `json:"example,omitempty"`          // Example hash format (pointer to handle NULL)
	NeedsProcessing bool    `json:"needs_processing"`           // Flag if special processing is needed before cracking (e.g., NTLM)
	ProcessingLogic *string `json:"processing_logic,omitempty"` // Description or identifier for the processing logic (pointer to handle NULL)
	IsEnabled       bool    `json:"is_enabled"`                 // Whether this hash type is currently supported/enabled
	Slow            bool    `json:"slow"`                       // Flag indicating if this is a slow hash algorithm (computationally expensive)
}

// Client represents a client or engagement associated with hashlists.
type Client struct {
	ID                  uuid.UUID `json:"id"`                            // Primary key
	Name                string    `json:"name"`                          // Client name (unique)
	Description         *string   `json:"description,omitempty"`         // Optional description (Use pointer for optional field)
	ContactInfo         *string   `json:"contactInfo,omitempty"`         // Optional contact information (Use pointer for optional field)
	DataRetentionMonths *int      `json:"dataRetentionMonths,omitempty"` // Use pointer for nullable INT (Keep forever=0, Use Default=NULL)
	ExcludeFromPotfile  bool      `json:"exclude_from_potfile"`          // Flag to exclude cracked passwords from potfile
	CreatedAt           time.Time `json:"createdAt"`                     // Timestamp of creation
	UpdatedAt           time.Time `json:"updatedAt"`                     // Timestamp of last update
	CrackedCount        *int      `json:"cracked_count,omitempty"`       // Count of cracked hashes for this client (computed field)
}

// HashListHash represents the many-to-many relationship between hashlists and hashes.
type HashListHash struct {
	HashlistID int64     `json:"hashlist_id"` // FK to hashlists table (Changed from UUID)
	HashID     uuid.UUID `json:"hash_id"`     // FK to hashes table
}

// HashSearchResult represents the result of searching for a specific hash.
// It includes the hash details and the hashlists it belongs to (for the requesting user).
type HashSearchResult struct {
	Hash
	Hashlists []HashlistInfo `json:"hashlists"` // List of hashlists this hash belongs to
}

// HashlistInfo provides basic info about a hashlist for search results.
type HashlistInfo struct {
	ID   int64  `json:"id"`   // Hashlist ID (Changed from UUID)
	Name string `json:"name"` // Hashlist Name
}

// LinkedHashlistDetection represents the detection results for LM/NTLM hashes
type LinkedHashlistDetection struct {
	HasBothTypes   bool   `json:"has_both_types"`    // Whether file contains both LM and NTLM hashes
	LMCount        int    `json:"lm_count"`          // Count of LM hashes (excluding blank hashes)
	NTLMCount      int    `json:"ntlm_count"`        // Count of NTLM hashes
	BlankLMCount   int    `json:"blank_lm_count"`    // Count of blank LM hashes (AAD3B435B51404EE)
	TotalLines     int    `json:"total_lines"`       // Total lines processed
	SampleLine     string `json:"sample_line"`       // Sample line for format verification
	DetectedFormat string `json:"detected_format"`   // Format detected (e.g., "pwdump")
}

// LinkedHashlistCreationRequest represents the request to create linked hashlists
type LinkedHashlistCreationRequest struct {
	Name               string    `json:"name" binding:"required"`
	ClientID           uuid.UUID `json:"client_id"`
	ExcludeFromPotfile bool      `json:"exclude_from_potfile"`
	CreateLinked       bool      `json:"create_linked"`       // Whether to create linked hashlists
	LMHashlistName     string    `json:"lm_hashlist_name"`   // Optional custom name for LM hashlist
	NTLMHashlistName   string    `json:"ntlm_hashlist_name"` // Optional custom name for NTLM hashlist
}

// AssociationWordlist represents a wordlist uploaded for association attacks.
// Association wordlists are tied to a specific hashlist (not job) and can be reused
// across multiple jobs with different rules.
type AssociationWordlist struct {
	ID         uuid.UUID `json:"id"`
	HashlistID int64     `json:"hashlist_id"`
	FilePath   string    `json:"-"`              // Internal path, not exposed to API
	FileName   string    `json:"file_name"`
	FileSize   int64     `json:"file_size"`
	LineCount  int64     `json:"line_count"`
	MD5Hash    string    `json:"md5_hash"`
	CreatedAt  time.Time `json:"created_at"`
}
