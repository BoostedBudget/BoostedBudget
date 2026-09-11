package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// =============================================================================
// Issue #1300: Savings progress recompute service (basis-aware, Codex C5/C9/C10)
// =============================================================================

// SavingsProgressGoalRepo is the minimal interface for goal-side operations.
type SavingsProgressGoalRepo interface {
	// GetActiveByUserID returns all non-completed goals for a user.
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
	// GetByIDForUser retrieves a single goal by ID with ownership validation.
	// Used by RecomputeGoalByID (R3-2) to load a specific goal (incl. completed)
	// without scanning all active goals.
	GetByIDForUser(ctx context.Context, id, userID string) (*domain.SavingsGoal, error)
	// UpdateCurrentAmount stores the recomputed current_amount + timestamps.
	//
	// basis is the progress_basis this figure was computed for, and the repository binds it into
	// the statement's WHERE (#1625). A recompute that lost a race to a basis switch therefore
	// lands nowhere and reports domain.ErrSavingsGoalBasisConflict, instead of overwriting the
	// winner's value with one that is right for a basis the row no longer has.
	UpdateCurrentAmount(ctx context.Context, goalID, basis string, currentAmount float64, lastCalculatedAt time.Time, balanceAsOf *time.Time) error
	// MarkCompleted sets is_completed and sticky completed_at (Codex C5).
	MarkCompleted(ctx context.Context, goalID string) error
	// CountCompletedByUserID counts the user's completed goals. Required because
	// GetActiveByUserID's universe excludes them — see Issue #1569 Task 20 (A4).
	CountCompletedByUserID(ctx context.Context, userID string) (int, error)
}

// SavingsProgressContribRepo is the minimal interface for reading contribution totals.
type SavingsProgressContribRepo interface {
	// SumConfirmedByGoal returns the sum of confirmed contributions for progress_basis="contributions".
	// userID is required for ownership scoping (RG2 / M17 — Codex C1).
	SumConfirmedByGoal(ctx context.Context, userID, goalID string) (float64, error)
}

// SavingsProgressAccountRepo is the minimal interface for reading account balances.
type SavingsProgressAccountRepo interface {
	// GetByIDAndUser returns an account with its current_balance for a given user.
	GetByIDAndUser(ctx context.Context, accountID, userID string) (*domain.Account, error)
}

// SavingsProgressService recomputes goal progress based on the configured progress_basis
// and handles goal completion detection (Codex C5 — sticky completed_at).
type SavingsProgressService interface {
	// RecomputeGoal recomputes current_amount for a single goal according to its
	// progress_basis and updates last_calculated_at. Marks the goal completed
	// (sticky) when current_amount >= target_amount (Codex C5). No-op for manual basis.
	RecomputeGoal(ctx context.Context, goal *domain.SavingsGoal) (float64, error)

	// RecomputeGoalByID recomputes progress for a specific goal identified by
	// (userID, goalID). Unlike RecomputeUser, it also recomputes completed goals
	// (R3-2 / Codex C5 — completed_at stays sticky but current_amount still updates).
	RecomputeGoalByID(ctx context.Context, userID, goalID string) error

	// RecomputeUser recomputes all active goals for a user. Per-goal errors are
	// logged but do not abort processing of the remaining goals.
	RecomputeUser(ctx context.Context, userID string) error

	// GetProgressSummary returns basis-aware progress details for all active goals
	// plus portfolio totals. Designed for single-round-trip MCP/analytics consumption
	// (Codex C11 — consumers must not misread mixed-basis current_amount values).
	GetProgressSummary(ctx context.Context, userID string) (*domain.SavingsProgressSummary, error)
}

// savingsProgressService implements SavingsProgressService.
type savingsProgressService struct {
	goalRepo    SavingsProgressGoalRepo
	contribRepo SavingsProgressContribRepo
	accountRepo SavingsProgressAccountRepo
	// chainResolver maps a goal's stable user_account_id to the live account in its
	// re-link chain (Issue #1569). May be nil — resolution then degrades to the goal's
	// physical account_id, which is the pre-#1569 behaviour.
	chainResolver AccountChainResolver
	logger        *slog.Logger
}

