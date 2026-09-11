package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// SavingsGoalRepository defines the interface for savings goal data access.
type SavingsGoalRepository interface {
	// CRUD operations
	Create(ctx context.Context, goal *domain.SavingsGoal) error
	GetByID(ctx context.Context, id string) (*domain.SavingsGoal, error)
	// GetByIDForUser retrieves a savings goal by ID with ownership validation.
	// Returns ErrSavingsGoalNotFound if the goal does not exist or belongs to a
	// different user. Use instead of GetByID in security-sensitive paths (GAP AE).
	GetByIDForUser(ctx context.Context, id, userID string) (*domain.SavingsGoal, error)
	GetByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)

	// GetDeletedByUserID lists the user's TOMBSTONED goals, newest deletion first. It is the
	// deliberate inverse of every other read here, and it exists because task 8 shipped a
	// tombstone and a purge with no way to reach either: once deleted_at is set, no query in this
	// repository can find the goal again, so the user can neither see that their hand-logged
	// contributions were kept nor satisfy PurgeGoal's already-deleted precondition. Retention
	// nobody can act on is deletion with a longer receipt.
	//
	// Scoped to the caller's own user_id, like every other listing here — the tombstone predicate
	// is inverted, the tenancy predicate is not.
	GetDeletedByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	GetByName(ctx context.Context, userID, name string) (*domain.SavingsGoal, error)
	Update(ctx context.Context, goal *domain.SavingsGoal) error

	// BeginTx starts a transaction on this repository's pool and returns it as the narrow
	// Transaction handle. It exists so the SERVICE layer can own a transaction boundary that
	// spans the goal row and the contribution ledger — see savingsGoalService.applyBasisSwitch
	// (#1625) — without importing pgx or reaching for a second pool.
	BeginTx(ctx context.Context) (Transaction, error)

	// UpdateWithBasisGuardInTx is Update, run inside the caller's transaction, behind a
	// SELECT … FOR UPDATE on the goal row and a check that the row's CURRENT progress_basis is
	// still priorBasis.
	//
	// #1625. Update alone has no row lock and no version predicate, so two concurrent
	// manual→contributions switches both read "manual", both archive, and both seed — and a
	// synthetic seed carries no transaction_id, so the partial unique index does not stop the
	// second. The lock serialises them and the predicate makes the loser fail loudly instead of
	// silently doubling the goal.
	//
	// Returns ErrSavingsGoalNotFound if the goal does not exist, belongs to another user or is
	// tombstoned; ErrSavingsGoalBasisConflict if the row moved to a different basis first. The
	// lock is held until the CALLER commits or rolls back, which is the point: everything the
	// switch does afterwards (archive, seed) happens with the goal pinned.
	UpdateWithBasisGuardInTx(ctx context.Context, tx Transaction, goal *domain.SavingsGoal, priorBasis string) error

	// Delete TOMBSTONES a savings goal: it sets deleted_at/deleted_reason/deleted_by instead of
	// issuing `DELETE FROM savings_goals`, so that savings_goal_contributions -- money the user
	// logged BY HAND, refetchable from no provider -- survives instead of being cascaded away by
	// fk_contributions_savings_goal. Issue #1569, Task 8.
	//
	// The goal disappears from every read in this codebase (all of them carry
	// `deleted_at IS NULL`, enforced by TestNoConfigReadForgetsTheTombstoneFilter), and its
	// auto-created savings budget_categories are tombstoned with it so they do not outlive the
	// goal they budget for.
	//
	// Runs in one transaction with an "savings_goal.deleted" audit entry written via
	// auditLog.LogInTx, so the entry and the state change commit or roll back together. auditLog
	// may be nil, in which case no entry is written. Safe on this path because the transaction
	// never locks a users row -- see AuditLogRepository.LogInTx's doc comment.
	//
	// Returns ErrSavingsGoalNotFound if the goal does not exist, belongs to another user, or is
	// already tombstoned.
	Delete(ctx context.Context, id, userID string, auditLog AuditLogRepository) error

	// PurgeGoal permanently destroys a savings goal and, via ON DELETE CASCADE, every
	// savings_goal_contribution, budget_category and savings_goal_category_detach row that
	// references it. This is the genuinely destructive verb, and it CANNOT BE UNDONE.
	//
	// It refuses with ErrSavingsGoalNotDeleted, doing and changing nothing, unless the goal is
	// ALREADY tombstoned. That two-step is the explicit confirmation the account purge established
	// (repository.ErrAccountNotDisconnected, Issue #1569 Task 6): destroying hand-logged money is
	// never something a single request can do by accident.
	//
	// Writes a "savings_goal.history_purged" audit entry inside the same transaction, mirroring
	// PurgeAccount's "account.history_purged". Returns ErrSavingsGoalNotFound if the goal does not
	// exist or belongs to another user.
	PurgeGoal(ctx context.Context, id, userID string, auditLog AuditLogRepository) error

	CountByUserID(ctx context.Context, userID string) (int, error)

	// CountCompletedByUserID counts the user's COMPLETED, non-tombstoned savings goals.
	//
	// Issue #1569 Task 20 (A4). SavingsProgressSummary.CompletedGoalCount used to be derived by
	// counting IsCompleted over GetActiveByUserID's result — a universe whose WHERE clause is
	// `is_completed = false`. The count was therefore structurally zero for every user who has
	// ever completed a goal, and the field is REQUIRED in the JSON contract, so an MCP or LLM
	// consumer asking "how many goals have I completed?" got a confident, well-typed, wrong 0.
	// A separate count is the fix rather than widening the summary's universe: the universe is
	// deliberately active-only (see savings_progress_chain_test.go's
	// TestSavingsProgress_Summary_UniverseIsActiveOnly), and widening it would silently change
	// Goals, TotalSaved, TotalTarget and OverallPercentComplete too.
	CountCompletedByUserID(ctx context.Context, userID string) (int, error)

	// Balance sync operations (Issue #452, extended by #1300 for multi-basis progress)
	GetByAccountID(ctx context.Context, accountID string) ([]*domain.SavingsGoal, error)
	// UpdateCurrentAmount stores the recomputed current_amount and records the recompute timestamp.
	// balanceAsOf is non-nil when the basis reads account balance (balance / account_percentage).
	//
	// basis is the progress_basis the caller COMPUTED currentAmount for, and it is bound into the
	// statement's WHERE (#1625). Without it this filtered on id and deleted_at only, so a paused
	// request A could overwrite request B's already-persisted value for a DIFFERENT basis: A's
	// figure is arithmetically right and semantically wrong, and nothing recomputes it back.
	// Returns ErrSavingsGoalBasisConflict when the row exists but has since moved to another
	// basis, and ErrSavingsGoalNotFound when it does not exist at all — the two are distinguished
	// deliberately, because only the first is a race a caller can sensibly retry.
	UpdateCurrentAmount(ctx context.Context, goalID, basis string, currentAmount float64, lastCalculatedAt time.Time, balanceAsOf *time.Time) error
	// MarkCompleted sets is_completed and completed_at — sticky: only updates completed_at if not already set (Codex C5).
	MarkCompleted(ctx context.Context, goalID string) error
}

