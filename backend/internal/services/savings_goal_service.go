package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// AccountGetter is a minimal interface for retrieving user accounts.
// This allows SavingsGoalService to validate account ownership without
// depending on the full AccountRepository.
type AccountGetter interface {
	GetByUserID(ctx context.Context, userID string) ([]domain.Account, error)
}

// GoalAutoCreateBudgetRepo is a minimal interface for auto-creating savings categories
// in active budgets when a new savings goal is created (Issue #1300).
// Since migration 000098 the detach tombstone is keyed on series_id, so all methods
// that touch the tombstone use the budget's SeriesID rather than its ID.
type GoalAutoCreateBudgetRepo interface {
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.Budget, error)
	CreateCategory(ctx context.Context, category *domain.BudgetCategory) error
	IsGoalDetachedForSeries(ctx context.Context, seriesID, goalID string) (bool, error)
}

// GoalProgressRecomputer is a narrow interface for triggering goal progress recomputation.
// Used by SavingsGoalService after CreateGoal to initialize current_amount per progress_basis
// (Issue #1300 M13 / G3). Implementations must be non-blocking-safe; errors are logged only.
//
// #1625: returns the recomputed amount. Recomputation persists through UpdateCurrentAmount, which
// does NOT mutate the in-memory goal — so CreateGoal used to hand back the caller-supplied
// current_amount while the database held the recomputed one. Under the contributions basis that
// is a brand-new goal reporting progress it does not have. The error contract is unchanged:
// still best-effort, still logged only.
type GoalProgressRecomputer interface {
	RecomputeGoal(ctx context.Context, goal *domain.SavingsGoal) (float64, error)
}

// GoalBasisSwitchContribRepo is a narrow interface for contribution operations needed during
// progress_basis changes (Issue #1300 M14 / Codex C10).
//   - Insert: seeds a "manual"-source contribution row equal to prior current_amount when
//     switching manual → contributions, so existing savings history is preserved.
//   - ArchiveByGoal: archives contributions for a goal by setting managed_by_reconciliation=false
//     and archived_at=NOW() (or preserving existing archived_at). Called on switches INTO
//     contributions (to close any prior contributions period) and away from contributions (to
//     close the current period). Excludes rejected rows (sticky tombstones remain managed).
//
// #1625: both methods are the InTx forms. The switch is no longer three independent writes that
// each commit on their own — it is ONE transaction owned by applyBasisSwitch, and these run inside
// it. A non-transactional variant is deliberately absent from this interface so a future edit
// cannot quietly step back outside the boundary.
type GoalBasisSwitchContribRepo interface {
	InsertInTx(ctx context.Context, tx repository.Transaction, userID string, contrib *domain.SavingsGoalContribution) error
	ArchiveByGoalInTx(ctx context.Context, tx repository.Transaction, goalID string) error
}