// NewSavingsProgressService creates a new SavingsProgressService.
//
// chainResolver is a required constructor parameter rather than a setter on purpose
// (Issue #1569): this branch fixed a production defect where a chain-aware collaborator
// was constructed but never injected, so every row silently took the degraded path.
// Pass nil only in tests that deliberately exercise the no-resolver fallback.
func NewSavingsProgressService(
	goalRepo SavingsProgressGoalRepo,
	contribRepo SavingsProgressContribRepo,
	accountRepo SavingsProgressAccountRepo,
	chainResolver AccountChainResolver,
	logger *slog.Logger,
) SavingsProgressService {
	return &savingsProgressService{
		goalRepo:      goalRepo,
		contribRepo:   contribRepo,
		accountRepo:   accountRepo,
		chainResolver: chainResolver,
		logger:        logger,
	}
}

// RecomputeGoal recomputes the goal's current_amount according to its progress_basis,
// persists last_calculated_at (and balance_as_of for balance-based bases), then
// marks the goal completed when current_amount >= target_amount (Codex C5).
func (s *savingsProgressService) RecomputeGoal(ctx context.Context, goal *domain.SavingsGoal) (float64, error) {
	basis := goal.ProgressBasis
	if basis == "" {
		basis = domain.ProgressBasisManual
	}

	// Manual basis: the user owns current_amount — nothing to recompute.
	if basis == domain.ProgressBasisManual {
		return goal.CurrentAmount, nil
	}

	now := time.Now()
	var newAmount float64
	var balanceAsOf *time.Time

	switch basis {
	case domain.ProgressBasisContributions:
		sum, err := s.contribRepo.SumConfirmedByGoal(ctx, goal.UserID, goal.ID)
		if err != nil {
			return goal.CurrentAmount, fmt.Errorf("savings_progress: sum contributions for goal %s: %w", goal.ID, err)
		}
		newAmount = sum

	case domain.ProgressBasisBalance:
		account, err := s.resolveBasisAccount(ctx, goal.UserID, goal)
		if err != nil {
			return goal.CurrentAmount, err
		}

		// Issue #1569: a retained (non-active) account's current_balance is frozen at
		// disconnect and will never update again. Freeze current_amount rather than
		// recomputing from that frozen figure on every run — same treatment as the nil-balance
		// case below, and for the same reason (nothing new to report).
		//
		// ORDER MATTERS, and it is the opposite of what it was. Task 21 (item D) made a terminal
		// account carry NO balance at all, so such an account now ALSO satisfies the nil check —
		// and with the nil check first, every disconnected account was reported as merely "stale".
		// The outcome is identical either way (freeze, never zero); what was lost was the operator
		// signal that says WHY, and "stale" invites waiting for a sync that is never coming.
		// Checking the specific condition before the general one keeps the log honest.
		//
		// After resolveBasisAccount this fires only when the chain genuinely has no live
		// member (or the goal carries no chain reference at all). An ordinary re-link
		// resolves to the live row and lands below.
		if isAccountDisconnected(account) {
			s.logger.Warn("savings_progress: basis account is no longer active — skipping recompute",
				slog.String("goal_id", goal.ID),
				slog.String("account_id", account.ID),
				slog.String("account_status", string(account.Status)),
			)
			return goal.CurrentAmount, nil
		}

		// Codex C9: NULL/stale balance — leave prior value, do NOT zero.
		if account.CurrentBalance == nil {
			s.logger.Warn("savings_progress: account balance is nil — skipping recompute (stale)",
				slog.String("goal_id", goal.ID),
				slog.String("account_id", account.ID),
			)
			return goal.CurrentAmount, nil
		}

		newAmount = clampNonNegative(*account.CurrentBalance)
		balanceAt := account.UpdatedAt
		balanceAsOf = &balanceAt

	case domain.ProgressBasisAccountPercentage:
		if goal.AccountPercentage == nil {
			return goal.CurrentAmount, fmt.Errorf("savings_progress: goal %s has account_percentage basis but nil percentage", goal.ID)
		}
		account, err := s.resolveBasisAccount(ctx, goal.UserID, goal)
		if err != nil {
			return goal.CurrentAmount, err
		}

		// Issue #1569: see the identical check in the balance-basis branch above.
		if isAccountDisconnected(account) {
			s.logger.Warn("savings_progress: basis account is no longer active — skipping recompute",
				slog.String("goal_id", goal.ID),
				slog.String("account_id", account.ID),
				slog.String("account_status", string(account.Status)),
			)
			return goal.CurrentAmount, nil
		}

		// Codex C9: same null/stale handling.
		if account.CurrentBalance == nil {
			s.logger.Warn("savings_progress: account balance is nil — skipping recompute (stale)",
				slog.String("goal_id", goal.ID),
				slog.String("account_id", account.ID),
			)
			return goal.CurrentAmount, nil
		}

		newAmount = clampNonNegative(*account.CurrentBalance) * (*goal.AccountPercentage)
		balanceAt := account.UpdatedAt
		balanceAsOf = &balanceAt

	default:
		return goal.CurrentAmount, fmt.Errorf("savings_progress: unknown progress_basis %q for goal %s", basis, goal.ID)
	}

	// Round to cents (2dp) — deterministic to avoid floating-point drift.
	newAmount = roundToCents(newAmount)

	// ⚠ #1625: every return below reports THE AMOUNT THE ROW NOW HOLDS, not a success flag.
	// This function writes in two steps, and the second can fail after the first committed —
	// so "returned an error" and "changed nothing" are different states, and callers that
	// conflated them reported a figure the database did not have.

	// Persist updated current_amount and timestamps.
	if err := s.goalRepo.UpdateCurrentAmount(ctx, goal.ID, basis, newAmount, now, balanceAsOf); err != nil {
		// Nothing was written: the row still holds the amount it had on entry.
		return goal.CurrentAmount, fmt.Errorf("savings_progress: update current_amount for goal %s: %w", goal.ID, err)
	}

	// Mark completed (sticky — MarkCompleted only sets completed_at the first time, Codex C5).
	// Never un-complete based on a subsequent computed drop.
	if newAmount >= goal.TargetAmount {
		// ⚠ Deliberately does NOT mirror the completion onto the in-memory goal. A previous
		// attempt set IsCompleted/CompletedAt here, which was wrong twice over: MarkCompleted
		// stamps its OWN time.Now() (savings_goal_repository.go), so the mirrored timestamp
		// could never equal the row's; and the two writes here also touch last_calculated_at,
		// balance_as_of and updated_at, which no amount of field-by-field mirroring was going
		// to keep up with. Callers that need a consistent struct re-read the goal — see
		// reloadAfterRecompute in savings_goal_service.go.
		if err := s.goalRepo.MarkCompleted(ctx, goal.ID); err != nil {
			// The amount update ALREADY COMMITTED. Returning 0 here (as this did) made
			// CreateGoal/UpdateGoal keep the pre-recompute figure while the row held newAmount.
			// Reachable for the balance and account_percentage bases, whose computed value comes
			// straight off an account balance and can meet the target immediately, and for a
			// manual→contributions switch whose seeded contribution already meets it.
			return newAmount, fmt.Errorf("savings_progress: mark completed for goal %s: %w", goal.ID, err)
		}
	}

	return newAmount, nil
}

