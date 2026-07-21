package services

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
)

// loopbackMonitorInterval is how often the controller re-evaluates in-flight loopback
// sessions. It is idle-gated (see LoopbackRepository.HasActiveSessions), so a tick with
// no active session is a single cheap EXISTS query.
const loopbackMonitorInterval = 7 * time.Second

// defaultLoopbackMaxRounds bounds how many delta rounds a session runs if the
// loopback_max_rounds system setting is missing.
const defaultLoopbackMaxRounds = 10

// LoopbackOrigin describes a round-0 job that belongs to a new loopback session.
type LoopbackOrigin struct {
	JobExecutionID uuid.UUID
	IsMutatable    bool
}

// LoopbackService is the durable controller for the loopback feature (GH #64). It
// monitors each session's current round, and once that round's jobs finish it computes
// the delta of newly-cracked plaintexts and spawns re-runs of the mutatable jobs against
// it — repeating until a round produces nothing new (or the max-rounds cap is hit).
type LoopbackService struct {
	repo               *repository.LoopbackRepository
	jobExecService     *JobExecutionService
	jobExecRepo        *repository.JobExecutionRepository
	wordlistManager    wordlist.Manager
	systemSettingsRepo *repository.SystemSettingsRepository
}

// NewLoopbackService creates the loopback controller.
func NewLoopbackService(
	repo *repository.LoopbackRepository,
	jobExecService *JobExecutionService,
	jobExecRepo *repository.JobExecutionRepository,
	wordlistManager wordlist.Manager,
	systemSettingsRepo *repository.SystemSettingsRepository,
) *LoopbackService {
	return &LoopbackService{
		repo:               repo,
		jobExecService:     jobExecService,
		jobExecRepo:        jobExecRepo,
		wordlistManager:    wordlistManager,
		systemSettingsRepo: systemSettingsRepo,
	}
}

// CreateSession records a new loopback session and its round-0 jobs. Called from the
// create-job flow once the original jobs have been created. Origins with IsMutatable
// true are the lineages that will be re-run against the delta each round.
func (s *LoopbackService) CreateSession(ctx context.Context, hashlistID int64, sourceType models.LoopbackSourceType, sourceWorkflowID *uuid.UUID, name string, createdBy *uuid.UUID, origins []LoopbackOrigin) (*models.LoopbackSession, error) {
	if len(origins) == 0 {
		return nil, fmt.Errorf("cannot create a loopback session with no jobs")
	}

	session := &models.LoopbackSession{
		HashlistID:       hashlistID,
		SourceType:       sourceType,
		SourceWorkflowID: sourceWorkflowID,
		Name:             name,
		Status:           models.LoopbackSessionStatusWaiting,
		CurrentRound:     0,
		MaxRounds:        s.resolveMaxRounds(ctx),
		CreatedBy:        createdBy,
	}

	jobs := make([]models.LoopbackSessionJob, 0, len(origins))
	for _, o := range origins {
		jobs = append(jobs, models.LoopbackSessionJob{
			JobExecutionID: o.JobExecutionID,
			Round:          0,
			Role:           models.LoopbackJobRoleOriginal,
			IsMutatable:    o.IsMutatable,
		})
	}

	if err := s.repo.CreateSessionWithJobs(ctx, session, jobs); err != nil {
		return nil, err
	}

	debug.Info("Created loopback session %s (%s) for hashlist %d with %d round-0 jobs", session.ID, sourceType, hashlistID, len(origins))
	return session, nil
}

// ListSessions returns loopback sessions (with their jobs populated) for the UI. Pass a
// non-nil createdBy to scope to one user, or nil for all (admins).
func (s *LoopbackService) ListSessions(ctx context.Context, createdBy *uuid.UUID) ([]*models.LoopbackSession, error) {
	sessions, err := s.repo.ListSessions(ctx, createdBy, 100)
	if err != nil {
		return nil, err
	}
	for _, sess := range sessions {
		jobs, jerr := s.repo.GetSessionJobs(ctx, sess.ID)
		if jerr != nil {
			debug.Warning("Failed to load jobs for loopback session %s: %v", sess.ID, jerr)
			continue
		}
		sess.Jobs = jobs
	}
	return sessions, nil
}