// SavingsGoalService defines the interface for savings goal business logic.
type SavingsGoalService interface {
	// Goal CRUD
	CreateGoal(ctx context.Context, userID string, req *domain.CreateSavingsGoalRequest) (*domain.SavingsGoal, error)
	GetGoal(ctx context.Context, userID, goalID string) (*domain.SavingsGoal, error)
	GetGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)

	// GetDeletedGoals lists the user's deleted (tombstoned) goals so the UI can show what was
	// retained and offer PurgeGoal on it. Without this the purge endpoint has no reachable
	// precondition. Issue #1569, Task 12.
	GetDeletedGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	GetActiveGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	UpdateGoal(ctx context.Context, userID, goalID string, req *domain.UpdateSavingsGoalRequest) (*domain.SavingsGoal, error)
	// DeleteGoal tombstones a savings goal. It is NOT destructive: every
	// savings_goal_contribution the user logged by hand survives, and the goal simply becomes
	// invisible. Issue #1569, Task 8.
	DeleteGoal(ctx context.Context, userID, goalID string) error

	// PurgeGoal permanently destroys an ALREADY-DELETED goal and every contribution under it.
	// Refuses with domain.ErrSavingsGoalNotDeleted on a live goal — that two-step is the explicit
	// confirmation, the same shape the account purge uses. Issue #1569, Task 8.
	PurgeGoal(ctx context.Context, userID, goalID string) error

	// SetAuditLogRepository wires the savings_goal.deleted / savings_goal.history_purged audit
	// entries, which are written INSIDE the repository's own transaction via LogInTx so the entry
	// and the state change commit or roll back together. Issue #1569, Task 8.
	//
	// ⚠ THE TWO PATHS DIFFER SINCE #1569 ROUND 17 TASK 11, and this comment claimed a blanket
	// "nil-safe optional dependency: when unset, the deletes still work and simply write no entry"
	// until round 17 corrected it. That is now true of exactly one of them:
	//   - DeleteGoal (TOMBSTONE, savings_goal.deleted) — still nil-tolerant. It runs with no sink
	//     and writes no entry, because nothing is destroyed.
	//   - PurgeGoal (DESTRUCTION, savings_goal.history_purged) — REFUSES with
	//     repository.ErrAuditLogRepositoryNotWired, a 500, rather than destroying un-audited.
	// So leaving this unwired does not silently downgrade the trail; it disables the purge endpoint.
	//
	// ⚠ goalRepo.PurgeGoal itself is STILL nil-tolerant (it skips the audit write on a nil sink so
	// integration tests need no audit fixture). PurgeGoal's own nil check is the only barrier.
	SetAuditLogRepository(ar repository.AuditLogRepository)

	// SetBudgetRepo wires the optional budget repository for auto-creating savings categories
	// in active budgets when a goal is created. If not set, auto-create is skipped. Issue #1300.
	SetBudgetRepo(repo GoalAutoCreateBudgetRepo)

	// SetProgressService wires the optional progress recomputer for initializing
	// current_amount after CreateGoal. If not set, recomputation is skipped (Issue #1300 M13).
	SetProgressService(svc GoalProgressRecomputer)

	// SetBasisSwitchContribRepo wires the optional contribution repository for basis-switch
	// semantics in UpdateGoal (seed on manual→contributions; archive on contributions→other).
	// If not set, basis-switch contribution operations are skipped (Issue #1300 M14).
	SetBasisSwitchContribRepo(repo GoalBasisSwitchContribRepo)

	// SetAnalytics wires the optional PostHog analytics client. When set, a
	// "savings_goal_progress_basis_changed" event is fired on basis change (Issue #1300 M14).
	SetAnalytics(analytics phclient.Analytics)
}

// savingsGoalService implements SavingsGoalService
type savingsGoalService struct {
	goalRepo    repository.SavingsGoalRepository
	accountRepo AccountGetter
	logger      *slog.Logger
	// budgetRepo is optional; when set, CreateGoal auto-creates savings categories
	// in all active budgets for the new goal (best-effort, Issue #1300).
	budgetRepo GoalAutoCreateBudgetRepo
	// progressService is optional; when set, CreateGoal calls RecomputeGoal to
	// initialise current_amount per progress_basis (best-effort, Issue #1300 M13).
	progressService GoalProgressRecomputer
	// basisSwitchRepo is optional; when set, UpdateGoal seeds / archives contributions
	// on progress_basis changes (best-effort, Issue #1300 M14 / Codex C10).
	basisSwitchRepo GoalBasisSwitchContribRepo
	// analytics is optional; when set, basis-change events are fired via PostHog
	// (Issue #1300 M14). Nil-safe: guard with "if s.analytics != nil".
	analytics phclient.Analytics
	// auditLogRepo records savings_goal.deleted / savings_goal.history_purged entries INSIDE the
	// repository's own transaction (Issue #1569 Task 8), via LogInTx. Wired by
	// SetAuditLogRepository — same pattern as accountService.auditLogRepo, and safe for the same
	// reason: neither transaction locks a users row.
	//
	// ⚠ Optional for DeleteGoal (tombstone), REQUIRED for PurgeGoal (destruction), which returns
	// repository.ErrAuditLogRepositoryNotWired while this is nil. See the setter's doc.
	auditLogRepo repository.AuditLogRepository
}

// NewSavingsGoalService creates a new SavingsGoalService instance.
func NewSavingsGoalService(
	goalRepo repository.SavingsGoalRepository,
	accountRepo AccountGetter,
	logger *slog.Logger,
) SavingsGoalService {
	return &savingsGoalService{
		goalRepo:    goalRepo,
		accountRepo: accountRepo,
		logger:      logger,
	}
}

