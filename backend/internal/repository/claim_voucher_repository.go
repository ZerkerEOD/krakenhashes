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

	err := r.db.QueryRowContext(ctx, queries.GetClaimVoucherByCode, code).Scan(
		&voucher.Code,
		&voucher.IsActive,
		&voucher.IsContinuous,
		&voucher.CreatedByID,
		&usedByAgentID,
		&usedAt,
		&voucher.CreatedAt,
		&voucher.UpdatedAt,
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
