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

// CustomCharset represents a saved/reusable charset definition
type CustomCharset struct {
	ID          uuid.UUID          `json:"id" db:"id"`
	Name        string             `json:"name" db:"name"`
	Description string             `json:"description" db:"description"`
	Definition  string             `json:"definition" db:"definition"`
	Scope       CustomCharsetScope `json:"scope" db:"scope"`
	OwnerID     *uuid.UUID         `json:"owner_id,omitempty" db:"owner_id"`
	CreatedBy   *uuid.UUID         `json:"created_by,omitempty" db:"created_by"`
	CreatedAt   time.Time          `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at" db:"updated_at"`
}