// savingsGoalRepository implements SavingsGoalRepository using PostgreSQL.
type savingsGoalRepository struct {
	db DB
}

// NewSavingsGoalRepository creates a new SavingsGoalRepository instance.
func NewSavingsGoalRepository(db DB) SavingsGoalRepository {
	return &savingsGoalRepository{db: db}
}

// goalSelectCols is the canonical SELECT list for savings goals to avoid duplication.
// Includes progress tracking columns added in Issue #1300.
const goalSelectCols = `
	id, user_id, name, description, target_amount, current_amount,
	currency_code, account_id, user_account_id, deadline, is_completed, completed_at,
	created_at, updated_at,
	progress_basis, account_percentage, last_calculated_at, balance_as_of
`

// goalSelectColsWithDeletedAt is goalSelectCols plus the tombstone timestamp, for the ONE read
// that returns tombstoned rows. Kept separate rather than folded into goalSelectCols so that the
// eighteen existing scan sites are untouched: adding a column to the shared list would mean
// editing every one of them, and a missed site is a runtime scan mismatch that no compiler
// catches.
const goalSelectColsWithDeletedAt = goalSelectCols + `, deleted_at`

// Create creates a new savings goal in the database.
func (r *savingsGoalRepository) Create(ctx context.Context, goal *domain.SavingsGoal) error {
	if goal.ID == "" {
		goal.ID = uuid.New().String()
	}

	now := time.Now()
	goal.CreatedAt = now
	goal.UpdatedAt = now

	if goal.CurrencyCode == "" {
		goal.CurrencyCode = "USD"
	}

	if goal.ProgressBasis == "" {
		goal.ProgressBasis = domain.ProgressBasisManual
	}

	query := `
		INSERT INTO savings_goals (
			id, user_id, name, description, target_amount, current_amount,
			currency_code, account_id, user_account_id, deadline, is_completed, completed_at,
			created_at, updated_at,
			progress_basis, account_percentage, last_calculated_at, balance_as_of
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`

	_, err := r.db.Exec(ctx, query,
		goal.ID,
		goal.UserID,
		goal.Name,
		goal.Description,
		goal.TargetAmount,
		goal.CurrentAmount,
		goal.CurrencyCode,
		goal.AccountID,
		goal.UserAccountID,
		goal.Deadline,
		goal.IsCompleted,
		goal.CompletedAt,
		goal.CreatedAt,
		goal.UpdatedAt,
		goal.ProgressBasis,
		goal.AccountPercentage,
		goal.LastCalculatedAt,
		goal.BalanceAsOf,
	)

	if err != nil {
		if strings.Contains(err.Error(), "unique_savings_goal_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrSavingsGoalAlreadyExists
		}
		return err
	}

	return nil
}

