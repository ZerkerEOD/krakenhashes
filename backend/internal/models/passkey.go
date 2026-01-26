package models

import (
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

// UserPasskey represents a WebAuthn/Passkey credential registered by a user
type UserPasskey struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	UserID         uuid.UUID  `json:"user_id" db:"user_id"`
	CredentialID   []byte     `json:"-" db:"credential_id"`
	PublicKey      []byte     `json:"-" db:"public_key"`
	AAGUID         []byte     `json:"-" db:"aaguid"`
	SignCount      uint32     `json:"sign_count" db:"sign_count"`
	Transports     []string   `json:"transports" db:"transports"`
	Name           string     `json:"name" db:"name"`
	BackupEligible bool       `json:"-" db:"backup_eligible"`
	BackupState    bool       `json:"-" db:"backup_state"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at" db:"last_used_at"`
}

// UserPasskeyResponse is the API response for a passkey (without sensitive data)
type UserPasskeyResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// ToResponse converts a UserPasskey to its API response format
func (p *UserPasskey) ToResponse() UserPasskeyResponse {
	return UserPasskeyResponse{
		ID:         p.ID.String(),
		Name:       p.Name,
		CreatedAt:  p.CreatedAt,
		LastUsedAt: p.LastUsedAt,
	}
}

// PendingPasskeyRegistration represents a pending passkey registration challenge
type PendingPasskeyRegistration struct {
	UserID      uuid.UUID `db:"user_id"`
	Challenge   []byte    `db:"challenge"`
	SessionData []byte    `db:"session_data"`
	CreatedAt   time.Time `db:"created_at"`
}

// PendingPasskeyAuthentication represents a pending passkey authentication challenge
type PendingPasskeyAuthentication struct {
	SessionToken string    `db:"session_token"`
	UserID       uuid.UUID `db:"user_id"`
	Challenge    []byte    `db:"challenge"`
	SessionData  []byte    `db:"session_data"`
	CreatedAt    time.Time `db:"created_at"`
}

// WebAuthnSettings represents the WebAuthn configuration stored in auth_settings
type WebAuthnSettings struct {
	RPID        string   `json:"rpId" db:"webauthn_rp_id"`
	RPOrigins   []string `json:"rpOrigins" db:"webauthn_rp_origins"`
	RPDisplayName string `json:"rpDisplayName" db:"webauthn_rp_display_name"`
}

// IsConfigured returns true if WebAuthn is properly configured
func (s *WebAuthnSettings) IsConfigured() bool {
	return s.RPID != "" && len(s.RPOrigins) > 0
}

// WebAuthnUser implements the webauthn.User interface for a KrakenHashes user
type WebAuthnUser struct {
	ID          uuid.UUID
	Username    string
	Email       string
	Credentials []webauthn.Credential
}

// WebAuthnID returns the user's ID as bytes (required by webauthn.User)
func (u *WebAuthnUser) WebAuthnID() []byte {
	return u.ID[:]
}

// WebAuthnName returns the user's name (required by webauthn.User)
func (u *WebAuthnUser) WebAuthnName() string {
	return u.Username
}

// WebAuthnDisplayName returns the user's display name (required by webauthn.User)
func (u *WebAuthnUser) WebAuthnDisplayName() string {
	if u.Email != "" {
		return u.Email
	}
	return u.Username
}

// WebAuthnCredentials returns the user's credentials (required by webauthn.User)
func (u *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.Credentials
}

// PasskeyRegistrationBeginRequest represents the request to begin passkey registration
type PasskeyRegistrationBeginRequest struct {
	Name string `json:"name"`
}

// PasskeyRegistrationBeginResponse represents the response for beginning passkey registration
type PasskeyRegistrationBeginResponse struct {
	Options interface{} `json:"options"`
}

// PasskeyRegistrationFinishRequest represents the request to finish passkey registration
type PasskeyRegistrationFinishRequest struct {
	Name       string      `json:"name"`
	Credential interface{} `json:"credential"`
}

// PasskeyAuthenticationBeginRequest represents the request to begin passkey authentication
type PasskeyAuthenticationBeginRequest struct {
	SessionToken string `json:"sessionToken"`
}

// PasskeyAuthenticationBeginResponse represents the response for beginning passkey authentication
type PasskeyAuthenticationBeginResponse struct {
	Options interface{} `json:"options"`
}

// PasskeyAuthenticationFinishRequest represents the request to finish passkey authentication
type PasskeyAuthenticationFinishRequest struct {
	SessionToken string      `json:"sessionToken"`
	Credential   interface{} `json:"credential"`
}

// PasskeyRenameRequest represents the request to rename a passkey
type PasskeyRenameRequest struct {
	Name string `json:"name"`
}
