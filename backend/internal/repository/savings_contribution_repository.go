package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// SavingsContributionRepository manages the savings_goal_contributions ledger (Issue #1300).
// Only "confirmed" contributions count toward goal progress (progress_basis = "contributions").
// Auto-derived rows (managed_by_reconciliation = true) may be re-derived or deleted by the
// reconciliation service; user-entered/confirmed rows (managed_by_reconciliation = false)
// are never touched by the reconciler (Codex C3).
type SavingsContributionRepository interface {
	// Insert inserts a new contribution row. ID is generated if empty.
	Insert(ctx context.Context, contrib *domain.SavingsGoalContribution) error

	// UpsertFromTransaction inserts or updates a contribution linked to a transaction.
	// Race-safe: locks the transaction row (SELECT … FOR UPDATE), checks that
	// the new amount does not exceed the transaction's remaining allocation capacity
	// across all goals (Codex C2), then inserts on the partial unique key
	// (transaction_id, savings_goal_id) with idempotent upsert semantics.
	// Returns ErrContributionOverAllocated if the transaction would be over-allocated.
	UpsertFromTransaction(ctx context.Context, goalID, transactionID string, amount float64, source string) error

	// SumConfirmedByGoal returns the sum of all confirmed contributions for a goal.
	// Used by RecomputeGoal when progress_basis = "contributions".
	SumConfirmedByGoal(ctx context.Context, goalID string) (float64, error)

	// SumConfirmedByGoalInPeriod returns the confirmed contribution sum within a date range.
	// Used by budget_sync_service to derive savings category "spent" for a period.
	SumConfirmedByGoalInPeriod(ctx context.Context, goalID string, start, end time.Time) (float64, error)

	// ListByGoal returns all contributions for a goal ordered by occurred_on DESC.
	ListByGoal(ctx context.Context, goalID string) ([]*domain.SavingsGoalContribution, error)

	// DeleteManagedForTransaction removes all managed_by_reconciliation rows for a transaction.
	// Called before deleting a transaction to avoid phantom progress (Codex C4).
	DeleteManagedForTransaction(ctx context.Context, transactionID string) error

	// ArchiveByGoal clears managed_by_reconciliation on all rows for a goal so the
	// reconciler no longer modifies them. Used when switching progress_basis away from
	// "contributions" — rows become historical records (Codex C10 basis-switch semantics).
	ArchiveByGoal(ctx context.Context, goalID string) error
}

// contributionSelectCols is the canonical column list for contribution rows.
const contributionSelectCols = `
	id, savings_goal_id, transaction_id, amount, source, status,
	managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at
`

// savingsContributionRepository implements SavingsContributionRepository using PostgreSQL.
type savingsContributionRepository struct {
	db DB
}

// NewSavingsContributionRepository creates a new SavingsContributionRepository.
func NewSavingsContributionRepository(db DB) SavingsContributionRepository {
	return &savingsContributionRepository{db: db}
}