// GetByID retrieves a savings goal by its ID.
func (r *savingsGoalRepository) GetByID(ctx context.Context, id string) (*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectCols + ` FROM savings_goals WHERE id = $1 AND deleted_at IS NULL`
	goal := &domain.SavingsGoal{}
	err := r.db.QueryRow(ctx, query, id).Scan(
		&goal.ID,
		&goal.UserID,
		&goal.Name,
		&goal.Description,
		&goal.TargetAmount,
		&goal.CurrentAmount,
		&goal.CurrencyCode,
		&goal.AccountID,
		&goal.UserAccountID,
		&goal.Deadline,
		&goal.IsCompleted,
		&goal.CompletedAt,
		&goal.CreatedAt,
		&goal.UpdatedAt,
		&goal.ProgressBasis,
		&goal.AccountPercentage,
		&goal.LastCalculatedAt,
		&goal.BalanceAsOf,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSavingsGoalNotFound
	}
	if err != nil {
		return nil, err
	}

	goal.PopulateDateDisplay() // #1631 Batch F2
	return goal, nil
}

// GetByIDForUser retrieves a savings goal by ID with ownership validation.
// Returns ErrSavingsGoalNotFound if the goal does not exist or belongs to a different user.
func (r *savingsGoalRepository) GetByIDForUser(ctx context.Context, id, userID string) (*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectCols + ` FROM savings_goals WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`
	goal := &domain.SavingsGoal{}
	err := r.db.QueryRow(ctx, query, id, userID).Scan(
		&goal.ID,
		&goal.UserID,
		&goal.Name,
		&goal.Description,
		&goal.TargetAmount,
		&goal.CurrentAmount,
		&goal.CurrencyCode,
		&goal.AccountID,
		&goal.UserAccountID,
		&goal.Deadline,
		&goal.IsCompleted,
		&goal.CompletedAt,
		&goal.CreatedAt,
		&goal.UpdatedAt,
		&goal.ProgressBasis,
		&goal.AccountPercentage,
		&goal.LastCalculatedAt,
		&goal.BalanceAsOf,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSavingsGoalNotFound
	}
	if err != nil {
		return nil, err
	}
	goal.PopulateDateDisplay() // #1631 Batch F2
	return goal, nil
}

// GetByUserID retrieves all savings goals for a specific user.
func (r *savingsGoalRepository) GetByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectCols + ` FROM savings_goals WHERE user_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC`
	return r.scanGoals(ctx, query, userID)
}

