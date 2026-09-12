package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	//
	// #1625 Task 22: the ownership probe is a LOCKED basis assertion, taken before the transaction
	// lock. Returns ErrContributionBasisMismatch when the goal has left the contributions basis —
	// the reconciler may not write into a ledger the user closed. Locking the goal FIRST is what
	// keeps this path on the branch's goal → contribution → transaction order.
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
	//
	// #1625 Task 22: transactional, so its locked basis assertion is honest. Returns
	// ErrContributionBasisMismatch when the goal has left the contributions basis — a proposal is
	// still a write into a closed ledger, and it re-takes the uniqueness slot ArchiveByGoal freed.
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
	// Returns ErrContributionAlreadyRejected if the row is a rejected sticky tombstone (R3-7,
	// #1625 Task 9) — a rejection is a deliberate user decision and confirm must not reverse it.
	ConfirmContribution(ctx context.Context, userID, goalID, id string) (*domain.SavingsGoalContribution, error)

	// RejectContribution sets a proposed contribution's status to 'rejected' (sticky tombstone).
	// A rejected row persists in the table so that UpsertProposedFromTransaction's ON CONFLICT
	// clause skips re-proposing the same (transaction_id, savings_goal_id) pair on future syncs
	// (R3-7). Returns ErrContributionNotFound if id is not found or does not belong to userID.
	// Returns ErrContributionNotProposed if the row is not in 'proposed' status.
	RejectContribution(ctx context.Context, userID, goalID, id string) (*domain.SavingsGoalContribution, error)

	// DeleteContribution PERMANENTLY DESTROYS one contribution the user named. It is the single
	// deliberate, user-facing hard delete of financial history on this branch — see the
	// implementation's doc comment for the four ADR controls it carries and what they do not cover.
	// userID is required for ownership verification; goalID binds the delete to the goal the
	// caller named in the URL (#1625), mirroring ConfirmContribution/RejectContribution — without
	// it a same-user DELETE through goal A's URL could destroy goal B's contribution row.
	// auditLog, when non-nil, writes a "savings_goal_contribution.history_purged" entry INSIDE the
	// same transaction as the delete, mirroring PurgeAccount/PurgeLinkedItem/PurgeGoal.
	// Returns ErrContributionNotFound if id does not exist, does not belong to userID, does not
	// belong to goalID, sits under a tombstoned goal, is itself tombstoned, or is a 'rejected'
	// sticky tombstone.
	DeleteContribution(ctx context.Context, userID, goalID, id string, auditLog AuditLogRepository) error

	// DeleteManagedForTransaction TOMBSTONES managed_by_reconciliation rows for a transaction whose
	// goal is still LIVE, so a removed transaction cannot leave phantom progress behind (Codex C4).
	// #1569 round 12 task 2: this is an UPDATE (deleted_at/deleted_reason), NOT a hard delete —
	// Plaid reporting a transaction as `removed` is the provider acting, not the user asking for a
	// contribution to be destroyed. Rows belonging to a TOMBSTONED goal are left alone entirely:
	// they are that goal's retained ledger and a provider re-statement is not a deletion request
	// (#1569 C1).
	DeleteManagedForTransaction(ctx context.Context, transactionID string) error

	// ArchiveByGoal clears managed_by_reconciliation on all rows for a goal so the
	// reconciler no longer modifies them. Used when switching progress_basis away from
	// "contributions" — rows become historical records (Codex C10 basis-switch semantics).
	ArchiveByGoal(ctx context.Context, goalID string) error

	// ArchiveByGoalInTx is ArchiveByGoal run inside the caller's transaction. The savings basis
	// switch drives it there so that persisting the new basis, closing the old period and seeding
	// the carry-over contribution commit or roll back as one (#1625).
	ArchiveByGoalInTx(ctx context.Context, tx Transaction, goalID string) error

	// InsertInTx is Insert run inside the caller's transaction — same reason as
	// ArchiveByGoalInTx. The basis-switch seed goes through this, so a failed seed rolls the whole
	// switch back instead of leaving a goal on the contributions basis with an empty ledger and a
	// current_amount of zero.
	InsertInTx(ctx context.Context, tx Transaction, userID string, contrib *domain.SavingsGoalContribution) error

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
	// Returns ErrSavingsGoalNotFound, ErrContributionNotFound, ErrContributionOverAllocated, or —
	// since #1625 Task 22, from the same locked goal assertion its siblings take before the
	// transaction lock — ErrContributionBasisMismatch.
	InsertManualFromTransaction(ctx context.Context, userID, goalID, transactionID string, amount float64, occurredOn string) (*domain.SavingsGoalContribution, error)
}

// contributionSelectCols is the canonical column list for contribution rows.
const contributionSelectCols = `
	id, savings_goal_id, transaction_id, amount, source, status,
	managed_by_reconciliation, confirmed_by_user, occurred_on::text, created_at, updated_at,
	archived_at
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
	c.managed_by_reconciliation, c.confirmed_by_user, c.occurred_on::text, c.created_at, c.updated_at,
	c.archived_at
`

