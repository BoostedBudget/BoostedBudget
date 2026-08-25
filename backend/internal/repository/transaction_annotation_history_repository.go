package repository

import (
	"context"
	"fmt"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1569 Task 6 (#1579) — the READ half of the annotation trail.
//
// There is deliberately no Create here. The writer is not a Go call at all: it is a CTE inside each
// of the three UPDATE statements in transaction_repository.go
// (UpdateCategory, BulkUpdateCategory, SetReviewed), because the value being recorded is the value
// that same statement destroys. Codex's finding on this task was that a SELECT-then-UPDATE writer
// records a WRONG prior value under concurrency; putting the INSERT in the same statement removes
// the window rather than narrowing it, and it also makes it impossible to add a fourth write path
// that forgets to call the writer — a new statement either carries the CTE or it does not update
// the annotation at all.
//
// A writer with no reader ships nothing observable (the lesson task 5 paid for on
// rule_execution_history, which had a table and a migration and no way to see a single row), so
// this repository, a service and an endpoint land in the same commit as the capture.

// TransactionAnnotationHistoryRepository reads a user's annotation-overwrite history.
type TransactionAnnotationHistoryRepository interface {
	// GetByUserID returns the user's annotation changes, newest first, with the total row count.
	GetByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.TransactionAnnotationChange, int, error)
}

type transactionAnnotationHistoryRepository struct {
	db DB
}

// NewTransactionAnnotationHistoryRepository creates a TransactionAnnotationHistoryRepository.
func NewTransactionAnnotationHistoryRepository(db DB) TransactionAnnotationHistoryRepository {
	return &transactionAnnotationHistoryRepository{db: db}
}

// transactionAnnotationHistoryColumns is written once so a column added to the table cannot be
// added to the scan of one read and forgotten in another.
const transactionAnnotationHistoryColumns = `id, user_id, transaction_id, change_source,
	prior_category_override_kind, prior_pfc_primary_override, prior_pfc_detailed_override,
	prior_custom_category_override_id, prior_effective_category_name, prior_category_source,
	prior_reviewed, prior_reviewed_at,
	new_category_override_kind, new_pfc_primary_override, new_pfc_detailed_override,
	new_custom_category_override_id, new_effective_category_name, new_category_source,
	new_reviewed, new_reviewed_at,
	changed_at`

// GetByUserID returns the user's annotation history, newest first.
//
// TENANCY is the row's own denormalised user_id and nothing else. It is NOT a join to
// `transactions`: a transaction can be tombstoned (task 7) or hard-deleted by one of the three
// purges, and a join would then hide exactly the rows that matter most — the record of an
// annotation on a transaction the user can no longer see. Retention that reads as deletion is the
// failure this whole issue is about.
//
// ⚠ NO deleted_at FILTER, AND THAT IS NOT AN OVERSIGHT. This table has no deleted_at, and the
// tombstone state of the referenced transaction is deliberately not consulted here, for the reason
// above. The tombstone guards scan reads OF the tombstoned tables; this reads its own table.
//
// The id tiebreak is load-bearing: changed_at is clock_timestamp() with microsecond resolution and
// a bulk recategorisation writes every row from a single statement, so two rows can share a
// microsecond and an unstable sort would make pagination drop or repeat rows.
func (r *transactionAnnotationHistoryRepository) GetByUserID(
	ctx context.Context,
	userID string,
	limit, offset int,
) ([]*domain.TransactionAnnotationChange, int, error) {
	var total int
	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM transaction_annotation_history WHERE user_id = $1`, userID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count transaction annotation changes: %w", err)
	}

	rows, err := r.db.Query(ctx, `
		SELECT `+transactionAnnotationHistoryColumns+`
		FROM transaction_annotation_history
		WHERE user_id = $1
		ORDER BY changed_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query transaction annotation changes: %w", err)
	}
	defer rows.Close()

	changes := make([]*domain.TransactionAnnotationChange, 0)
	for rows.Next() {
		c := &domain.TransactionAnnotationChange{}
		if err := rows.Scan(
			&c.ID,
			&c.UserID,
			&c.TransactionID,
			&c.ChangeSource,
			&c.Prior.CategoryOverrideKind,
			&c.Prior.PFCPrimaryOverride,
			&c.Prior.PFCDetailedOverride,
			&c.Prior.CustomCategoryOverrideID,
			&c.Prior.EffectiveCategoryName,
			&c.Prior.CategorySource,
			&c.Prior.Reviewed,
			&c.Prior.ReviewedAt,
			&c.New.CategoryOverrideKind,
			&c.New.PFCPrimaryOverride,
			&c.New.PFCDetailedOverride,
			&c.New.CustomCategoryOverrideID,
			&c.New.EffectiveCategoryName,
			&c.New.CategorySource,
			&c.New.Reviewed,
			&c.New.ReviewedAt,
			&c.ChangedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan transaction annotation change: %w", err)
		}
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("failed to iterate transaction annotation changes: %w", err)
	}

	return changes, total, nil
}
