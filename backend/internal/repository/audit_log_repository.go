package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/google/uuid"
)

// AuditLogRepository handles database operations for audit logs
type AuditLogRepository struct {
	db *db.DB
}

// NewAuditLogRepository creates a new audit log repository
func NewAuditLogRepository(db *db.DB) *AuditLogRepository {
	return &AuditLogRepository{db: db}
}

// Create creates a new audit log entry
func (r *AuditLogRepository) Create(ctx context.Context, log *models.AuditLog) error {
	query := `
		INSERT INTO audit_log (
			id, event_type, severity, user_id, username, user_email,
			title, message, data, source_type, source_id,
			ip_address, user_agent, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id
	`

	// Convert IP address to string for INET type
	var ipStr *string
	if log.IPAddress != nil {
		s := log.IPAddress.String()
		ipStr = &s
	}

	err := r.db.QueryRowContext(ctx, query,
		log.ID,
		log.EventType,
		log.Severity,
		log.UserID,
		log.Username,
		log.UserEmail,
		log.Title,
		log.Message,
		log.Data,
		log.SourceType,
		log.SourceID,
		ipStr,
		log.UserAgent,
		log.CreatedAt,
	).Scan(&log.ID)

	if err != nil {
		return fmt.Errorf("failed to create audit log: %w", err)
	}

	return nil
}

// List retrieves audit logs with pagination and filtering
func (r *AuditLogRepository) List(ctx context.Context, params models.AuditLogListParams) (*models.AuditLogListResponse, error) {
	var whereConditions []string
	var args []interface{}
	argIndex := 1

	// Filter by event types
	if len(params.EventTypes) > 0 {
		placeholders := make([]string, len(params.EventTypes))
		for i, t := range params.EventTypes {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			args = append(args, t)
			argIndex++
		}
		whereConditions = append(whereConditions, fmt.Sprintf("event_type IN (%s)", strings.Join(placeholders, ", ")))
	}

	// Filter by user ID
	if params.UserID != nil {
		whereConditions = append(whereConditions, fmt.Sprintf("user_id = $%d", argIndex))
		args = append(args, *params.UserID)
		argIndex++
	}

	// Filter by severity
	if params.Severity != nil {
		whereConditions = append(whereConditions, fmt.Sprintf("severity = $%d", argIndex))
		args = append(args, *params.Severity)
		argIndex++
	}

	// Filter by date range
	if params.StartDate != nil {
		whereConditions = append(whereConditions, fmt.Sprintf("created_at >= $%d", argIndex))
		args = append(args, *params.StartDate)
		argIndex++
	}

	if params.EndDate != nil {
		whereConditions = append(whereConditions, fmt.Sprintf("created_at <= $%d", argIndex))
		args = append(args, *params.EndDate)
		argIndex++
	}

	// Build WHERE clause
	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_log %s", whereClause)
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("failed to count audit logs: %w", err)
	}

	// Set default and max limits
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Get audit logs
	query := fmt.Sprintf(`
		SELECT id, event_type, severity, user_id, username, user_email,
		       title, message, data, source_type, source_id,
		       ip_address, user_agent, created_at
		FROM audit_log
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIndex, argIndex+1)

	args = append(args, limit, params.Offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list audit logs: %w", err)
	}
	defer rows.Close()

	var auditLogs []models.AuditLog
	for rows.Next() {
		var log models.AuditLog
		var userID sql.NullString
		var sourceType, sourceID sql.NullString
		var ipAddress sql.NullString
		var userAgent sql.NullString

		err := rows.Scan(
			&log.ID,
			&log.EventType,
			&log.Severity,
			&userID,
			&log.Username,
			&log.UserEmail,
			&log.Title,
			&log.Message,
			&log.Data,
			&sourceType,
			&sourceID,
			&ipAddress,
			&userAgent,
			&log.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit log: %w", err)
		}

		// Handle nullable fields
		if userID.Valid {
			uid, _ := uuid.Parse(userID.String)
			log.UserID = &uid
		}
		if sourceType.Valid {
			log.SourceType = sourceType.String
		}
		if sourceID.Valid {
			log.SourceID = sourceID.String
		}
		if ipAddress.Valid {
			ip := net.ParseIP(ipAddress.String)
			if ip != nil {
				log.IPAddress = &ip
			}
		}
		if userAgent.Valid {
			log.UserAgent = userAgent.String
		}

		auditLogs = append(auditLogs, log)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating audit logs: %w", err)
	}

	return &models.AuditLogListResponse{
		AuditLogs: auditLogs,
		Total:     total,
		Limit:     limit,
		Offset:    params.Offset,
	}, nil
}

// GetByID retrieves an audit log by ID
func (r *AuditLogRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.AuditLog, error) {
	log := &models.AuditLog{}
	var userID sql.NullString
	var sourceType, sourceID sql.NullString
	var ipAddress sql.NullString
	var userAgent sql.NullString

	query := `
		SELECT id, event_type, severity, user_id, username, user_email,
		       title, message, data, source_type, source_id,
		       ip_address, user_agent, created_at
		FROM audit_log
		WHERE id = $1
	`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&log.ID,
		&log.EventType,
		&log.Severity,
		&userID,
		&log.Username,
		&log.UserEmail,
		&log.Title,
		&log.Message,
		&log.Data,
		&sourceType,
		&sourceID,
		&ipAddress,
		&userAgent,
		&log.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get audit log: %w", err)
	}

	// Handle nullable fields
	if userID.Valid {
		uid, _ := uuid.Parse(userID.String)
		log.UserID = &uid
	}
	if sourceType.Valid {
		log.SourceType = sourceType.String
	}
	if sourceID.Valid {
		log.SourceID = sourceID.String
	}
	if ipAddress.Valid {
		ip := net.ParseIP(ipAddress.String)
		if ip != nil {
			log.IPAddress = &ip
		}
	}
	if userAgent.Valid {
		log.UserAgent = userAgent.String
	}

	return log, nil
}
