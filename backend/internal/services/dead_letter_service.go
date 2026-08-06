package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Sentinel errors for dead letter job operations.
var (
	ErrDeadLetterJobNotFound      = errors.New("dead letter job not found")
	ErrDeadLetterJobAlreadyRetried = errors.New("dead letter job has already been retried")

	// ErrDeadLetterRetryKindUnknown means no uniqueness policy is registered for the row's kind,
	// so the replay was refused rather than enqueued without one. See UniqueOptsResolver.
	ErrDeadLetterRetryKindUnknown = errors.New("dead letter job kind has no registered uniqueness policy")

	// ErrDeadLetterRetryDeduplicated means the replay was deduplicated away by the kind's own
	// uniqueness options — River enqueued NOTHING (JobResult.UniqueSkipped). Either a live job for
	// these args already exists, or the kind's ByPeriod window has not elapsed. A release of the retry
	// slot is attempted so the row can be replayed again once that stops being true; if that release
	// itself fails it is logged at Error and the row stays consumed (see releaseClaim).
	ErrDeadLetterRetryDeduplicated = errors.New("dead letter retry was deduplicated against an existing job")
)

// UniqueOptsResolver reports the uniqueness options a REPLAY of the given River job kind must
// carry, plus whether kind is a job kind the resolver knows at all. A known kind may legitimately
// have no uniqueness (nil opts, known true) — five kinds are enqueued by their real producers with no
// options at all, and a replay must not invent a dedup key those producers never used.
//
// Why this is injected rather than looked up here (#1490/#1491): internal/services cannot import
// internal/queue/jobs — jobs imports services, so the *UniqueOpts() functions that define each
// kind's dedup contract are unreachable from this file. That same cycle is why six args types are
// mirrored into this package. cmd/server/main.go already imports jobs and is where the resolver is
// wired from (jobs.UniqueOptsForKind), which keeps the kind→options mapping in the package that owns
// the options.
//
// That placement does NOT make the mapping drift-proof. The producers live in three packages and pass
// their options in four different shapes, so no test binds the mapping to them; it rests on the manual
// producer audit recorded in internal/queue/jobs/unique_opts_by_kind.go — which found three kinds
// whose *UniqueOpts() helper no producer actually calls.
type UniqueOptsResolver func(kind string) (opts *queue.UniqueOpts, known bool)

// deadLetterRepo defines the repository methods the service depends on.
// This interface allows unit testing with a mock.
type deadLetterRepo interface {
	GetByID(ctx context.Context, id int64) (*domain.DeadLetterJob, error)
	ListAll(ctx context.Context, filters domain.DeadLetterJobFilters, limit, offset int) ([]*domain.DeadLetterJob, int, error)
	AtomicMarkRetried(ctx context.Context, id, retryJobID int64) (bool, error)
	ReleaseRetryClaim(ctx context.Context, id int64) error
	MarkRetried(ctx context.Context, id int64, retryJobID int64) error
	AddNotes(ctx context.Context, id int64, notes string) error
	DeleteByID(ctx context.Context, id int64) error
	DeleteByFilters(ctx context.Context, filters domain.DeadLetterJobFilters) (int64, error)
	GetStats(ctx context.Context) (*repository.DeadLetterStats, error)
	GetStatsByKind(ctx context.Context) (map[string]int64, error)
	DeleteOlderThan(ctx context.Context, before time.Time, skipWithNotes bool) (int64, error)
}

// DeadLetterService manages dead letter queue entries.
type DeadLetterService interface {
	List(ctx context.Context, filters domain.DeadLetterJobFilters, page, pageSize int) (*domain.DeadLetterJobListResponse, error)
	GetByID(ctx context.Context, id int64) (*domain.DeadLetterJob, error)
	GetRawArgs(ctx context.Context, id int64) (json.RawMessage, error)
	Retry(ctx context.Context, id int64) (*domain.DeadLetterRetryResponse, error)
	AddNotes(ctx context.Context, id int64, notes string) error
	Delete(ctx context.Context, id int64) error
	DeleteByFilters(ctx context.Context, filters domain.DeadLetterJobFilters) (int64, error)
	GetStats(ctx context.Context) (*domain.DeadLetterStatsResponse, error)
	Cleanup(ctx context.Context, retentionDays int) (int64, error)
}