// GetDeletedByUserID retrieves the user's tombstoned savings goals, newest deletion first.
func (r *savingsGoalRepository) GetDeletedByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectColsWithDeletedAt + `
		FROM savings_goals
		-- tombstone-exempt(savings_goals): THIS READ IS THE TOMBSTONE LISTING ITSELF (#1569 Task 12). Inverting
		-- the predicate is the entire purpose; see GetDeletedByUserID's interface doc for why the
		-- listing has to exist at all. Tenancy is unchanged and is asserted by
		-- TestSavingsGoals_DeletedListing_Integration, which deletes a second user's goal in the
		-- same transaction precisely so a listing keyed on deleted_at alone would fail.
		WHERE user_id = $1 AND deleted_at IS NOT NULL
		ORDER BY deleted_at DESC`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	goals := []*domain.SavingsGoal{}
	for rows.Next() {
		goal := &domain.SavingsGoal{}
		if err := rows.Scan(
			&goal.ID,
			&goal.UserID,
			&goal.Name,
			&goal.Description,
			&goal.TargetAmount,
			&goal.CurrentAmount,
			&goal.CurrencyCode,
			&goal.AccountID,
			&goal.UserAccountID,
			&goal.Deadline,
			&goal.IsCompleted,
			&goal.CompletedAt,
			&goal.CreatedAt,
			&goal.UpdatedAt,
			&goal.ProgressBasis,
			&goal.AccountPercentage,
			&goal.LastCalculatedAt,
			&goal.BalanceAsOf,
			&goal.DeletedAt,
		); err != nil {
			return nil, err
		}
		goal.PopulateDateDisplay() // #1631 Batch F2
		goals = append(goals, goal)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return goals, nil
}

// GetActiveByUserID retrieves all active (not completed) savings goals for a user.
func (r *savingsGoalRepository) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectCols + ` FROM savings_goals WHERE user_id = $1 AND is_completed = false AND deleted_at IS NULL ORDER BY created_at DESC`
	return r.scanGoals(ctx, query, userID)
}

// GetByName retrieves a savings goal by user ID and name.
func (r *savingsGoalRepository) GetByName(ctx context.Context, userID, name string) (*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectCols + ` FROM savings_goals WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL`
	goal := &domain.SavingsGoal{}
	err := r.db.QueryRow(ctx, query, userID, name).Scan(
		&goal.ID,
		&goal.UserID,
		&goal.Name,
		&goal.Description,
		&goal.TargetAmount,
		&goal.CurrentAmount,
		&goal.CurrencyCode,
		&goal.AccountID,
		&goal.UserAccountID,
		&goal.Deadline,
		&goal.IsCompleted,
		&goal.CompletedAt,
		&goal.CreatedAt,
		&goal.UpdatedAt,
		&goal.ProgressBasis,
		&goal.AccountPercentage,
		&goal.LastCalculatedAt,
		&goal.BalanceAsOf,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSavingsGoalNotFound
	}
	if err != nil {
		return nil, err
	}

	goal.PopulateDateDisplay() // #1631 Batch F2
	return goal, nil
}

// goalUpdateQuery is the full-row UPDATE shared by Update and UpdateWithBasisGuardInTx, so the
// transactional basis-switch path cannot drift from the ordinary one. It is a package-level
// constant rather than a local so the cache-completeness write-set extractor can resolve it.
const goalUpdateQuery = `
		UPDATE savings_goals
		SET name = $1, description = $2, target_amount = $3, current_amount = $4,
			currency_code = $5, account_id = $6, deadline = $7, is_completed = $8,
			completed_at = $9, updated_at = $10, user_account_id = $11,
			progress_basis = $12, account_percentage = $13, last_calculated_at = $14,
			balance_as_of = $15
		WHERE id = $16 AND user_id = $17 AND deleted_at IS NULL
	`

// goalBasisLockQuery reads a goal's current progress_basis and holds the row until the caller's
// transaction ends. Every writer that cares about the basis — the switch itself and each
// contribution mutation — takes THIS lock first, so they all serialise on the same row in the same
// order and cannot deadlock against one another (#1625).
const goalBasisLockQuery = `
		SELECT progress_basis
		FROM savings_goals
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
		FOR UPDATE
	`

