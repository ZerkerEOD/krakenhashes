package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// populateSchedulingUnitsIfEnabled is called from CreateJobExecution /
// CreateCustomJobExecution after the job_executions row is inserted. It
// creates the scheduling_units rows the scheduler-v2 cycle reads from.
//
// Errors are logged and swallowed: a partial failure means the unit
// never lands and the job will sit pending with no runner. The caller
// can investigate via the log and either fix the underlying issue
// (e.g., missing wordlist) or delete the job from the UI.
func (s *JobExecutionService) populateSchedulingUnitsIfEnabled(ctx context.Context, jobExec *models.JobExecution) {
	if err := s.populateSchedulingUnits(ctx, jobExec); err != nil {
		debug.Warning("scheduler-v2: populateSchedulingUnits for job %s failed: %v",
			jobExec.ID, err)
	}
}

// ConvertLegacyJobsToV2 is a one-shot startup migration. For every
// job_execution in a non-terminal state that has no scheduling_units
// row, either:
//
//   - Convert it to a v2 job: create scheduling_units, delete in-flight
//     job_tasks, reset progress fields, reset increment layers.
//   - Delete it entirely if its wordlist or rule refs can no longer be
//     resolved (file/record removed from disk or DB). The job could
//     not run under v1 or v2, so it's removed.
//
// Idempotent: re-runs skip jobs that already have scheduling_units.
// The legacy progress columns on job_executions/job_increment_layers
// are reset for UI consistency but v2 never reads them.
func (s *JobExecutionService) ConvertLegacyJobsToV2(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT je.id
		FROM job_executions je
		WHERE je.status IN ('pending', 'running', 'paused')
		  AND NOT EXISTS (
		      SELECT 1 FROM scheduling_units WHERE parent_job_id = je.id
		  )
	`)
	if err != nil {
		return fmt.Errorf("query legacy jobs: %w", err)
	}
	var jobIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if scanErr := rows.Scan(&id); scanErr != nil {
			rows.Close()
			return fmt.Errorf("scan legacy job id: %w", scanErr)
		}
		jobIDs = append(jobIDs, id)
	}
	rows.Close()

	if len(jobIDs) == 0 {
		debug.Info("scheduler-v2 converter: no in-flight legacy jobs to convert")
		return nil
	}
	debug.Info("scheduler-v2 converter: found %d legacy job(s) to process", len(jobIDs))

	var converted, deleted, errored int
	for _, jobID := range jobIDs {
		outcome, convErr := s.convertLegacyJob(ctx, jobID)
		switch {
		case convErr != nil:
			debug.Error("scheduler-v2 converter: job %s failed (left in legacy state): %v", jobID, convErr)
			errored++
		case outcome == "deleted":
			deleted++
		case outcome == "converted":
			converted++
		}
	}

	// Best-effort cleanup of legacy state that no longer references
	// any runnable job. Failures here are non-fatal — at worst the
	// stale rows linger and operators clean up manually.
	if _, mdErr := s.db.ExecContext(ctx, `
		UPDATE agents
		SET metadata = metadata
		    - 'pending_benchmark_job'
		    - 'benchmark_requested_at'
		    - 'forced_benchmark_completed_for_job'
		WHERE metadata ?| array['pending_benchmark_job', 'benchmark_requested_at', 'forced_benchmark_completed_for_job']
	`); mdErr != nil {
		debug.Warning("scheduler-v2 converter: clear stale agent metadata: %v", mdErr)
	}
	if _, brErr := s.db.ExecContext(ctx, `
		DELETE FROM benchmark_requests WHERE completed_at IS NULL
	`); brErr != nil {
		debug.Warning("scheduler-v2 converter: clear stale benchmark_requests: %v", brErr)
	}

	debug.Info("scheduler-v2 converter: %d converted, %d deleted (unresolvable refs), %d errored",
		converted, deleted, errored)
	return nil
}

// convertLegacyJob handles a single job_execution. Returns "converted",
// "deleted", or "" with an error.
func (s *JobExecutionService) convertLegacyJob(ctx context.Context, jobID uuid.UUID) (string, error) {
	jobExec, err := s.jobExecRepo.GetByID(ctx, jobID)
	if err != nil {
		return "", fmt.Errorf("fetch job: %w", err)
	}

	// Try populating scheduling_units. The wordlist/rule resolvers
	// hit the file_repo (DB) and the wordlist/rule managers
	// (filesystem). If any ref no longer resolves, the job can't run
	// — delete it and move on. ON DELETE CASCADE on job_executions
	// cleans up job_tasks, job_increment_layers, and any
	// agent_benchmark_blocklist entries.
	if popErr := s.populateSchedulingUnits(ctx, jobExec); popErr != nil {
		debug.Info("scheduler-v2 converter: deleting unconvertible job %s: %v", jobID, popErr)
		if _, delErr := s.db.ExecContext(ctx, `DELETE FROM job_executions WHERE id = $1`, jobID); delErr != nil {
			return "", fmt.Errorf("delete unconvertible job: %w", delErr)
		}
		return "deleted", nil
	}

	// scheduling_units now exist for this job. Clean up the legacy
	// state so the UI doesn't show stale progress and the v2 sweeper
	// doesn't see ghost "running" task rows.
	//
	// In-flight v1 tasks all have scheduling_unit_id IS NULL. Delete
	// only the non-terminal ones; keep completed/failed/cancelled as
	// audit trail. The job_execution row is the same row v2 will use
	// — we just reset its progress counters.
	if _, txErr := s.db.ExecContext(ctx, `
		DELETE FROM job_tasks
		WHERE job_execution_id = $1
		  AND status IN ('pending','assigned','reconnect_pending','running','processing','processing_error')
	`, jobID); txErr != nil {
		return "", fmt.Errorf("delete in-flight tasks: %w", txErr)
	}

	if _, txErr := s.db.ExecContext(ctx, `
		UPDATE job_executions SET
			status = 'pending',
			processed_keyspace = 0,
			dispatched_keyspace = 0,
			overall_progress_percent = 0,
			consecutive_failures = 0,
			started_at = NULL,
			last_progress_update = NULL
		WHERE id = $1
	`, jobID); txErr != nil {
		return "", fmt.Errorf("reset job_executions: %w", txErr)
	}

	if jobExec.IncrementMode != "" && jobExec.IncrementMode != "off" {
		if _, txErr := s.db.ExecContext(ctx, `
			UPDATE job_increment_layers SET
				status = 'pending',
				dispatched_keyspace = 0,
				processed_keyspace = 0,
				overall_progress_percent = 0,
				started_at = NULL,
				last_progress_update = NULL
			WHERE job_execution_id = $1
		`, jobID); txErr != nil {
			return "", fmt.Errorf("reset job_increment_layers: %w", txErr)
		}
	}

	debug.Info("scheduler-v2 converter: converted job %s", jobID)
	return "converted", nil
}

// populateSchedulingUnits creates scheduling_units rows for a newly
// created job_execution. One row per "atom of scheduling":
//   - Non-increment job: one unit with mask=jobExec.Mask (empty for
//     dict-only modes).
//   - --increment job: one unit per row in job_increment_layers (the
//     legacy code already populated that table before this hook ran).
//
// Each unit carries the same wordlist/rule refs, attack_mode,
// priority, and max_agents as the parent job. Effective keyspace comes
// from the layer (increment) or the job (non-increment).
func (s *JobExecutionService) populateSchedulingUnits(ctx context.Context, jobExec *models.JobExecution) error {
	unitRepo := repository.NewSchedulingUnitRepository(s.db)

	var (
		wordlistRefs []string
		err          error
	)
	if int(jobExec.AttackMode) == 9 {
		// Mode 9 (association) routes its single wordlist through
		// association_wordlist_id, not the generic WordlistIDs
		// array. The path format mirrors the legacy emitter at
		// job_websocket_integration.go:620 —
		// "wordlists/association/<hashlist_id>_<filename>".
		wordlistRefs, err = s.resolveAssociationWordlistRefForV2(ctx, jobExec)
		if err != nil {
			return fmt.Errorf("resolve association wordlist: %w", err)
		}
	} else {
		wordlistRefs, err = s.resolveWordlistRefsForV2(ctx, jobExec.WordlistIDs)
		if err != nil {
			return fmt.Errorf("resolve wordlist refs: %w", err)
		}
	}
	ruleFileRefs, err := s.resolveRuleRefsForV2(ctx, jobExec.RuleIDs)
	if err != nil {
		return fmt.Errorf("resolve rule refs: %w", err)
	}

	isIncrement := jobExec.IncrementMode != "" &&
		jobExec.IncrementMode != "off" &&
		s.jobIncrementLayerRepo != nil

	if isIncrement {
		return s.populateIncrementUnits(ctx, unitRepo, jobExec, wordlistRefs, ruleFileRefs)
	}
	return s.populateSingleUnit(ctx, unitRepo, jobExec, wordlistRefs, ruleFileRefs)
}

func (s *JobExecutionService) populateSingleUnit(
	ctx context.Context,
	unitRepo *repository.SchedulingUnitRepository,
	jobExec *models.JobExecution,
	wordlistRefs []string,
	ruleFileRefs []string,
) error {
	effective := int64(0)
	if jobExec.EffectiveKeyspace != nil {
		effective = *jobExec.EffectiveKeyspace
	} else if jobExec.BaseKeyspace != nil {
		// Fallback to base when effective wasn't computed (mostly for
		// dict-only attacks where they're identical).
		effective = *jobExec.BaseKeyspace
	}

	var maskPtr *string
	if jobExec.Mask != "" {
		m := jobExec.Mask
		maskPtr = &m
	}

	unit := &models.SchedulingUnit{
		ParentJobID:       jobExec.ID,
		LayerIndex:        0,
		Status:            models.SchedulingUnitStatusPending,
		// Priority and MaxAgents are NOT denormalized onto the unit.
		// scheduler/cycle.go buildUnitInfos JOINs job_executions live
		// so operator edits propagate on the next cycle.
		AttackMode:        int(jobExec.AttackMode),
		EffectiveKeyspace: effective,
		// BaseKeyspace denormalized from the parent job — dispatcher uses it
		// to size chunks via v1's multiplier formula (base_per_sec =
		// effective_speed / (effective / base)) AND to tile intervals on the
		// chunkable dimension that hashcat's --skip/--limit actually
		// operates on.
		BaseKeyspace:       jobExec.BaseKeyspace,
		IsAccurateKeyspace: jobExec.IsAccurateKeyspace,
		WordlistRefs:       wordlistRefs,
		RuleFileRefs:       ruleFileRefs,
		MaskString:         maskPtr,
	}

	if err := unitRepo.Create(ctx, unit); err != nil {
		return fmt.Errorf("create single unit: %w", err)
	}
	var baseLog int64
	if unit.BaseKeyspace != nil {
		baseLog = *unit.BaseKeyspace
	}
	debug.Info("scheduler-v2: created scheduling_unit %s for job %s (eff_keyspace=%d, base_keyspace=%d, accurate=%v)",
		unit.ID, jobExec.ID, unit.EffectiveKeyspace, baseLog, unit.IsAccurateKeyspace)
	return nil
}

func (s *JobExecutionService) populateIncrementUnits(
	ctx context.Context,
	unitRepo *repository.SchedulingUnitRepository,
	jobExec *models.JobExecution,
	wordlistRefs []string,
	ruleFileRefs []string,
) error {
	layers, err := s.jobIncrementLayerRepo.GetByJobExecutionID(ctx, jobExec.ID)
	if err != nil {
		return fmt.Errorf("get increment layers: %w", err)
	}
	if len(layers) == 0 {
		return errors.New("increment-mode job has no layers (legacy initialization may have failed)")
	}

	for _, layer := range layers {
		effective := int64(0)
		if layer.EffectiveKeyspace != nil {
			effective = *layer.EffectiveKeyspace
		} else if layer.BaseKeyspace != nil {
			effective = *layer.BaseKeyspace
		}

		mask := layer.Mask
		maskPtr := &mask

		unit := &models.SchedulingUnit{
			ParentJobID:       jobExec.ID,
			LayerIndex:        layer.LayerIndex,
			Status:            models.SchedulingUnitStatusPending,
			// Priority and MaxAgents are read live from job_executions
			// in buildUnitInfos — see comment in the single-unit branch
			// and migration 000153.
			AttackMode:        int(jobExec.AttackMode),
			EffectiveKeyspace: effective,
			// BaseKeyspace per-layer — each increment layer has its own base
			// (different mask length = different total candidate count).
			BaseKeyspace:       layer.BaseKeyspace,
			IsAccurateKeyspace: layer.IsAccurateKeyspace,
			WordlistRefs:       wordlistRefs,
			RuleFileRefs:       ruleFileRefs,
			MaskString:         maskPtr,
		}

		if err := unitRepo.Create(ctx, unit); err != nil {
			return fmt.Errorf("create unit for layer %d: %w", layer.LayerIndex, err)
		}
		var baseLog int64
		if unit.BaseKeyspace != nil {
			baseLog = *unit.BaseKeyspace
		}
		debug.Info("scheduler-v2: created scheduling_unit %s for job %s layer %d (mask=%q, eff=%d, base=%d)",
			unit.ID, jobExec.ID, layer.LayerIndex, mask, effective, baseLog)
	}
	return nil
}

// resolveWordlistRefsForV2 turns the job's IDArray of wordlist IDs
// into relative paths the agent uses (e.g., "wordlists/general/x.txt").
// Reuses the existing absolute-path resolver and strips the data-
// directory prefix so the result matches what the legacy integration
// builds in job_websocket_integration.go.
func (s *JobExecutionService) resolveWordlistRefsForV2(ctx context.Context, ids models.IDArray) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(ids))
	for _, idStr := range ids {
		abs, err := s.resolveWordlistPath(ctx, idStr)
		if err != nil {
			return nil, fmt.Errorf("wordlist %s: %w", idStr, err)
		}
		out = append(out, relPath(s.dataDirectory, abs))
	}
	return out, nil
}

// resolveAssociationWordlistRefForV2 produces the single-entry
// wordlist_refs slice for a -a 9 job. The path matches the legacy
// emitter at job_websocket_integration.go:620,1120 —
// "wordlists/association/<hashlist_id>_<filename>". One entry only:
// the BuildTaskAssignment for -a 9 reads wordlist_refs[0] and mirrors
// it into both WordlistPaths[0] and AssociationWordlistPath on the
// payload.
func (s *JobExecutionService) resolveAssociationWordlistRefForV2(ctx context.Context, jobExec *models.JobExecution) ([]string, error) {
	if jobExec.AssociationWordlistID == nil {
		return nil, errors.New("mode 9 job has no association_wordlist_id")
	}
	if s.assocWordlistRepo == nil {
		return nil, errors.New("association wordlist repository not configured")
	}
	assocWL, err := s.assocWordlistRepo.GetByID(ctx, *jobExec.AssociationWordlistID)
	if err != nil {
		return nil, fmt.Errorf("get association wordlist %s: %w", *jobExec.AssociationWordlistID, err)
	}
	if assocWL == nil {
		return nil, fmt.Errorf("association wordlist %s not found", *jobExec.AssociationWordlistID)
	}
	path := fmt.Sprintf("wordlists/association/%d_%s", jobExec.HashlistID, assocWL.FileName)
	return []string{path}, nil
}

func (s *JobExecutionService) resolveRuleRefsForV2(ctx context.Context, ids models.IDArray) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(ids))
	for _, idStr := range ids {
		abs, err := s.resolveRulePath(ctx, idStr)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", idStr, err)
		}
		out = append(out, relPath(s.dataDirectory, abs))
	}
	return out, nil
}

// relPath returns p stripped of the dataDir prefix (plus a leading
// separator) so the result is what the agent expects. If p doesn't
// start with dataDir, returns p unchanged — defensive against
// non-canonical paths.
func relPath(dataDir, p string) string {
	dataDir = strings.TrimRight(dataDir, "/\\")
	if !strings.HasPrefix(p, dataDir) {
		return p
	}
	rel := strings.TrimPrefix(p, dataDir)
	rel = strings.TrimLeft(rel, "/\\")
	return rel
}