type deadLetterService struct {
	repo          deadLetterRepo
	jobQueue      queue.JobQueue
	uniqueOptsFor UniqueOptsResolver
	logger        *slog.Logger
}

// NewDeadLetterService creates a new DeadLetterService.
// jobQueue may be nil; retry operations will return an error if nil.
//
// uniqueOptsFor may be nil, and then EVERY Retry fails with ErrDeadLetterRetryKindUnknown. That is
// deliberate: the alternative default — replaying with no uniqueness options — is exactly the
// silent, every-kind bug #1492 fixed, and a wiring mistake must not restore it invisibly. Callers
// that never retry (most unit tests) can pass nil.
func NewDeadLetterService(repo deadLetterRepo, jobQueue queue.JobQueue, uniqueOptsFor UniqueOptsResolver) DeadLetterService {
	return &deadLetterService{
		repo:          repo,
		jobQueue:      jobQueue,
		uniqueOptsFor: uniqueOptsFor,
		logger:        slog.Default(),
	}
}

// List returns a paginated, masked list of dead letter jobs.
func (s *deadLetterService) List(ctx context.Context, filters domain.DeadLetterJobFilters, page, pageSize int) (*domain.DeadLetterJobListResponse, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	rawJobs, total, err := s.repo.ListAll(ctx, filters, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list dead letter jobs: %w", err)
	}

	masked := make([]*domain.DeadLetterJob, 0, len(rawJobs))
	for _, job := range rawJobs {
		maskedJob := *job
		maskedJob.Args = MaskJobArgs(job.Args)
		maskedJob.Errors = MaskJobErrors(job.Errors)
		masked = append(masked, &maskedJob)
	}

	totalPages := 1
	if pageSize > 0 && total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}

	return &domain.DeadLetterJobListResponse{
		Jobs:       masked,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		HasMore:    offset+pageSize < total,
	}, nil
}

// GetByID returns a single dead letter job with args and errors masked.
func (s *deadLetterService) GetByID(ctx context.Context, id int64) (*domain.DeadLetterJob, error) {
	job, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, ErrDeadLetterJobNotFound
	}

	maskedJob := *job
	maskedJob.Args = MaskJobArgs(job.Args)
	maskedJob.Errors = MaskJobErrors(job.Errors)
	return &maskedJob, nil
}

// GetRawArgs returns the unmasked job args for the reveal-args admin endpoint.
func (s *deadLetterService) GetRawArgs(ctx context.Context, id int64) (json.RawMessage, error) {
	job, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, ErrDeadLetterJobNotFound
	}
	return job.Args, nil
}