// RecomputeGoalByID recomputes progress for a specific goal by (userID, goalID).
// Unlike RecomputeUser, it includes completed goals so that contribution writes
// (log/confirm/delete) on a completed goal keep current_amount in sync while
// preserving the sticky completed_at (R3-2 / Codex C5).
func (s *savingsProgressService) RecomputeGoalByID(ctx context.Context, userID, goalID string) error {
	goal, err := s.goalRepo.GetByIDForUser(ctx, goalID, userID)
	if err != nil {
		return fmt.Errorf("savings_progress: load goal %s for recompute: %w", goalID, err)
	}
	_, err = s.RecomputeGoal(ctx, goal)
	return err
}

// RecomputeUser recomputes all active goals for a user. Per-goal errors are logged
// but do not stop processing of the remaining goals (best-effort, non-fatal).
func (s *savingsProgressService) RecomputeUser(ctx context.Context, userID string) error {
	goals, err := s.goalRepo.GetActiveByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("savings_progress: get active goals for user %s: %w", userID, err)
	}

	for _, goal := range goals {
		if _, err := s.RecomputeGoal(ctx, goal); err != nil {
			s.logger.Error("savings_progress: recompute goal failed (skipping)",
				slog.String("goal_id", goal.ID),
				slog.String("user_id", userID),
				slog.String("basis", goal.ProgressBasis),
				slog.String("error", err.Error()),
			)
			// Continue to the next goal.
		}
	}

	return nil
}

