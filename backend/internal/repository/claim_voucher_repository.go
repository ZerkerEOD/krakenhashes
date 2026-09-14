package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db/queries"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// ClaimVoucherRepository handles database operations for claim vouchers
type ClaimVoucherRepository struct {
	db *db.DB
}

// NewClaimVoucherRepository creates a new claim voucher repository
func NewClaimVoucherRepository(db *db.DB) *ClaimVoucherRepository {
	return &ClaimVoucherRepository{db: db}
}

// Create creates a new claim voucher
func (r *ClaimVoucherRepository) Create(ctx context.Context, voucher *models.ClaimVoucher) error {
	err := r.db.QueryRowContext(ctx, queries.CreateClaimVoucher,
		voucher.Code,
		voucher.IsActive,
		voucher.IsContinuous,
		voucher.CreatedByID,
		voucher.CreatedAt,
		voucher.UpdatedAt,
		voucher.ExpiresAt,
		voucher.CloudInstanceID,
	).Scan(&voucher.Code)

	if err != nil {
		return fmt.Errorf("failed to create claim voucher: %w", err)
	}

	return nil
}

// GetByCode retrieves a claim voucher by code
func (r *ClaimVoucherRepository) GetByCode(ctx context.Context, code string) (*models.ClaimVoucher, error) {
	debug.Debug("GetByCode: Looking up voucher with code: %q", code)
	voucher := &models.ClaimVoucher{}
	var createdByUser models.User
	var usedByAgent models.Agent
	var usedByAgentID sql.NullInt64
	var usedAt sql.NullTime
	var createdByUsername, createdByEmail, createdByRole sql.NullString
	var agentID sql.NullInt64
	var agentName, agentStatus sql.NullString
	var cloudInstanceID uuid.NullUUID

	err := r.db.QueryRowContext(ctx, queries.GetClaimVoucherByCode, code).Scan(
		&voucher.Code,
		&voucher.IsActive,
		&voucher.IsContinuous,
		&voucher.CreatedByID,
		&usedByAgentID,
		&usedAt,
		&voucher.CreatedAt,
		&voucher.UpdatedAt,
		&voucher.ExpiresAt,
		&cloudInstanceID,
		&createdByUser.ID,
		&createdByUsername,
		&createdByEmail,
		&createdByRole,
		&agentID,
		&agentName,
		&agentStatus,
	)

	if err == sql.ErrNoRows {
		debug.Debug("GetByCode: No voucher found with code: %q", code)
		return nil, fmt.Errorf("claim voucher not found with code: %s", code)
	} else if err != nil {
		debug.Error("GetByCode: Failed to get voucher: %v", err)
		return nil, fmt.Errorf("failed to get claim voucher: %w", err)
	}

	debug.Debug("GetByCode: Found voucher - Active: %v, Continuous: %v, Used: %v",
		voucher.IsActive, voucher.IsContinuous, usedByAgentID.Valid)

	voucher.UsedAt = usedAt
	voucher.UsedByAgentID = usedByAgentID
	if cloudInstanceID.Valid {
		id := cloudInstanceID.UUID
		voucher.CloudInstanceID = &id
	}

	// Only set the created by user if we have valid data
	if createdByUsername.Valid {
		createdByUser.Username = createdByUsername.String
		createdByUser.Email = createdByEmail.String
		createdByUser.Role = createdByRole.String
		voucher.CreatedBy = &createdByUser
	}

	// Only set the used by agent if we have valid data
	if agentID.Valid && agentName.Valid {
		usedByAgent.ID = int(agentID.Int64)
		usedByAgent.Name = agentName.String
		usedByAgent.Status = agentStatus.String
		voucher.UsedByAgent = &usedByAgent
	}

	return voucher, nil
}