// ErrUnguardedBasisChange is returned by Update when the goal it was handed carries a different
// progress_basis from the row on disk.
//
// #1625 Task 17, fix round 1. The basis switch is atomic and serialisable only along ONE path —
// UpdateWithBasisGuardInTx, inside the transaction that also archives the closing period and seeds
// the carry-over contribution. The service chose that path only when its OPTIONAL basis-switch
// ledger repo had been wired by a setter, so a construction site that forgot the setter sent a
// basis change through the plain Update instead: no row lock, no version predicate, no archive, no
// seed — the exact pre-#1625 behaviour, reinstated silently, with every test still green.
//
// Refusing here rather than in the service is what makes the bypass UNREPRESENTABLE instead of
// merely tested against. No caller — service, handler, worker, or one written next year — can move
// a progress_basis except through the guarded path, whatever it forgot to wire.
//
// It is deliberately NOT mapped to a 4xx in internal/huma/errors/mapper.go. It is unreachable from
// any correct wiring, so it is a genuine server fault and a 500 with a Sentry event is the honest
// answer. That is the opposite call from ErrSavingsGoalBasisConflict, which a user CAN hit by
// losing a race and which must therefore be a 409.
var ErrUnguardedBasisChange = errors.New(
	"savings goal progress_basis cannot be changed through Update: use UpdateWithBasisGuardInTx, " +
		"which takes the goal row lock and archives/seeds the contribution ledger in the same transaction")

// Update updates an existing savings goal, refusing any change to progress_basis.
//
// The refusal is not incidental — see ErrUnguardedBasisChange. Every other field is unaffected: a
// rename, a new target, a recompute-adjacent write all behave exactly as before. Only a basis
// CHANGE is refused, and a blank stored column counts as manual, the same normalisation every
// other write path here applies, so a legacy row does not become unwritable.
//
// This became transactional to hold that check honestly: the basis is read under the same
// `SELECT … FOR UPDATE` the switch takes, so the row cannot move between the check and the write.
func (r *savingsGoalRepository) Update(ctx context.Context, goal *domain.SavingsGoal) error {
	if goal.ProgressBasis == "" {
		goal.ProgressBasis = domain.ProgressBasisManual
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	var stored string
	if err := tx.QueryRow(ctx, goalBasisLockQuery, goal.ID, goal.UserID).Scan(&stored); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrSavingsGoalNotFound
		}
		return err
	}
	if normaliseBasis(stored) != normaliseBasis(goal.ProgressBasis) {
		return fmt.Errorf("%w (goal %s is on %q, this write carries %q)",
			ErrUnguardedBasisChange, goal.ID, normaliseBasis(stored), normaliseBasis(goal.ProgressBasis))
	}

	if err := r.update(ctx, tx, goal); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BeginTx implements SavingsGoalRepository.BeginTx — see that method's doc comment.
func (r *savingsGoalRepository) BeginTx(ctx context.Context) (Transaction, error) {
	return r.db.Begin(ctx)
}

// UpdateWithBasisGuardInTx implements SavingsGoalRepository.UpdateWithBasisGuardInTx — see that
// method's doc comment for why the lock and the predicate are both required.
func (r *savingsGoalRepository) UpdateWithBasisGuardInTx(ctx context.Context, tx Transaction, goal *domain.SavingsGoal, priorBasis string) error {
	var observed string
	if err := tx.QueryRow(ctx, goalBasisLockQuery, goal.ID, goal.UserID).Scan(&observed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrSavingsGoalNotFound
		}
		return err
	}

	// A blank column and "manual" are the same basis — the repository has always defaulted one to
	// the other on write, so comparing them raw would report a conflict for a row nobody touched.
	if normaliseBasis(observed) != normaliseBasis(priorBasis) {
		return fmt.Errorf("%w: goal %s is now on %q, not the %q this switch was planned from",
			domain.ErrSavingsGoalBasisConflict, goal.ID, normaliseBasis(observed), normaliseBasis(priorBasis))
	}

	return r.update(ctx, tx, goal)
}