// CreateGoal creates a new savings goal for a user.
func (s *savingsGoalService) CreateGoal(ctx context.Context, userID string, req *domain.CreateSavingsGoalRequest) (*domain.SavingsGoal, error) {
	s.logger.Info("creating savings goal",
		slog.String("user_id", userID),
		slog.String("goal_name", req.Name),
		slog.String("account_id", req.AccountID),
	)

	// Create goal from request
	// #1625: default NEW goals to the contributions basis. domain/savings_goal.go has documented
	// "contributions" as the default for new goals since #1300, but the code defaulted to
	// "manual" — so POST /savings-goals/{id}/contributions rejected every brand-new goal with a
	// bare 422 "basis mismatch" and no way to discover the precondition. Existing goals are
	// untouched: this is the create path only, and the repository still defaults a blank basis to
	// manual for rows written by any other route.
	progressBasis := req.ProgressBasis
	if progressBasis == "" {
		progressBasis = domain.ProgressBasisContributions
	}
	goal := &domain.SavingsGoal{
		ID:                uuid.New().String(),
		UserID:            userID,
		Name:              req.Name,
		Description:       req.Description,
		TargetAmount:      req.TargetAmount,
		CurrentAmount:     seedCurrentAmount(progressBasis, req.CurrentAmount),
		CurrencyCode:      req.CurrencyCode,
		AccountID:         req.AccountID,
		Deadline:          req.Deadline,
		IsCompleted:       false,
		ProgressBasis:     progressBasis,
		AccountPercentage: req.AccountPercentage,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	// Default currency if not provided
	if goal.CurrencyCode == "" {
		goal.CurrencyCode = "USD"
	}

	// Validate goal fields first
	if err := goal.Validate(); err != nil {
		s.logger.Warn("savings goal validation failed",
			slog.String("user_id", userID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	// Issue #1109: Validate account ownership and capture stable UserAccountID for re-link stability.
	account, err := s.validateAccountOwnershipWithAccount(ctx, userID, req.AccountID)
	if err != nil {
		return nil, err
	}
	if account != nil && account.UserAccountID != nil {
		goal.UserAccountID = account.UserAccountID
	}

	// Save to database
	if err := s.goalRepo.Create(ctx, goal); err != nil {
		s.logger.Error("failed to create savings goal",
			slog.String("user_id", userID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	// Auto-create savings categories in all active budgets (Issue #1300).
	// Best-effort: NOT in the goal-insert transaction; unique index no-ops dupes.
	if s.budgetRepo != nil {
		s.autoCreateSavingsCategories(ctx, userID, goal.ID)
	}

	// Trigger an initial recompute so current_amount reflects the configured
	// progress_basis from creation time (Issue #1300 M13 / G3). Best-effort:
	// an error here must not fail goal creation.
	if s.progressService != nil {
		recomputed, err := s.progressService.RecomputeGoal(ctx, goal)
		// Assigned even on error: RecomputeGoal reports the amount the ROW now holds, and its
		// two writes mean a failure does not imply nothing changed. Reporting the caller's seed
		// after a partial write is how the response and the database came to disagree.
		goal.CurrentAmount = recomputed
		if err != nil {
			s.logger.Warn("savings goal created but RecomputeGoal failed (non-fatal)",
				slog.String("user_id", userID),
				slog.String("goal_id", goal.ID),
				slog.String("error", err.Error()),
			)
		} else {
			// Success: the recompute touched five more columns than the amount. Re-read rather
			// than guess at them.
			goal = s.reloadAfterRecompute(ctx, goal)
		}
	}

	s.logger.Info("savings goal created",
		slog.String("user_id", userID),
		slog.String("goal_id", goal.ID),
	)

	return goal, nil
}

// SetBudgetRepo wires the optional budget repository for auto-creating savings categories.
func (s *savingsGoalService) SetBudgetRepo(repo GoalAutoCreateBudgetRepo) {
	s.budgetRepo = repo
}

// SetProgressService wires the optional progress recomputer (Issue #1300 M13).
func (s *savingsGoalService) SetProgressService(svc GoalProgressRecomputer) {
	s.progressService = svc
}

// SetBasisSwitchContribRepo wires the optional contribution repo for basis-switch
// seed / archive operations (Issue #1300 M14).
func (s *savingsGoalService) SetBasisSwitchContribRepo(repo GoalBasisSwitchContribRepo) {
	s.basisSwitchRepo = repo
}

// SetAuditLogRepository wires the optional audit-log repository. See the interface method.
func (s *savingsGoalService) SetAuditLogRepository(ar repository.AuditLogRepository) {
	s.auditLogRepo = ar
}

// SetAnalytics wires the optional PostHog analytics client (Issue #1300 M14).
func (s *savingsGoalService) SetAnalytics(analytics phclient.Analytics) {
	s.analytics = analytics
}

// autoCreateSavingsCategories fans out a savings category insert to each active budget
// for which the (budget, goal) pair is not tombstoned. Errors are logged and ignored
// (best-effort per plan — goal creation already succeeded at this point).
func (s *savingsGoalService) autoCreateSavingsCategories(ctx context.Context, userID, goalID string) {
	budgets, err := s.budgetRepo.GetActiveByUserID(ctx, userID)
	if err != nil {
		s.logger.Warn("auto-create: failed to fetch active budgets",
			slog.String("goal_id", goalID),
			slog.String("error", err.Error()),
		)
		return
	}

	for _, budget := range budgets {
		detached, err := s.budgetRepo.IsGoalDetachedForSeries(ctx, budget.SeriesID, goalID)
		if err != nil {
			s.logger.Warn("auto-create: failed to check tombstone",
				slog.String("budget_id", budget.ID),
				slog.String("series_id", budget.SeriesID),
				slog.String("goal_id", goalID),
				slog.String("error", err.Error()),
			)
			continue
		}
		if detached {
			continue
		}

		category := &domain.BudgetCategory{
			ID:              uuid.New().String(),
			BudgetID:        budget.ID,
			CategoryKind:    "savings",
			SavingsGoalID:   &goalID,
			AllocatedAmount: 0,
			AutoCreated:     true,
			ManagedByGoalID: &goalID,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}

		if err := s.budgetRepo.CreateCategory(ctx, category); err != nil {
			s.logger.Warn("auto-create: failed to create savings category",
				slog.String("budget_id", budget.ID),
				slog.String("goal_id", goalID),
				slog.String("error", err.Error()),
			)
		}
	}
}

// GetGoal retrieves a savings goal by ID, verifying ownership.
func (s *savingsGoalService) GetGoal(ctx context.Context, userID, goalID string) (*domain.SavingsGoal, error) {
	goal, err := s.goalRepo.GetByID(ctx, goalID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if goal.UserID != userID {
		return nil, domain.ErrSavingsGoalNotFound
	}

	return goal, nil
}

// GetDeletedGoals retrieves the user's tombstoned savings goals, newest deletion first.
//
// Deliberately does NOT recompute progress or hydrate accounts the way the live listing does: a
// deleted goal is a historical record, and recomputing a balance-based current_amount for one
// would state a figure that has kept moving since the user stopped tracking it. The stored values
// are what the goal looked like when it was deleted, which is what the purge screen should show.
func (s *savingsGoalService) GetDeletedGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	return s.goalRepo.GetDeletedByUserID(ctx, userID)
}

// GetGoals retrieves all savings goals for a user.
func (s *savingsGoalService) GetGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	return s.goalRepo.GetByUserID(ctx, userID)
}

// GetActiveGoals retrieves all active (not completed) savings goals for a user.
func (s *savingsGoalService) GetActiveGoals(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	return s.goalRepo.GetActiveByUserID(ctx, userID)
}

// UpdateGoal updates an existing savings goal.
func (s *savingsGoalService) UpdateGoal(ctx context.Context, userID, goalID string, req *domain.UpdateSavingsGoalRequest) (*domain.SavingsGoal, error) {
	s.logger.Info("updating savings goal",
		slog.String("user_id", userID),
		slog.String("goal_id", goalID),
	)

	// Get existing goal
	goal, err := s.goalRepo.GetByID(ctx, goalID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if goal.UserID != userID {
		return nil, domain.ErrSavingsGoalNotFound
	}

	// If changing account, validate new account ownership and refresh UserAccountID
	// Issue #1109: B.5 — refresh to stable identity rather than potentially replaced account_id
	if req.AccountID != nil && *req.AccountID != goal.AccountID {
		newAccount, err := s.validateAccountOwnershipWithAccount(ctx, userID, *req.AccountID)
		if err != nil {
			return nil, err
		}
		if newAccount != nil {
			goal.UserAccountID = newAccount.UserAccountID
		}
	}

	// Snapshot pre-update state for basis-switch semantics (Issue #1300 M14 / Codex C10).
	// These must be captured BEFORE applying any req.* field updates so that
	// handleBasisSwitch receives the truly prior values even when a caller changes
	// both progress_basis and current_amount in the same request.
	priorBasis := goal.ProgressBasis
	priorAmount := goal.CurrentAmount
	basisChanged := false

	// Apply updates
	if req.Name != nil {
		goal.Name = *req.Name
	}
	if req.Description != nil {
		goal.Description = req.Description
	}
	if req.TargetAmount != nil {
		goal.TargetAmount = *req.TargetAmount
	}
	if req.CurrentAmount != nil {
		// ⚠ #1625: a DERIVED basis owns this number, so a caller-supplied one is refused rather
		// than written. Without this check, PUT {"current_amount": 5000} on a contributions goal
		// persists 5000 against an empty ledger — the ORIGINAL bug of this ticket, restored
		// through the update endpoint. The create path was fixed and this one was not; found by
		// adversarial review, not by me.
		//
		// Refused, not silently ignored: the caller asked for something the basis cannot honour,
		// and a 4xx that says so beats a 200 that pretends.
		//
		// The check is on the TARGET basis, so switching TO manual while setting the amount is
		// ALLOWED — that is the escape hatch the error message advertises, and it must work or
		// the advice is a dead end. Only a switch INTO a derived basis is refused. (An earlier
		// version of this comment claimed any basis switch with an amount was refused; the code
		// never did that, and the test table now covers both directions.)
		targetBasis := goal.ProgressBasis
		if req.ProgressBasis != nil {
			targetBasis = *req.ProgressBasis
		}
		if targetBasis != domain.ProgressBasisManual {
			return nil, fmt.Errorf(
				"%w: current_amount cannot be set on a goal whose progress_basis is %q — that value is "+
					"derived. Log a contribution instead (POST /savings-goals/{id}/contributions); "+
					"balance-based goals track their account automatically. Switching progress_basis to %q "+
					"would let you set it by hand, but on a contributions goal that ARCHIVES the existing "+
					"contribution ledger and switching back seeds only a single synthetic contribution",
				domain.ErrInvalidGoalUpdate, targetBasis, domain.ProgressBasisManual)
		}
		goal.CurrentAmount = *req.CurrentAmount
	}
	if req.AccountID != nil {
		goal.AccountID = *req.AccountID
	}
	if req.Deadline != nil {
		goal.Deadline = req.Deadline
	}
	// Issue #1300 gap G1: apply progress_basis / account_percentage changes.
	// If switching away from account_percentage, clear the percentage so the
	// discriminator check in goal.Validate() does not reject the new basis.
	if req.ProgressBasis != nil {
		goal.ProgressBasis = *req.ProgressBasis
		if goal.ProgressBasis != domain.ProgressBasisAccountPercentage {
			goal.AccountPercentage = nil
		}
		basisChanged = goal.ProgressBasis != priorBasis
	}
	if req.AccountPercentage != nil {
		goal.AccountPercentage = req.AccountPercentage
	}

	// Validate
	if err := goal.Validate(); err != nil {
		return nil, err
	}

	// Save changes.
	//
	// #1625: a basis switch takes a DIFFERENT path — one transaction covering the new basis, the
	// archive of the closing period and the carry-over seed, behind a lock on the goal row. See
	// applyBasisSwitch. Everything else is the ordinary single-statement update.
	switch {
	case basisChanged && s.basisSwitchRepo == nil:
		// #1625 Task 17, fix round 1. This used to fall through to the plain Update below, which
		// takes no lock and checks no predicate — so a service whose OPTIONAL ledger repo was never
		// wired performed exactly the unguarded switch this ticket exists to remove, silently, with
		// every test green. Refused instead.
		//
		// The repository refuses it too (repository.ErrUnguardedBasisChange, raised inside Update
		// itself), which is what makes the bypass unrepresentable rather than merely tested
		// against. This branch exists so the error names the actual cause — an unwired service —
		// instead of arriving as a mystery from two layers down. Which production sites must wire
		// it is pinned by TestEverySavingsGoalServiceConstructionWiresTheBasisSwitchLedger.
		err := fmt.Errorf("%w: savings goal service has no basis-switch contribution repository wired, "+
			"so the progress_basis change on goal %s cannot archive the closing period or seed the "+
			"carry-over contribution", repository.ErrUnguardedBasisChange, goalID)
		s.logger.Error("refusing an unguarded progress_basis change",
			slog.String("goal_id", goalID),
			slog.String("prior_basis", priorBasis),
			slog.String("new_basis", goal.ProgressBasis),
		)
		return nil, err

	case basisChanged:
		if err := s.applyBasisSwitch(ctx, userID, goal, priorBasis, priorAmount); err != nil {
			s.logger.Error("failed to switch savings goal progress_basis",
				slog.String("goal_id", goalID),
				slog.String("prior_basis", priorBasis),
				slog.String("new_basis", goal.ProgressBasis),
				slog.String("error", err.Error()),
			)
			return nil, err
		}

	default:
		if err := s.goalRepo.Update(ctx, goal); err != nil {
			s.logger.Error("failed to update savings goal",
				slog.String("goal_id", goalID),
				slog.String("error", err.Error()),
			)
			return nil, err
		}
	}

	// Emit analytics event for the basis change (best-effort).
	if basisChanged && s.analytics != nil {
		s.analytics.Capture(userID, phclient.EventSavingsGoalProgressBasisChanged, map[string]interface{}{
			"goal_id":    goalID,
			"prior":      priorBasis,
			"new":        goal.ProgressBasis,
		})
	}

	// Re-run recompute so current_amount reflects the new basis immediately.
	if basisChanged && s.progressService != nil {
		recomputed, err := s.progressService.RecomputeGoal(ctx, goal)
		// Same reasoning as CreateGoal: this is the amount the row holds, error or not.
		goal.CurrentAmount = recomputed
		if err != nil {
			s.logger.Warn("basis-changed RecomputeGoal failed (non-fatal)",
				slog.String("goal_id", goalID),
				slog.String("error", err.Error()),
			)
		} else {
			goal = s.reloadAfterRecompute(ctx, goal)
		}
	}

	s.logger.Info("savings goal updated",
		slog.String("user_id", userID),
		slog.String("goal_id", goalID),
	)

	return goal, nil
}

// applyBasisSwitch persists a progress_basis change and everything that change implies, in ONE
// transaction, behind a SELECT … FOR UPDATE on the goal row.
//
// THE BOUNDARY, and why it is drawn here (#1625).
//
// This used to be three independent commits in a row: goalRepo.Update, then ArchiveByGoal, then
// Insert — with the seed's failure logged and swallowed. That produced a DATA-LOSS path with no
// warning attached to it. A manual goal holding $100 switches to the contributions basis; the new
// basis commits; the archive commits; the seed insert fails transiently; UpdateGoal returns 200;
// RecomputeGoal then sums a live ledger that is now empty and persists $0. The $100 was a number
// the user typed, no provider can supply it again, and nothing on this branch restores it.
// Retention is this codebase's default (see docs/decisions/2026-08-24-financial-history-retained-
// by-default.md) and a partially-applied switch violated it while reporting success.
//
// Inside the boundary: the goal-row lock, the basis predicate, the full goal UPDATE, the archive
// of the closing period, and the seed. Those four either all happen or none do. Rolling back
// CREATES nothing and DESTROYS nothing — the archive is an UPDATE of archived_at, so an aborted
// switch leaves the prior period exactly as live as it was. That matters: a rollback here must
// never be a deletion of retained history, and it is not.
//
// Outside the boundary: RecomputeGoal, which the caller runs after the commit. Three reasons, and
// the first is the one that decides it:
//
//  1. It does not need to be inside. The defect was that the seed could be missing while the
//     basis was present; once the seed is committed WITH the basis, the sum RecomputeGoal takes
//     can no longer come up empty. If the recompute then fails, current_amount still holds the
//     prior manual amount — which on a freshly seeded ledger is the same number the sum would
//     have produced. The failure mode is a stale timestamp, not a lost balance.
//  2. It cannot be inside for every basis. RecomputeGoal is basis-polymorphic: switching TO the
//     balance or account_percentage basis makes it read an account, resolve a re-link chain and
//     consult a separate repository. Dragging those into a transaction that holds a write lock on
//     the goal row would hold that lock across unrelated reads for no correctness gain.
//  3. The write it performs is now guarded in its own right. UpdateCurrentAmount binds the basis
//     it computed for into its WHERE, so a recompute that lost a race cannot land on a row that
//     has moved on. That is defect 4, and it is what makes leaving the recompute outside safe
//     rather than merely convenient.
//
// A FAILED ARCHIVE STILL STOPS THE SEED — the invariant an earlier round of #1625 established, and
// this must not regress it. It is preserved in a strictly stronger form: the archive error aborts
// the transaction, so not only is nothing seeded, the new basis is not persisted either. There is
// no interleaving in which the prior period's rows stay live beside a synthetic seed and the goal
// counts both.
func (s *savingsGoalService) applyBasisSwitch(
	ctx context.Context,
	userID string,
	goal *domain.SavingsGoal,
	priorBasis string,
	priorAmount float64,
) error {
	tx, err := s.goalRepo.BeginTx(ctx)
	if err != nil {
		return err
	}
	// Rollback after a successful commit is a no-op; its error is not actionable.
	defer tx.Rollback(ctx) //nolint:errcheck

	// Locks the goal row and refuses if another request already moved it off priorBasis. Two
	// concurrent manual→contributions switches would otherwise both archive and both seed, and a
	// seed carries no transaction_id so the partial unique index does not stop the second.
	if err := s.goalRepo.UpdateWithBasisGuardInTx(ctx, tx, goal, priorBasis); err != nil {
		return err
	}

	switch {
	case priorBasis == domain.ProgressBasisManual && goal.ProgressBasis == domain.ProgressBasisContributions:
		// Close out any PRIOR contributions period before seeding (#1625 Task 12). Without it, a
		// goal that was on the contributions basis earlier still has those rows live and
		// confirmed; the seed below then adds a synthetic row for the same money and the sums
		// report both (100 + 100 = 200). It is a no-op for a goal that has never been on the
		// contributions basis.
		if err := s.basisSwitchRepo.ArchiveByGoalInTx(ctx, tx, goal.ID); err != nil {
			return fmt.Errorf("basis switch: archive prior period for goal %s: %w", goal.ID, err)
		}

		// Seed: carry the existing manual balance as a confirmed contribution.
		if priorAmount > 0 {
			seed := &domain.SavingsGoalContribution{
				SavingsGoalID:           goal.ID,
				Amount:                  priorAmount,
				Source:                  "manual",
				Status:                  "confirmed",
				ManagedByReconciliation: false,
				ConfirmedByUser:         true,
				OccurredOn:              time.Now().Format("2006-01-02"),
			}
			if err := s.basisSwitchRepo.InsertInTx(ctx, tx, userID, seed); err != nil {
				// FATAL, where this was once logged and ignored. Returning the error rolls the
				// whole switch back, so the goal stays manual and keeps its balance — the user
				// sees a failure and retries, instead of a success that quietly zeroed them.
				return fmt.Errorf("basis switch: seed carry-over contribution for goal %s: %w", goal.ID, err)
			}
		}

	case priorBasis == domain.ProgressBasisContributions:
		// Leaving the contributions basis closes the period: the reconciler stops modifying these
		// rows and no sum counts them, but they are RETAINED and still readable.
		if err := s.basisSwitchRepo.ArchiveByGoalInTx(ctx, tx, goal.ID); err != nil {
			return fmt.Errorf("basis switch: archive contributions for goal %s: %w", goal.ID, err)
		}
	}

	return tx.Commit(ctx)
}

// DeleteGoal tombstones a savings goal — see the interface method's doc comment.
func (s *savingsGoalService) DeleteGoal(ctx context.Context, userID, goalID string) error {
	s.logger.Info("deleting savings goal",
		slog.String("user_id", userID),
		slog.String("goal_id", goalID),
	)

	// Get existing goal to verify ownership
	goal, err := s.goalRepo.GetByID(ctx, goalID)
	if err != nil {
		return err
	}

	// Verify ownership
	if goal.UserID != userID {
		return domain.ErrSavingsGoalNotFound
	}

	// Tombstone the goal. Every contribution STILL logged against it is retained (one the user
	// explicitly deleted was hard-deleted then and does not come back); see
	// SavingsGoalRepository.Delete.
	if err := s.goalRepo.Delete(ctx, goalID, userID, s.auditLogRepo); err != nil {
		s.logger.Error("failed to delete savings goal",
			slog.String("goal_id", goalID),
			slog.String("error", err.Error()),
		)
		return err
	}

	s.logger.Info("savings goal deleted",
		slog.String("user_id", userID),
		slog.String("goal_id", goalID),
	)

	return nil
}

// PurgeGoal permanently destroys an already-deleted goal — see the interface method's doc comment.
//
// Ownership is enforced by the repository query itself (`WHERE id = $1 AND user_id = $2`), not by
// a read-then-check in this method: DeleteGoal's GetByID-then-compare cannot be reused here
// because every read excludes tombstoned goals, and a purge acts only on tombstoned goals.
func (s *savingsGoalService) PurgeGoal(ctx context.Context, userID, goalID string) error {
	// Control 2 is not optional: refuse rather than destroy un-audited. See
	// repository.ErrAuditLogRepositoryNotWired.
	if s.auditLogRepo == nil {
		return repository.ErrAuditLogRepositoryNotWired
	}

	s.logger.Info("purging savings goal",
		slog.String("user_id", userID),
		slog.String("goal_id", goalID),
	)

	if err := s.goalRepo.PurgeGoal(ctx, goalID, userID, s.auditLogRepo); err != nil {
		s.logger.Error("failed to purge savings goal",
			slog.String("goal_id", goalID),
			slog.String("error", err.Error()),
		)
		return err
	}

	s.logger.Info("savings goal purged",
		slog.String("user_id", userID),
		slog.String("goal_id", goalID),
	)
	return nil
}

// validateAccountOwnershipWithAccount checks account ownership and returns the matched account.
// Issue #1109: Returns the account so the caller can capture its UserAccountID.
func (s *savingsGoalService) validateAccountOwnershipWithAccount(ctx context.Context, userID, accountID string) (*domain.Account, error) {
	accounts, err := s.accountRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	for i := range accounts {
		if accounts[i].ID == accountID {
			return &accounts[i], nil
		}
	}

	return nil, errors.New("account not found or does not belong to user")
}

// seedCurrentAmount decides what current_amount a goal is CREATED with (#1625).
//
// A goal created this instant under the contributions basis has, by definition, no
// contributions, so its current_amount is zero — no query required. A caller-supplied figure is
// meaningless there: the ledger owns that number.
//
// ⚠ This runs BEFORE the insert, deliberately. The first version of this fix zeroed the
// in-memory goal AFTER goalRepo.Create, which left the row holding the caller's 5000 while the
// response said 0 — a worse bug than the one it replaced, because the two now disagreed and
// only one of them was visible. Codex review caught it; the mock expectation in the test was
// mock.AnythingOfType, so the test could not see the persisted argument at all.
//
// Scoped to `contributions` on purpose. For the balance and account_percentage bases zero would
// be a fabricated answer rather than a derived one, so those keep the caller's value and are
// corrected by the next recompute.
func seedCurrentAmount(progressBasis string, requested float64) float64 {
	if progressBasis == domain.ProgressBasisContributions {
		return 0
	}
	return requested
}

// reloadAfterRecompute re-reads the goal so the RESPONSE matches the ROW.
//
// #1625. RecomputeGoal writes through UpdateCurrentAmount (current_amount, last_calculated_at,
// balance_as_of, updated_at) and MarkCompleted (is_completed, completed_at, updated_at) — six
// columns across two statements, on two different clocks: UpdateCurrentAmount takes a Go
// timestamp while MarkCompleted stamps its own, and updated_at is written by both.
//
// Three successive attempts to keep the in-memory struct in step by copying fields back were
// each wrong in a new way, because the set of fields kept growing and the timestamps were
// unknowable from here. Re-reading is one query and is correct for every column, including ones
// added later. It replaces the mirroring rather than supplementing it.
//
// Best-effort, like the recompute it follows: a failed re-read logs and leaves the caller with
// the pre-recompute struct, which is no worse than what it had before this existed.
func (s *savingsGoalService) reloadAfterRecompute(ctx context.Context, goal *domain.SavingsGoal) *domain.SavingsGoal {
	fresh, err := s.goalRepo.GetByID(ctx, goal.ID)
	if err != nil || fresh == nil {
		if err != nil {
			s.logger.Warn("could not re-read goal after recompute; response may lag the row",
				slog.String("goal_id", goal.ID),
				slog.String("error", err.Error()),
			)
		}
		return goal
	}
	return fresh
}
