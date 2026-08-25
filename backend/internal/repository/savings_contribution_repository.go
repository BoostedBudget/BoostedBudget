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
	// Insert inserts a new manually-logged contribution. ID is generated if empty.
	// userID is required: the savings goal must belong to userID (Codex C1 / IDOR guard).
	// If contrib.TransactionID is non-nil, the referenced transaction must also belong to userID.
	Insert(ctx context.Context, userID string, contrib *domain.SavingsGoalContribution) error

	// UpsertFromTransaction inserts or updates a contribution linked to a transaction.
	// userID is required: both the savings goal and the transaction must belong to
	// userID or the call is rejected (Codex C1 / G7 ownership guard).
	// Race-safe: locks the transaction row (SELECT … FOR UPDATE), checks that
	// the new amount does not exceed the transaction's remaining allocation capacity
	// across all goals (Codex C2), then inserts on the partial unique key
	// (transaction_id, savings_goal_id) with idempotent upsert semantics.
	// Returns ErrSavingsGoalNotFound if goalID does not belong to userID.
	// Returns ErrContributionNotFound if transactionID does not belong to userID.
	// Returns ErrContributionOverAllocated if the transaction would be over-allocated.
	UpsertFromTransaction(ctx context.Context, userID, goalID, transactionID string, amount float64, source string) error

	// SumConfirmedByGoal returns the sum of all confirmed contributions for a goal.
	// Used by RecomputeGoal when progress_basis = "contributions".
	// userID is required to enforce ownership scoping (RG2 / M17).
	SumConfirmedByGoal(ctx context.Context, userID, goalID string) (float64, error)

	// SumConfirmedByGoalInPeriod returns the confirmed contribution sum within a date range.
	// Used by budget_sync_service to derive savings category "spent" for a period.
	// userID is required to enforce ownership scoping (RG2 / M17).
	SumConfirmedByGoalInPeriod(ctx context.Context, userID, goalID string, start, end time.Time) (float64, error)

	// UpsertProposedFromTransaction inserts or updates a managed_by_reconciliation=true,
	// status='proposed' contribution linked to a transaction. Unlike UpsertFromTransaction,
	// this call skips the allocation-cap check because proposed rows do NOT consume capacity
	// until the user confirms them (M18 / D6 source-b).
	// userID is required: both the savings goal and the transaction must belong to userID.
	UpsertProposedFromTransaction(ctx context.Context, userID, goalID, transactionID string, magnitude float64) error

	// ListByGoal returns all contributions for a goal ordered by occurred_on DESC.
	// userID is required to enforce ownership scoping (Codex C1 / M20 contribution API).
	// Excludes TOMBSTONED goals: use ListByDeletedGoal to read a deleted goal's ledger.
	ListByGoal(ctx context.Context, userID, goalID string) ([]*domain.SavingsGoalContribution, error)

	// ListByDeletedGoal returns the contributions of a TOMBSTONED goal, ordered like ListByGoal.
	// It is the deliberate inverse of ListByGoal and exists for the same reason
	// SavingsGoalRepository.GetDeletedByUserID does: task 8 made DELETE /savings-goals/{id} a
	// tombstone and told the user, in the endpoint's own description, that "every contribution
	// logged against the goal is retained" — while ListByGoal's `g.deleted_at IS NULL` join made
	// GET /savings-goals/{goalID}/contributions answer 200 with an empty array, indistinguishable
	// from a goal that never had one. There is no restore path anywhere on this branch, so those
	// itemised entries — dates, amounts, sources, and money the user typed in BY HAND, which no
	// provider can supply again — could never be read back. Retention nobody can read is deletion
	// with a longer receipt (#1569 Task 12's phrasing, and T2's defect).
	//
	// Scoped to the caller's own user_id: the TOMBSTONE predicate is inverted, the TENANCY
	// predicate is not. Returns an empty slice for a live goal — this is a separate read, NOT a
	// relaxed one, because widening ListByGoal/SumConfirmedByGoal would resurrect deleted goals
	// into progress recomputation and budget "spent" totals.
	//
	// Both directions are pinned by TestSavingsContributions_DeletedGoal_Integration: it asserts
	// that this read sees the tombstoned goal AND that ListByGoal, SumConfirmedByGoal and
	// SumConfirmedByGoalInPeriod still do not. (This copy used to exist because the reasons in the
	// SQL below live in a raw string literal, which the cited-test-name guard in
	// internal/testsupport/citations could not see. It can now — #1569 T8 taught it to read SQL
	// comments inside string literals — so both copies are checked, and this one is kept only
	// because a reader of the interface should not have to open the implementation.)
	ListByDeletedGoal(ctx context.Context, userID, goalID string) ([]*domain.SavingsGoalContribution, error)

	// SumConfirmedByDeletedGoal is SumConfirmedByGoal for a TOMBSTONED goal: the confirmed total
	// the user is owed a truthful figure for once the goal is gone. It feeds the deleted-goal
	// contributions endpoint only; it is NOT wired into progress recomputation or budget sync, and
	// must never be — see ListByDeletedGoal for why the live reads stay narrow.
	SumConfirmedByDeletedGoal(ctx context.Context, userID, goalID string) (float64, error)

	// ConfirmContribution promotes a proposed contribution to confirmed status.
	// userID is required for ownership verification (Codex C1).
	// Race-safe: if the contribution references a transaction, locks the txn row and
	// re-checks the allocation capacity before flipping status to 'confirmed' (Codex C2).
	// Returns ErrContributionNotFound if id is not found or does not belong to userID.
	// Returns ErrContributionOverAllocated if confirming would exceed the txn's capacity.
	// Returns ErrContributionAlreadyConfirmed if the row is already confirmed.
	ConfirmContribution(ctx context.Context, userID, id string) (*domain.SavingsGoalContribution, error)

	// RejectContribution sets a proposed contribution's status to 'rejected' (sticky tombstone).
	// A rejected row persists in the table so that UpsertProposedFromTransaction's ON CONFLICT
	// clause skips re-proposing the same (transaction_id, savings_goal_id) pair on future syncs
	// (R3-7). Returns ErrContributionNotFound if id is not found or does not belong to userID.
	// Returns ErrContributionNotProposed if the row is not in 'proposed' status.
	RejectContribution(ctx context.Context, userID, id string) (*domain.SavingsGoalContribution, error)

	// DeleteContribution removes a contribution. userID is required for ownership verification.
	// Returns ErrContributionNotFound if id does not exist or does not belong to userID.
	DeleteContribution(ctx context.Context, userID, id string) error

	// DeleteManagedForTransaction removes managed_by_reconciliation rows for a transaction whose
	// goal is still LIVE. Called before removing a transaction to avoid phantom progress (Codex C4).
	// Rows belonging to a TOMBSTONED goal are left alone: they are that goal's retained ledger and a
	// provider re-statement is not a deletion request (#1569 C1).
	DeleteManagedForTransaction(ctx context.Context, transactionID string) error

	// ArchiveByGoal clears managed_by_reconciliation on all rows for a goal so the
	// reconciler no longer modifies them. Used when switching progress_basis away from
	// "contributions" — rows become historical records (Codex C10 basis-switch semantics).
	ArchiveByGoal(ctx context.Context, goalID string) error

	// InsertManualFromTransaction inserts (or updates) a user-initiated contribution that
	// is linked to a specific transaction. Keeps manual semantics: managed_by_reconciliation=false,
	// confirmed_by_user=true, source='manual', occurred_on=caller's date.
	//
	// This is the R4-1 fix for the defect where R3-1 routed txn-linked manual logs through
	// UpsertFromTransaction, which hardcodes reconciler metadata (managed=true, today's date)
	// and returns no row — violating C3 (reconciler owns) and D6 (user-entered rows are faithful).
	//
	// Race-safe: same BEGIN+goal-ownership+SELECT ABS(amount)…FOR UPDATE+capacity-recompute
	// as UpsertFromTransaction (Codex C2), then upserts with manual metadata and RETURNING the row.
	// Returns ErrSavingsGoalNotFound, ErrContributionNotFound, or ErrContributionOverAllocated.
	InsertManualFromTransaction(ctx context.Context, userID, goalID, transactionID string, amount float64, occurredOn string) (*domain.SavingsGoalContribution, error)
}

