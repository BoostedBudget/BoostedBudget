// Package repository provides data access implementations.
package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// DeadLetterJobRepository handles persistence of dead letter jobs.
type DeadLetterJobRepository struct {
	db *pgxpool.Pool
}

// NewDeadLetterJobRepository creates a new dead letter job repository.
func NewDeadLetterJobRepository(db *pgxpool.Pool) *DeadLetterJobRepository {
	return &DeadLetterJobRepository{db: db}
}

// Create inserts a new dead letter job record.
func (r *DeadLetterJobRepository) Create(ctx context.Context, job *domain.DeadLetterJob) error {
	query := `
		INSERT INTO dead_letter_jobs (
			original_job_id, kind, queue, args, errors, max_attempts,
			created_at, failed_at, user_id, notified
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`

	return r.db.QueryRow(ctx, query,
		job.OriginalJobID,
		job.Kind,
		job.Queue,
		job.Args,
		job.Errors,
		job.MaxAttempts,
		job.CreatedAt,
		job.FailedAt,
		job.UserID,
		job.Notified,
	).Scan(&job.ID)
}

// GetByID retrieves a dead letter job by ID.
func (r *DeadLetterJobRepository) GetByID(ctx context.Context, id int64) (*domain.DeadLetterJob, error) {
	query := `
		SELECT id, original_job_id, kind, queue, args, errors, max_attempts,
		       created_at, failed_at, user_id, notified, retried_at, retry_job_id, notes
		FROM dead_letter_jobs
		WHERE id = $1`

	job := &domain.DeadLetterJob{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&job.ID,
		&job.OriginalJobID,
		&job.Kind,
		&job.Queue,
		&job.Args,
		&job.Errors,
		&job.MaxAttempts,
		&job.CreatedAt,
		&job.FailedAt,
		&job.UserID,
		&job.Notified,
		&job.RetriedAt,
		&job.RetryJobID,
		&job.Notes,
	)
	if err != nil {
		return nil, err
	}
	return job, nil
}

