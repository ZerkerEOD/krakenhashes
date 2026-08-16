package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// CloudProviderRepository owns cloud_provider_configs.
type CloudProviderRepository struct {
	db *db.DB
}

// NewCloudProviderRepository creates a repository.
func NewCloudProviderRepository(database *db.DB) *CloudProviderRepository {
	return &CloudProviderRepository{db: database}
}

const cloudProviderColumns = `
	id, provider, name, enabled, credentials_encrypted, settings,
	max_concurrent_instances, max_instance_hourly_cents,
	vpn_provider, vpn_credential_kind, vpn_credential_encrypted,
	vpn_credential_expires_at, vpn_tag_or_group, backend_vpn_host,
	third_party_ack_at, third_party_ack_by, created_at, updated_at`

func scanCloudProvider(s interface{ Scan(...interface{}) error }) (*models.CloudProviderConfig, error) {
	var c models.CloudProviderConfig
	var creds, vpnCred, vpnProvider, vpnKind, vpnTag, backendHost sql.NullString
	var ackBy uuid.NullUUID

	err := s.Scan(
		&c.ID, &c.Provider, &c.Name, &c.Enabled, &creds, &c.Settings,
		&c.MaxConcurrentInstances, &c.MaxInstanceHourlyCents,
		&vpnProvider, &vpnKind, &vpnCred,
		&c.VPNCredentialExpiresAt, &vpnTag, &backendHost,
		&c.ThirdPartyAckAt, &ackBy, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	c.CredentialsEncrypted = creds.String
	c.VPNCredentialEncrypted = vpnCred.String
	c.VPNProvider = models.VPNProvider(vpnProvider.String)
	c.VPNCredentialKind = models.VPNCredentialKind(vpnKind.String)
	c.VPNTagOrGroup = vpnTag.String
	c.BackendVPNHost = backendHost.String
	if ackBy.Valid {
		c.ThirdPartyAckBy = &ackBy.UUID
	}
	return &c, nil
}

// GetByID retrieves one provider configuration.
func (r *CloudProviderRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.CloudProviderConfig, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+cloudProviderColumns+` FROM cloud_provider_configs WHERE id = $1`, id)
	c, err := scanCloudProvider(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("cloud provider config %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cloud provider config: %w", err)
	}
	return c, nil
}

// List returns all provider configurations.
func (r *CloudProviderRepository) List(ctx context.Context) ([]*models.CloudProviderConfig, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+cloudProviderColumns+` FROM cloud_provider_configs ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("failed to list cloud provider configs: %w", err)
	}
	defer rows.Close()

	var out []*models.CloudProviderConfig
	for rows.Next() {
		c, err := scanCloudProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cloud provider config: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListEnabled returns provider configurations that may be used right now.
func (r *CloudProviderRepository) ListEnabled(ctx context.Context) ([]*models.CloudProviderConfig, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+cloudProviderColumns+` FROM cloud_provider_configs WHERE enabled = true ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled cloud provider configs: %w", err)
	}
	defer rows.Close()

	var out []*models.CloudProviderConfig
	for rows.Next() {
		c, err := scanCloudProvider(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cloud provider config: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

/*
 * Upsert creates or updates a provider configuration.
 *
 * Encrypted secrets follow the SSO pattern: written only when non-empty, so a
 * PUT that omits them leaves the stored ciphertext alone. Without this an
 * admin editing an unrelated field would silently blank the credentials.
 */
func (r *CloudProviderRepository) Upsert(ctx context.Context, c *models.CloudProviderConfig) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	settings := c.Settings
	if settings == nil {
		settings = models.JSONMap{}
	}

	err := r.db.QueryRowContext(ctx, `
		INSERT INTO cloud_provider_configs (
			id, provider, name, enabled, credentials_encrypted, settings,
			max_concurrent_instances, max_instance_hourly_cents,
			vpn_provider, vpn_credential_kind, vpn_credential_encrypted,
			vpn_credential_expires_at, vpn_tag_or_group, backend_vpn_host
		) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),$12,NULLIF($13,''),NULLIF($14,''))
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			enabled = EXCLUDED.enabled,
			-- COALESCE keeps the existing secret when the input omits it.
			credentials_encrypted = COALESCE(EXCLUDED.credentials_encrypted, cloud_provider_configs.credentials_encrypted),
			settings = EXCLUDED.settings,
			max_concurrent_instances = EXCLUDED.max_concurrent_instances,
			max_instance_hourly_cents = EXCLUDED.max_instance_hourly_cents,
			vpn_provider = EXCLUDED.vpn_provider,
			vpn_credential_kind = EXCLUDED.vpn_credential_kind,
			vpn_credential_encrypted = COALESCE(EXCLUDED.vpn_credential_encrypted, cloud_provider_configs.vpn_credential_encrypted),
			vpn_credential_expires_at = EXCLUDED.vpn_credential_expires_at,
			vpn_tag_or_group = EXCLUDED.vpn_tag_or_group,
			backend_vpn_host = EXCLUDED.backend_vpn_host,
			updated_at = NOW()
		RETURNING created_at, updated_at`,
		c.ID, c.Provider, c.Name, c.Enabled, c.CredentialsEncrypted, settings,
		c.MaxConcurrentInstances, c.MaxInstanceHourlyCents,
		string(c.VPNProvider), string(c.VPNCredentialKind), c.VPNCredentialEncrypted,
		c.VPNCredentialExpiresAt, c.VPNTagOrGroup, c.BackendVPNHost,
	).Scan(&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert cloud provider config: %w", err)
	}
	return nil
}

// RecordThirdPartyAck stores the acknowledgement that a provider places client
// data on machines the operator does not control.
func (r *CloudProviderRepository) RecordThirdPartyAck(ctx context.Context, id, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cloud_provider_configs
		SET third_party_ack_at = NOW(), third_party_ack_by = $2, updated_at = NOW()
		WHERE id = $1`, id, userID)
	if err != nil {
		return fmt.Errorf("failed to record third-party acknowledgement: %w", err)
	}
	return nil
}

// Delete removes a provider configuration. The FK from cloud_instances is
// RESTRICT, so this fails while any instance still references it — deliberately,
// since deleting the config would orphan live rented hardware.
func (r *CloudProviderRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM cloud_provider_configs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete cloud provider config (live instances may still reference it): %w", err)
	}
	return nil
}
