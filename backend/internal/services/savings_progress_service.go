package services

import (
	"context"
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
	UpdateCurrentAmount(ctx context.Context, goalID string, currentAmount float64, lastCalculatedAt time.Time, balanceAsOf *time.Time) error
	// MarkCompleted sets is_completed and sticky completed_at (Codex C5).
	MarkCompleted(ctx context.Context, goalID string) error
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
	RecomputeGoal(ctx context.Context, goal *domain.SavingsGoal) error

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
	logger      *slog.Logger
}

// NewSavingsProgressService creates a new SavingsProgressService.
func NewSavingsProgressService(
	goalRepo SavingsProgressGoalRepo,
	contribRepo SavingsProgressContribRepo,
	accountRepo SavingsProgressAccountRepo,
	logger *slog.Logger,
) SavingsProgressService {
	return &savingsProgressService{
		goalRepo:    goalRepo,
		contribRepo: contribRepo,
		accountRepo: accountRepo,
		logger:      logger,
	}
}

// RecomputeGoal recomputes the goal's current_amount according to its progress_basis,
// persists last_calculated_at (and balance_as_of for balance-based bases), then
// marks the goal completed when current_amount >= target_amount (Codex C5).
func (s *savingsProgressService) RecomputeGoal(ctx context.Context, goal *domain.SavingsGoal) error {
	basis := goal.ProgressBasis
	if basis == "" {
		basis = domain.ProgressBasisManual
	}

	// Manual basis: the user owns current_amount — nothing to recompute.
	if basis == domain.ProgressBasisManual {
		return nil
	}

	now := time.Now()
	var newAmount float64
	var balanceAsOf *time.Time

	switch basis {
	case domain.ProgressBasisContributions:
		sum, err := s.contribRepo.SumConfirmedByGoal(ctx, goal.UserID, goal.ID)
		if err != nil {
			return fmt.Errorf("savings_progress: sum contributions for goal %s: %w", goal.ID, err)
		}
		newAmount = sum

	case domain.ProgressBasisBalance:
		account, err := s.accountRepo.GetByIDAndUser(ctx, goal.AccountID, goal.UserID)
		if err != nil {
			return fmt.Errorf("savings_progress: get account %s for goal %s: %w", goal.AccountID, goal.ID, err)
		}

		// Codex C9: NULL/stale balance — leave prior value, do NOT zero.
		if account.CurrentBalance == nil {
			s.logger.Warn("savings_progress: account balance is nil — skipping recompute (stale)",
				slog.String("goal_id", goal.ID),
				slog.String("account_id", goal.AccountID),
			)
			return nil
		}

		// Issue #1569: a retained (non-active) account's current_balance is frozen at
		// disconnect and will never update again. Freeze current_amount rather than
		// recomputing from that frozen figure on every run — same treatment as the nil-balance
		// case above, and for the same reason (nothing new to report).
		if isAccountDisconnected(account) {
			s.logger.Warn("savings_progress: basis account is no longer active — skipping recompute",
				slog.String("goal_id", goal.ID),
				slog.String("account_id", goal.AccountID),
				slog.String("account_status", string(account.Status)),
			)
			return nil
		}

		newAmount = clampNonNegative(*account.CurrentBalance)
		balanceAt := account.UpdatedAt
		balanceAsOf = &balanceAt

	case domain.ProgressBasisAccountPercentage:
		if goal.AccountPercentage == nil {
			return fmt.Errorf("savings_progress: goal %s has account_percentage basis but nil percentage", goal.ID)
		}
		account, err := s.accountRepo.GetByIDAndUser(ctx, goal.AccountID, goal.UserID)
		if err != nil {
			return fmt.Errorf("savings_progress: get account %s for goal %s: %w", goal.AccountID, goal.ID, err)
		}

		// Codex C9: same null/stale handling.
		if account.CurrentBalance == nil {
			s.logger.Warn("savings_progress: account balance is nil — skipping recompute (stale)",
				slog.String("goal_id", goal.ID),
				slog.String("account_id", goal.AccountID),
			)
			return nil
		}

		// Issue #1569: see the identical check in the balance-basis branch above.
		if isAccountDisconnected(account) {
			s.logger.Warn("savings_progress: basis account is no longer active — skipping recompute",
				slog.String("goal_id", goal.ID),
				slog.String("account_id", goal.AccountID),
				slog.String("account_status", string(account.Status)),
			)
			return nil
		}

		newAmount = clampNonNegative(*account.CurrentBalance) * (*goal.AccountPercentage)
		balanceAt := account.UpdatedAt
		balanceAsOf = &balanceAt

	default:
		return fmt.Errorf("savings_progress: unknown progress_basis %q for goal %s", basis, goal.ID)
	}

	// Round to cents (2dp) — deterministic to avoid floating-point drift.
	newAmount = roundToCents(newAmount)

	// Persist updated current_amount and timestamps.
	if err := s.goalRepo.UpdateCurrentAmount(ctx, goal.ID, newAmount, now, balanceAsOf); err != nil {
		return fmt.Errorf("savings_progress: update current_amount for goal %s: %w", goal.ID, err)
	}

	// Mark completed (sticky — MarkCompleted only sets completed_at the first time, Codex C5).
	// Never un-complete based on a subsequent computed drop.
	if newAmount >= goal.TargetAmount {
		if err := s.goalRepo.MarkCompleted(ctx, goal.ID); err != nil {
			return fmt.Errorf("savings_progress: mark completed for goal %s: %w", goal.ID, err)
		}
	}

	return nil
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
	return s.RecomputeGoal(ctx, goal)
}

