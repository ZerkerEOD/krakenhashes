package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

/*
DashboardRepository backs the personal dashboard: headline counts, the recent
cracks feed and the agent-health panel.

Every query takes a DashboardScope. Team scoping follows the same rule as the
job list: a client is visible when it is assigned to one of the scope's teams
(hashlist → client → client_teams). A nil TeamIDs means "unrestricted" (teams
disabled, or an admin with no team selected); an empty non-nil slice means
"nothing" (fail-closed for a non-admin with no teams).

OwnerUserID (the "Mine" view) additionally narrows jobs to those the user
created and hashlists/cracks to hashlists the user uploaded.
*/

// DashboardScope narrows every dashboard query to what the caller may see.
type DashboardScope struct {
	// TeamIDs restricts hashlist/job/crack data by client team. nil = unrestricted.
	TeamIDs []uuid.UUID
	// AgentIDs restricts the agent panel. nil = unrestricted.
	AgentIDs []int
	// OwnerUserID restricts jobs to job_executions.created_by and hashlists
	// (and their cracks) to hashlists.user_id. nil = no owner filter.
	OwnerUserID *uuid.UUID
}

func (s DashboardScope) teamRestricted() bool  { return s.TeamIDs != nil }
func (s DashboardScope) agentRestricted() bool { return s.AgentIDs != nil }

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// DashboardRepository runs the aggregate queries for the dashboard endpoints.
type DashboardRepository struct {
	db *db.DB
}

func NewDashboardRepository(database *db.DB) *DashboardRepository {
	return &DashboardRepository{db: database}
}

// DashboardJobCounts is the job portion of /api/dashboard/stats.
type DashboardJobCounts struct {
	Running      int `json:"running"`
	Pending      int `json:"pending"`
	Paused       int `json:"paused"`
	Processing   int `json:"processing"`
	Completed24h int `json:"completed_24h"`
	Failed24h    int `json:"failed_24h"`
}

// DashboardHashlistTotals is the hashlist portion of /api/dashboard/stats.
type DashboardHashlistTotals struct {
	Active        int   `json:"active"`
	TotalHashes   int64 `json:"total_hashes"`
	CrackedHashes int64 `json:"cracked_hashes"`
}

// DashboardCrackCounts is the crack portion of /api/dashboard/stats.
type DashboardCrackCounts struct {
	Last24h int64 `json:"last_24h"`
	Last7d  int64 `json:"last_7d"`
}

// teamClause returns the SQL fragment restricting `h` (an alias of hashlists)
// to the scope's teams, appending the array argument. Empty string when
// unrestricted. The caller must already have handled the fail-closed case.
func teamClause(scope DashboardScope, args *[]interface{}) string {
	if !scope.teamRestricted() {
		return ""
	}
	*args = append(*args, pq.Array(uuidStrings(scope.TeamIDs)))
	return fmt.Sprintf(" AND h.client_id IN (SELECT ct.client_id FROM client_teams ct WHERE ct.team_id = ANY($%d::uuid[]))", len(*args))
}

// jobOwnerClause restricts `je` (job_executions) to the scope's owner.
func jobOwnerClause(scope DashboardScope, args *[]interface{}) string {
	if scope.OwnerUserID == nil {
		return ""
	}
	*args = append(*args, *scope.OwnerUserID)
	return fmt.Sprintf(" AND je.created_by = $%d", len(*args))
}

// hashlistOwnerClause restricts `alias` (a hashlists alias) to the scope's owner.
func hashlistOwnerClause(scope DashboardScope, alias string, args *[]interface{}) string {
	if scope.OwnerUserID == nil {
		return ""
	}
	*args = append(*args, *scope.OwnerUserID)
	return fmt.Sprintf(" AND %s.user_id = $%d", alias, len(*args))
}