// contributionStatusRejected is the 'rejected' sticky-tombstone status (R3-7). It is bound as a
// query PARAMETER rather than inlined as a literal in DeleteContribution's WHERE on purpose: the G2
// predicate guard blanks SQL string literals before matching, so an inlined 'rejected' would be
// invisible to the fragment it is supposed to pin, while `c.status <> $4` is not (renumbered from
// $3 by #1625's `c.savings_goal_id = $3` ownership leg). Same precedent as PurgeAccount's
// `status <> $3` (bound to 'active').
const contributionStatusRejected = "rejected"

// savingsContributionRepository implements SavingsContributionRepository using PostgreSQL.
type savingsContributionRepository struct {
	db DB
}

// NewSavingsContributionRepository creates a new SavingsContributionRepository.
func NewSavingsContributionRepository(db DB) SavingsContributionRepository {
	return &savingsContributionRepository{db: db}
}

// assertLedgerIsLiveLocked takes the goal row's lock and refuses the mutation unless the goal is
// STILL on the contributions basis (#1625, defect 3).
//
// The handler has a gate of its own (ContributionHandler.assertLedgerIsLive) and it is worth
// keeping — it produces a far better message. But it is a check-then-act against a row it does not
// hold, so on its own it is a TOCTOU: a contribution could clear the gate, a concurrent PUT could
// switch the goal to the manual basis and archive the period, and the contribution would then land
// LIVE and UNARCHIVED under a goal whose current_amount no longer reads the ledger at all. Money
// recorded where nothing counts it.
//
// `FOR UPDATE` and not a plain read: the lock must outlive the statement so the basis cannot move
// between this check and the write that follows. It is the SAME lock, on the same row, that
// UpdateWithBasisGuardInTx takes, and every path here takes it BEFORE locking any contribution
// row — one global lock order (goal, then contribution), so a switch racing a confirm blocks
// rather than deadlocking.
//
// Returns ErrSavingsGoalNotFound when the goal does not exist, is tombstoned or belongs to another
// user, and ErrContributionBasisMismatch when it exists but has left the contributions basis. Both
// are 4xx: the caller's request was refused, nothing faulted.
func assertLedgerIsLiveLocked(ctx context.Context, q Querier, goalID, userID string) error {
	var basis string
	if err := q.QueryRow(ctx, goalBasisLockQuery, goalID, userID).Scan(&basis); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrSavingsGoalNotFound
		}
		return err
	}
	if normaliseBasis(basis) != domain.ProgressBasisContributions {
		return fmt.Errorf("%w: goal %s is on progress_basis %q",
			domain.ErrContributionBasisMismatch, goalID, normaliseBasis(basis))
	}
	return nil
}

// Insert inserts a new contribution row. ID is generated if empty.
func (r *savingsContributionRepository) Insert(ctx context.Context, userID string, contrib *domain.SavingsGoalContribution) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	if err := r.insertInTx(ctx, tx, userID, contrib); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// InsertInTx implements SavingsContributionRepository.InsertInTx — see that method's doc comment.
func (r *savingsContributionRepository) InsertInTx(ctx context.Context, tx Transaction, userID string, contrib *domain.SavingsGoalContribution) error {
	return r.insertInTx(ctx, tx, userID, contrib)
}