// applyBasisAccountFreshness overwrites item.Freshness / item.LinkedBalance from the goal's
// live basis account (Issue #1569).
//
// It distinguishes the two ways resolution can fail to produce an account, because they mean
// opposite things to the reader:
//
//   - no live member in the chain, or a retained physical row -> FreshnessBasisAccountDisconnected.
//     The number is frozen forever and the UI says so.
//   - an infrastructure failure (database blip, resolver error) -> FreshnessUnknown. We do NOT
//     know whether the account is live, so reporting the time-based `fresh` computed from
//     balance_as_of would assert something we did not check. The earlier version of this code
//     swallowed the error with `if err == nil`, which made a frozen goal claim freshness and
//     the disconnected badge disappear for the duration of the blip.
func (s *savingsProgressService) applyBasisAccountFreshness(
	ctx context.Context,
	requesterID string,
	goal *domain.SavingsGoal,
	item *domain.SavingsGoalProgressItem,
) {
	acct, err := s.resolveBasisAccount(ctx, requesterID, goal)
	if err != nil {
		s.logger.Warn("progress_summary: could not resolve basis account — reporting unknown freshness",
			slog.String("goal_id", goal.ID),
			slog.String("error", err.Error()),
		)
		item.Freshness = domain.FreshnessUnknown
		return
	}
	if acct == nil {
		return
	}

	// A disconnected basis account's balance will never update again — report that distinctly
	// from "still updating, just not recently" (FreshnessStale).
	if isAccountDisconnected(acct) {
		item.Freshness = domain.FreshnessBasisAccountDisconnected
	}
	if acct.CurrentBalance != nil {
		bal := clampNonNegative(*acct.CurrentBalance)
		item.LinkedBalance = &bal
	}
}

