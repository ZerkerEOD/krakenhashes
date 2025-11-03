package models

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// LMHashMetadata tracks partial crack status for LM hashes (hash type 3000).
// LM hashes are split into two 16-character halves that are cracked independently.
// This model stores the crack status and password for each half separately.
type LMHashMetadata struct {
	HashID              uuid.UUID      `db:"hash_id"`
	FirstHalfCracked    bool           `db:"first_half_cracked"`
	SecondHalfCracked   bool           `db:"second_half_cracked"`
	FirstHalfPassword   sql.NullString `db:"first_half_password"`
	SecondHalfPassword  sql.NullString `db:"second_half_password"`
	CreatedAt           time.Time      `db:"created_at"`
	UpdatedAt           time.Time      `db:"updated_at"`
}

// IsPartiallyLMCracked returns true if one half is cracked but not both
func (lm *LMHashMetadata) IsPartiallyLMCracked() bool {
	return (lm.FirstHalfCracked || lm.SecondHalfCracked) &&
		!(lm.FirstHalfCracked && lm.SecondHalfCracked)
}

// IsFullyLMCracked returns true if both halves are cracked
func (lm *LMHashMetadata) IsFullyLMCracked() bool {
	return lm.FirstHalfCracked && lm.SecondHalfCracked
}

// GetFullPassword combines both password halves into the complete password
func (lm *LMHashMetadata) GetFullPassword() string {
	first := ""
	second := ""
	if lm.FirstHalfPassword.Valid {
		first = lm.FirstHalfPassword.String
	}
	if lm.SecondHalfPassword.Valid {
		second = lm.SecondHalfPassword.String
	}
	return first + second
}
