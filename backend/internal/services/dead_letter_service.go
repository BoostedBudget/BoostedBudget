package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
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
	// these args already exists, or the kind's ByPeriod window has not elapsed. Since #1508 the
	// claim is taken inside the replay's transaction, so returning this error rolls it back and the
	// row is replayable again once that stops being true — no compensating write is involved, and
	// therefore none can fail.
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
	// BeginRetryTx, AtomicMarkRetriedInTx and MarkRetriedInTx are the three halves of one atomic
	// replay — see Retry, and BeginRetryTx's own comment for why #1508 needed them to share a
	// transaction with the River insert.
	BeginRetryTx(ctx context.Context) (pgx.Tx, error)
	AtomicMarkRetriedInTx(ctx context.Context, tx pgx.Tx, id, retryJobID int64) (bool, error)
	MarkRetriedInTx(ctx context.Context, tx pgx.Tx, id int64, retryJobID int64) error
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
// Flow (claim-first to prevent double-enqueue, all of steps 3-6 in ONE transaction):
//  1. Fetch job and check RetriedAt (fast pre-check for better error messages)
//  2. Resolve the kind's uniqueness options — BEFORE claiming, so a kind with no registered policy
//     is refused without opening a transaction or spending the row's one retry
//  3. BeginRetryTx
//  4. AtomicMarkRetriedInTx (CAS with placeholder 0) — claims the retry slot before enqueueing
//  5. EnqueueTx the new River job using RawJobArgs (preserves original queue/maxAttempts/uniqueness)
//  6. MarkRetriedInTx to record the real job ID, then COMMIT
//
// ⚠ Steps 4 and 5 must stay in this order. Claiming after a successful enqueue re-opens the
// concurrent double-enqueue race the compare-and-set exists to close; the transaction preserves the
// order rather than replacing it.
//
// Issue #1508 — why one transaction. The claim and the enqueue used to be separate commits, so they
// could disagree: a committed claim with no job (the row's one replay spent on nothing), or a job
// with no recorded id (best-effort MarkRetried failing). Both left retry_job_id = 0, which is why
// that column could not discriminate them and why no reaper could be built on it. Now every failure
// after step 3 — a queue error, a dedup, a failed id write, a failed COMMIT — rolls the whole thing
// back, so the only committed outcomes are "claimed, with a real job id, and that job exists" and
// "nothing happened". The rollback IS the release; there is no compensating write left to fail.
//
// River's insert is safe to make through the caller's transaction: it inserts into river_job on
// that connection and sends its "new job" notification in the same transaction, so no producer can
// see or work the job before COMMIT, and a rollback removes it (river v0.38.0, client.go InsertTx:
// "Because of snapshot visibility guarantees across transactions, the job will not be worked until
// the transaction has committed, and if the transaction rolls back, so too is the inserted job").
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

	tx, err := s.repo.BeginRetryTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin dead letter retry transaction: %w", err)
	}
	// Every path below except the final COMMIT leaves the replay undone. Rollback after a commit is
	// a no-op error (pgx.ErrTxClosed) and is not worth reporting; anything else is logged, because a
	// rollback that does not happen is the one way a claim could still outlive its replay.
	committed := false
	defer func() {
		if committed {
			return
		}
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			s.logger.ErrorContext(ctx, "dead letter retry transaction could not be rolled back",
				"dead_letter_id", id, "error", rbErr)
		}
	}()

	// Claim the retry slot first — prevents concurrent double-enqueue.
	// Use 0 as placeholder; overwritten with the real job ID before this transaction commits.
	claimed, err := s.repo.AtomicMarkRetriedInTx(ctx, tx, id, 0)
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

	result, err := s.jobQueue.EnqueueTx(ctx, tx, rawArgs, enqOpts...)
	if err != nil {
		// The claim is inside this transaction, so returning here undoes it: an enqueue error can no
		// longer permanently consume the row's only retry.
		//
		// It also no longer matters that an enqueue error is not PROOF that nothing was inserted. The
		// insert went through THIS transaction, so whatever it did dies with the rollback — where the
		// old autocommit Insert could have committed a job whose response was lost, leaving a
		// release to trade a stranded row against a possible duplicate.
		s.logger.ErrorContext(ctx, "dead letter retry enqueue failed after claiming",
			"dead_letter_id", id, "error", err)
		return nil, fmt.Errorf("failed to enqueue retry job: %w", err)
	}

	if result.UniqueSkipped {
		// River deduplicated the replay against an existing job and inserted nothing — result.ID is
		// the EXISTING job's ID, not a new one. Reporting success here would tell the operator a
		// retry is running when none was created. Rolling back returns the slot.
		s.logger.WarnContext(ctx, "dead letter retry deduplicated against existing job",
			"dead_letter_id", id, "kind", job.Kind, "existing_job_id", result.ID)
		return nil, fmt.Errorf("%w: existing job %d", ErrDeadLetterRetryDeduplicated, result.ID)
	}

	// Replace the 0 placeholder with the real job ID. No longer best-effort: it is what makes a
	// committed row's retry_job_id trustworthy, so its failure must take the whole replay with it.
	if err := s.repo.MarkRetriedInTx(ctx, tx, id, result.ID); err != nil {
		s.logger.ErrorContext(ctx, "failed to record retry job ID; rolling the replay back",
			"dead_letter_id", id, "new_job_id", result.ID, "error", err)
		return nil, fmt.Errorf("failed to record retry job id: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		// A COMMIT whose response is lost may in fact have committed — and that is fine here, because
		// what would then be committed is the CONSISTENT outcome: claim, real retry_job_id, and the
		// job. The operator sees an error over a row that either replayed completely or not at all.
		return nil, fmt.Errorf("failed to commit dead letter retry: %w", err)
	}
	committed = true

	newJobID := result.ID
	return &domain.DeadLetterRetryResponse{
		Success:      true,
		Message:      "Job re-enqueued successfully",
		NewJobID:     &newJobID,
		DeadLetterID: id,
	}, nil
}

// Both residual holes the pre-#1508 compensating release left open are now closed by construction,
// and neither has a detector because neither has a state to detect:
//
//   - A failed release (Postgres unavailable for both the enqueue and the release) stranded the
//     claim permanently. There is no release any more — the rollback is the release, and a
//     transaction whose connection dies is aborted by the server, so the claim never commits.
//   - A crash between the claim committing and the enqueue stranded it the same way. The claim no
//     longer commits on its own.
//
// What remains uncovered is HISTORY: rows stranded at retry_job_id = 0 by the old two-commit path
// before this change. They are still undecidable in exactly the way #1508 describes — the column
// cannot say whether a live job sits behind them — so no reaper is shipped for them, and releasing
// them automatically could double-enqueue a running job. See the issue for the disposition.

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
