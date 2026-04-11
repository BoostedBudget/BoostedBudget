// Package repository provides data access implementations.
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// DeadLetterJobRepository handles persistence of dead letter jobs.
type DeadLetterJobRepository struct {
	db DB
}

// NewDeadLetterJobRepository creates a new dead letter job repository.
func NewDeadLetterJobRepository(db DB) *DeadLetterJobRepository {
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

// ListAll returns a paginated list of dead letter jobs with optional filters.
// Returns the jobs, total count, and any error.
func (r *DeadLetterJobRepository) ListAll(ctx context.Context, filters domain.DeadLetterJobFilters, limit, offset int) ([]*domain.DeadLetterJob, int, error) {
	where, args := buildDLQWhereClause(filters)

	listQuery := fmt.Sprintf(`
		SELECT id, original_job_id, kind, queue, args, errors, max_attempts,
		       created_at, failed_at, user_id, notified, retried_at, retry_job_id, notes
		FROM dead_letter_jobs
		%s
		ORDER BY failed_at DESC
		LIMIT %d OFFSET %d`, where, limit, offset)

	rows, err := r.db.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var jobs []*domain.DeadLetterJob
	for rows.Next() {
		job := &domain.DeadLetterJob{}
		if err := rows.Scan(
			&job.ID, &job.OriginalJobID, &job.Kind, &job.Queue,
			&job.Args, &job.Errors, &job.MaxAttempts,
			&job.CreatedAt, &job.FailedAt, &job.UserID, &job.Notified,
			&job.RetriedAt, &job.RetryJobID, &job.Notes,
		); err != nil {
			return nil, 0, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM dead_letter_jobs %s`, where)
	var total int
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	return jobs, total, nil
}

// buildDLQWhereClause constructs a WHERE clause and args slice from DLQ filters.
func buildDLQWhereClause(filters domain.DeadLetterJobFilters) (string, []interface{}) {
	var conditions []string
	var args []interface{}
	argIdx := 1

	if filters.Kind != nil {
		conditions = append(conditions, fmt.Sprintf("kind = $%d", argIdx))
		args = append(args, *filters.Kind)
		argIdx++
	}
	if filters.UserID != nil {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", argIdx))
		args = append(args, *filters.UserID)
		argIdx++
	}
	if filters.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("failed_at >= $%d", argIdx))
		args = append(args, *filters.StartDate)
		argIdx++
	}
	if filters.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("failed_at <= $%d", argIdx))
		args = append(args, *filters.EndDate)
		argIdx++
	}
	if filters.Retried != nil {
		if *filters.Retried {
			conditions = append(conditions, "retried_at IS NOT NULL")
		} else {
			conditions = append(conditions, "retried_at IS NULL")
		}
	}

	if len(conditions) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

// DeleteByID permanently deletes a single dead letter job by ID.
func (r *DeadLetterJobRepository) DeleteByID(ctx context.Context, id int64) error {
	query := `DELETE FROM dead_letter_jobs WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id)
	return err
}

// DeleteOlderThan deletes dead letter jobs older than the given time.
// If skipWithNotes is true, jobs that have admin notes are preserved.
// Returns the number of deleted rows.
func (r *DeadLetterJobRepository) DeleteOlderThan(ctx context.Context, before time.Time, skipWithNotes bool) (int64, error) {
	var query string
	if skipWithNotes {
		query = `DELETE FROM dead_letter_jobs WHERE failed_at < $1 AND notes IS NULL`
	} else {
		query = `DELETE FROM dead_letter_jobs WHERE failed_at < $1`
	}

	result, err := r.db.Exec(ctx, query, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// DeleteByFilters bulk-deletes dead letter jobs matching the given filters.
// Returns the number of deleted rows.
func (r *DeadLetterJobRepository) DeleteByFilters(ctx context.Context, filters domain.DeadLetterJobFilters) (int64, error) {
	where, args := buildDLQWhereClause(filters)
	query := fmt.Sprintf(`DELETE FROM dead_letter_jobs %s`, where)

	result, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// AtomicMarkRetried atomically claims a DLQ job for retry using a compare-and-set.
// Returns true if the claim succeeded (retried_at was NULL), false if already retried.
// This prevents double-retry race conditions at the database level.
func (r *DeadLetterJobRepository) AtomicMarkRetried(ctx context.Context, id int64, retryJobID int64) (bool, error) {
	query := `
		UPDATE dead_letter_jobs
		SET retried_at = $1, retry_job_id = $2
		WHERE id = $3 AND retried_at IS NULL
		RETURNING id`

	var returnedID int64
	err := r.db.QueryRow(ctx, query, time.Now(), retryJobID, id).Scan(&returnedID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
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

// CleanupOldJobs deletes dead letter entries older than retentionDays, skipping entries with notes.
// This is a convenience wrapper around DeleteOlderThan used by the DLQ cleanup worker.
func (r *DeadLetterJobRepository) CleanupOldJobs(ctx context.Context, retentionDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	return r.DeleteOlderThan(ctx, cutoff, true)
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