// Retry re-enqueues a dead letter job with the original kind, args, queue, max attempts, and the
// uniqueness options its kind's real producers use.
// Returns ErrDeadLetterJobAlreadyRetried if the job has already been retried.
//
// Flow (claim-first to prevent double-enqueue):
//  1. Fetch job and check RetriedAt (fast pre-check for better error messages)
//  2. Resolve the kind's uniqueness options — BEFORE claiming, so a kind with no registered policy
//     is refused without spending the row's one retry
//  3. AtomicMarkRetried (CAS with placeholder 0) — claims the retry slot before enqueueing
//  4. Enqueue new River job using RawJobArgs (preserves original queue/maxAttempts/uniqueness)
//  5. MarkRetried to update the tracked job ID (best-effort)
//
// Step 4 has two outcomes in which NOTHING was enqueued — a queue error, and a UniqueSkipped dedup.
// Both release the claim taken in step 3, rather than moving the claim after the enqueue: see
// ReleaseRetryClaim's comment on why claim-then-act must stay in that order.
func (s *deadLetterService) Retry(ctx context.Context, id int64) (*domain.DeadLetterRetryResponse, error) {
	job, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, ErrDeadLetterJobNotFound
	}

	if job.RetriedAt != nil {
		return nil, ErrDeadLetterJobAlreadyRetried
	}

	if s.jobQueue == nil {
		return nil, errors.New("job queue not configured")
	}

	// Issue #1492: this is the third, kind-agnostic producer of every job kind, and until now the
	// only one that passed no uniqueness options — so a replay silently dropped the dedup guarantee
	// its kind was designed around. Resolve before claiming: an unknown kind is refused fail-closed
	// (enqueueing it without uniqueness is the bug this fixes, and doing that silently is worse than
	// making an operator add the missing registry entry), and refusing before the claim leaves the
	// row's single retry intact for after that entry exists.
	if s.uniqueOptsFor == nil {
		s.logger.ErrorContext(ctx, "dead letter retry refused: no uniqueness resolver wired",
			"dead_letter_id", id, "kind", job.Kind)
		return nil, ErrDeadLetterRetryKindUnknown
	}
	uniqueOpts, known := s.uniqueOptsFor(job.Kind)
	if !known {
		s.logger.ErrorContext(ctx, "dead letter retry refused: unknown job kind",
			"dead_letter_id", id, "kind", job.Kind)
		return nil, fmt.Errorf("%w: %s", ErrDeadLetterRetryKindUnknown, job.Kind)
	}

	// Claim the retry slot first — prevents concurrent double-enqueue.
	// Use 0 as placeholder; updated after enqueueing.
	claimed, err := s.repo.AtomicMarkRetried(ctx, id, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to claim dead letter job for retry: %w", err)
	}
	if !claimed {
		return nil, ErrDeadLetterJobAlreadyRetried
	}

	rawArgs := queue.RawJobArgs{
		JobKind: job.Kind,
		RawArgs: job.Args,
	}

	enqOpts := []queue.EnqueueOption{
		queue.WithQueue(job.Queue),
	}
	if job.MaxAttempts > 0 {
		enqOpts = append(enqOpts, queue.WithMaxAttempts(job.MaxAttempts))
	}
	if uniqueOpts != nil {
		enqOpts = append(enqOpts, queue.WithUnique(*uniqueOpts))
	}

	result, err := s.jobQueue.Enqueue(ctx, rawArgs, enqOpts...)
	if err != nil {
		// Slot claimed but enqueue failed, so the claim must not stand or this transient failure would
		// permanently consume the row's only retry.
		//
		// An Enqueue error is not PROOF that nothing was inserted — River's Insert is an autocommit
		// statement, so a commit whose response is lost surfaces here as an error over a job that
		// exists. Releasing therefore trades "one transient error strands the row forever" for "an
		// ambiguous error may allow a second insert". That trade is only safe because of part A of this
		// change: with the kind's uniqueness options now on the replay, a duplicate insert of the same
		// args is UniqueSkipped rather than duplicated — for every kind whose producers pass options.
		// The five noUniqueness kinds have no such protection, and for them a duplicate is possible;
		// all five are idempotent or naturally repeat-safe (a category refresh, an email-change
		// propagation, a rule evaluation, a webhook reprocess, and a kind with no producer at all).
		s.logger.ErrorContext(ctx, "dead letter retry enqueue failed after claiming",
			"dead_letter_id", id, "error", err)
		s.releaseClaim(ctx, id, "enqueue failed")
		return nil, fmt.Errorf("failed to enqueue retry job: %w", err)
	}

	if result.UniqueSkipped {
		// River deduplicated the replay against an existing job and inserted nothing — result.ID is
		// the EXISTING job's ID, not a new one. Reporting success here would tell the operator a
		// retry is running when none was created.
		s.logger.WarnContext(ctx, "dead letter retry deduplicated against existing job",
			"dead_letter_id", id, "kind", job.Kind, "existing_job_id", result.ID)
		s.releaseClaim(ctx, id, "retry deduplicated")
		return nil, fmt.Errorf("%w: existing job %d", ErrDeadLetterRetryDeduplicated, result.ID)
	}

	// Update claim with real job ID (best-effort — failure doesn't affect job execution)
	if updateErr := s.repo.MarkRetried(ctx, id, result.ID); updateErr != nil {
		s.logger.ErrorContext(ctx, "failed to update retry job ID",
			"dead_letter_id", id, "new_job_id", result.ID, "error", updateErr)
	}

	newJobID := result.ID
	return &domain.DeadLetterRetryResponse{
		Success:      true,
		Message:      "Job re-enqueued successfully",
		NewJobID:     &newJobID,
		DeadLetterID: id,
	}, nil
}