// UseByAgent marks a claim voucher as used by an agent
func (r *ClaimVoucherRepository) UseByAgent(ctx context.Context, code string, agentID int) error {
	now := time.Now()
	result, err := r.db.ExecContext(ctx, queries.UseClaimVoucherByAgent,
		code,
		agentID,
		now,
	)

	if err != nil {
		return fmt.Errorf("failed to use claim voucher: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("claim voucher not found or already used: %s", code)
	}

	return nil
}

// Deactivate deactivates a claim voucher
func (r *ClaimVoucherRepository) Deactivate(ctx context.Context, code string) error {
	result, err := r.db.ExecContext(ctx, queries.DeactivateClaimVoucher, code)
	if err != nil {
		return fmt.Errorf("failed to deactivate claim voucher: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("claim voucher not found: %s", code)
	}

	return nil
}

/*
 * DeactivateForCloudInstance kills any UNREDEEMED voucher bound to an instance.
 *
 * A cloud voucher is a registration credential, minted before the provider is
 * called and handed to a machine that may never exist. When a launch attempt
 * definitively fails, that credential is live until its TTL runs out with
 * nothing left that could legitimately redeem it — and the launch path mints one
 * PER CANDIDATE OFFER, so a provision that walks a run of capacity refusals
 * leaves one behind for each. Measured on this deployment before the fix: 673
 * unredeemed vouchers belonging to failed instances, every one still active.
 *
 * REDEEMED VOUCHERS ARE LEFT ALONE, deliberately. `used_at` already blocks
 * reuse, and the row is the audit link between an agent and the rental it
 * joined; flipping is_active on it would destroy that record for no gain.
 *
 * Returns the number deactivated so callers can log a real figure rather than
 * "probably did something". Not finding one is NOT an error: the ambiguous
 * launch path deliberately leaves vouchers alone, and instances that never got
 * as far as minting have none.
 */
func (r *ClaimVoucherRepository) DeactivateForCloudInstance(ctx context.Context, instanceID uuid.UUID) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE claim_vouchers
		SET is_active = false, updated_at = NOW()
		WHERE cloud_instance_id = $1 AND used_at IS NULL AND is_active = true`, instanceID)
	if err != nil {
		return 0, fmt.Errorf("failed to deactivate vouchers for cloud instance %s: %w", instanceID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to count deactivated vouchers: %w", err)
	}
	return rows, nil
}

/*
 * PurgeExpired deletes vouchers that expired before `before` and were never
 * redeemed.
 *
 * NEVER touches redeemed vouchers, whatever their age: used_by_agent_id is the
 * audit trail tying an agent to the credential it joined with, and it is also a
 * NO ACTION foreign key, so deleting one would fail against any surviving agent
 * anyway.
 *
 * Deliberately does NOT filter on is_active. Deactivation above and expiry here
 * are independent lifecycles — a voucher deactivated on failure is exactly the
 * kind this sweep exists to remove, and requiring is_active would skip every
 * one of them.
 */
func (r *ClaimVoucherRepository) PurgeExpired(ctx context.Context, before time.Time) (int64, error) {
	// claim_voucher_usage.voucher_code is a NO ACTION foreign key. No Go code
	// writes that table today, so it is empty in practice — but clearing the
	// children first costs nothing and means this does not start failing the
	// day someone starts using it.
	if _, err := r.db.ExecContext(ctx, `
		DELETE FROM claim_voucher_usage
		WHERE voucher_code IN (
			SELECT code FROM claim_vouchers
			WHERE expires_at IS NOT NULL AND expires_at < $1 AND used_at IS NULL)`, before); err != nil {
		return 0, fmt.Errorf("failed to purge claim voucher usage rows: %w", err)
	}

	result, err := r.db.ExecContext(ctx, `
		DELETE FROM claim_vouchers
		WHERE expires_at IS NOT NULL AND expires_at < $1 AND used_at IS NULL`, before)
	if err != nil {
		return 0, fmt.Errorf("failed to purge expired claim vouchers: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to count purged claim vouchers: %w", err)
	}
	return rows, nil
}

// ListActive retrieves all active claim vouchers
func (r *ClaimVoucherRepository) ListActive(ctx context.Context) ([]models.ClaimVoucher, error) {
	rows, err := r.db.QueryContext(ctx, queries.ListActiveVouchers)
	if err != nil {
		return nil, fmt.Errorf("failed to list active claim vouchers: %w", err)
	}
	defer rows.Close()

	var vouchers []models.ClaimVoucher
	for rows.Next() {
		var voucher models.ClaimVoucher
		var createdByUser models.User
		var usedByAgent models.Agent
		var usedByAgentID sql.NullInt64
		var usedAt sql.NullTime
		var createdByUsername, createdByEmail, createdByRole sql.NullString
		var agentID sql.NullInt64
		var agentName, agentStatus sql.NullString

		err := rows.Scan(
			&voucher.Code,
			&voucher.IsActive,
			&voucher.IsContinuous,
			&voucher.CreatedByID,
			&usedByAgentID,
			&usedAt,
			&voucher.CreatedAt,
			&voucher.UpdatedAt,
			&voucher.ExpiresAt,
			&createdByUser.ID,
			&createdByUsername,
			&createdByEmail,
			&createdByRole,
			&agentID,
			&agentName,
			&agentStatus,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan claim voucher: %w", err)
		}

		voucher.UsedAt = usedAt
		voucher.UsedByAgentID = usedByAgentID

		// Only set the created by user if we have valid data
		if createdByUsername.Valid {
			createdByUser.Username = createdByUsername.String
			createdByUser.Email = createdByEmail.String
			createdByUser.Role = createdByRole.String
			voucher.CreatedBy = &createdByUser
		}

		// Only set the used by agent if we have valid data
		if agentID.Valid && agentName.Valid {
			usedByAgent.ID = int(agentID.Int64)
			usedByAgent.Name = agentName.String
			usedByAgent.Status = agentStatus.String
			voucher.UsedByAgent = &usedByAgent
		}

		vouchers = append(vouchers, voucher)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating claim vouchers: %w", err)
	}

	return vouchers, nil
}

// ListActiveByUser retrieves active claim vouchers created by a specific user
func (r *ClaimVoucherRepository) ListActiveByUser(ctx context.Context, userID uuid.UUID) ([]models.ClaimVoucher, error) {
	rows, err := r.db.QueryContext(ctx, queries.ListActiveVouchersByUser, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list active claim vouchers by user: %w", err)
	}
	defer rows.Close()

	var vouchers []models.ClaimVoucher
	for rows.Next() {
		var voucher models.ClaimVoucher
		var createdByUser models.User
		var usedByAgent models.Agent
		var usedByAgentID sql.NullInt64
		var usedAt sql.NullTime
		var createdByUsername, createdByEmail, createdByRole sql.NullString
		var agentID sql.NullInt64
		var agentName, agentStatus sql.NullString

		err := rows.Scan(
			&voucher.Code,
			&voucher.IsActive,
			&voucher.IsContinuous,
			&voucher.CreatedByID,
			&usedByAgentID,
			&usedAt,
			&voucher.CreatedAt,
			&voucher.UpdatedAt,
			&voucher.ExpiresAt,
			&createdByUser.ID,
			&createdByUsername,
			&createdByEmail,
			&createdByRole,
			&agentID,
			&agentName,
			&agentStatus,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan claim voucher: %w", err)
		}

		voucher.UsedAt = usedAt
		voucher.UsedByAgentID = usedByAgentID

		if createdByUsername.Valid {
			createdByUser.Username = createdByUsername.String
			createdByUser.Email = createdByEmail.String
			createdByUser.Role = createdByRole.String
			voucher.CreatedBy = &createdByUser
		}

		if agentID.Valid && agentName.Valid {
			usedByAgent.ID = int(agentID.Int64)
			usedByAgent.Name = agentName.String
			usedByAgent.Status = agentStatus.String
			voucher.UsedByAgent = &usedByAgent
		}

		vouchers = append(vouchers, voucher)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating claim vouchers: %w", err)
	}

	return vouchers, nil
}