// ListUnnotified returns dead letter jobs where users haven't been notified yet.
func (r *DeadLetterJobRepository) ListUnnotified(ctx context.Context, limit int) ([]*domain.DeadLetterJob, error) {
	query := `
		SELECT id, original_job_id, kind, queue, args, errors, max_attempts,
		       created_at, failed_at, user_id, notified, retried_at, retry_job_id, notes
		FROM dead_letter_jobs
		WHERE notified = FALSE AND user_id IS NOT NULL
		ORDER BY failed_at ASC
		LIMIT $1`

	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*domain.DeadLetterJob
	for rows.Next() {
		job := &domain.DeadLetterJob{}
		err := rows.Scan(
			&job.ID,
			&job.OriginalJobID,
			&job.Kind,
			&job.Queue,
			&job.Args,
			&job.Errors,
			&job.MaxAttempts,
			&job.CreatedAt,
			&job.FailedAt,
			&job.UserID,
			&job.Notified,
			&job.RetriedAt,
			&job.RetryJobID,
			&job.Notes,
		)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// ListByUserID returns dead letter jobs for a specific user.
func (r *DeadLetterJobRepository) ListByUserID(ctx context.Context, userID string, limit int) ([]*domain.DeadLetterJob, error) {
	query := `
		SELECT id, original_job_id, kind, queue, args, errors, max_attempts,
		       created_at, failed_at, user_id, notified, retried_at, retry_job_id, notes
		FROM dead_letter_jobs
		WHERE user_id = $1
		ORDER BY failed_at DESC
		LIMIT $2`

	rows, err := r.db.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*domain.DeadLetterJob
	for rows.Next() {
		job := &domain.DeadLetterJob{}
		err := rows.Scan(
			&job.ID,
			&job.OriginalJobID,
			&job.Kind,
			&job.Queue,
			&job.Args,
			&job.Errors,
			&job.MaxAttempts,
			&job.CreatedAt,
			&job.FailedAt,
			&job.UserID,
			&job.Notified,
			&job.RetriedAt,
			&job.RetryJobID,
			&job.Notes,
		)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// MarkNotified marks a dead letter job as notified.
func (r *DeadLetterJobRepository) MarkNotified(ctx context.Context, id int64) error {
	query := `UPDATE dead_letter_jobs SET notified = TRUE WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id)
	return err
}

// MarkRetried records that a dead letter job was manually retried.
func (r *DeadLetterJobRepository) MarkRetried(ctx context.Context, id int64, retryJobID int64) error {
	query := `UPDATE dead_letter_jobs SET retried_at = $1, retry_job_id = $2 WHERE id = $3`
	_, err := r.db.Exec(ctx, query, time.Now(), retryJobID, id)
	return err
}

// AddNotes adds admin notes to a dead letter job.
func (r *DeadLetterJobRepository) AddNotes(ctx context.Context, id int64, notes string) error {
	query := `UPDATE dead_letter_jobs SET notes = $1 WHERE id = $2`
	_, err := r.db.Exec(ctx, query, notes, id)
	return err
}

// GetStats returns statistics about dead letter jobs.
func (r *DeadLetterJobRepository) GetStats(ctx context.Context) (*DeadLetterStats, error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE notified = FALSE AND user_id IS NOT NULL) as unnotified,
			COUNT(*) FILTER (WHERE retried_at IS NOT NULL) as retried,
			COUNT(*) FILTER (WHERE failed_at > NOW() - INTERVAL '24 hours') as last_24h
		FROM dead_letter_jobs`

	stats := &DeadLetterStats{}
	err := r.db.QueryRow(ctx, query).Scan(
		&stats.Total,
		&stats.Unnotified,
		&stats.Retried,
		&stats.Last24Hours,
	)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// GetStatsByKind returns dead letter job counts grouped by job kind.
func (r *DeadLetterJobRepository) GetStatsByKind(ctx context.Context) (map[string]int64, error) {
	query := `
		SELECT kind, COUNT(*) as count
		FROM dead_letter_jobs
		GROUP BY kind
		ORDER BY count DESC`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make(map[string]int64)
	for rows.Next() {
		var kind string
		var count int64
		if err := rows.Scan(&kind, &count); err != nil {
			return nil, err
		}
		stats[kind] = count
	}
	return stats, rows.Err()
}

// GetOldestJobAge returns the age in hours of the oldest unretried dead letter job.
// Returns 0 if no unretried jobs exist.
func (r *DeadLetterJobRepository) GetOldestJobAge(ctx context.Context) (int64, error) {
	query := `
		SELECT COALESCE(EXTRACT(epoch FROM (NOW() - MIN(failed_at)))::BIGINT / 3600, 0)
		FROM dead_letter_jobs
		WHERE retried_at IS NULL
	`

	var ageHours int64
	err := r.db.QueryRow(ctx, query).Scan(&ageHours)
	if err != nil {
		return 0, err
	}

	return ageHours, nil
}

// DeadLetterStats contains aggregate statistics about dead letter jobs.
type DeadLetterStats struct {
	Total       int64 `json:"total"`
	Unnotified  int64 `json:"unnotified"`
	Retried     int64 `json:"retried"`
	Last24Hours int64 `json:"last_24_hours"`
}

// UserIDExtractor extracts user ID from job args for different job types.
// This is used when moving jobs to the dead letter queue to associate them with users.
func UserIDExtractor(kind string, args json.RawMessage) *string {
	// Different job types store user_id in different locations in their args
	var data map[string]interface{}
	if err := json.Unmarshal(args, &data); err != nil {
		return nil
	}

	// Most jobs have user_id at top level
	if userID, ok := data["user_id"].(string); ok && userID != "" {
		return &userID
	}

	return nil
}