// GetJobCounts counts active jobs by status plus the last 24h of terminal jobs.
func (r *DashboardRepository) GetJobCounts(ctx context.Context, scope DashboardScope) (DashboardJobCounts, error) {
	var out DashboardJobCounts
	if scope.teamRestricted() && len(scope.TeamIDs) == 0 {
		return out, nil
	}
	args := []interface{}{}
	query := `
		SELECT je.status,
		       COUNT(*) FILTER (WHERE je.status IN ('running','pending','paused','processing','preparing')) AS active,
		       COUNT(*) FILTER (WHERE je.status IN ('completed','failed') AND COALESCE(je.completed_at, je.updated_at) > NOW() - INTERVAL '24 hours') AS recent
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		WHERE je.archived_at IS NULL` + teamClause(scope, &args) + jobOwnerClause(scope, &args) + `
		GROUP BY je.status`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, fmt.Errorf("dashboard job counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var active, recent int
		if err := rows.Scan(&status, &active, &recent); err != nil {
			return out, fmt.Errorf("dashboard job counts scan: %w", err)
		}
		switch status {
		case "running":
			out.Running = active
		case "pending", "preparing":
			out.Pending += active
		case "paused":
			out.Paused = active
		case "processing":
			out.Processing = active
		case "completed":
			out.Completed24h = recent
		case "failed":
			out.Failed24h = recent
		}
	}
	return out, rows.Err()
}

// GetHashlistTotals sums the non-archived hashlists in scope.
func (r *DashboardRepository) GetHashlistTotals(ctx context.Context, scope DashboardScope) (DashboardHashlistTotals, error) {
	var out DashboardHashlistTotals
	if scope.teamRestricted() && len(scope.TeamIDs) == 0 {
		return out, nil
	}
	args := []interface{}{}
	query := `
		SELECT COUNT(*), COALESCE(SUM(h.total_hashes), 0), COALESCE(SUM(h.cracked_hashes), 0)
		FROM hashlists h
		WHERE h.archived_at IS NULL` + teamClause(scope, &args) + hashlistOwnerClause(scope, "h", &args)
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&out.Active, &out.TotalHashes, &out.CrackedHashes); err != nil {
		return out, fmt.Errorf("dashboard hashlist totals: %w", err)
	}
	return out, nil
}

// GetCrackCounts counts hashes cracked in the last 24h / 7d. The only crack
// timestamp on hashes is last_updated, which is set when the crack lands.
func (r *DashboardRepository) GetCrackCounts(ctx context.Context, scope DashboardScope) (DashboardCrackCounts, error) {
	var out DashboardCrackCounts
	if scope.teamRestricted() && len(scope.TeamIDs) == 0 {
		return out, nil
	}
	args := []interface{}{}
	var query string
	if scope.teamRestricted() || scope.OwnerUserID != nil {
		query = `
			SELECT COUNT(DISTINCT hs.id) FILTER (WHERE hs.last_updated > NOW() - INTERVAL '24 hours'),
			       COUNT(DISTINCT hs.id) FILTER (WHERE hs.last_updated > NOW() - INTERVAL '7 days')
			FROM hashes hs
			JOIN hashlist_hashes hh ON hh.hash_id = hs.id
			JOIN hashlists h ON h.id = hh.hashlist_id
			WHERE hs.is_cracked = true
			  AND hs.last_updated > NOW() - INTERVAL '7 days'` + teamClause(scope, &args) + hashlistOwnerClause(scope, "h", &args)
	} else {
		query = `
			SELECT COUNT(*) FILTER (WHERE last_updated > NOW() - INTERVAL '24 hours'),
			       COUNT(*) FILTER (WHERE last_updated > NOW() - INTERVAL '7 days')
			FROM hashes
			WHERE is_cracked = true AND last_updated > NOW() - INTERVAL '7 days'`
	}
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&out.Last24h, &out.Last7d); err != nil {
		return out, fmt.Errorf("dashboard crack counts: %w", err)
	}
	return out, nil
}

// GetAggregateHashRate sums the current speed of running tasks in scope.
func (r *DashboardRepository) GetAggregateHashRate(ctx context.Context, scope DashboardScope) (int64, error) {
	if scope.teamRestricted() && len(scope.TeamIDs) == 0 {
		return 0, nil
	}
	args := []interface{}{}
	query := `
		SELECT COALESCE(SUM(jt.benchmark_speed), 0)
		FROM job_tasks jt
		JOIN job_executions je ON je.id = jt.job_execution_id
		JOIN hashlists h ON h.id = je.hashlist_id
		WHERE jt.status = 'running'` + teamClause(scope, &args) + jobOwnerClause(scope, &args)
	var rate int64
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&rate); err != nil {
		return 0, fmt.Errorf("dashboard hash rate: %w", err)
	}
	return rate, nil
}

