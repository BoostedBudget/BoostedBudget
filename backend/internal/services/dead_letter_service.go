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
)

// deadLetterRepo defines the repository methods the service depends on.
// This interface allows unit testing with a mock.
type deadLetterRepo interface {
	GetByID(ctx context.Context, id int64) (*domain.DeadLetterJob, error)
	ListAll(ctx context.Context, filters domain.DeadLetterJobFilters, limit, offset int) ([]*domain.DeadLetterJob, int, error)
	AtomicMarkRetried(ctx context.Context, id, retryJobID int64) (bool, error)
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
	repo     deadLetterRepo
	jobQueue queue.JobQueue
	logger   *slog.Logger
}

// NewDeadLetterService creates a new DeadLetterService.
// jobQueue may be nil; retry operations will return an error if nil.
func NewDeadLetterService(repo deadLetterRepo, jobQueue queue.JobQueue) DeadLetterService {
	return &deadLetterService{
		repo:     repo,
		jobQueue: jobQueue,
		logger:   slog.Default(),
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

// Retry re-enqueues a dead letter job with the original kind, args, queue, and max attempts.
// Returns ErrDeadLetterJobAlreadyRetried if the job has already been retried.
//
// Flow (claim-first to prevent double-enqueue):
//  1. Fetch job and check RetriedAt (fast pre-check for better error messages)
//  2. AtomicMarkRetried (CAS with placeholder 0) — claims the retry slot before enqueueing
//  3. Enqueue new River job using RawJobArgs (preserves original queue/maxAttempts)
//  4. MarkRetried to update the tracked job ID (best-effort)
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

	result, err := s.jobQueue.Enqueue(ctx, rawArgs, enqOpts...)
	if err != nil {
		// Slot claimed but enqueue failed — job is marked retried with no real job ID.
		// Log prominently so ops can investigate.
		s.logger.ErrorContext(ctx, "dead letter retry enqueue failed after claiming",
			"dead_letter_id", id, "error", err)
		return nil, fmt.Errorf("failed to enqueue retry job: %w", err)
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