// contributionSelectCols is the canonical column list for contribution rows.
const contributionSelectCols = `
	id, savings_goal_id, transaction_id, amount, source, status,
	managed_by_reconciliation, confirmed_by_user, occurred_on::text, created_at, updated_at
`

// contributionSelectColsJoined is contributionSelectCols for queries that JOIN savings_goals.
//
// Both tables have an `id`, so the unqualified list is ambiguous the moment a JOIN is present and
// Postgres rejects the whole statement with `column reference "id" is ambiguous`. Use this one in a
// JOIN context and contributionSelectCols in a single-table SELECT or a RETURNING clause (where no
// alias exists to qualify with).
//
// Both cast occurred_on to text: the column is DATE and SavingsGoalContribution.OccurredOn is a
// `string` holding YYYY-MM-DD, and pgx cannot scan a binary-format date straight into *string.
//
// Neither problem is #1569's — the tombstone work only added `deleted_at IS NULL` to five
// ownership probes in this file. They were found because #1569 round 1 added the first integration
// coverage these three methods have ever had (finding M-1), and both defects made the affected
// methods fail 100% of the time against a real database. See
// TestSavingsContributionRepository_Integration_*_RejectsTombstoned.
const contributionSelectColsJoined = `
	c.id, c.savings_goal_id, c.transaction_id, c.amount, c.source, c.status,
	c.managed_by_reconciliation, c.confirmed_by_user, c.occurred_on::text, c.created_at, c.updated_at
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
func (r *savingsContributionRepository) Insert(ctx context.Context, userID string, contrib *domain.SavingsGoalContribution) error {
	if contrib.ID == "" {
		contrib.ID = uuid.New().String()
	}

	// Verify goal ownership (Codex C1 / IDOR guard) — mirrors UpsertFromTransaction.
	var dummy int
	if err := r.db.QueryRow(ctx,
		`SELECT 1 FROM savings_goals WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		contrib.SavingsGoalID, userID,
	).Scan(&dummy); err != nil {
		if err == pgx.ErrNoRows {
			return domain.ErrSavingsGoalNotFound
		}
		return err
	}

	// Verify transaction ownership if an optional link was supplied.
	if contrib.TransactionID != nil {
		if err := r.db.QueryRow(ctx,
			`SELECT 1 FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
			*contrib.TransactionID, userID,
		).Scan(&dummy); err != nil {
			if err == pgx.ErrNoRows {
				return domain.ErrContributionNotFound
			}
			return err
		}
	}

	now := time.Now()
	contrib.CreatedAt = now
	contrib.UpdatedAt = now

	if contrib.Status == "" {
		contrib.Status = "confirmed"
	}

	_, err := r.db.Exec(ctx, `
		INSERT INTO savings_goal_contributions (
			id, savings_goal_id, transaction_id, amount, source, status,
			managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
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
// Verifies that both the goal and the transaction belong to userID (Codex C1 / G7).
// Acquires a row-level lock on the transaction to prevent concurrent over-allocation
// (Codex C2). Uses ON CONFLICT on the partial unique index
// (transaction_id, savings_goal_id) WHERE transaction_id IS NOT NULL.
//
// occurred_on is written as CURRENT_DATE (see the INSERT below), which Postgres evaluates in the
// SESSION time zone — Issue #1461 decision: now that NewPool pins every pooled connection's
// session TimeZone to UTC, CURRENT_DATE here resolves to the UTC calendar day, not any particular
// user's local day. This is a deliberate choice, not an oversight:
//  1. It matches this repository's OWN sibling convention just below (UpsertProposedFromTransaction
//     uses the identical CURRENT_DATE) and the rest of the repository layer's established
//     UTC-calendar-day convention (analytics_repository.go, balance_snapshot_repository.go,
//     savings_impact_repository.go all bucket by AT TIME ZONE 'UTC' explicitly).
//  2. It is very unlikely to change PRODUCTION behavior: managed Postgres providers (this app
//     deploys to Fly.io) default new sessions to UTC already, and nothing in docker-compose.yml or
//     fly.toml pins a different zone — the inconsistency this fix closes was primarily LOCAL DEV
//     (confirmed: this machine's Postgres defaults to America/New_York) disagreeing with
//     presumed-UTC production, not a production behavior change.
//  3. InsertManualFromTransaction (below, occurred_on=caller-supplied) remains the correct choice
//     whenever the caller has and wants to assert a specific date — this UTC-CURRENT_DATE path is
//     only for the two auto-derived/reconciliation-owned paths where "today" is inferred, not
//     asserted.
func (r *savingsContributionRepository) UpsertFromTransaction(
	ctx context.Context,
	userID, goalID, transactionID string,
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

	// Step 0: Verify goal ownership (Codex C1 / G7 ownership guard).
	var dummy int
	goalCheckQuery := `SELECT 1 FROM savings_goals WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`
	if err = tx.QueryRow(ctx, goalCheckQuery, goalID, userID).Scan(&dummy); err != nil {
		if err == pgx.ErrNoRows {
			err = domain.ErrSavingsGoalNotFound
		}
		return err
	}

	// Step 1: Lock the transaction row, verify ownership, and get its magnitude.
	var txnMagnitude float64
	lockQuery := `SELECT ABS(amount) FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL FOR UPDATE`
	if err = tx.QueryRow(ctx, lockQuery, transactionID, userID).Scan(&txnMagnitude); err != nil {
		if err == pgx.ErrNoRows {
			err = domain.ErrContributionNotFound
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
	// Step 3: Upsert as confirmed. Also promotes a pre-existing proposed row to confirmed
	// so that a boost-match (M19) or user-confirm does not create a duplicate.
	upsertQuery := `
		INSERT INTO savings_goal_contributions (
			id, savings_goal_id, transaction_id, amount, source, status,
			managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at
		) VALUES (gen_random_uuid(), $1, $2, $3, $4, 'confirmed', true, false, CURRENT_DATE, $5, $5)
		ON CONFLICT (transaction_id, savings_goal_id) WHERE transaction_id IS NOT NULL
		DO UPDATE SET
			amount     = EXCLUDED.amount,
			source     = EXCLUDED.source,
			status     = 'confirmed',
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

// InsertManualFromTransaction inserts or updates a user-initiated contribution linked to
// a transaction (D6 source-a, txn-linked variant — R4-1 fix).
// Unlike UpsertFromTransaction (auto-derivation/boost paths), this method preserves manual
// semantics: managed_by_reconciliation=false, confirmed_by_user=true, source='manual',
// occurred_on=caller-supplied date (not CURRENT_DATE). Returns the persisted row.
// Race-safe: acquires a row-level lock on the transaction (SELECT ABS(amount) … FOR UPDATE)
// and checks capacity before upsert (Codex C2). Uses the same partial unique index conflict
// target as UpsertFromTransaction so re-logging the same (txn, goal) is idempotent.
func (r *savingsContributionRepository) InsertManualFromTransaction(
	ctx context.Context,
	userID, goalID, transactionID string,
	amount float64,
	occurredOn string,
) (*domain.SavingsGoalContribution, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Step 0: Verify goal ownership (Codex C1 / G7 ownership guard).
	var dummy int
	goalCheckQuery := `SELECT 1 FROM savings_goals WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`
	if err = tx.QueryRow(ctx, goalCheckQuery, goalID, userID).Scan(&dummy); err != nil {
		if err == pgx.ErrNoRows {
			err = domain.ErrSavingsGoalNotFound
		}
		return nil, err
	}

	// Step 1: Lock the transaction row, verify ownership, and get its magnitude.
	var txnMagnitude float64
	lockQuery := `SELECT ABS(amount) FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL FOR UPDATE`
	if err = tx.QueryRow(ctx, lockQuery, transactionID, userID).Scan(&txnMagnitude); err != nil {
		if err == pgx.ErrNoRows {
			err = domain.ErrContributionNotFound
		}
		return nil, err
	}

	// Step 2: Compute remaining capacity = |txn.amount| - SUM(confirmed, excluding this goal).
	var allocatedElsewhere float64
	capQuery := `
		SELECT COALESCE(SUM(amount), 0)
		FROM savings_goal_contributions
		WHERE transaction_id = $1
		  AND status = 'confirmed'
		  AND savings_goal_id != $2
	`
	if err = tx.QueryRow(ctx, capQuery, transactionID, goalID).Scan(&allocatedElsewhere); err != nil {
		return nil, err
	}

	remaining := txnMagnitude - allocatedElsewhere
	if amount > remaining {
		err = domain.ErrContributionOverAllocated
		return nil, err
	}

	// Step 3: Upsert with manual semantics and RETURNING the persisted row.
	// managed_by_reconciliation=false, confirmed_by_user=true, source='manual', occurred_on=$4.
	// ON CONFLICT: only promote rows that are still in 'proposed' status (e.g. a sync-detected
	// proposed row that the user is now manually confirming with an explicit amount/date).
	// Rows already in 'confirmed' status (boost_match, sync-confirmed, or a prior manual log)
	// and 'rejected' tombstones are NOT overwritten — the WHERE predicate makes the DO UPDATE
	// a no-op, which causes RETURNING to return NULL; the handler will return ErrContributionNotFound
	// in that case. This guards against silent source/metadata corruption (Codex R4-1 follow-up).
	now := time.Now()
	upsertQuery := `
		INSERT INTO savings_goal_contributions (
			id, savings_goal_id, transaction_id, amount, source, status,
			managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at
		) VALUES (gen_random_uuid(), $1, $2, $3, 'manual', 'confirmed', false, true, $4, $5, $5)
		ON CONFLICT (transaction_id, savings_goal_id) WHERE transaction_id IS NOT NULL
		DO UPDATE SET
			amount                    = EXCLUDED.amount,
			source                    = 'manual',
			status                    = 'confirmed',
			managed_by_reconciliation = false,
			confirmed_by_user         = true,
			occurred_on               = EXCLUDED.occurred_on,
			updated_at                = EXCLUDED.updated_at
		WHERE savings_goal_contributions.status = 'proposed'
		RETURNING ` + contributionSelectCols

	row := tx.QueryRow(ctx, upsertQuery, goalID, transactionID, amount, occurredOn, now)
	contrib, scanErr := scanContribution(row)
	if scanErr != nil {
		// ErrNoRows means the ON CONFLICT DO UPDATE WHERE status='proposed' predicate was false:
		// a confirmed or rejected row already holds this (transaction_id, savings_goal_id) slot.
		// Return ErrContributionAlreadyConfirmed so the handler can surface a 422 rather than 500.
		if scanErr == pgx.ErrNoRows {
			err = domain.ErrContributionAlreadyConfirmed
		} else {
			err = scanErr
		}
		return nil, err
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return contrib, nil
}

// UpsertProposedFromTransaction inserts or updates a proposed (status='proposed'),
// managed contribution from a detected transfer-in (D6 source-b / M18).
// Proposed rows skip the allocation-cap gate — they don't consume capacity until confirmed.
// Uses ON CONFLICT on the partial unique key so re-syncing the same txn is idempotent.
// If a row already exists in 'confirmed' status it is NOT downgraded back to proposed.
func (r *savingsContributionRepository) UpsertProposedFromTransaction(
	ctx context.Context,
	userID, goalID, transactionID string,
	magnitude float64,
) error {
	// Step 0: Verify goal ownership.
	var dummy int
	goalCheckQuery := `SELECT 1 FROM savings_goals WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`
	if err := r.db.QueryRow(ctx, goalCheckQuery, goalID, userID).Scan(&dummy); err != nil {
		if err == pgx.ErrNoRows {
			return domain.ErrSavingsGoalNotFound
		}
		return err
	}

	// Step 1: Verify transaction ownership (no lock — proposed rows are best-effort).
	var txnExists int
	txnCheckQuery := `SELECT 1 FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`
	if err := r.db.QueryRow(ctx, txnCheckQuery, transactionID, userID).Scan(&txnExists); err != nil {
		if err == pgx.ErrNoRows {
			return domain.ErrContributionNotFound
		}
		return err
	}

	// Step 2: Upsert as proposed; do NOT downgrade a confirmed or rejected row.
	// Codex C3 (R3-6): restrict the conflict update to rows the reconciler owns
	// (managed_by_reconciliation = true) — manual rows with status='proposed'
	// are left untouched even if they share the same (transaction_id, savings_goal_id) key.
	// R3-7: rejected rows (tombstones) must also NOT be overwritten — the
	// WHERE status='proposed' clause already excludes them.
	// occurred_on=CURRENT_DATE — same Issue #1461 UTC-calendar-day decision as
	// UpsertFromTransaction's own doc comment above; both auto-derived paths share it.
	now := time.Now()
	upsertQuery := `
		INSERT INTO savings_goal_contributions (
			id, savings_goal_id, transaction_id, amount, source, status,
			managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at
		) VALUES (gen_random_uuid(), $1, $2, $3, 'confirmed_transfer', 'proposed', true, false, CURRENT_DATE, $4, $4)
		ON CONFLICT (transaction_id, savings_goal_id) WHERE transaction_id IS NOT NULL
		DO UPDATE SET
			amount     = EXCLUDED.amount,
			updated_at = EXCLUDED.updated_at
		WHERE savings_goal_contributions.status = 'proposed'
		  AND savings_goal_contributions.managed_by_reconciliation = true
	`
	_, err := r.db.Exec(ctx, upsertQuery, goalID, transactionID, magnitude, now)
	return err
}

// SumConfirmedByGoal returns the sum of all confirmed contributions for a goal.
// userID is joined through savings_goals to enforce ownership scoping (RG2 / M17 — Codex C1).
// A cross-user query returns 0, never another user's total.
func (r *savingsContributionRepository) SumConfirmedByGoal(ctx context.Context, userID, goalID string) (float64, error) {
	query := `
		SELECT COALESCE(SUM(c.amount), 0)
		FROM savings_goal_contributions c
		JOIN savings_goals g ON g.id = c.savings_goal_id AND g.deleted_at IS NULL
		WHERE c.savings_goal_id = $1
		  AND g.user_id = $2
		  AND c.status = 'confirmed'
	`
	var total float64
	if err := r.db.QueryRow(ctx, query, goalID, userID).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// SumConfirmedByGoalInPeriod returns the confirmed contribution total within occurred_on range.
// userID is joined through savings_goals to enforce ownership scoping (RG2 / M17 — Codex C1).
func (r *savingsContributionRepository) SumConfirmedByGoalInPeriod(ctx context.Context, userID, goalID string, start, end time.Time) (float64, error) {
	query := `
		SELECT COALESCE(SUM(c.amount), 0)
		FROM savings_goal_contributions c
		JOIN savings_goals g ON g.id = c.savings_goal_id AND g.deleted_at IS NULL
		WHERE c.savings_goal_id = $1
		  AND g.user_id = $2
		  AND c.status = 'confirmed'
		  AND c.occurred_on >= $3::date
		  AND c.occurred_on <= $4::date
	`
	var total float64
	if err := r.db.QueryRow(ctx, query, goalID, userID, start, end).Scan(&total); err != nil {
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
// userID is joined through savings_goals to enforce ownership scoping (Codex C1 / M20).
func (r *savingsContributionRepository) ListByGoal(ctx context.Context, userID, goalID string) ([]*domain.SavingsGoalContribution, error) {
	query := `SELECT ` + contributionSelectColsJoined + `
		FROM savings_goal_contributions c
		JOIN savings_goals g ON g.id = c.savings_goal_id AND g.deleted_at IS NULL
		WHERE c.savings_goal_id = $1
		  AND g.user_id = $2
		ORDER BY c.occurred_on DESC, c.created_at DESC`

	rows, err := r.db.Query(ctx, query, goalID, userID)
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

// ListByDeletedGoal returns a TOMBSTONED goal's contributions, newest-first.
// See the interface doc for why this read exists and why it is separate from ListByGoal.
func (r *savingsContributionRepository) ListByDeletedGoal(ctx context.Context, userID, goalID string) ([]*domain.SavingsGoalContribution, error) {
	query := `SELECT ` + contributionSelectColsJoined + `
		FROM savings_goal_contributions c
		-- tombstone-exempt(savings_goals g): THIS READ IS THE DELETED GOAL'S LEDGER ITSELF (#1569 T2). Inverting the
		-- predicate is the entire purpose, exactly as in SavingsGoalRepository.GetDeletedByUserID:
		-- the caller has deliberately named a goal it knows is deleted, from the deleted-goals
		-- surface, and the endpoint's whole job is to prove the contributions were retained.
		-- Widening ListByGoal instead would resurrect the goal into progress recomputation and
		-- budget "spent" totals, so this is a SEPARATE read and the live ones are untouched.
		-- Tenancy is unchanged (g.user_id = $2) and is asserted by
		-- TestSavingsContributions_DeletedGoal_Integration, which deletes a SECOND user's goal
		-- with its own contribution precisely so a read keyed on deleted_at alone would fail.
		JOIN savings_goals g ON g.id = c.savings_goal_id AND g.deleted_at IS NOT NULL
		WHERE c.savings_goal_id = $1
		  AND g.user_id = $2
		ORDER BY c.occurred_on DESC, c.created_at DESC`

	rows, err := r.db.Query(ctx, query, goalID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	contribs := []*domain.SavingsGoalContribution{}
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
	return contribs, nil
}

// SumConfirmedByDeletedGoal returns the confirmed contribution total of a TOMBSTONED goal.
// See the interface doc: this feeds the deleted-goal ledger endpoint and nothing else.
func (r *savingsContributionRepository) SumConfirmedByDeletedGoal(ctx context.Context, userID, goalID string) (float64, error) {
	query := `
		SELECT COALESCE(SUM(c.amount), 0)
		FROM savings_goal_contributions c
		-- tombstone-exempt(savings_goals g): the companion sum for ListByDeletedGoal (#1569 T2) — same inverted
		-- predicate, same reason, same unchanged tenancy scoping. Deliberately NOT called from
		-- SavingsProgressService or BudgetSyncService: those must keep using SumConfirmedByGoal so
		-- a deleted goal contributes nothing to progress or to a budget period's spend.
		JOIN savings_goals g ON g.id = c.savings_goal_id AND g.deleted_at IS NOT NULL
		WHERE c.savings_goal_id = $1
		  AND g.user_id = $2
		  AND c.status = 'confirmed'
	`
	var total float64
	if err := r.db.QueryRow(ctx, query, goalID, userID).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// ConfirmContribution promotes a proposed contribution to confirmed status.
// Acquires a row-level lock on the contribution, then (if txn-linked) re-checks
// allocation capacity before flipping to 'confirmed' (Codex C2 / M20).
func (r *savingsContributionRepository) ConfirmContribution(ctx context.Context, userID, id string) (*domain.SavingsGoalContribution, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Step 0: Load + lock the contribution row; verify it belongs to userID via goal ownership.
	fetchQuery := `
		SELECT ` + contributionSelectColsJoined + `
		FROM savings_goal_contributions c
		JOIN savings_goals g ON g.id = c.savings_goal_id AND g.deleted_at IS NULL
		WHERE c.id = $1 AND g.user_id = $2
		FOR UPDATE OF c
	`
	contrib := &domain.SavingsGoalContribution{}
	var row = tx.QueryRow(ctx, fetchQuery, id, userID)
	if scanErr := row.Scan(
		&contrib.ID, &contrib.SavingsGoalID, &contrib.TransactionID,
		&contrib.Amount, &contrib.Source, &contrib.Status,
		&contrib.ManagedByReconciliation, &contrib.ConfirmedByUser,
		&contrib.OccurredOn, &contrib.CreatedAt, &contrib.UpdatedAt,
	); scanErr != nil {
		if scanErr == pgx.ErrNoRows {
			err = domain.ErrContributionNotFound
		} else {
			err = scanErr
		}
		return nil, err
	}

	if contrib.Status == "confirmed" {
		err = domain.ErrContributionAlreadyConfirmed
		return nil, err
	}

	// Step 1: If transaction-linked, re-check allocation capacity (Codex C2).
	if contrib.TransactionID != nil {
		var txnMagnitude float64
		lockQuery := `SELECT ABS(amount) FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL FOR UPDATE`
		if err = tx.QueryRow(ctx, lockQuery, *contrib.TransactionID, userID).Scan(&txnMagnitude); err != nil {
			if err == pgx.ErrNoRows {
				err = domain.ErrContributionNotFound
			}
			return nil, err
		}

		var allocatedElsewhere float64
		capQuery := `
			SELECT COALESCE(SUM(amount), 0)
			FROM savings_goal_contributions
			WHERE transaction_id = $1
			  AND status = 'confirmed'
			  AND savings_goal_id != $2
		`
		if err = tx.QueryRow(ctx, capQuery, *contrib.TransactionID, contrib.SavingsGoalID).Scan(&allocatedElsewhere); err != nil {
			return nil, err
		}

		remaining := txnMagnitude - allocatedElsewhere
		if contrib.Amount > remaining {
			err = domain.ErrContributionOverAllocated
			return nil, err
		}
	}

	// Step 2: Flip status to confirmed, set confirmed_by_user=true.
	now := time.Now()
	updateQuery := `
		UPDATE savings_goal_contributions
		SET status = 'confirmed', confirmed_by_user = true, managed_by_reconciliation = false, updated_at = $1
		WHERE id = $2
		RETURNING ` + contributionSelectCols
	updatedRow := tx.QueryRow(ctx, updateQuery, now, id)
	updated, scanErr := scanContribution(updatedRow)
	if scanErr != nil {
		err = scanErr
		return nil, err
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteContribution removes a contribution row owned by userID.
// Returns ErrContributionNotFound if the row does not exist or is not owned by userID.
func (r *savingsContributionRepository) DeleteContribution(ctx context.Context, userID, id string) error {
	query := `
		DELETE FROM savings_goal_contributions c
		USING savings_goals g
		WHERE c.id = $1
		  AND c.savings_goal_id = g.id
		  AND g.user_id = $2
		  AND g.deleted_at IS NULL
	`
	result, err := r.db.Exec(ctx, query, id, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return domain.ErrContributionNotFound
	}
	return nil
}

// RejectContribution sets a proposed contribution to 'rejected' status (sticky tombstone).
// The row is retained so the partial-unique-index key (transaction_id, savings_goal_id)
// blocks UpsertProposedFromTransaction from re-proposing the same transfer-in (R3-7).
func (r *savingsContributionRepository) RejectContribution(ctx context.Context, userID, id string) (*domain.SavingsGoalContribution, error) {
	// UPDATE … RETURNING so we can return the updated row and verify it was 'proposed'.
	// Join through savings_goals to enforce ownership (Codex C1).
	query := `
		UPDATE savings_goal_contributions c
		SET status = 'rejected', updated_at = NOW()
		FROM savings_goals g
		WHERE c.id = $1
		  AND c.savings_goal_id = g.id
		  AND g.user_id = $2
		  AND g.deleted_at IS NULL
		  AND c.status = 'proposed'
		RETURNING c.id, c.savings_goal_id, c.transaction_id, c.amount, c.source, c.status,
			c.managed_by_reconciliation, c.confirmed_by_user,
			to_char(c.occurred_on, 'YYYY-MM-DD'), c.created_at, c.updated_at`
	contrib := &domain.SavingsGoalContribution{}
	err := r.db.QueryRow(ctx, query, id, userID).Scan(
		&contrib.ID, &contrib.SavingsGoalID, &contrib.TransactionID,
		&contrib.Amount, &contrib.Source, &contrib.Status,
		&contrib.ManagedByReconciliation, &contrib.ConfirmedByUser,
		&contrib.OccurredOn, &contrib.CreatedAt, &contrib.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			// Distinguish between not-found and non-proposed by checking existence.
			var exists int
			checkQuery := `
				SELECT 1 FROM savings_goal_contributions c
				JOIN savings_goals g ON g.id = c.savings_goal_id AND g.deleted_at IS NULL
				WHERE c.id = $1 AND g.user_id = $2
			`
			if chkErr := r.db.QueryRow(ctx, checkQuery, id, userID).Scan(&exists); chkErr != nil {
				// Row not found or wrong user.
				return nil, domain.ErrContributionNotFound
			}
			// Row exists but status != 'proposed' (already confirmed or rejected).
			return nil, domain.ErrContributionNotProposed
		}
		return nil, err
	}
	return contrib, nil
}

// DeleteManagedForTransaction removes reconciler-owned contributions for a transaction whose
// goal is still LIVE, so a removed transaction cannot leave phantom progress behind (Codex C4).
//
// The `g.deleted_at IS NULL` join is load-bearing and is #1569 whole-branch review C1. Without it a
// Plaid `removed` entry arriving weeks after the user deleted a goal silently shrank that goal's
// RETAINED ledger — side-effect destruction, the class the ADR forbids, on the exact surface T2/T7
// built to prove retention. The tempting refutation ("ConfirmContribution clears
// managed_by_reconciliation, so a managed row is never confirmed") is false: UpsertFromTransaction
// INSERTS status='confirmed' with managed_by_reconciliation=true — reached from
// transferCorrelationService.createBoostMatchContribution with source='boost_match' — and its
// ON CONFLICT DO UPDATE promotes a proposed row to confirmed without clearing the flag. So the rows
// this predicate matches are NOT uniformly derived-and-regenerable. For a deleted goal there is no
// progress left to keep honest (nothing reads it into progress), only a ledger to preserve.
// Regression: TestDeleteManagedForTransaction_DeletedGoalLedgerSurvives_Integration.
func (r *savingsContributionRepository) DeleteManagedForTransaction(ctx context.Context, transactionID string) error {
	query := `
		DELETE FROM savings_goal_contributions c
		USING savings_goals g
		WHERE c.savings_goal_id = g.id
		  AND c.transaction_id = $1
		  AND c.managed_by_reconciliation = true
		  AND g.deleted_at IS NULL
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