// normaliseBasis collapses the empty string onto the manual basis, which is what every write path
// in this repository already does implicitly.
func normaliseBasis(basis string) string {
	if basis == "" {
		return domain.ProgressBasisManual
	}
	return basis
}

// update is the shared body of Update and UpdateWithBasisGuardInTx. q is the pool for the former
// and the caller's transaction for the latter.
func (r *savingsGoalRepository) update(ctx context.Context, q Querier, goal *domain.SavingsGoal) error {
	goal.UpdatedAt = time.Now()

	if goal.ProgressBasis == "" {
		goal.ProgressBasis = domain.ProgressBasisManual
	}

	ct, err := q.Exec(ctx, goalUpdateQuery,
		goal.Name,
		goal.Description,
		goal.TargetAmount,
		goal.CurrentAmount,
		goal.CurrencyCode,
		goal.AccountID,
		goal.Deadline,
		goal.IsCompleted,
		goal.CompletedAt,
		goal.UpdatedAt,
		goal.UserAccountID,
		goal.ProgressBasis,
		goal.AccountPercentage,
		goal.LastCalculatedAt,
		goal.BalanceAsOf,
		goal.ID,
		goal.UserID,
	)

	if err != nil {
		if strings.Contains(err.Error(), "unique_savings_goal_name_per_user") ||
			strings.Contains(err.Error(), "duplicate key") {
			return domain.ErrSavingsGoalAlreadyExists
		}
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrSavingsGoalNotFound
	}

	return nil
}