// RecentCrack is one row of the recent cracks feed.
type RecentCrack struct {
	HashID       uuid.UUID  `json:"hash_id"`
	Hash         string     `json:"hash"`
	Plaintext    string     `json:"plaintext"`
	Username     *string    `json:"username,omitempty"`
	Domain       *string    `json:"domain,omitempty"`
	CrackedAt    time.Time  `json:"cracked_at"`
	HashlistID   *int64     `json:"hashlist_id,omitempty"`
	HashlistName *string    `json:"hashlist_name,omitempty"`
	JobID        *uuid.UUID `json:"job_id,omitempty"`
	JobName      *string    `json:"job_name,omitempty"`
	AgentID      *int       `json:"agent_id,omitempty"`
	AgentName    *string    `json:"agent_name,omitempty"`
}

// ListRecentCracks returns the newest cracked hashes in scope, with the
// hashlist, job and agent that produced them when known.
func (r *DashboardRepository) ListRecentCracks(ctx context.Context, scope DashboardScope, limit int) ([]RecentCrack, error) {
	if scope.teamRestricted() && len(scope.TeamIDs) == 0 {
		return []RecentCrack{}, nil
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	args := []interface{}{limit}
	teamFilter := ""
	if scope.teamRestricted() {
		args = append(args, pq.Array(uuidStrings(scope.TeamIDs)))
		teamFilter = fmt.Sprintf(" AND hl.client_id IN (SELECT ct.client_id FROM client_teams ct WHERE ct.team_id = ANY($%d::uuid[]))", len(args))
	}
	ownerFilter, ownerOrder := "", ""
	if scope.OwnerUserID != nil {
		// A hash can sit in several hashlists: keep it when any of them is the
		// user's, and label it with the user's hashlist.
		args = append(args, *scope.OwnerUserID)
		n := len(args)
		ownerFilter = fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM hashlist_hashes hh3 JOIN hashlists h3 ON h3.id = hh3.hashlist_id
			WHERE hh3.hash_id = hs.id AND h3.user_id = $%d)`, n)
		ownerOrder = fmt.Sprintf("(hl2.user_id = $%d) DESC, ", n)
	}
	query := `
		SELECT hs.id, COALESCE(hs.original_hash, hs.hash_value), COALESCE(hs.password, ''), hs.username, hs.domain, hs.last_updated,
		       hl.id, hl.name, je.id, je.name, jt.agent_id, a.name
		FROM hashes hs
		LEFT JOIN job_tasks jt ON jt.id = hs.cracked_by_task_id
		LEFT JOIN job_executions je ON je.id = jt.job_execution_id
		LEFT JOIN agents a ON a.id = jt.agent_id
		LEFT JOIN LATERAL (
			SELECT hl2.id, hl2.name, hl2.client_id
			FROM hashlist_hashes hh
			JOIN hashlists hl2 ON hl2.id = hh.hashlist_id
			WHERE hh.hash_id = hs.id
			ORDER BY ` + ownerOrder + `(je.hashlist_id IS NOT NULL AND hl2.id = je.hashlist_id) DESC, hl2.id DESC
			LIMIT 1
		) hl ON true
		WHERE hs.is_cracked = true` + teamFilter + ownerFilter + `
		ORDER BY hs.last_updated DESC
		LIMIT $1`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("dashboard recent cracks: %w", err)
	}
	defer rows.Close()

	out := []RecentCrack{}
	for rows.Next() {
		var c RecentCrack
		var hlID sql.NullInt64
		var hlName, jobName, agentName sql.NullString
		var jobID sql.Null[uuid.UUID]
		var agentID sql.NullInt64
		if err := rows.Scan(&c.HashID, &c.Hash, &c.Plaintext, &c.Username, &c.Domain, &c.CrackedAt,
			&hlID, &hlName, &jobID, &jobName, &agentID, &agentName); err != nil {
			return nil, fmt.Errorf("dashboard recent cracks scan: %w", err)
		}
		if hlID.Valid {
			c.HashlistID = &hlID.Int64
		}
		if hlName.Valid {
			c.HashlistName = &hlName.String
		}
		if jobID.Valid {
			v := jobID.V
			c.JobID = &v
		}
		if jobName.Valid {
			c.JobName = &jobName.String
		}
		if agentID.Valid {
			v := int(agentID.Int64)
			c.AgentID = &v
		}
		if agentName.Valid {
			c.AgentName = &agentName.String
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AgentHealthMetrics is the agent's most recent realtime sample from
// agent_performance_metrics, aggregated across its devices: hottest device,
// mean utilisation, summed hash rate and power.
type AgentHealthMetrics struct {
	GPUUtilization float64   `json:"gpu_utilization"`
	GPUTemp        float64   `json:"gpu_temp"`
	HashRate       float64   `json:"hash_rate"`
	PowerUsage     float64   `json:"power_usage"`
	Timestamp      time.Time `json:"timestamp"`
}

// ListOwnedAgentIDs returns the ids of non-retired agents a user owns (or
// created, when the agent has no explicit owner).
func (r *DashboardRepository) ListOwnedAgentIDs(ctx context.Context, userID uuid.UUID) ([]int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id FROM agents
		WHERE retired_at IS NULL AND (owner_id = $1 OR (owner_id IS NULL AND created_by_id = $1))`, userID)
	if err != nil {
		return nil, fmt.Errorf("dashboard owned agents: %w", err)
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("dashboard owned agents scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AgentHealth is one row of the agent-health panel.
type AgentHealth struct {
	ID              int                 `json:"id"`
	Name            string              `json:"name"`
	Status          string              `json:"status"`
	IsEnabled       bool                `json:"is_enabled"`
	LastHeartbeat   *time.Time          `json:"last_heartbeat,omitempty"`
	Version         string              `json:"version"`
	UpdatePending   bool                `json:"update_pending"`
	UpdateError     *string             `json:"update_error,omitempty"`
	LastError       *string             `json:"last_error,omitempty"`
	OwnerID         *uuid.UUID          `json:"owner_id,omitempty"`
	OwnerUsername   *string             `json:"owner_username,omitempty"`
	CurrentJobID    *uuid.UUID          `json:"current_job_id,omitempty"`
	CurrentJobName  *string             `json:"current_job_name,omitempty"`
	CurrentProgress *float64            `json:"current_job_progress,omitempty"`
	Metrics         *AgentHealthMetrics `json:"metrics,omitempty"`
	Warnings        []string            `json:"warnings"`
}

// ListAgentHealth returns every non-retired agent in scope with its latest
// metrics sample and current job, ordered so agents needing attention come first.
func (r *DashboardRepository) ListAgentHealth(ctx context.Context, scope DashboardScope) ([]AgentHealth, error) {
	if scope.agentRestricted() && len(scope.AgentIDs) == 0 {
		return []AgentHealth{}, nil
	}
	args := []interface{}{}
	idFilter := ""
	if scope.agentRestricted() {
		ids := make([]int64, len(scope.AgentIDs))
		for i, id := range scope.AgentIDs {
			ids[i] = int64(id)
		}
		args = append(args, pq.Array(ids))
		idFilter = " AND a.id = ANY($1::int[])"
	}
	query := `
		SELECT a.id, a.name, a.status, a.is_enabled, a.last_heartbeat, a.version,
		       a.update_pending, a.update_error, a.last_error, a.owner_id, o.username,
		       jt.job_execution_id, je.name, je.overall_progress_percent,
		       m.gpu_utilization, m.gpu_temp, m.hash_rate, m.power_usage, lt.ts
		FROM agents a
		LEFT JOIN users o ON o.id = a.owner_id
		LEFT JOIN LATERAL (
			SELECT t.job_execution_id FROM job_tasks t
			WHERE t.agent_id = a.id AND t.status IN ('assigned','running','processing')
			ORDER BY t.started_at DESC NULLS LAST, t.created_at DESC LIMIT 1
		) jt ON true
		LEFT JOIN job_executions je ON je.id = jt.job_execution_id
		LEFT JOIN LATERAL (
			SELECT MAX(pm.timestamp) AS ts
			FROM agent_performance_metrics pm
			WHERE pm.agent_id = a.id AND pm.aggregation_level = 'realtime'
		) lt ON true
		LEFT JOIN LATERAL (
			SELECT
				AVG(pm.value) FILTER (WHERE pm.metric_type = 'utilization') AS gpu_utilization,
				MAX(pm.value) FILTER (WHERE pm.metric_type = 'temperature') AS gpu_temp,
				SUM(pm.value) FILTER (WHERE pm.metric_type = 'hash_rate')   AS hash_rate,
				SUM(pm.value) FILTER (WHERE pm.metric_type = 'power_usage') AS power_usage
			FROM agent_performance_metrics pm
			WHERE pm.agent_id = a.id AND pm.aggregation_level = 'realtime'
			  AND lt.ts IS NOT NULL AND pm.timestamp > lt.ts - INTERVAL '60 seconds'
		) m ON true
		WHERE a.retired_at IS NULL` + idFilter + `
		ORDER BY
		  CASE a.status WHEN 'error' THEN 0 WHEN 'inactive' THEN 1 WHEN 'pending' THEN 2 WHEN 'updating' THEN 3 WHEN 'disabled' THEN 5 ELSE 4 END,
		  a.name ASC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("dashboard agent health: %w", err)
	}
	defer rows.Close()

	out := []AgentHealth{}
	for rows.Next() {
		var h AgentHealth
		var heartbeat sql.NullTime
		var updateErr, lastErr, ownerName, jobName sql.NullString
		var ownerID sql.NullString
		var jobID sql.Null[uuid.UUID]
		var progress sql.NullFloat64
		var gpu, temp, rate, power sql.NullFloat64
		var ts sql.NullTime
		if err := rows.Scan(&h.ID, &h.Name, &h.Status, &h.IsEnabled, &heartbeat, &h.Version,
			&h.UpdatePending, &updateErr, &lastErr, &ownerID, &ownerName,
			&jobID, &jobName, &progress,
			&gpu, &temp, &rate, &power, &ts); err != nil {
			return nil, fmt.Errorf("dashboard agent health scan: %w", err)
		}
		if heartbeat.Valid {
			t := heartbeat.Time
			h.LastHeartbeat = &t
		}
		if updateErr.Valid && updateErr.String != "" {
			h.UpdateError = &updateErr.String
		}
		if lastErr.Valid && lastErr.String != "" {
			h.LastError = &lastErr.String
		}
		if ownerID.Valid {
			if u, perr := uuid.Parse(ownerID.String); perr == nil {
				h.OwnerID = &u
			}
		}
		if ownerName.Valid && ownerName.String != "" {
			h.OwnerUsername = &ownerName.String
		}
		if jobID.Valid {
			v := jobID.V
			h.CurrentJobID = &v
		}
		if jobName.Valid {
			h.CurrentJobName = &jobName.String
		}
		if progress.Valid {
			h.CurrentProgress = &progress.Float64
		}
		if ts.Valid {
			h.Metrics = &AgentHealthMetrics{
				GPUUtilization: gpu.Float64,
				GPUTemp:        temp.Float64,
				HashRate:       rate.Float64,
				PowerUsage:     power.Float64,
				Timestamp:      ts.Time,
			}
		}
		h.Warnings = agentWarnings(h)
		out = append(out, h)
	}
	return out, rows.Err()
}

// agentWarnings derives the attention flags the panel shows as icons.
func agentWarnings(h AgentHealth) []string {
	w := []string{}
	switch h.Status {
	case "error":
		w = append(w, "error")
	case "inactive":
		w = append(w, "offline")
	}
	if h.UpdateError != nil {
		w = append(w, "update_failed")
	}
	if h.LastHeartbeat != nil && h.Status == "active" && time.Since(*h.LastHeartbeat) > 2*time.Minute {
		w = append(w, "stale_heartbeat")
	}
	if h.Metrics != nil && h.Metrics.GPUTemp >= 85 {
		w = append(w, "gpu_hot")
	}
	return w
}

// AttentionItem is one row of the "needs attention" card: something broken or
// waiting on the user, with the entity it links to.
type AttentionItem struct {
	Kind       string    `json:"kind"`
	Severity   string    `json:"severity"`
	EntityType string    `json:"entity_type"`
	EntityID   string    `json:"entity_id"`
	Name       string    `json:"name"`
	Detail     string    `json:"detail,omitempty"`
	At         time.Time `json:"at"`
}

const attentionLimit = 25

// ListAttention gathers failed and blocked jobs, problem hashlists and agents in
// error for the scope. Errors first, then newest first, capped at 25.
func (r *DashboardRepository) ListAttention(ctx context.Context, scope DashboardScope) ([]AttentionItem, error) {
	out := []AttentionItem{}
	if !scope.teamRestricted() || len(scope.TeamIDs) > 0 {
		jobs, err := r.attentionJobs(ctx, scope)
		if err != nil {
			return nil, err
		}
		out = append(out, jobs...)
		hashlists, err := r.attentionHashlists(ctx, scope)
		if err != nil {
			return nil, err
		}
		out = append(out, hashlists...)
	}
	if !scope.agentRestricted() || len(scope.AgentIDs) > 0 {
		agents, err := r.attentionAgents(ctx, scope)
		if err != nil {
			return nil, err
		}
		out = append(out, agents...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity == "error"
		}
		return out[i].At.After(out[j].At)
	})
	if len(out) > attentionLimit {
		out = out[:attentionLimit]
	}
	return out, nil
}

func (r *DashboardRepository) attentionJobs(ctx context.Context, scope DashboardScope) ([]AttentionItem, error) {
	args := []interface{}{}
	filters := teamClause(scope, &args) + jobOwnerClause(scope, &args)
	query := `
		SELECT 'job_failed', 'error', je.id::text, je.name, COALESCE(je.error_message, ''),
		       COALESCE(je.completed_at, je.updated_at)
		FROM job_executions je
		JOIN hashlists h ON h.id = je.hashlist_id
		WHERE je.status = 'failed' AND je.archived_at IS NULL
		  AND COALESCE(je.completed_at, je.updated_at) > NOW() - INTERVAL '24 hours'` + filters + `
		UNION ALL
		SELECT DISTINCT ON (je.id) 'job_blocked', 'warning', je.id::text, je.name, COALESCE(sd.detail, sd.reason_code),
		       sd.last_seen
		FROM scheduling_diagnostics sd
		JOIN job_executions je ON je.id::text = sd.scope_id
		JOIN hashlists h ON h.id = je.hashlist_id
		WHERE sd.scope = 'job' AND sd.cleared_at IS NULL AND sd.severity IN ('warning', 'error')
		  AND je.status IN ('pending', 'running', 'paused', 'preparing') AND je.archived_at IS NULL` + filters
	return r.scanAttention(ctx, "job", query, args...)
}

func (r *DashboardRepository) attentionHashlists(ctx context.Context, scope DashboardScope) ([]AttentionItem, error) {
	args := []interface{}{}
	query := `
		SELECT CASE
		         WHEN h.status = 'error' THEN 'hashlist_error'
		         WHEN h.status = 'awaiting_validation_decision' THEN 'hashlist_awaiting_decision'
		         ELSE 'hashlist_stuck'
		       END,
		       CASE WHEN h.status = 'error' THEN 'error' ELSE 'warning' END,
		       h.id::text, h.name, COALESCE(h.error_message, ''), h.updated_at
		FROM hashlists h
		WHERE h.archived_at IS NULL
		  AND (h.status IN ('error', 'awaiting_validation_decision')
		       OR (h.status IN ('processing', 'uploading') AND h.updated_at < NOW() - INTERVAL '30 minutes'))` +
		teamClause(scope, &args) + hashlistOwnerClause(scope, "h", &args)
	return r.scanAttention(ctx, "hashlist", query, args...)
}

func (r *DashboardRepository) attentionAgents(ctx context.Context, scope DashboardScope) ([]AttentionItem, error) {
	args := []interface{}{}
	idFilter := ""
	if scope.agentRestricted() {
		ids := make([]int64, len(scope.AgentIDs))
		for i, id := range scope.AgentIDs {
			ids[i] = int64(id)
		}
		args = append(args, pq.Array(ids))
		idFilter = " AND a.id = ANY($1::int[])"
	}
	query := `
		SELECT CASE WHEN a.status = 'error' THEN 'agent_error' ELSE 'agent_update_failed' END,
		       'error', a.id::text, a.name,
		       CASE WHEN a.status = 'error' THEN COALESCE(a.last_error, '') ELSE COALESCE(a.update_error, '') END,
		       COALESCE(a.last_heartbeat, a.updated_at)
		FROM agents a
		WHERE a.retired_at IS NULL
		  AND (a.status = 'error' OR COALESCE(a.update_error, '') <> '')` + idFilter
	return r.scanAttention(ctx, "agent", query, args...)
}

func (r *DashboardRepository) scanAttention(ctx context.Context, entityType, query string, args ...interface{}) ([]AttentionItem, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("dashboard attention (%s): %w", entityType, err)
	}
	defer rows.Close()
	out := []AttentionItem{}
	for rows.Next() {
		item := AttentionItem{EntityType: entityType}
		if err := rows.Scan(&item.Kind, &item.Severity, &item.EntityID, &item.Name, &item.Detail, &item.At); err != nil {
			return nil, fmt.Errorf("dashboard attention (%s) scan: %w", entityType, err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// CrackTrendDay is one bar of the cracks-over-time chart.
type CrackTrendDay struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// CrackTrendHashlist is one row of "top hashlists this week".
type CrackTrendHashlist struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// CrackTrend is GET /api/dashboard/crack-trend.
type CrackTrend struct {
	Days         []CrackTrendDay      `json:"days"`
	TopHashlists []CrackTrendHashlist `json:"top_hashlists"`
}

// GetCrackTrend counts cracks per UTC day for the last `days` days (every day
// present, zero-filled) and the five hashlists with the most cracks in 7 days.
func (r *DashboardRepository) GetCrackTrend(ctx context.Context, scope DashboardScope, days int) (CrackTrend, error) {
	if days < 1 {
		days = 14
	}
	if days > 30 {
		days = 30
	}
	out := CrackTrend{Days: []CrackTrendDay{}, TopHashlists: []CrackTrendHashlist{}}
	empty := scope.teamRestricted() && len(scope.TeamIDs) == 0

	args := []interface{}{days}
	cracked := `
		SELECT hs.id, hs.last_updated FROM hashes hs
		WHERE hs.is_cracked = true AND hs.last_updated >= (NOW() AT TIME ZONE 'UTC')::date - ($1::int - 1)`
	if empty {
		cracked += ` AND false`
	} else if scope.teamRestricted() || scope.OwnerUserID != nil {
		cracked = `
		SELECT DISTINCT hs.id, hs.last_updated FROM hashes hs
		JOIN hashlist_hashes hh ON hh.hash_id = hs.id
		JOIN hashlists h ON h.id = hh.hashlist_id
		WHERE hs.is_cracked = true AND hs.last_updated >= (NOW() AT TIME ZONE 'UTC')::date - ($1::int - 1)` +
			teamClause(scope, &args) + hashlistOwnerClause(scope, "h", &args)
	}
	query := `
		WITH cracked AS (` + cracked + `)
		SELECT to_char(d.day, 'YYYY-MM-DD'), COUNT(c.id)
		FROM generate_series((NOW() AT TIME ZONE 'UTC')::date - ($1::int - 1), (NOW() AT TIME ZONE 'UTC')::date, INTERVAL '1 day') AS d(day)
		LEFT JOIN cracked c ON (c.last_updated AT TIME ZONE 'UTC')::date = d.day::date
		GROUP BY d.day
		ORDER BY d.day`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, fmt.Errorf("dashboard crack trend: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d CrackTrendDay
		if err := rows.Scan(&d.Date, &d.Count); err != nil {
			return out, fmt.Errorf("dashboard crack trend scan: %w", err)
		}
		out.Days = append(out.Days, d)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if empty {
		return out, nil
	}

	topArgs := []interface{}{}
	topQuery := `
		SELECT h.id, h.name, COUNT(DISTINCT hs.id) AS n
		FROM hashes hs
		JOIN hashlist_hashes hh ON hh.hash_id = hs.id
		JOIN hashlists h ON h.id = hh.hashlist_id
		WHERE hs.is_cracked = true AND hs.last_updated > NOW() - INTERVAL '7 days' AND h.archived_at IS NULL` +
		teamClause(scope, &topArgs) + hashlistOwnerClause(scope, "h", &topArgs) + `
		GROUP BY h.id, h.name
		ORDER BY n DESC, h.id DESC
		LIMIT 5`
	topRows, err := r.db.QueryContext(ctx, topQuery, topArgs...)
	if err != nil {
		return out, fmt.Errorf("dashboard top hashlists: %w", err)
	}
	defer topRows.Close()
	for topRows.Next() {
		var h CrackTrendHashlist
		if err := topRows.Scan(&h.ID, &h.Name, &h.Count); err != nil {
			return out, fmt.Errorf("dashboard top hashlists scan: %w", err)
		}
		out.TopHashlists = append(out.TopHashlists, h)
	}
	return out, topRows.Err()
}
