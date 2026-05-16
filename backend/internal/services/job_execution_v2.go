package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// populateSchedulingUnitsIfEnabled is the Phase E hook called from
// CreateJobExecution / CreateCustomJobExecution. When
// SCHEDULER_V2_ENABLED=true at runtime, it creates one or more
// scheduling_units rows pointing at the just-created job_execution so
// the scheduler-v2 cycle (cycle.go) picks it up. The legacy scheduler
// is taught (via the NOT EXISTS clause in GetJobsWithPendingWork) to
// skip jobs that have any scheduling_units rows, so each new job is
// owned by exactly one scheduler.
//
// Errors are logged as warnings and swallowed: a partial failure means
// the v2 unit never lands, and the legacy scheduler will pick up the
// job by default. Better to leave the job runnable on legacy than to
// fail job creation entirely on a v2 wiring bug.
func (s *JobExecutionService) populateSchedulingUnitsIfEnabled(ctx context.Context, jobExec *models.JobExecution) {
	if os.Getenv("SCHEDULER_V2_ENABLED") != "true" {
		return
	}

	if err := s.populateSchedulingUnits(ctx, jobExec); err != nil {
		debug.Warning("scheduler-v2: populateSchedulingUnits for job %s failed: %v — job remains legacy-owned",
			jobExec.ID, err)
	}
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
		ParentJobID:        jobExec.ID,
		LayerIndex:         0,
		Status:             models.SchedulingUnitStatusPending,
		Priority:           jobExec.Priority,
		MaxAgents:          jobExec.MaxAgents,
		AttackMode:         int(jobExec.AttackMode),
		EffectiveKeyspace:  effective,
		IsAccurateKeyspace: jobExec.IsAccurateKeyspace,
		WordlistRefs:       wordlistRefs,
		RuleFileRefs:       ruleFileRefs,
		MaskString:         maskPtr,
	}

	if err := unitRepo.Create(ctx, unit); err != nil {
		return fmt.Errorf("create single unit: %w", err)
	}
	debug.Info("scheduler-v2: created scheduling_unit %s for job %s (eff_keyspace=%d, accurate=%v)",
		unit.ID, jobExec.ID, unit.EffectiveKeyspace, unit.IsAccurateKeyspace)
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
			ParentJobID:        jobExec.ID,
			LayerIndex:         layer.LayerIndex,
			Status:             models.SchedulingUnitStatusPending,
			Priority:           jobExec.Priority,
			MaxAgents:          jobExec.MaxAgents,
			AttackMode:         int(jobExec.AttackMode),
			EffectiveKeyspace:  effective,
			IsAccurateKeyspace: layer.IsAccurateKeyspace,
			WordlistRefs:       wordlistRefs,
			RuleFileRefs:       ruleFileRefs,
			MaskString:         maskPtr,
		}

		if err := unitRepo.Create(ctx, unit); err != nil {
			return fmt.Errorf("create unit for layer %d: %w", layer.LayerIndex, err)
		}
		debug.Info("scheduler-v2: created scheduling_unit %s for job %s layer %d (mask=%q, eff=%d)",
			unit.ID, jobExec.ID, layer.LayerIndex, mask, effective)
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