// resolveBasisAccount returns the account whose current_balance a balance-basis (or
// account_percentage-basis) goal must be computed from (Issue #1569).
//
// A goal stores two references to its account:
//   - AccountID     — the PHYSICAL accounts row it was created against. A re-link inserts a
//     new physical row and marks this one 'replaced', freezing its balance forever.
//   - UserAccountID — the STABLE chain identity shared by every row in the chain (#1109).
//
// When the goal carries a chain reference and a resolver is wired, this returns the LIVE
// member of the chain, so an ordinary re-link resumes recomputation instead of freezing.
//
// It deliberately does NOT decide whether to freeze. When the chain has no live member it
// falls through to the physical row, which the caller's isAccountDisconnected guard then
// freezes exactly as before — the freeze decision stays in one place.
//
// Degradation is honest, not silent-wrong: with no chain reference (UserAccountID nil or
// empty — real production rows are still like this, see #1569's main.go wiring defect) or no
// resolver, it reads the physical row, which is the pre-#1569 behaviour and still freezes when
// that row is terminal. It never substitutes a different chain's account.
func (s *savingsProgressService) resolveBasisAccount(ctx context.Context, requesterID string, goal *domain.SavingsGoal) (*domain.Account, error) {
	accountID := goal.AccountID

	if s.chainResolver != nil && goal.UserAccountID != nil && *goal.UserAccountID != "" {
		activeID, err := s.chainResolver.ResolveActiveID(ctx, *goal.UserAccountID, requesterID)
		switch {
		case err == nil:
			accountID = activeID
		case errors.Is(err, ErrNoActiveAccount):
			// The chain exists but every member is retained — the user disconnected and did
			// not re-link. Keep the physical row so the caller's guard freezes the goal.
			s.logger.Warn("savings_progress: no live account in chain — falling back to the retained row",
				slog.String("goal_id", goal.ID),
				slog.String("user_account_id", *goal.UserAccountID),
			)
		case errors.Is(err, ErrAmbiguousActiveAccount):
			// #1569 R11 T2: two live siblings share this user_account_id (an ordinary Plaid
			// shape — see ErrAmbiguousActiveAccount's doc comment). We cannot tell which one
			// the goal actually means, and guessing is exactly the bug being fixed. Follow the
			// SAME degradation philosophy as ErrNoActiveAccount above: keep the physical row so
			// the caller's isAccountDisconnected guard freezes the goal, rather than silently
			// recomputing from an arbitrarily-chosen sibling's balance.
			s.logger.Warn("savings_progress: chain has more than one live account — refusing to guess, falling back to the retained row",
				slog.String("goal_id", goal.ID),
				slog.String("user_account_id", *goal.UserAccountID),
			)
		case errors.Is(err, ErrEmptyUserAccountID):
			// Unreachable (the empty string is filtered above); degrade rather than error.
		default:
			// An infrastructure failure is NOT "no live member" — surface it instead of
			// freezing a healthy goal on a transient database error.
			return nil, fmt.Errorf("savings_progress: resolve live account for goal %s: %w", goal.ID, err)
		}
	}

	// Scope the read to the REQUESTER, never to goal.UserID. Passing the goal's own owner id
	// would make this ownership check compare the goal against itself, so it could never fail
	// closed — and this path sets item.LinkedBalance, a financial amount. The pre-#1569 code
	// passed the requester's id here; keeping that is not a refinement, it is preserving the
	// only ownership gate on the read.
	account, err := s.accountRepo.GetByIDAndUser(ctx, accountID, requesterID)
	if err != nil {
		return nil, fmt.Errorf("savings_progress: get account %s for goal %s: %w", accountID, goal.ID, err)
	}
	return account, nil
}

// isAccountDisconnected reports whether a balance-basis goal's linked account is no longer
// active — replaced, expired, or removed (Issue #1569). An empty Status is treated as active
// for backward compatibility with any pre-#1569 caller path that never populated the field
// (mirrors the frontend's isInactive() legacy-account handling in useActiveAccounts.ts).
func isAccountDisconnected(account *domain.Account) bool {
	switch account.Status {
	case domain.AccountStatusReplaced, domain.AccountStatusExpired, domain.AccountStatusRemoved:
		return true
	default:
		return false
	}
}