// RecomputeUser recomputes all active goals for a user. Per-goal errors are logged
// but do not stop processing of the remaining goals (best-effort, non-fatal).
func (s *savingsProgressService) RecomputeUser(ctx context.Context, userID string) error {
	goals, err := s.goalRepo.GetActiveByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("savings_progress: get active goals for user %s: %w", userID, err)
	}

	for _, goal := range goals {
		if err := s.RecomputeGoal(ctx, goal); err != nil {
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

	items := make([]domain.SavingsGoalProgressItem, 0, len(goals))
	var totalSaved, totalTarget float64
	completedCount := 0

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
			if acct, err := s.accountRepo.GetByIDAndUser(ctx, goal.AccountID, userID); err == nil && acct != nil {
				// Issue #1569: a disconnected basis account's balance will never update again —
				// report that distinctly from "still updating, just not recently" (FreshnessStale).
				if isAccountDisconnected(acct) {
					item.Freshness = domain.FreshnessBasisAccountDisconnected
				}
				if acct.CurrentBalance != nil {
					bal := clampNonNegative(*acct.CurrentBalance)
					item.LinkedBalance = &bal
				}
			}
		case domain.ProgressBasisAccountPercentage:
			item.ComputedFrom = domain.ComputedFromAccountPercentage
			item.Freshness = s.computeFreshness(goal.BalanceAsOf, now, freshnessWindow)
			if acct, err := s.accountRepo.GetByIDAndUser(ctx, goal.AccountID, userID); err == nil && acct != nil {
				// Issue #1569: see the identical check in the balance-basis branch above.
				if isAccountDisconnected(acct) {
					item.Freshness = domain.FreshnessBasisAccountDisconnected
				}
				if acct.CurrentBalance != nil {
					bal := clampNonNegative(*acct.CurrentBalance)
					item.LinkedBalance = &bal
				}
			}
		default: // manual or empty
			item.ProgressBasis = domain.ProgressBasisManual // normalise empty string
			item.ComputedFrom = domain.ComputedFromManual
			item.Freshness = domain.FreshnessNotApplicable
		}

		items = append(items, item)
		totalSaved += goal.CurrentAmount
		totalTarget += goal.TargetAmount
		if goal.IsCompleted {
			completedCount++
		}
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