// Delete implements SavingsGoalRepository.Delete — see that method's doc comment for why this is
// a tombstone rather than a DELETE.
func (r *savingsGoalRepository) Delete(ctx context.Context, id, userID string, auditLog AuditLogRepository) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	ct, err := tx.Exec(ctx, `
		UPDATE savings_goals
		SET deleted_at = NOW(), deleted_reason = $3, deleted_by = $4, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, id, userID, domain.SoftDeleteReasonUser, domain.SoftDeletedByUser)
	if err != nil {
		return fmt.Errorf("delete savings goal %s: %w", id, err)
	}
	if ct.RowsAffected() == 0 {
		// Either it never existed, belongs to someone else, or is already tombstoned. All three
		// are "not found" from the caller's side: a tombstoned goal is invisible to every read.
		return domain.ErrSavingsGoalNotFound
	}

	// budget_categories.savings_goal_id -> savings_goals(id) is ON DELETE CASCADE (migration
	// 000097), so before this change deleting a goal also removed the savings categories that
	// CreateGoal auto-creates in every active budget. Tombstoning the goal alone would leave those
	// categories behind, budgeting against a goal the user can no longer see. They are tombstoned
	// with reason 'parent_deleted' so that behaviour is preserved -- while rollover_history rows
	// that name one of them as their source, which the CASCADE used to destroy, survive.
	if _, err := tx.Exec(ctx, `
		UPDATE budget_categories
		SET deleted_at = NOW(), deleted_reason = $2, deleted_by = $3, updated_at = NOW()
		WHERE savings_goal_id = $1 AND deleted_at IS NULL
	`, id, domain.SoftDeleteReasonParent, domain.SoftDeletedByUser); err != nil {
		return fmt.Errorf("delete savings goal %s: tombstoning its budget categories: %w", id, err)
	}

	if auditLog != nil {
		if err := auditLog.LogInTx(ctx, tx, "savings_goal.deleted", "savings_goal", id); err != nil {
			return fmt.Errorf("delete savings goal %s: audit log: %w", id, err)
		}
	}

	return tx.Commit(ctx)
}

// PurgeGoal implements SavingsGoalRepository.PurgeGoal — see that method's doc comment.
func (r *savingsGoalRepository) PurgeGoal(ctx context.Context, id, userID string, auditLog AuditLogRepository) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after a successful commit is a no-op; its error is not actionable

	// Lock the row and check the precondition BEFORE destroying anything. FOR UPDATE here is a row
	// lock on savings_goals, never on users, so it cannot participate in the users-row lock cycle
	// AuditLogRepository.LogInTx's doc comment warns about.
	//
	var deletedAt *time.Time
	err = tx.QueryRow(ctx, `
		-- tombstone-exempt(savings_goals): this is the purge path. It exists precisely to act on an
		-- already-tombstoned goal, so excluding tombstones here would leave it nothing to purge.
		SELECT deleted_at FROM savings_goals WHERE id = $1 AND user_id = $2 FOR UPDATE`,
		id, userID,
	).Scan(&deletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrSavingsGoalNotFound
		}
		return fmt.Errorf("purge savings goal %s: %w", id, err)
	}
	if deletedAt == nil {
		return domain.ErrSavingsGoalNotDeleted
	}

	// This is the destruction the tombstone was deferring: ON DELETE CASCADE takes
	// savings_goal_contributions, budget_categories and savings_goal_category_detach with it.
	//
	// `AND deleted_at IS NOT NULL` is repeated here on purpose (#1569 round 14, reversing the
	// earlier "restating it only lets the two predicates disagree" reasoning). That reasoning was
	// backwards: the SELECT ... FOR UPDATE's Go `if deletedAt == nil` is real protection today, but
	// it is a fact about THIS FUNCTION'S CONTROL FLOW, not about the SQL text, and no guard can see
	// it — TestEveryAllowlistedHistoryDeleteStillCarriesItsPredicate pins only the DELETE's own
	// WHERE clause. A future edit that weakened or deleted the Go check while leaving this DELETE
	// untouched would still hard-delete a live goal with every guard green. Restating the leg here
	// means the two predicates CAN disagree — and when they do, this DELETE affects zero rows and
	// fails closed (returns ErrSavingsGoalNotFound below) instead of destroying a live goal. The row
	// lock from the SELECT above still prevents any concurrent write from doing the reverse: nothing
	// can flip deleted_at back to NULL between that SELECT and this DELETE.
	ct, err := tx.Exec(ctx, `
		-- tombstone-exempt(write:savings_goals): the predicate is deliberately narrow, not absent --
		-- see the comment above (#1569 round 14 write-target audit).
		DELETE FROM savings_goals WHERE id = $1 AND user_id = $2 AND deleted_at IS NOT NULL`, id, userID)
	if err != nil {
		return fmt.Errorf("purge savings goal %s: %w", id, err)
	}
	if ct.RowsAffected() == 0 {
		// Either the goal does not exist / belongs to another user, or (should be unreachable given
		// the FOR UPDATE check above) it was live after all. ErrSavingsGoalNotFound is imprecise for
		// the second case, but the important property holds regardless: nothing was destroyed.
		return domain.ErrSavingsGoalNotFound
	}

	if auditLog != nil {
		if err := auditLog.LogInTx(ctx, tx, "savings_goal.history_purged", "savings_goal", id); err != nil {
			return fmt.Errorf("purge savings goal %s: audit log: %w", id, err)
		}
	}

	return tx.Commit(ctx)
}

// CountByUserID counts the total number of savings goals for a user.
func (r *savingsGoalRepository) CountByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM savings_goals WHERE user_id = $1 AND deleted_at IS NULL`

	var count int
	err := r.db.QueryRow(ctx, query, userID).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// CountCompletedByUserID counts the user's completed savings goals — see the interface method's
// doc comment (Issue #1569 Task 20) for why this cannot be derived from GetActiveByUserID.
//
// `deleted_at IS NULL` matches CountByUserID and GetActiveByUserID: a tombstoned goal is excluded
// from every other count the summary reports, and including it only here would make
// CompletedGoalCount incomparable with GoalCount.
func (r *savingsGoalRepository) CountCompletedByUserID(ctx context.Context, userID string) (int, error) {
	query := `SELECT COUNT(*) FROM savings_goals WHERE user_id = $1 AND is_completed = true AND deleted_at IS NULL`

	var count int
	if err := r.db.QueryRow(ctx, query, userID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// GetByAccountID retrieves all savings goals linked to a specific account.
// Issue #1109: Chain-expand via user_account_id to include pre-relink goals.
func (r *savingsGoalRepository) GetByAccountID(ctx context.Context, accountID string) ([]*domain.SavingsGoal, error) {
	query := `SELECT ` + goalSelectCols + ` FROM savings_goals
	WHERE (
		user_account_id = (SELECT user_account_id FROM accounts WHERE id = $1 AND user_account_id IS NOT NULL)
		OR (account_id = $1 AND user_account_id IS NULL)
	) AND is_completed = false AND deleted_at IS NULL ORDER BY created_at DESC`
	return r.scanGoals(ctx, query, accountID)
}

// scanGoals is a helper that scans savings goals from a query result.
func (r *savingsGoalRepository) scanGoals(ctx context.Context, query string, args ...interface{}) ([]*domain.SavingsGoal, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var goals []*domain.SavingsGoal
	for rows.Next() {
		goal := &domain.SavingsGoal{}
		if err := rows.Scan(
			&goal.ID,
			&goal.UserID,
			&goal.Name,
			&goal.Description,
			&goal.TargetAmount,
			&goal.CurrentAmount,
			&goal.CurrencyCode,
			&goal.AccountID,
			&goal.UserAccountID,
			&goal.Deadline,
			&goal.IsCompleted,
			&goal.CompletedAt,
			&goal.CreatedAt,
			&goal.UpdatedAt,
			&goal.ProgressBasis,
			&goal.AccountPercentage,
			&goal.LastCalculatedAt,
			&goal.BalanceAsOf,
		); err != nil {
			return nil, err
		}
		goal.PopulateDateDisplay() // #1631 Batch F2
		goals = append(goals, goal)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(goals) == 0 {
		return []*domain.SavingsGoal{}, nil
	}
	return goals, nil
}

// UpdateCurrentAmount stores the recomputed current_amount and records the recompute timestamp.
// balanceAsOf is non-nil when the basis reads account balance (balance / account_percentage).
// This is used by the savings progress service (Issue #452, extended by #1300).
func (r *savingsGoalRepository) UpdateCurrentAmount(ctx context.Context, goalID, basis string, currentAmount float64, lastCalculatedAt time.Time, balanceAsOf *time.Time) error {
	// #1625: `progress_basis = $5` is the whole point of the basis parameter. COALESCE mirrors the
	// blank-is-manual normalisation every write path here already applies, so a legacy row with an
	// empty column is still updatable by a manual-basis recompute rather than being permanently
	// unwritable.
	query := `
		UPDATE savings_goals
		SET current_amount = $1, last_calculated_at = $2, balance_as_of = $3, updated_at = NOW()
		WHERE id = $4 AND deleted_at IS NULL
		  AND COALESCE(NULLIF(progress_basis, ''), $6) = $5
	`

	ct, err := r.db.Exec(ctx, query, currentAmount, lastCalculatedAt, balanceAsOf, goalID,
		normaliseBasis(basis), domain.ProgressBasisManual)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		// Distinguish "gone" from "moved": only the second is a race worth retelling the caller
		// about, and reporting a live goal as not-found sent a 404 for a 409 situation.
		var live string
		probeErr := r.db.QueryRow(ctx,
			`SELECT COALESCE(NULLIF(progress_basis, ''), $2) FROM savings_goals WHERE id = $1 AND deleted_at IS NULL`,
			goalID, domain.ProgressBasisManual).Scan(&live)
		if probeErr == nil {
			return fmt.Errorf("%w: goal %s is on %q, but this recompute was computed for %q",
				domain.ErrSavingsGoalBasisConflict, goalID, live, normaliseBasis(basis))
		}
		return domain.ErrSavingsGoalNotFound
	}

	return nil
}

// MarkCompleted marks a savings goal as completed.
// completed_at is sticky: it is only set if not already set (Codex C5).
// This is called when the current amount reaches the target amount.
func (r *savingsGoalRepository) MarkCompleted(ctx context.Context, goalID string) error {
	now := time.Now()

	// completed_at is only written if NULL (sticky once set).
	query := `
		UPDATE savings_goals
		SET is_completed = true,
		    completed_at = CASE WHEN completed_at IS NULL THEN $1 ELSE completed_at END,
		    updated_at = $1
		WHERE id = $2 AND deleted_at IS NULL
	`

	ct, err := r.db.Exec(ctx, query, now, goalID)
	if err != nil {
		return err
	}

	if ct.RowsAffected() == 0 {
		return domain.ErrSavingsGoalNotFound
	}

	return nil
}