// clampNonNegative returns 0 if v < 0, otherwise v.
// Negative balances (overdrafts) are meaningless as savings progress.
func clampNonNegative(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

// roundToCents rounds a float to 2 decimal places.
func roundToCents(v float64) float64 {
	return math.Round(v*100) / 100
}

// GetProgressSummary returns basis-aware progress for all active goals + portfolio totals.
// All confirmed contributions are always fetched regardless of basis (Codex C11).
func (s *savingsProgressService) GetProgressSummary(ctx context.Context, userID string) (*domain.SavingsProgressSummary, error) {
	goals, err := s.goalRepo.GetActiveByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("progress_summary: get active goals: %w", err)
	}

	now := time.Now()
	const freshnessWindow = 24 * time.Hour

	// Issue #1569 Task 20 (A4). Counted with its OWN query, not by scanning `goals`: that slice
	// comes from GetActiveByUserID, whose WHERE clause is `is_completed = false`, so counting
	// IsCompleted within it could only ever produce 0 — which is exactly what this field reported
	// to every consumer, including the MCP/LLM one, for every user who has ever finished a goal.
	//
	// A failure here is PROPAGATED rather than defaulted to 0. Falling back to zero would restore
	// the original defect on the one path where nobody would notice, and a confidently wrong count
	// is worse for an LLM consumer than an error it can report.
	completedCount, err := s.goalRepo.CountCompletedByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("progress_summary: count completed goals: %w", err)
	}

	items := make([]domain.SavingsGoalProgressItem, 0, len(goals))
	var totalSaved, totalTarget float64

	for _, goal := range goals {
		// Always fetch confirmed contributions (Codex C11 — show even for non-contributions bases).
		confirmedTotal, err := s.contribRepo.SumConfirmedByGoal(ctx, goal.UserID, goal.ID)
		if err != nil {
			s.logger.Warn("progress_summary: failed to sum contributions",
				slog.String("goal_id", goal.ID),
				slog.String("error", err.Error()),
			)
			// Proceed with 0; don't abort the whole summary.
		}

		item := domain.SavingsGoalProgressItem{
			GoalID:                      goal.ID,
			GoalName:                    goal.Name,
			AccountID:                   goal.AccountID,
			CurrentAmount:               roundToCents(goal.CurrentAmount),
			TargetAmount:                roundToCents(goal.TargetAmount),
			ProgressBasis:               goal.ProgressBasis,
			ConfirmedContributionsTotal: roundToCents(confirmedTotal),
			CompletedAt:                 goal.CompletedAt,
			Deadline:                    goal.Deadline,
			DeadlineDisplay:             goal.DeadlineDisplay, // #1631 Batch F2 — carried from the already-populated goal
			AccountPercentage:           goal.AccountPercentage,
			BalanceAsOf:                 goal.BalanceAsOf,
		}

		// Percent complete: clamped 0–100.
		if goal.TargetAmount > 0 {
			pct := (goal.CurrentAmount / goal.TargetAmount) * 100
			if pct > 100 {
				pct = 100
			}
			item.PercentComplete = roundToCents(pct)
		}

		// ComputedFrom + Freshness + LinkedBalance based on basis.
		switch goal.ProgressBasis {
		case domain.ProgressBasisContributions:
			item.ComputedFrom = domain.ComputedFromConfirmedContribs
			item.Freshness = domain.FreshnessNotApplicable
		case domain.ProgressBasisBalance:
			item.ComputedFrom = domain.ComputedFromAccountBalance
			item.Freshness = s.computeFreshness(goal.BalanceAsOf, now, freshnessWindow)
			// Issue #1569: resolve through the re-link chain so the flag agrees with what
			// RecomputeGoal actually computed from — otherwise a re-linked goal would be
			// reported as disconnected while its amount is being updated normally.
			s.applyBasisAccountFreshness(ctx, userID, goal, &item)
		case domain.ProgressBasisAccountPercentage:
			item.ComputedFrom = domain.ComputedFromAccountPercentage
			item.Freshness = s.computeFreshness(goal.BalanceAsOf, now, freshnessWindow)
			// Issue #1569: see the identical chain resolution in the balance-basis branch above.
			s.applyBasisAccountFreshness(ctx, userID, goal, &item)
		default: // manual or empty
			item.ProgressBasis = domain.ProgressBasisManual // normalise empty string
			item.ComputedFrom = domain.ComputedFromManual
			item.Freshness = domain.FreshnessNotApplicable
		}

		items = append(items, item)
		totalSaved += goal.CurrentAmount
		totalTarget += goal.TargetAmount
	}

	var overallPct float64
	if totalTarget > 0 {
		overallPct = roundToCents(math.Min((totalSaved/totalTarget)*100, 100))
	}

	return &domain.SavingsProgressSummary{
		Goals:                  items,
		TotalSaved:             roundToCents(totalSaved),
		TotalTarget:            roundToCents(totalTarget),
		OverallPercentComplete: overallPct,
		GoalCount:              len(goals),
		CompletedGoalCount:     completedCount,
	}, nil
}

// computeFreshness determines data freshness for balance-based progress.
func (s *savingsProgressService) computeFreshness(balanceAsOf *time.Time, now time.Time, window time.Duration) string {
	if balanceAsOf == nil {
		return domain.FreshnessUnknown
	}
	if now.Sub(*balanceAsOf) <= window {
		return domain.FreshnessFresh
	}
	return domain.FreshnessStale
}