// insertInTx is the shared body. It runs in a transaction unconditionally because the basis
// assertion below is a SELECT … FOR UPDATE: outside one, the lock would be released the instant
// the statement returned and the check would be back to being advisory.
func (r *savingsContributionRepository) insertInTx(ctx context.Context, tx Querier, userID string, contrib *domain.SavingsGoalContribution) error {
	if contrib.ID == "" {
		contrib.ID = uuid.New().String()
	}

	// Verify goal ownership (Codex C1 / IDOR guard) AND that the ledger is still live, with the
	// goal row locked (#1625). This replaces a bare ownership probe: the handler's
	// assertLedgerIsLive check was a TOCTOU, so a contribution could pass the gate, race a switch
	// to the manual basis, and land unarchived and uncounted under a goal that no longer reads its
	// ledger. Asserted here because this is the only point that can hold the goal still while the
	// row goes in.
	if err := assertLedgerIsLiveLocked(ctx, tx, contrib.SavingsGoalID, userID); err != nil {
		return err
	}

	// Verify transaction ownership if an optional link was supplied.
	if contrib.TransactionID != nil {
		var dummy int
		if err := tx.QueryRow(ctx,
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

	// Only trusted writer-selected constants enter this SQL fragment; clients cannot
	// set FlowKind. Keeping provenance distinct avoids reporting carried balances as income.
	flowKindSQL := "'contribution'"
	if contrib.FlowKind == "opening_balance" {
		flowKindSQL = "'opening_balance'"
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO savings_goal_contributions (
			id, savings_goal_id, transaction_id, amount, source, status,
			managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at, flow_kind
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, `+flowKindSQL+`)`,
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

	// Step 0: Verify goal ownership AND that the ledger is still live, holding the goal row
	// (#1625 Task 22). This replaces a bare ownership probe that took no lock: the two automatic
	// callers both skip a goal whose basis is not "contributions", but they read that basis from a
	// goal list loaded before a whole loop of further round-trips, so it is a check-then-act. A
	// switch landing in that window used to let this write a live, unarchived, CONFIRMED row under
	// a goal whose current_amount no longer reads the ledger at all.
	//
	// ⚠ STEP 0, BEFORE the transaction lock below, and that placement is load-bearing rather than
	// stylistic. The lock order on this branch is goal → contribution → transaction, and
	// ConfirmContribution takes all three in it. Asserting here keeps this path on the same order;
	// asserting next to the capacity check it protects would make it transaction → goal and give
	// the two paths an ABBA cycle. Pinned by
	// TestSavingsBasisSwitch_Integration_UpsertPathsCannotDeadlockAgainstConfirm.
	if err = assertLedgerIsLiveLocked(ctx, tx, goalID, userID); err != nil {
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
	//
	// #1625 Task 7: `archived_at IS NULL` here mirrors migration 000191's narrowing of
	// unique_contribution_per_goal_per_transaction to the same predicate. 000191 released the
	// UNIQUENESS slot a closed period's row held; this releases the CAPACITY slot it also held —
	// the two must be freed together, or an archived row still blocks a DIFFERENT goal from
	// allocating the same transaction even though its own uniqueness slot is gone.
	var allocatedElsewhere float64
	capQuery := `
		SELECT COALESCE(SUM(amount), 0)
		FROM savings_goal_contributions
		WHERE transaction_id = $1
		  AND status = 'confirmed'
		  AND savings_goal_id != $2
		  AND deleted_at IS NULL
		  AND archived_at IS NULL
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
	// Step 3: Upsert as confirmed. Promotes a pre-existing MACHINE row (proposed, or already
	// auto-confirmed) to this call's amount/source, so a boost-match (M19) or a later automatic
	// correlation pass does not create a duplicate. It does NOT overwrite a row a human has
	// already decided about (#1569 round 12 task 3 — an independent Codex audit found this DO
	// UPDATE had no predicate at all, so an automatic pass silently clobbered a user-confirmed
	// amount with no audit trail and no user action).
	//
	// Discriminator: confirmed_by_user, not status. This method's OWN INSERT below sets
	// status='confirmed' with confirmed_by_user=false, so a later automatic pass legitimately
	// needs to keep updating rows this same (or a sibling automatic) writer produced — using
	// status='confirmed' as the guard would freeze that machine-to-machine correction, which is
	// not the defect being fixed here. On a TRANSACTION-LINKED row — the only kind this arbiter
	// can reach — confirmed_by_user is set to true by exactly two paths,
	// InsertManualFromTransaction and ConfirmContribution, both requiring an explicit user action,
	// so it is the correct "did a human decide this" bit.
	// ⚠ Deliberately NOT "the only writers in the codebase": savings_goal_service.go's
	// handleBasisSwitch seed also inserts ConfirmedByUser=true via the generic Insert. It sets no
	// TransactionID, so it can never occupy this partial index's slot (transaction_id IS NOT NULL)
	// — which is why the scoped claim above is the true one and the unscoped one would be false.
	//
	// `status != 'rejected'` is a second, independent leg, not a status-based version of the same
	// idea: RejectContribution never touches confirmed_by_user (a rejected row still reads
	// confirmed_by_user = false), so confirmed_by_user = false alone would let this automatic
	// writer silently resurrect a row the user explicitly rejected back to 'confirmed' — the exact
	// same class of bug, laundered through a different status value. This mirrors the sibling
	// automatic writer UpsertProposedFromTransaction's established rule (R3-7) that rejected
	// tombstones are never touched by an automatic path.
	//
	// When neither the INSERT nor the DO UPDATE fires (RowsAffected = 0), the slot is held by a
	// row a human has decided about; this is treated as an expected, non-fatal race (the caller
	// already treats UpsertFromTransaction errors as skip-and-log — see
	// transferCorrelationService.createBoostMatchContribution) and is logged here for visibility.
	//
	// ON CONFLICT target: migration 000180 (#1569 round 12 task 2) narrowed
	// unique_contribution_per_goal_per_transaction to `... AND deleted_at IS NULL`, so a
	// tombstoned row (Plaid `removed` on the prior transaction) no longer occupies this
	// (transaction_id, savings_goal_id) slot forever — a fresh INSERT for the same key lands as a
	// NEW row, never resurrecting the tombstone. The arbiter clause below must match the index
	// predicate exactly or Postgres rejects the ON CONFLICT target.
	upsertQuery := `
		INSERT INTO savings_goal_contributions (
			id, savings_goal_id, transaction_id, amount, source, status,
			managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at, flow_kind
		) VALUES (gen_random_uuid(), $1, $2, $3, $4, 'confirmed', true, false, CURRENT_DATE, $5, $5, 'contribution')
		ON CONFLICT (transaction_id, savings_goal_id) WHERE transaction_id IS NOT NULL AND deleted_at IS NULL AND archived_at IS NULL
		DO UPDATE SET
			amount     = EXCLUDED.amount,
			source     = EXCLUDED.source,
			status     = 'confirmed',
			updated_at = EXCLUDED.updated_at
		WHERE savings_goal_contributions.confirmed_by_user = false
		  AND savings_goal_contributions.status != 'rejected'
	`
	tag, execErr := tx.Exec(ctx, upsertQuery, goalID, transactionID, amount, source, now)
	if execErr != nil {
		err = execErr
		return err
	}
	if tag.RowsAffected() == 0 {
		slog.WarnContext(ctx, "UpsertFromTransaction: skipped automatic contribution write — "+
			"slot is held by a row a user already confirmed or rejected",
			"goal_id", goalID, "transaction_id", transactionID, "amount", amount, "source", source)
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

	// Step 0: Verify goal ownership AND that the ledger is still live, holding the goal row
	// (#1625 Task 22) — see UpsertFromTransaction's Step 0 for why this goes BEFORE the
	// transaction lock and not beside the capacity check. The handler has a gate of its own
	// (ContributionHandler.assertLedgerIsLive) and it is worth keeping for its message, but it is
	// a check against a row it does not hold.
	if err = assertLedgerIsLiveLocked(ctx, tx, goalID, userID); err != nil {
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
	//
	// #1625 Task 7: `archived_at IS NULL` mirrors migration 000191's narrowing of
	// unique_contribution_per_goal_per_transaction — see UpsertFromTransaction's comment above for
	// why the uniqueness slot and the capacity slot must be released together.
	var allocatedElsewhere float64
	capQuery := `
		SELECT COALESCE(SUM(amount), 0)
		FROM savings_goal_contributions
		WHERE transaction_id = $1
		  AND status = 'confirmed'
		  AND savings_goal_id != $2
		  AND deleted_at IS NULL
		  AND archived_at IS NULL
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
	// a no-op, which causes RETURNING to return no row (pgx.ErrNoRows); this repository method
	// returns domain.ErrContributionAlreadyConfirmed in that case — not ErrContributionNotFound,
	// which this comment previously and incorrectly claimed (see the scanErr handling below).
	// This guards against silent source/metadata corruption (Codex R4-1 follow-up).
	// ON CONFLICT target: matches migration 000180's narrowed
	// unique_contribution_per_goal_per_transaction — see UpsertFromTransaction's comment above for
	// why the `AND deleted_at IS NULL` leg must be here too.
	now := time.Now()
	upsertQuery := `
		INSERT INTO savings_goal_contributions (
			id, savings_goal_id, transaction_id, amount, source, status,
			managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at, flow_kind
		) VALUES (gen_random_uuid(), $1, $2, $3, 'manual', 'confirmed', false, true, $4, $5, $5, 'contribution')
		ON CONFLICT (transaction_id, savings_goal_id) WHERE transaction_id IS NOT NULL AND deleted_at IS NULL AND archived_at IS NULL
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
	// This became transactional for the same reason insertInTx did (#1625 Task 22): the basis
	// assertion below is a `SELECT … FOR UPDATE`, and outside a transaction the lock would be
	// released the instant the statement returned, leaving the check advisory — exactly the
	// check-then-act it exists to remove.
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	// Step 0: Verify goal ownership AND that the ledger is still live, holding the goal row.
	//
	// This path writes a PROPOSED row, which no progress sum counts, so the immediate damage is
	// smaller than the confirmed siblings' — but it is not nil and it is not merely cosmetic. A
	// proposal landing after the period closed is a live, unarchived row the UI offers the user to
	// confirm on a goal that no longer has a ledger; and because migration 000191 scopes
	// unique_contribution_per_goal_per_transaction to `archived_at IS NULL`, it also occupies the
	// (transaction_id, savings_goal_id) slot that ArchiveByGoal had just released. The reconciler
	// proposing into a ledger the user closed is the defect, whatever the row's status column says.
	//
	// ⚠ Step 0, ahead of everything else, for the lock-order reason spelled out in
	// UpsertFromTransaction. This path takes no transaction row lock at all, so goal-first is
	// trivially consistent with goal → contribution → transaction; taking it later would not be.
	if err := assertLedgerIsLiveLocked(ctx, tx, goalID, userID); err != nil {
		return err
	}

	// Step 1: Verify transaction ownership (no lock — proposed rows are best-effort).
	var txnExists int
	txnCheckQuery := `SELECT 1 FROM transactions WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`
	if err := tx.QueryRow(ctx, txnCheckQuery, transactionID, userID).Scan(&txnExists); err != nil {
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
	// ON CONFLICT target: matches migration 000180's narrowed
	// unique_contribution_per_goal_per_transaction — see UpsertFromTransaction's comment for why
	// the `AND deleted_at IS NULL` leg must be here too.
	now := time.Now()
	upsertQuery := `
		INSERT INTO savings_goal_contributions (
			id, savings_goal_id, transaction_id, amount, source, status,
			managed_by_reconciliation, confirmed_by_user, occurred_on, created_at, updated_at, flow_kind
		) VALUES (gen_random_uuid(), $1, $2, $3, 'confirmed_transfer', 'proposed', true, false, CURRENT_DATE, $4, $4, 'contribution')
		ON CONFLICT (transaction_id, savings_goal_id) WHERE transaction_id IS NOT NULL AND deleted_at IS NULL AND archived_at IS NULL
		DO UPDATE SET
			amount     = EXCLUDED.amount,
			updated_at = EXCLUDED.updated_at
		WHERE savings_goal_contributions.status = 'proposed'
		  AND savings_goal_contributions.managed_by_reconciliation = true
	`
	if _, err := tx.Exec(ctx, upsertQuery, goalID, transactionID, magnitude, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
		  AND c.deleted_at IS NULL
		  -- #1625: a row from a CLOSED contributions period is retained and readable but must not
		  -- count, or a basis round-trip sums it alongside the synthetic seed that replaced it.
		  AND c.archived_at IS NULL
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
		  AND c.deleted_at IS NULL
		  -- #1625: see SumConfirmedByGoal. Both sums must agree about archived rows, or the
		  -- period total and the lifetime total disagree about the same ledger.
		  AND c.archived_at IS NULL
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
		&c.ArchivedAt,
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
		  AND c.deleted_at IS NULL
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
		  -- c.deleted_at IS NULL (#1569 round 12 task 2): a contribution tombstoned by
		  -- DeleteManagedForTransaction is retained but permanently invisible EVERYWHERE in the
		  -- product, including this deleted-goal ledger view. This is a READ of the ledger, not the
		  -- exemption ListByGoal's sibling JOIN documents above — that one deliberately inverts
		  -- g.deleted_at to show a TOMBSTONED GOAL's history; this leg is the contribution row's
		  -- OWN tombstone and is never inverted anywhere.
		  AND c.deleted_at IS NULL
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
		  -- c.deleted_at IS NULL: same reasoning as ListByDeletedGoal's own comment above — this is
		  -- the contribution row's OWN tombstone, never inverted.
		  AND c.deleted_at IS NULL
		  -- Deliberately does NOT filter archived_at, because it pairs with ListByDeletedGoal and both
		  -- must describe the same set of rows. Unlike SumConfirmedByGoal (live goal, excludes archived),
		  -- this deleted-goal ledger read includes archived contributions to match what ListByDeletedGoal returns.
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
func (r *savingsContributionRepository) ConfirmContribution(ctx context.Context, userID, goalID, id string) (*domain.SavingsGoalContribution, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Step 0a: lock the GOAL and re-assert the contributions basis (#1625, defect 3). Taken
	// before the contribution row's own lock so every writer here acquires locks in the same
	// order — goal, then contribution — as the basis switch does.
	if err = assertLedgerIsLiveLocked(ctx, tx, goalID, userID); err != nil {
		return nil, err
	}

	// Step 0b: Load + lock the contribution row; verify it belongs to userID via goal ownership,
	// AND that it belongs to the goal the caller named.
	//
	// #1625: the goal binding is not cosmetic. Without it the handler's contributions-basis gate
	// could be bypassed — it validates the goal in the URL while this looked the contribution up
	// by its own id alone, so a same-user request naming a contributions-basis goal could mutate a
	// MANUAL goal's contribution and then recompute the wrong goal. Bound here rather than in the
	// handler because a handler-side check would be a TOCTOU race against this very FOR UPDATE.
	//
	// archived_at IS NULL: a row from a closed period cannot be confirmed. Confirming it would
	// change a status that no sum reads, reporting success for a no-op.
	fetchQuery := `
		SELECT ` + contributionSelectColsJoined + `
		FROM savings_goal_contributions c
		JOIN savings_goals g ON g.id = c.savings_goal_id AND g.deleted_at IS NULL
		WHERE c.id = $1 AND g.user_id = $2 AND c.savings_goal_id = $3
		  AND c.deleted_at IS NULL AND c.archived_at IS NULL
		FOR UPDATE OF c
	`
	contrib := &domain.SavingsGoalContribution{}
	var row = tx.QueryRow(ctx, fetchQuery, id, userID, goalID)
	if scanErr := row.Scan(
		&contrib.ID, &contrib.SavingsGoalID, &contrib.TransactionID,
		&contrib.Amount, &contrib.Source, &contrib.Status,
		&contrib.ManagedByReconciliation, &contrib.ConfirmedByUser,
		&contrib.OccurredOn, &contrib.CreatedAt, &contrib.UpdatedAt, &contrib.ArchivedAt,
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

	// #1625 Task 9: a rejected row is a sticky tombstone (R3-7) — the user deliberately declined
	// this contribution. Confirming it would undo that decision, so it must be refused here (not
	// just at the handler) since this is the single choke point every confirm path locks through.
	if contrib.Status == "rejected" {
		err = domain.ErrContributionAlreadyRejected
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

		// #1625 Task 7: `archived_at IS NULL` mirrors migration 000191's narrowing of
		// unique_contribution_per_goal_per_transaction — see UpsertFromTransaction's comment above
		// for why the uniqueness slot and the capacity slot must be released together.
		var allocatedElsewhere float64
		capQuery := `
			SELECT COALESCE(SUM(amount), 0)
			FROM savings_goal_contributions
			WHERE transaction_id = $1
			  AND status = 'confirmed'
			  AND savings_goal_id != $2
			  AND deleted_at IS NULL
			  AND archived_at IS NULL
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
		WHERE id = $2 AND deleted_at IS NULL
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

// DeleteContribution permanently destroys one contribution row owned by userID AND bound to
// goalID.
// Returns ErrContributionNotFound if the row does not exist, is not owned by userID, does not
// belong to goalID, or is one of the rows this path refuses (below).
//
// #1625: goalID is bound into the WHERE for the same reason ConfirmContribution/
// RejectContribution are — without it a same-user DELETE through goal A's URL destroyed goal B's
// contribution row outright (the ONE hard delete on this branch, so the bypass was irreversible,
// not just a wrong-goal recompute). Deliberately NOT paired with an `archived_at IS NULL` leg like
// Confirm/Reject: their leg exists because acting on a closed period's STATUS is a no-op nothing
// reads. A DELETE of a retained, archived row is not a no-op — it is the user destroying their own
// data, which is exactly this endpoint's job — so archived rows stay deletable.
//
// THIS IS THE ONE DELIBERATE, USER-FACING HARD DELETE OF FINANCIAL HISTORY ON THIS BRANCH: the user
// named this one contribution, in the UI, and asked for it to go away — the narrowest possible
// explicit request. #1569 round 17 task 6 brought it from 2 of the ADR's 4 explicit-destruction
// controls to 4 of 4 (docs/decisions/2026-08-24-financial-history-retained-by-default.md
// § Classification Test); the earlier version of this comment deferred two of them and never
// mentioned the third problem at all.
//
//  1. OWN ENDPOINT, distinct from the retaining path. DELETE
//     /savings-goals/{goalID}/contributions/{id}. Deleting the GOAL tombstones and retains every
//     contribution; rejecting a proposal retains it too.
//  2. AUDIT ROW IN THE SAME TRANSACTION. "savings_goal_contribution.history_purged", via
//     auditLog.LogInTx, committed or rolled back with the DELETE. The HTTP layer's slog line is
//     NOT this control — it survives a rolled-back transaction and it is not queryable as a trail.
//     Same shape as PurgeAccount/PurgeLinkedItem/PurgeGoal, and like them auditLog may be nil
//     (tests, and only tests — main.go wires it).
//  3. A PRECONDITION REFUSING DATA THE USER DID NOT NAME — the leg that needed judgement, because
//     the purges' form ("the parent is already tombstoned") INVERTS here. A contribution under a
//     LIVE goal is exactly what this endpoint exists to destroy, so refusing live data verbatim
//     would delete the feature. What the purges' precondition actually buys is that the
//     destruction cannot surprise anyone: the resource has already left every live surface. For a
//     per-row delete the same guarantee reads the other way round — destruction is permitted ONLY
//     for a row that is still ON the surface the user was looking at, and refused for every row
//     the product has already retired from it. There are exactly three such retirements, and all
//     three are now in the WHERE:
//     • `g.deleted_at IS NULL` — the goal is tombstoned, so its ledger is retained history read
//     through ListByDeletedGoal, not a live sheet. (Pre-existing; PurgeGoal is the path that
//     destroys those, and it has its own confirm copy.)
//     • `c.deleted_at IS NULL` — the row itself is tombstoned. This is the leg that was MISSING
//     and undocumented, and it is the one that destroyed retained data: a reconciler row
//     tombstoned by DeleteManagedForTransaction when Plaid reported the transaction as
//     `removed` ("the provider is not the user") is invisible in every product surface, so the
//     user cannot have seen it in a confirm dialog — yet its id was still reachable here.
//     • `c.status <> $4` (bound to 'rejected') — a rejected row is a STICKY TOMBSTONE kept on
//     purpose so UpsertProposedFromTransaction cannot re-propose the same transfer-in (R3-7),
//     and GoalContributionsSheet.tsx filters it out of the ledger. The endpoint's own
//     description promises to destroy a contribution "(proposed or confirmed)"; rejected was
//     never in its contract, and destroying one both erases a deliberate retention and
//     regresses R3-7 into a re-proposal loop.
//     ⚠ WHAT THIS PRECONDITION DOES NOT PROTECT, stated plainly:
//     • It does not protect the row the user DID name. A live, visible, confirmed contribution —
//     including hand-typed money no provider can supply again — is destroyed, irrecoverably.
//     That is the point of the endpoint, and there is no restore path anywhere on this branch.
//     • It does not check that the user was shown honest confirm copy. That lives in the UI.
//     • It does not distinguish a row the user entered from one the reconciler created: the
//     predicate keys on c.id alone (see the G2 allowlist entry, which says the same).
//     • It refuses with ErrContributionNotFound rather than a distinct conflict error, because
//     all three refused shapes are rows the caller cannot see; "not found" is the honest answer
//     and leaks nothing. The cost is that a refusal is indistinguishable from a wrong id.
//  4. ALLOWLIST ENTRY — allowedHistoryDeletes in history_delete_guard_test.go, whose `predicate`
//     pins these WHERE fragments. Widening this statement fails that guard with no DB attached.
//
// The three legs are in the DELETE's OWN WHERE rather than in a preceding Go check, for the reason
// #1569 round 14 reversed itself over on PurgeGoal: a Go `if` is invisible to the guard, so an edit
// could remove it while the guard stayed green. Here they are the only check, so there is no pair
// that can disagree and no check-then-act window to race.
//
// A FOURTH leg, `c.savings_goal_id = $3`, joined this WHERE in #1625: it is not one of the three
// retirement legs above (it does not decide whether the row is still on a live surface), it is the
// ownership fix — the caller named a goal in the URL and the delete must be scoped to it, exactly
// as ConfirmContribution/RejectContribution already are. Its absence was the bug this task closed.
func (r *savingsContributionRepository) DeleteContribution(ctx context.Context, userID, goalID, id string, auditLog AuditLogRepository) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	result, err := tx.Exec(ctx, `
		DELETE FROM savings_goal_contributions c
		USING savings_goals g
		WHERE c.id = $1
		  AND c.savings_goal_id = g.id
		  AND g.user_id = $2
		  AND c.savings_goal_id = $3
		  AND g.deleted_at IS NULL
		  AND c.deleted_at IS NULL
		  AND c.status <> $4
	`, id, userID, goalID, contributionStatusRejected)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return domain.ErrContributionNotFound
	}

	if auditLog != nil {
		if err := auditLog.LogInTx(ctx, tx, "savings_goal_contribution.history_purged",
			"savings_goal_contribution", id); err != nil {
			return fmt.Errorf("delete contribution %s: audit log: %w", id, err)
		}
	}

	return tx.Commit(ctx)
}

// RejectContribution sets a proposed contribution to 'rejected' status (sticky tombstone).
// The row is retained so the partial-unique-index key (transaction_id, savings_goal_id)
// blocks UpsertProposedFromTransaction from re-proposing the same transfer-in (R3-7).
func (r *savingsContributionRepository) RejectContribution(ctx context.Context, userID, goalID, id string) (*domain.SavingsGoalContribution, error) {
	// #1625 (defect 3): this became transactional so the contributions-basis assertion below can
	// hold the goal row. An `AND g.progress_basis = 'contributions'` bolted onto the UPDATE … FROM
	// would NOT have been equivalent: under READ COMMITTED, an UPDATE re-reads only its TARGET
	// table's concurrently-updated rows, so the joined savings_goals row is evaluated against the
	// statement's original snapshot and a switch that committed a moment earlier is invisible to
	// it. Locking the goal first is the only form of this check that is not still a race.
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	if err := assertLedgerIsLiveLocked(ctx, tx, goalID, userID); err != nil {
		return nil, err
	}

	// UPDATE … RETURNING so we can return the updated row and verify it was 'proposed'.
	// Join through savings_goals to enforce ownership (Codex C1).
	query := `
		UPDATE savings_goal_contributions c
		SET status = 'rejected', updated_at = NOW()
		FROM savings_goals g
		WHERE c.id = $1
		  AND c.savings_goal_id = g.id
		  AND g.user_id = $2
		  -- #1625: bind the contribution to the goal the caller named. See ConfirmContribution's
		  -- comment — without this the handler's basis gate is bypassable.
		  AND c.savings_goal_id = $3
		  AND g.deleted_at IS NULL
		  AND c.status = 'proposed'
		  AND c.deleted_at IS NULL
		  -- A closed period's proposal cannot be rejected; nothing reads its status.
		  AND c.archived_at IS NULL
		RETURNING c.id, c.savings_goal_id, c.transaction_id, c.amount, c.source, c.status,
			c.managed_by_reconciliation, c.confirmed_by_user,
			to_char(c.occurred_on, 'YYYY-MM-DD'), c.created_at, c.updated_at, c.archived_at`
	contrib := &domain.SavingsGoalContribution{}
	err = tx.QueryRow(ctx, query, id, userID, goalID).Scan(
		&contrib.ID, &contrib.SavingsGoalID, &contrib.TransactionID,
		&contrib.Amount, &contrib.Source, &contrib.Status,
		&contrib.ManagedByReconciliation, &contrib.ConfirmedByUser,
		&contrib.OccurredOn, &contrib.CreatedAt, &contrib.UpdatedAt, &contrib.ArchivedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			// Distinguish between not-found and non-proposed by checking existence.
			var exists int
			// Scoped the SAME way as the UPDATE above, or the two disagree about what "exists"
			// means: a contribution belonging to a DIFFERENT goal would be reported as
			// ErrContributionNotProposed ("it exists but isn't proposed") when the honest answer
			// for this goal is not-found. That distinction is what the bypass fix turns on.
			checkQuery := `
				SELECT 1 FROM savings_goal_contributions c
				JOIN savings_goals g ON g.id = c.savings_goal_id AND g.deleted_at IS NULL
				WHERE c.id = $1 AND g.user_id = $2 AND c.savings_goal_id = $3
				  AND c.deleted_at IS NULL AND c.archived_at IS NULL
			`
			if chkErr := tx.QueryRow(ctx, checkQuery, id, userID, goalID).Scan(&exists); chkErr != nil {
				// Row not found or wrong user.
				return nil, domain.ErrContributionNotFound
			}
			// Row exists but status != 'proposed' (already confirmed or rejected).
			return nil, domain.ErrContributionNotProposed
		}
		return nil, err
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return contrib, nil
}

// DeleteManagedForTransaction TOMBSTONES reconciler-owned contributions for a transaction whose
// goal is still LIVE, so a removed transaction cannot leave phantom progress behind (Codex C4).
//
// ⚠ #1569 round 12 task 2 — same NAME, different MECHANISM. This used to be a real `DELETE FROM
// savings_goal_contributions`, which was SIDE-EFFECT destruction: Plaid reporting a transaction as
// `removed` is the provider acting, not the user asking for a contribution to be destroyed ("the
// provider is not the user" — the branch's founding corollary, and an independent Codex audit
// named this exact call site as the headline finding that the branch's principle was not yet
// satisfied). The method keeps its name because every caller (transactionSyncService.
// processRemovedTransactions) and every progress computation this feeds still describes the right
// outcome — the row now leaves PROGRESS, not the table. See migration 000180 for the two tombstone
// columns this UPDATE writes, and domain.SavingsContributionDeletedReasonProviderRemovedTxn for why
// 'provider_removed_txn' is the one reason value this path ever writes.
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
//
// `c.deleted_at IS NULL` in the WHERE makes a re-delivery of the same `removed` entry a no-op
// instead of re-stamping deleted_at with a later timestamp — this UPDATE, unlike the old DELETE,
// is not naturally idempotent on its own.
// Regression: TestDeleteManagedForTransaction_DeletedGoalLedgerSurvives_Integration.
func (r *savingsContributionRepository) DeleteManagedForTransaction(ctx context.Context, transactionID string) error {
	query := `
		UPDATE savings_goal_contributions c
		SET deleted_at = NOW(), deleted_reason = $2, updated_at = NOW()
		FROM savings_goals g
		WHERE c.savings_goal_id = g.id
		  AND c.transaction_id = $1
		  AND c.managed_by_reconciliation = true
		  AND g.deleted_at IS NULL
		  AND c.deleted_at IS NULL
	`
	_, err := r.db.Exec(ctx, query, transactionID, domain.SavingsContributionDeletedReasonProviderRemovedTxn)
	return err
}

// ArchiveByGoal closes out a goal's contribution ledger: the rows become permanent historical
// records that neither the reconciler modifies nor the progress sums count.
// Called when a goal's progress_basis leaves — or re-enters — "contributions" (Codex C10).
//
// #1625 Task 12: this used to ONLY clear managed_by_reconciliation, which was not enough in two
// ways. It left the rows live and confirmed, so a round-trip back to the contributions basis
// double-counted them against the synthetic seed handleBasisSwitch inserts (100 + 100 = 200). And
// clearing the flag made reconciler rows indistinguishable from user-entered rows, which already
// carry false — destroying the only signal that could have told them apart afterwards.
//
// Note the WHERE no longer filters on managed_by_reconciliation: a closed period contains the
// user's own manual contributions too, and leaving those live is exactly what double-counted.
// Archiving is idempotent — an already-archived row keeps its original timestamp.
//
// #1625 Task 8: `status != 'rejected'` excludes R3-7's sticky tombstones. A rejected row is never
// a ledger entry — every progress sum filters `status = 'confirmed'` (see SumConfirmedByGoal and
// its siblings), so archiving a rejected row buys nothing for the double-count fix above. Worse,
// migration 000191 scoped unique_contribution_per_goal_per_transaction to `archived_at IS NULL`
// precisely so an archived row releases its uniqueness slot on basis re-entry — but a rejected row
// must NEVER release that slot, because the slot is the only thing stopping
// UpsertProposedFromTransaction from re-proposing a transfer the user already turned down on the
// next Plaid sync. Keeping rejected rows live (archived_at NULL) keeps them in the index, so a
// resync's INSERT still collides with the tombstone and its `WHERE status = 'proposed'` ON
// CONFLICT clause still refuses to touch it. A rejection is a decision about a transaction, not an
// entry in a period — it must outlive periods, which is exactly what "sticky" means.
func (r *savingsContributionRepository) ArchiveByGoal(ctx context.Context, goalID string) error {
	return r.archiveByGoal(ctx, r.db, goalID)
}

// ArchiveByGoalInTx implements SavingsContributionRepository.ArchiveByGoalInTx — see that
// method's doc comment.
func (r *savingsContributionRepository) ArchiveByGoalInTx(ctx context.Context, tx Transaction, goalID string) error {
	return r.archiveByGoal(ctx, tx, goalID)
}

func (r *savingsContributionRepository) archiveByGoal(ctx context.Context, q Querier, goalID string) error {
	query := `
		UPDATE savings_goal_contributions
		SET managed_by_reconciliation = false,
		    archived_at = COALESCE(archived_at, NOW()),
		    updated_at = NOW()
		WHERE savings_goal_id = $1
		  AND deleted_at IS NULL
		  AND status != 'rejected'
		  AND (archived_at IS NULL OR managed_by_reconciliation = true)
	`
	_, err := q.Exec(ctx, query, goalID)
	return err
}
