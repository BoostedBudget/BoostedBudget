// Package repository provides data access implementations.
package repository

import (
	"context"
	"encoding/json"
	"errors"
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

// dlqOrderByAllowlist maps sort_by column names to safe SQL ORDER BY entries.
var dlqOrderByAllowlist = map[string]orderByEntry{
	"failed_at":  {col: "failed_at"},
	"kind":       {col: "kind"},
	"retried_at": {col: "retried_at", nullsLast: true}, // nullable — NULLS LAST
}

// ListAll returns a paginated list of dead letter jobs with optional filters.
// Returns the jobs, total count, and any error.
func (r *DeadLetterJobRepository) ListAll(ctx context.Context, filters domain.DeadLetterJobFilters, limit, offset int) ([]*domain.DeadLetterJob, int, error) {
	where, args := buildDLQWhereClause(filters)

	entry := dlqOrderByAllowlist[filters.SortBy]
	if entry.col == "" {
		entry = orderByEntry{col: "failed_at"} // fallback
	}
	orderDir := strings.ToUpper(filters.SortDir)
	if orderDir != "ASC" && orderDir != "DESC" {
		orderDir = "DESC"
	}
	nullsClause := ""
	if entry.nullsLast {
		nullsClause = " NULLS LAST"
	}

	listQuery := fmt.Sprintf(`
		SELECT id, original_job_id, kind, queue, args, errors, max_attempts,
		       created_at, failed_at, user_id, notified, retried_at, retry_job_id, notes
		FROM dead_letter_jobs
		%s
		ORDER BY %s %s%s
		LIMIT %d OFFSET %d`, where, entry.col, orderDir, nullsClause, limit, offset)

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
		argIdx++ //nolint:ineffassign // kept so a filter added below inherits the correct arg index
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

// CountByFilters returns how many dead letter jobs match the given filters, using the
// same WHERE construction as DeleteByFilters so the preview and the delete can never
// disagree about what "these filters" means. It is the read half of the bulk-delete
// confirmation handshake in operations.BulkDeleteDLQJobs.
func (r *DeadLetterJobRepository) CountByFilters(ctx context.Context, filters domain.DeadLetterJobFilters) (int64, error) {
	where, args := buildDLQWhereClause(filters)
	query := fmt.Sprintf(`SELECT COUNT(*) FROM dead_letter_jobs %s`, where)

	var total int64
	if err := r.db.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// ErrDLQBulkDeleteCountDrifted reports that the number of rows the DELETE would have removed
// was not the number the caller confirmed, so nothing was removed. It is a distinct error
// because the correct operator response is to re-preview and re-confirm, not to retry blindly.
var ErrDLQBulkDeleteCountDrifted = errors.New("dead letter bulk delete: matching row count changed since it was confirmed")

// DeleteByFilters bulk-deletes dead letter jobs matching the given filters, but ONLY if
// exactly `expected` rows match when the DELETE runs. Returns the number of deleted rows.
//
// Issue #1432, batch EE re-review. CountByFilters and this DELETE were two independent
// statements, so under READ COMMITTED they took two snapshots: a dead letter job committed
// between them was deleted without ever having been counted, previewed, or confirmed — the
// confirmation handshake in operations.BulkDeleteDLQJobs was advisory rather than binding.
// Running the DELETE in its own transaction and aborting unless RowsAffected equals the
// confirmed count makes the handshake mean what it says: either the caller's number was
// still true and the delete commits, or nothing at all is removed and the caller re-previews.
// This is why `expected` is a parameter rather than something re-derived here — the number
// that matters is the one the OPERATOR saw and confirmed, not a fresher one.
//
// The WHERE clause construction is unchanged and still shared with CountByFilters, so the two
// can never disagree about what "these filters" means; the filterless case remains the
// repository's deliberate unconditional DELETE, refused one layer up.
func (r *DeadLetterJobRepository) DeleteByFilters(ctx context.Context, filters domain.DeadLetterJobFilters, expected int64) (int64, error) {
	where, args := buildDLQWhereClause(filters)
	query := fmt.Sprintf(`DELETE FROM dead_letter_jobs %s`, where)

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	// Rollback after a successful Commit is a no-op in pgx, so the deferred abort is safe on
	// every path, including a panic.
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	deleted := result.RowsAffected()
	if deleted != expected {
		return 0, fmt.Errorf("%w: confirmed %d, matched %d", ErrDLQBulkDeleteCountDrifted, expected, deleted)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return deleted, nil
}

// BeginRetryTx opens the transaction DeadLetterService.Retry runs its whole replay in — the claim,
// the River insert (queue.JobQueue.EnqueueTx) and the retry-id write, committing as one unit.
//
// Issue #1508: those were three separate commits, and the first two could disagree. A claim that
// committed without an insert spent the row's single replay slot on nothing; an insert whose id
// write then failed left the row at the placeholder with a live job behind it. Both wrote
// retry_job_id = 0, so the column could not tell them apart, and no reaper could be built on it.
// One transaction removes the disagreement rather than trying to detect it: either the row is
// claimed AND carries a real River job id AND that job exists, or none of the three happened.
//
// A tx handle is returned rather than taking a callback because the middle step is not a repository
// operation at all — it is an insert River performs through the same pgx.Tx.
func (r *DeadLetterJobRepository) BeginRetryTx(ctx context.Context) (pgx.Tx, error) {
	return r.db.Begin(ctx)
}

// AtomicMarkRetriedInTx atomically claims a DLQ job for retry using a compare-and-set, inside the
// caller's transaction. Returns true if the claim succeeded (retried_at was NULL), false if the row
// was already retried or does not exist.
//
// The claim must stay BEFORE the enqueue (see DeadLetterService.Retry): claiming afterwards re-opens
// the concurrent double-enqueue race this compare-and-set exists to close. Running it inside the
// transaction preserves that order and strengthens it — the UPDATE takes a row lock that a
// concurrent replay of the same row blocks on until this transaction ends, and then re-evaluates
// `retried_at IS NULL` against the committed result.
//
// retryJobID is the 0 placeholder on the way in; MarkRetriedInTx overwrites it with the real River
// job id before the same transaction commits, so no committed row is ever left at 0.
func (r *DeadLetterJobRepository) AtomicMarkRetriedInTx(ctx context.Context, tx pgx.Tx, id int64, retryJobID int64) (bool, error) {
	query := `
		UPDATE dead_letter_jobs
		SET retried_at = $1, retry_job_id = $2
		WHERE id = $3 AND retried_at IS NULL
		RETURNING id`

	var returnedID int64
	err := tx.QueryRow(ctx, query, time.Now(), retryJobID, id).Scan(&returnedID)
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

// MarkRetriedInTx records the River job id a manual retry produced, inside the transaction that
// took the claim (AtomicMarkRetriedInTx) and made the insert.
//
// This is NOT best-effort — it was, until Issue #1508, and that is precisely what made
// retry_job_id = 0 undecidable: a failed id write left a claimed row with a live job behind it,
// indistinguishable from a claim whose enqueue never happened. Its error must therefore abort the
// replay, taking the claim and the River insert with it.
func (r *DeadLetterJobRepository) MarkRetriedInTx(ctx context.Context, tx pgx.Tx, id int64, retryJobID int64) error {
	query := `UPDATE dead_letter_jobs SET retried_at = $1, retry_job_id = $2 WHERE id = $3`
	_, err := tx.Exec(ctx, query, time.Now(), retryJobID, id)
	return err
}

// AddNotes adds admin notes to a dead letter job.
func (r *DeadLetterJobRepository) AddNotes(ctx context.Context, id int64, notes string) error {
	query := `UPDATE dead_letter_jobs SET notes = $1 WHERE id = $2`
	_, err := r.db.Exec(ctx, query, notes, id)
	return err
}


// replayResolvedPredicate is the single definition of "this DLQ row's replay actually happened",
// shared by GetStats and GetOldestJobAge so the success count and the age alarm can never disagree
// about it — round 15, Tier E item 6.
//
// ⚠ `retried_at IS NOT NULL` ALONE IS NOT THAT QUESTION, and reading it as if it were is the defect.
// The pre-#1508 two-commit path could commit the claim (retried_at set) with the id write never
// landing, leaving the row at the retry_job_id = 0 placeholder — a state BeginRetryTx's own comment
// describes as undecidable: the column cannot say whether a live River job sits behind it. Those
// rows are historical and no new one can be created (AtomicMarkRetriedInTx and MarkRetriedInTx now
// commit together or not at all), but the ones that exist were being read in the two most flattering
// ways simultaneously:
//
//   - GetStats counted them under `retried`, i.e. as SUCCESSES. A row whose replay demonstrably did
//     not complete was inflating the number an operator reads as "how many of these we recovered".
//   - GetOldestJobAge's `WHERE retried_at IS NULL` filtered them OUT of the age alarm. So the one
//     signal that says "something has been sitting in the dead letter queue too long" was blind to
//     precisely the rows that are stuck with nobody looking at them.
//
// Both now use this predicate instead: a stranded row is not a success and it is not resolved, so it
// counts as neither, and the age alarm sees it.
//
// ⚠ NO REAPER, AND THAT IS DELIBERATE. #1508 is explicit that these rows cannot be released
// automatically without risking a double-enqueue of a job that may still be running. This change
// makes them VISIBLE; deciding what to do with each one stays a human's job, which is what a dead
// letter queue is for. The age alarm is clearable by an operator resolving or deleting the row —
// it is not an alarm with no action behind it.
const replayResolvedPredicate = `(retried_at IS NOT NULL AND retry_job_id IS NOT NULL AND retry_job_id <> 0)`

// GetStats returns statistics about dead letter jobs.
func (r *DeadLetterJobRepository) GetStats(ctx context.Context) (*DeadLetterStats, error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE notified = FALSE AND user_id IS NOT NULL) as unnotified,
			COUNT(*) FILTER (WHERE ` + replayResolvedPredicate + `) as retried,
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

// GetOldestJobAge returns the age in hours of the oldest UNRESOLVED dead letter job.
// Returns 0 if none exist.
//
// "Unresolved" is replayResolvedPredicate's complement, not `retried_at IS NULL` — see that const
// for why the two differ and which rows fell through the gap (round 15, Tier E item 6).
func (r *DeadLetterJobRepository) GetOldestJobAge(ctx context.Context) (int64, error) {
	query := `
		SELECT COALESCE(EXTRACT(epoch FROM (NOW() - MIN(failed_at)))::BIGINT / 3600, 0)
		FROM dead_letter_jobs
		WHERE NOT ` + replayResolvedPredicate + `
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