// resolveMaxRounds reads the loopback_max_rounds system setting (falling back to the
// default). It caps how many delta rounds any session runs.
func (s *LoopbackService) resolveMaxRounds(ctx context.Context) int {
	if s.systemSettingsRepo == nil {
		return defaultLoopbackMaxRounds
	}
	setting, err := s.systemSettingsRepo.GetSetting(ctx, "loopback_max_rounds")
	if err == nil && setting != nil && setting.Value != nil {
		if parsed, perr := parseIntValueFromString(*setting.Value); perr == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultLoopbackMaxRounds
}

// Start runs the monitor loop until the context is cancelled. Because it re-queries
// waiting/active sessions each tick, a backend restart is transparently recovered: the
// first tick after boot re-evaluates every unfinished session.
func (s *LoopbackService) Start(ctx context.Context) {
	debug.Info("Loopback monitor started (interval %s)", loopbackMonitorInterval)
	ticker := time.NewTicker(loopbackMonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			debug.Info("Loopback monitor stopping")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick processes all in-flight sessions. It is idle-gated so a system with no loopback
// work does a single EXISTS query per interval.
func (s *LoopbackService) tick(ctx context.Context) {
	active, err := s.repo.HasActiveSessions(ctx)
	if err != nil {
		debug.Error("Loopback monitor: failed to check for active sessions: %v", err)
		return
	}
	if !active {
		return
	}

	sessions, err := s.repo.GetActiveSessions(ctx)
	if err != nil {
		debug.Error("Loopback monitor: failed to list active sessions: %v", err)
		return
	}
	for _, session := range sessions {
		if err := s.processSession(ctx, session); err != nil {
			debug.Error("Loopback monitor: error processing session %s: %v", session.ID, err)
		}
	}
}

// processSession advances one session: if its current round is finished, it either
// spawns the next delta round or completes the session.
func (s *LoopbackService) processSession(ctx context.Context, session *models.LoopbackSession) error {
	// Wait until every job of the current round has reached a terminal state.
	nonTerminal, err := s.repo.CountNonTerminalRoundJobs(ctx, session.ID, session.CurrentRound)
	if err != nil {
		return err
	}
	if nonTerminal > 0 {
		return nil // still running; re-check next tick
	}

	// A round-0 with no jobs (e.g. a workflow whose steps were all ineligible or failed
	// to create) has nothing to loop back.
	if session.CurrentRound == 0 {
		count, err := s.repo.CountRoundJobs(ctx, session.ID, 0)
		if err != nil {
			return err
		}
		if count == 0 {
			return s.complete(ctx, session, "no round-0 jobs to monitor")
		}
	}

	// If there are no mutatable lineages, the loopback can never produce a re-run.
	origins, err := s.repo.GetMutatableOriginJobs(ctx, session.ID)
	if err != nil {
		return err
	}
	if len(origins) == 0 {
		return s.complete(ctx, session, "no mutatable jobs in this session")
	}

	// If the hashlist is fully cracked, there is nothing left for another round to find —
	// stop now instead of spawning a wasted round against an exhausted hashlist.
	hasUncracked, err := s.repo.HashlistHasUncracked(ctx, session.HashlistID)
	if err != nil {
		return err
	}
	if !hasUncracked {
		return s.complete(ctx, session, "hashlist fully cracked")
	}

	// Honour the safety cap.
	if session.CurrentRound >= session.MaxRounds {
		debug.Info("Loopback session %s reached max rounds (%d); completing", session.ID, session.MaxRounds)
		return s.complete(ctx, session, "")
	}

	// Compute the delta of never-before-used newly-cracked plaintexts.
	delta, err := s.repo.GetNewDeltaPlaintexts(ctx, session.ID, session.HashlistID)
	if err != nil {
		return err
	}
	if len(delta) == 0 {
		debug.Info("Loopback session %s is dry (no new plaintexts after round %d); completing", session.ID, session.CurrentRound)
		return s.complete(ctx, session, "")
	}

	// Spawn a re-run for each mutatable lineage against the delta.
	nextRound := session.CurrentRound + 1
	spawned := 0
	for _, origin := range origins {
		originJob, err := s.jobExecRepo.GetByID(ctx, origin.JobExecutionID)
		if err != nil {
			debug.Error("Loopback session %s: failed to load origin job %s: %v", session.ID, origin.JobExecutionID, err)
			continue
		}
		rerun, err := s.spawnRerun(ctx, session, originJob, delta, nextRound)
		if err != nil {
			debug.Error("Loopback session %s: failed to spawn re-run for origin %s: %v", session.ID, origin.JobExecutionID, err)
			continue
		}
		originID := origin.JobExecutionID
		if addErr := s.repo.AddSessionJob(ctx, &models.LoopbackSessionJob{
			SessionID:      session.ID,
			JobExecutionID: rerun.ID,
			Round:          nextRound,
			Role:           models.LoopbackJobRoleRerun,
			IsMutatable:    true,
			OriginJobID:    &originID,
		}); addErr != nil {
			debug.Error("Loopback session %s: failed to record re-run %s: %v", session.ID, rerun.ID, addErr)
		}
		spawned++
	}

	if spawned == 0 {
		msg := "failed to spawn any loopback re-runs"
		debug.Error("Loopback session %s: %s", session.ID, msg)
		return s.repo.UpdateSessionStatus(ctx, session.ID, models.LoopbackSessionStatusFailed, session.CurrentRound, &msg)
	}

	// Mark this delta consumed so it is never re-run, and advance the round.
	if err := s.repo.MarkPlaintextsUsed(ctx, session.ID, delta); err != nil {
		debug.Error("Loopback session %s: failed to mark %d plaintexts used: %v", session.ID, len(delta), err)
	}
	debug.Info("Loopback session %s: spawned round %d with %d re-run(s) against %d new plaintext(s)", session.ID, nextRound, spawned, len(delta))
	return s.repo.UpdateSessionStatus(ctx, session.ID, models.LoopbackSessionStatusActive, nextRound, nil)
}

// spawnRerun creates one loopback re-run: a job with the origin's mutation config
// (rules/mask/mode/…) but its wordlist swapped for a freshly-materialized ephemeral
// wordlist of the delta. It reuses the preparing→finalize flow so the ephemeral wordlist
// is owned by (and cleaned up with) the new job, and keyspace is computed on the real
// file before the job becomes schedulable.
func (s *LoopbackService) spawnRerun(ctx context.Context, session *models.LoopbackSession, originJob *models.JobExecution, delta []string, round int) (*models.JobExecution, error) {
	creator := session.CreatedBy
	if creator == nil {
		creator = originJob.CreatedBy
	}
	if creator == nil {
		return nil, fmt.Errorf("cannot spawn loopback re-run: no creator to own the ephemeral wordlist")
	}

	name := fmt.Sprintf("%s (loopback R%d)", originJob.Name, round)
	config := loopbackConfigFromJob(originJob, name)

	// 1. Create the placeholder job (owns the ephemeral wordlist, ignored by scheduler).
	prep, err := s.jobExecService.CreatePreparingFilterJob(ctx, config, session.HashlistID, creator, name)
	if err != nil {
		return nil, fmt.Errorf("failed to create preparing loopback job: %w", err)
	}

	// 2. Materialize the delta into an ephemeral wordlist owned by that job.
	wlName := fmt.Sprintf("loopback_%s_r%d", prep.ID.String()[:8], round)
	wl, err := s.wordlistManager.MaterializeEphemeralWordlist(ctx, delta, wlName, prep.ID, *creator)
	if err != nil {
		_ = s.jobExecService.FailJob(ctx, prep.ID, fmt.Sprintf("loopback: failed to materialize delta wordlist: %v", err))
		return nil, err
	}

	// 3. Swap the wordlist to the delta, compute keyspace, and open the dispatch gate.
	config.WordlistIDs = models.IDArray{strconv.Itoa(wl.ID)}
	if err := s.jobExecService.FinalizeFilterJob(ctx, prep.ID, config); err != nil {
		_ = s.jobExecService.FailJob(ctx, prep.ID, fmt.Sprintf("loopback: failed to finalize job: %v", err))
		return nil, err
	}
	return prep, nil
}

// complete marks a session completed (optionally with an informational reason).
func (s *LoopbackService) complete(ctx context.Context, session *models.LoopbackSession, reason string) error {
	if reason != "" {
		debug.Info("Loopback session %s completed: %s", session.ID, reason)
	}
	return s.repo.UpdateSessionStatus(ctx, session.ID, models.LoopbackSessionStatusCompleted, session.CurrentRound, nil)
}

// loopbackConfigFromJob builds a custom-job config from an existing job_execution,
// carrying over the mutation (rules/mask/mode/charsets/…) but leaving the wordlist for
// the caller to fill with the delta.
func loopbackConfigFromJob(job *models.JobExecution, name string) CustomJobConfig {
	return CustomJobConfig{
		Name:                      name,
		WordlistIDs:               models.IDArray{}, // filled with the delta wordlist by the caller
		RuleIDs:                   job.RuleIDs,
		AttackMode:                job.AttackMode,
		Mask:                      job.Mask,
		CustomCharsets:            job.CustomCharsets,
		CustomCharsetFiles:        job.CustomCharsetFiles,
		HexCharset:                job.HexCharset,
		Priority:                  job.Priority,
		MaxAgents:                 job.MaxAgents,
		BinaryVersion:             job.BinaryVersion,
		AllowHighPriorityOverride: job.AllowHighPriorityOverride,
		ChunkSizeSeconds:          job.ChunkSizeSeconds,
		IncrementMode:             job.IncrementMode,
		IncrementMin:              job.IncrementMin,
		IncrementMax:              job.IncrementMax,
		AdditionalArgs:            job.AdditionalArgs,
	}
}