// releaseClaim un-stamps a retry claim taken for a replay that then enqueued nothing, restoring
// retried_at to NULL so the row is retryable again.
//
// A release failure leaves the row in the one state this mechanism exists to eliminate — a consumed
// retry with no live job behind it — and this function CANNOT fix that: it logs at Error and returns.
// It deliberately does not replace the caller's error (the enqueue or dedup outcome is what the
// operator must act on), which means a release failure reaches an operator only through logs, not
// through the API response. Two residual holes follow from that and neither is closed here:
//
//   - Release failure (e.g. Postgres unavailable for both the enqueue and the release) strands the
//     claim permanently, exactly as before this change — just now confined to the case where two
//     things fail instead of one.
//   - A crash between AtomicMarkRetried committing and Enqueue being called strands it the same way.
//
// Both need a reaper — something that finds rows with retried_at set, retry_job_id = 0, and no
// corresponding river_job, and releases them — which is a separate piece of work, not a comment.
func (s *deadLetterService) releaseClaim(ctx context.Context, id int64, reason string) {
	if err := s.repo.ReleaseRetryClaim(ctx, id); err != nil {
		s.logger.ErrorContext(ctx, "dead letter retry claim could not be released",
			"dead_letter_id", id, "reason", reason, "error", err)
	}
}

// AddNotes sets admin notes on a dead letter job.
func (s *deadLetterService) AddNotes(ctx context.Context, id int64, notes string) error {
	return s.repo.AddNotes(ctx, id, notes)
}

// Delete permanently removes a single dead letter job.
func (s *deadLetterService) Delete(ctx context.Context, id int64) error {
	return s.repo.DeleteByID(ctx, id)
}

// DeleteByFilters bulk-deletes dead letter jobs matching the given filters.
func (s *deadLetterService) DeleteByFilters(ctx context.Context, filters domain.DeadLetterJobFilters) (int64, error) {
	return s.repo.DeleteByFilters(ctx, filters)
}

// GetStats returns aggregate statistics about dead letter jobs.
func (s *deadLetterService) GetStats(ctx context.Context) (*domain.DeadLetterStatsResponse, error) {
	stats, err := s.repo.GetStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get dead letter stats: %w", err)
	}

	byKind, err := s.repo.GetStatsByKind(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get dead letter stats by kind: %w", err)
	}

	return &domain.DeadLetterStatsResponse{
		Total:       stats.Total,
		Unnotified:  stats.Unnotified,
		Retried:     stats.Retried,
		Last24Hours: stats.Last24Hours,
		ByKind:      byKind,
	}, nil
}

// Cleanup deletes dead letter jobs older than retentionDays.
// Jobs with admin notes are preserved (they may be under investigation).
func (s *deadLetterService) Cleanup(ctx context.Context, retentionDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	count, err := s.repo.DeleteOlderThan(ctx, cutoff, true)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup dead letter jobs: %w", err)
	}

	if count > 0 {
		s.logger.InfoContext(ctx, "cleaned up dead letter jobs",
			"deleted_count", count,
			"retention_days", retentionDays,
			"cutoff", cutoff,
		)
	}

	return count, nil
}