// Insert inserts a new contribution row. ID is generated if empty.
func (r *savingsContributionRepository) Insert(ctx context.Context, contrib *domain.SavingsGoalContribution) error {
	if contrib.ID == "" {
		contrib.ID = uuid.New().String()
	}

	now := time.Now()
	contrib.CreatedAt = now
	contrib.UpdatedAt = now

	if contrib.Status == "" {
		contrib.Status = "confirmed"
	}

	query := `
		INSERT INTO savings_goal_contributions (
			id, savings_goal_id, transaction_id, amount, source, status,
			managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err := r.db.Exec(ctx, query,
		contrib.ID,
		contrib.SavingsGoalID,
		contrib.TransactionID,
		contrib.Amount,
		contrib.Source,
		contrib.Status,
		contrib.ManagedByReconciliation,
		contrib.ConfirmedByUser,
		contrib.OccurredOn,
		contrib.CreatedAt,
		contrib.UpdatedAt,
	)
	return err
}

// UpsertFromTransaction inserts or updates a transaction-linked contribution.
// Acquires a row-level lock on the transaction to prevent concurrent over-allocation
// (Codex C2). Uses ON CONFLICT on the partial unique index
// (transaction_id, savings_goal_id) WHERE transaction_id IS NOT NULL.
func (r *savingsContributionRepository) UpsertFromTransaction(
	ctx context.Context,
	goalID, transactionID string,
	amount float64,
	source string,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Step 1: Lock the transaction row and get its magnitude.
	var txnMagnitude float64
	lockQuery := `SELECT ABS(amount) FROM transactions WHERE id = $1 FOR UPDATE`
	if err = tx.QueryRow(ctx, lockQuery, transactionID).Scan(&txnMagnitude); err != nil {
		if err == pgx.ErrNoRows {
			return domain.ErrContributionNotFound
		}
		return err
	}

	// Step 2: Compute remaining capacity = |txn.amount| - SUM(confirmed, excluding this goal).
	// Excluding this goal lets us update (upsert) an existing allocation.
	var allocatedElsewhere float64
	capQuery := `
		SELECT COALESCE(SUM(amount), 0)
		FROM savings_goal_contributions
		WHERE transaction_id = $1
		  AND status = 'confirmed'
		  AND savings_goal_id != $2
	`
	if err = tx.QueryRow(ctx, capQuery, transactionID, goalID).Scan(&allocatedElsewhere); err != nil {
		return err
	}

	remaining := txnMagnitude - allocatedElsewhere
	if amount > remaining {
		err = domain.ErrContributionOverAllocated
		return err
	}

	// Step 3: Upsert on the partial unique key.
	now := time.Now()
	upsertQuery := `
		INSERT INTO savings_goal_contributions (
			id, savings_goal_id, transaction_id, amount, source, status,
			managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at
		) VALUES (gen_random_uuid(), $1, $2, $3, $4, 'confirmed', true, false, CURRENT_DATE, $5, $5)
		ON CONFLICT (transaction_id, savings_goal_id) WHERE transaction_id IS NOT NULL
		DO UPDATE SET
			amount     = EXCLUDED.amount,
			source     = EXCLUDED.source,
			updated_at = EXCLUDED.updated_at
	`
	if _, err = tx.Exec(ctx, upsertQuery, goalID, transactionID, amount, source, now); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

// SumConfirmedByGoal returns the sum of all confirmed contributions for a goal.
func (r *savingsContributionRepository) SumConfirmedByGoal(ctx context.Context, goalID string) (float64, error) {
	query := `
		SELECT COALESCE(SUM(amount), 0)
		FROM savings_goal_contributions
		WHERE savings_goal_id = $1 AND status = 'confirmed'
	`
	var total float64
	if err := r.db.QueryRow(ctx, query, goalID).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// SumConfirmedByGoalInPeriod returns the confirmed contribution total within occurred_on range.
func (r *savingsContributionRepository) SumConfirmedByGoalInPeriod(ctx context.Context, goalID string, start, end time.Time) (float64, error) {
	query := `
		SELECT COALESCE(SUM(amount), 0)
		FROM savings_goal_contributions
		WHERE savings_goal_id = $1
		  AND status = 'confirmed'
		  AND occurred_on >= $2::date
		  AND occurred_on <= $3::date
	`
	var total float64
	if err := r.db.QueryRow(ctx, query, goalID, start, end).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// scanContribution scans a single contribution row.
func scanContribution(row scannable) (*domain.SavingsGoalContribution, error) {
	c := &domain.SavingsGoalContribution{}
	err := row.Scan(
		&c.ID,
		&c.SavingsGoalID,
		&c.TransactionID,
		&c.Amount,
		&c.Source,
		&c.Status,
		&c.ManagedByReconciliation,
		&c.ConfirmedByUser,
		&c.OccurredOn,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	return c, err
}

// scannable is satisfied by both pgx.Row and pgx.Rows (for single-row helper).
type scannable interface {
	Scan(dest ...interface{}) error
}

// ListByGoal returns all contributions for a goal ordered newest-first.
func (r *savingsContributionRepository) ListByGoal(ctx context.Context, goalID string) ([]*domain.SavingsGoalContribution, error) {
	query := `SELECT ` + contributionSelectCols + `
		FROM savings_goal_contributions
		WHERE savings_goal_id = $1
		ORDER BY occurred_on DESC, created_at DESC`

	rows, err := r.db.Query(ctx, query, goalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contribs []*domain.SavingsGoalContribution
	for rows.Next() {
		c, err := scanContribution(rows)
		if err != nil {
			return nil, err
		}
		contribs = append(contribs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(contribs) == 0 {
		return []*domain.SavingsGoalContribution{}, nil
	}
	return contribs, nil
}

// DeleteManagedForTransaction removes all reconciler-owned contributions for a transaction.
// Must be called BEFORE deleting the transaction to prevent phantom progress (Codex C4).
func (r *savingsContributionRepository) DeleteManagedForTransaction(ctx context.Context, transactionID string) error {
	query := `
		DELETE FROM savings_goal_contributions
		WHERE transaction_id = $1 AND managed_by_reconciliation = true
	`
	_, err := r.db.Exec(ctx, query, transactionID)
	return err
}

// ArchiveByGoal clears managed_by_reconciliation on all rows for a goal so they
// become permanent historical records that the reconciler will not modify.
// Called when switching a goal's progress_basis away from "contributions" (Codex C10).
func (r *savingsContributionRepository) ArchiveByGoal(ctx context.Context, goalID string) error {
	query := `
		UPDATE savings_goal_contributions
		SET managed_by_reconciliation = false, updated_at = NOW()
		WHERE savings_goal_id = $1 AND managed_by_reconciliation = true
	`
	_, err := r.db.Exec(ctx, query, goalID)
	return err
}
