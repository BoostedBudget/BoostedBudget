package services

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ─── mock implementations ────────────────────────────────────────────────────

type mockSavingsProgressGoalRepo struct{ mock.Mock }

func (m *mockSavingsProgressGoalRepo) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.SavingsGoal), args.Error(1)
}

func (m *mockSavingsProgressGoalRepo) GetByIDForUser(ctx context.Context, id, userID string) (*domain.SavingsGoal, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.SavingsGoal), args.Error(1)
}

func (m *mockSavingsProgressGoalRepo) UpdateCurrentAmount(ctx context.Context, goalID string, currentAmount float64, lastCalculatedAt time.Time, balanceAsOf *time.Time) error {
	return m.Called(ctx, goalID, currentAmount, lastCalculatedAt, balanceAsOf).Error(0)
}

func (m *mockSavingsProgressGoalRepo) MarkCompleted(ctx context.Context, goalID string) error {
	return m.Called(ctx, goalID).Error(0)
}

type mockSavingsProgressContribRepo struct{ mock.Mock }

func (m *mockSavingsProgressContribRepo) SumConfirmedByGoal(ctx context.Context, userID, goalID string) (float64, error) {
	args := m.Called(ctx, goalID)
	return args.Get(0).(float64), args.Error(1)
}

type mockSavingsProgressAccountRepo struct{ mock.Mock }

func (m *mockSavingsProgressAccountRepo) GetByIDAndUser(ctx context.Context, accountID, userID string) (*domain.Account, error) {
	args := m.Called(ctx, accountID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Account), args.Error(1)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func newTestProgressService(
	goalRepo *mockSavingsProgressGoalRepo,
	contribRepo *mockSavingsProgressContribRepo,
	accountRepo *mockSavingsProgressAccountRepo,
) SavingsProgressService {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewSavingsProgressService(goalRepo, contribRepo, accountRepo, logger)
}

func makeGoal(id, userID, accountID, basis string, target, current float64) *domain.SavingsGoal {
	return &domain.SavingsGoal{
		ID:            id,
		UserID:        userID,
		AccountID:     accountID,
		Name:          "Test Goal",
		TargetAmount:  target,
		CurrentAmount: current,
		ProgressBasis: basis,
		CurrencyCode:  "USD",
		IsCompleted:   false,
	}
}

func float64Ptr(v float64) *float64 { return &v }

// ─── manual basis ─────────────────────────────────────────────────────────────

// TestSavingsProgress_Manual_NoOp tests that manual basis is a no-op — no DB calls.
func TestSavingsProgress_Manual_NoOp(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisManual, 1000, 500)
	err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	goalRepo.AssertNotCalled(t, "UpdateCurrentAmount")
	goalRepo.AssertNotCalled(t, "MarkCompleted")
	contribRepo.AssertNotCalled(t, "SumConfirmedByGoal")
}

// TestSavingsProgress_Manual_EmptyBasis treats empty progress_basis as manual (backward compat).
func TestSavingsProgress_Manual_EmptyBasis(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", "", 1000, 500) // empty basis → manual
	err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	goalRepo.AssertNotCalled(t, "UpdateCurrentAmount")
}

// ─── contributions basis ──────────────────────────────────────────────────────

// TestSavingsProgress_Contributions_UpdatesAmount tests that contributions basis
// sums confirmed contributions and stores the result.
func TestSavingsProgress_Contributions_UpdatesAmount(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisContributions, 1000, 0)

	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(750.00, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", 750.00, mock.Anything, (*time.Time)(nil)).Return(nil)

	err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	contribRepo.AssertExpectations(t)
	goalRepo.AssertExpectations(t)
	// Not completed — 750 < 1000
	goalRepo.AssertNotCalled(t, "MarkCompleted")
}

// TestSavingsProgress_Contributions_MarksCompleted tests sticky completion
// when contributions reach target (Codex C5).
func TestSavingsProgress_Contributions_MarksCompleted(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisContributions, 1000, 0)

	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(1000.00, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", 1000.00, mock.Anything, (*time.Time)(nil)).Return(nil)
	goalRepo.On("MarkCompleted", mock.Anything, "g1").Return(nil)

	err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	goalRepo.AssertCalled(t, "MarkCompleted", mock.Anything, "g1")
}

// TestSavingsProgress_Contributions_ExceedsTarget_StillMarksCompleted tests
// over-target (e.g. 1200/1000) still marks completion.
func TestSavingsProgress_Contributions_ExceedsTarget_StillMarksCompleted(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisContributions, 1000, 0)

	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(1200.00, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", 1200.00, mock.Anything, (*time.Time)(nil)).Return(nil)
	goalRepo.On("MarkCompleted", mock.Anything, "g1").Return(nil)

	err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	goalRepo.AssertCalled(t, "MarkCompleted", mock.Anything, "g1")
}

// TestSavingsProgress_Contributions_RoundsToCents tests deterministic 2dp rounding.
func TestSavingsProgress_Contributions_RoundsToCents(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisContributions, 1000, 0)

	// 333.333... should round to 333.33
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(333.3333333, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", 333.33, mock.Anything, (*time.Time)(nil)).Return(nil)

	err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	goalRepo.AssertExpectations(t)
}

// ─── balance basis ────────────────────────────────────────────────────────────

// TestSavingsProgress_Balance_MirrorsAccountBalance tests the balance basis.
func TestSavingsProgress_Balance_MirrorsAccountBalance(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisBalance, 5000, 0)
	balance := 3500.00
	account := &domain.Account{
		ID:             "acc1",
		UserID:         "u1",
		CurrentBalance: &balance,
		UpdatedAt:      time.Now(),
	}

	accountRepo.On("GetByIDAndUser", mock.Anything, "acc1", "u1").Return(account, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", 3500.00, mock.Anything, mock.Anything).Return(nil)

	err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	accountRepo.AssertExpectations(t)
	goalRepo.AssertExpectations(t)
}

// TestSavingsProgress_Balance_ClampsNegative tests that negative balances are clamped to 0
// (overdraft does not corrupt goal progress — Codex C9).
func TestSavingsProgress_Balance_ClampsNegative(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisBalance, 5000, 1000)
	balance := -200.00
	account := &domain.Account{ID: "acc1", CurrentBalance: &balance, UpdatedAt: time.Now()}

	accountRepo.On("GetByIDAndUser", mock.Anything, "acc1", "u1").Return(account, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", 0.00, mock.Anything, mock.Anything).Return(nil)

	err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	goalRepo.AssertExpectations(t)
}

// TestSavingsProgress_Balance_NilBalance_SkipsRecompute tests that a nil balance
// leaves the prior value unchanged and does NOT zero the goal (Codex C9 stale handling).
func TestSavingsProgress_Balance_NilBalance_SkipsRecompute(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisBalance, 5000, 1000)
	account := &domain.Account{ID: "acc1", CurrentBalance: nil, UpdatedAt: time.Now()}

	accountRepo.On("GetByIDAndUser", mock.Anything, "acc1", "u1").Return(account, nil)

	err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	// Neither UpdateCurrentAmount nor MarkCompleted should be called.
	goalRepo.AssertNotCalled(t, "UpdateCurrentAmount")
	goalRepo.AssertNotCalled(t, "MarkCompleted")
}

// TestSavingsProgress_Balance_SetsBalanceAsOf tests that balance_as_of is set to
// account.UpdatedAt when basis reads the account balance.
func TestSavingsProgress_Balance_SetsBalanceAsOf(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisBalance, 5000, 0)
	syncedAt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	balance := 2000.00
	account := &domain.Account{ID: "acc1", CurrentBalance: &balance, UpdatedAt: syncedAt}

	accountRepo.On("GetByIDAndUser", mock.Anything, "acc1", "u1").Return(account, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", 2000.00, mock.Anything,
		mock.MatchedBy(func(bao *time.Time) bool {
			return bao != nil && bao.Equal(syncedAt)
		}),
	).Return(nil)

	err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	goalRepo.AssertExpectations(t)
}

// ─── account_percentage basis ─────────────────────────────────────────────────

// TestSavingsProgress_AccountPercentage_ComputesCorrectly tests the percent-of-balance basis.
func TestSavingsProgress_AccountPercentage_ComputesCorrectly(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	pct := 0.4 // 40%
	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisAccountPercentage, 2000, 0)
	goal.AccountPercentage = &pct
	balance := 3000.00
	account := &domain.Account{ID: "acc1", CurrentBalance: &balance, UpdatedAt: time.Now()}

	accountRepo.On("GetByIDAndUser", mock.Anything, "acc1", "u1").Return(account, nil)
	// 3000 * 0.4 = 1200.00
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", 1200.00, mock.Anything, mock.Anything).Return(nil)

	err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	goalRepo.AssertExpectations(t)
}

// TestSavingsProgress_AccountPercentage_NilBalance_SkipsRecompute tests stale-balance
// handling for account_percentage basis (same as balance basis — Codex C9).
func TestSavingsProgress_AccountPercentage_NilBalance_SkipsRecompute(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	pct := 0.5
	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisAccountPercentage, 2000, 800)
	goal.AccountPercentage = &pct
	account := &domain.Account{ID: "acc1", CurrentBalance: nil, UpdatedAt: time.Now()}

	accountRepo.On("GetByIDAndUser", mock.Anything, "acc1", "u1").Return(account, nil)

	err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	goalRepo.AssertNotCalled(t, "UpdateCurrentAmount")
}

// TestSavingsProgress_AccountPercentage_NilPercentage_ReturnsError tests that
// a nil AccountPercentage with account_percentage basis returns an error (defensive guard).
func TestSavingsProgress_AccountPercentage_NilPercentage_ReturnsError(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisAccountPercentage, 2000, 0)
	goal.AccountPercentage = nil // should not happen after validation, but guard it

	err := svc.RecomputeGoal(context.Background(), goal)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil percentage")
}

// ─── sticky completion (Codex C5) ────────────────────────────────────────────

// TestSavingsProgress_StickyCompletion_NotClearedOnDrop tests that a completed goal
// that drops below target is NOT un-completed (Codex C5 — sticky completed_at).
// The repo's MarkCompleted uses CASE WHEN completed_at IS NULL to enforce stickiness.
// The service does not call MarkCompleted again if already completed — but completion
// once set is guarded at the DB layer.
func TestSavingsProgress_StickyCompletion_DoesNotCallMarkCompletedTwice(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisContributions, 1000, 0)

	// First call: reaches target.
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(1000.00, nil).Once()
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", 1000.00, mock.Anything, (*time.Time)(nil)).Return(nil).Once()
	goalRepo.On("MarkCompleted", mock.Anything, "g1").Return(nil).Once()

	require.NoError(t, svc.RecomputeGoal(context.Background(), goal))
	goalRepo.AssertCalled(t, "MarkCompleted", mock.Anything, "g1")

	// Second call: drops below target (balance_regression simulation).
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(400.00, nil).Once()
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", 400.00, mock.Anything, (*time.Time)(nil)).Return(nil).Once()

	require.NoError(t, svc.RecomputeGoal(context.Background(), goal))
	// MarkCompleted should NOT be called again (400 < 1000).
	goalRepo.AssertNumberOfCalls(t, "MarkCompleted", 1)
}

// ─── RecomputeUser ────────────────────────────────────────────────────────────

// TestSavingsProgress_RecomputeUser_ProcessesAllGoals tests that all active goals
// are recomputed (manual goals are skipped, non-manual are updated).
func TestSavingsProgress_RecomputeUser_ProcessesAllGoals(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	g1 := makeGoal("g1", "u1", "acc1", domain.ProgressBasisManual, 1000, 500)
	g2 := makeGoal("g2", "u1", "acc1", domain.ProgressBasisContributions, 2000, 0)

	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{g1, g2}, nil)
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g2").Return(800.00, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g2", 800.00, mock.Anything, (*time.Time)(nil)).Return(nil)

	err := svc.RecomputeUser(context.Background(), "u1")

	require.NoError(t, err)
	goalRepo.AssertExpectations(t)
	contribRepo.AssertExpectations(t)
	// g1 is manual — no contrib call, no update call for it.
	contribRepo.AssertNumberOfCalls(t, "SumConfirmedByGoal", 1)
}

// TestSavingsProgress_RecomputeUser_ContinuesOnGoalError tests that a per-goal error
// does NOT abort processing of remaining goals (best-effort, non-fatal).
func TestSavingsProgress_RecomputeUser_ContinuesOnGoalError(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	g1 := makeGoal("g1", "u1", "acc1", domain.ProgressBasisContributions, 1000, 0)
	g2 := makeGoal("g2", "u1", "acc1", domain.ProgressBasisContributions, 2000, 0)

	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{g1, g2}, nil)
	// g1 fails
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(0.0, errors.New("db error")).Once()
	// g2 succeeds
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g2").Return(500.00, nil).Once()
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g2", 500.00, mock.Anything, (*time.Time)(nil)).Return(nil)

	err := svc.RecomputeUser(context.Background(), "u1")

	// RecomputeUser itself succeeds (g1 error is logged, not returned).
	require.NoError(t, err)
	goalRepo.AssertExpectations(t)
	contribRepo.AssertExpectations(t)
}

// TestSavingsProgress_RecomputeUser_GetActiveGoalsError tests propagation when
// listing goals fails.
func TestSavingsProgress_RecomputeUser_GetActiveGoalsError(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return(nil, errors.New("db error"))

	err := svc.RecomputeUser(context.Background(), "u1")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "get active goals")
}

// =============================================================================
// GetProgressSummary tests (Issue #1300 Codex C11)
// =============================================================================

func TestSavingsProgressService_GetProgressSummary(t *testing.T) {
	t.Run("returns empty summary when no goals", func(t *testing.T) {
		goalRepo := &mockSavingsProgressGoalRepo{}
		contribRepo := &mockSavingsProgressContribRepo{}
		accountRepo := &mockSavingsProgressAccountRepo{}
		svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

		goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{}, nil)

		summary, err := svc.GetProgressSummary(context.Background(), "u1")

		require.NoError(t, err)
		assert.Empty(t, summary.Goals)
		assert.Equal(t, 0.0, summary.TotalSaved)
		assert.Equal(t, 0.0, summary.TotalTarget)
		assert.Equal(t, 0.0, summary.OverallPercentComplete)
		assert.Equal(t, 0, summary.GoalCount)
		goalRepo.AssertExpectations(t)
	})

	t.Run("contributions basis: correct computed_from + freshness not_applicable", func(t *testing.T) {
		goalRepo := &mockSavingsProgressGoalRepo{}
		contribRepo := &mockSavingsProgressContribRepo{}
		accountRepo := &mockSavingsProgressAccountRepo{}
		svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

		goal := &domain.SavingsGoal{
			ID: "g1", UserID: "u1", Name: "Vaca",
			CurrentAmount: 250.0, TargetAmount: 1000.0,
			ProgressBasis: domain.ProgressBasisContributions, AccountID: "acct-1",
		}

		goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{goal}, nil)
		contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(250.0, nil)

		summary, err := svc.GetProgressSummary(context.Background(), "u1")

		require.NoError(t, err)
		require.Len(t, summary.Goals, 1)
		item := summary.Goals[0]
		assert.Equal(t, domain.ComputedFromConfirmedContribs, item.ComputedFrom)
		assert.Equal(t, domain.FreshnessNotApplicable, item.Freshness)
		assert.Equal(t, 250.0, item.ConfirmedContributionsTotal)
		assert.Equal(t, 25.0, item.PercentComplete) // 250/1000*100
		assert.Nil(t, item.LinkedBalance)
		goalRepo.AssertExpectations(t)
		contribRepo.AssertExpectations(t)
	})

	t.Run("balance basis: linked_balance + freshness fresh when recent", func(t *testing.T) {
		goalRepo := &mockSavingsProgressGoalRepo{}
		contribRepo := &mockSavingsProgressContribRepo{}
		accountRepo := &mockSavingsProgressAccountRepo{}
		svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

		recentBalance := time.Now().Add(-1 * time.Hour) // 1h ago → fresh
		bal := 800.0
		goal := &domain.SavingsGoal{
			ID: "g2", UserID: "u1", Name: "Emergency",
			CurrentAmount: 800.0, TargetAmount: 2000.0,
			ProgressBasis: domain.ProgressBasisBalance, AccountID: "acct-2",
			BalanceAsOf: &recentBalance,
		}
		account := &domain.Account{ID: "acct-2", UserID: "u1", CurrentBalance: &bal}

		goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{goal}, nil)
		contribRepo.On("SumConfirmedByGoal", mock.Anything, "g2").Return(0.0, nil)
		accountRepo.On("GetByIDAndUser", mock.Anything, "acct-2", "u1").Return(account, nil)

		summary, err := svc.GetProgressSummary(context.Background(), "u1")

		require.NoError(t, err)
		require.Len(t, summary.Goals, 1)
		item := summary.Goals[0]
		assert.Equal(t, domain.ComputedFromAccountBalance, item.ComputedFrom)
		assert.Equal(t, domain.FreshnessFresh, item.Freshness)
		assert.NotNil(t, item.LinkedBalance)
		assert.Equal(t, 800.0, *item.LinkedBalance)
		assert.Equal(t, 40.0, item.PercentComplete) // 800/2000*100
		goalRepo.AssertExpectations(t)
		accountRepo.AssertExpectations(t)
	})

	t.Run("balance basis: freshness stale when balance_as_of older than 24h", func(t *testing.T) {
		goalRepo := &mockSavingsProgressGoalRepo{}
		contribRepo := &mockSavingsProgressContribRepo{}
		accountRepo := &mockSavingsProgressAccountRepo{}
		svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

		staleBalance := time.Now().Add(-48 * time.Hour) // 2 days ago → stale
		bal := 500.0
		goal := &domain.SavingsGoal{
			ID: "g3", UserID: "u1", Name: "Car",
			CurrentAmount: 500.0, TargetAmount: 5000.0,
			ProgressBasis: domain.ProgressBasisBalance, AccountID: "acct-3",
			BalanceAsOf: &staleBalance,
		}
		account := &domain.Account{ID: "acct-3", UserID: "u1", CurrentBalance: &bal}

		goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{goal}, nil)
		contribRepo.On("SumConfirmedByGoal", mock.Anything, "g3").Return(0.0, nil)
		accountRepo.On("GetByIDAndUser", mock.Anything, "acct-3", "u1").Return(account, nil)

		summary, err := svc.GetProgressSummary(context.Background(), "u1")

		require.NoError(t, err)
		assert.Equal(t, domain.FreshnessStale, summary.Goals[0].Freshness)
		goalRepo.AssertExpectations(t)
	})

	t.Run("balance basis: freshness unknown when balance_as_of nil", func(t *testing.T) {
		goalRepo := &mockSavingsProgressGoalRepo{}
		contribRepo := &mockSavingsProgressContribRepo{}
		accountRepo := &mockSavingsProgressAccountRepo{}
		svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

		bal := 300.0
		goal := &domain.SavingsGoal{
			ID: "g4", UserID: "u1", Name: "Laptop",
			CurrentAmount: 300.0, TargetAmount: 1500.0,
			ProgressBasis: domain.ProgressBasisBalance, AccountID: "acct-4",
			BalanceAsOf: nil, // never set → unknown
		}
		account := &domain.Account{ID: "acct-4", UserID: "u1", CurrentBalance: &bal}

		goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{goal}, nil)
		contribRepo.On("SumConfirmedByGoal", mock.Anything, "g4").Return(0.0, nil)
		accountRepo.On("GetByIDAndUser", mock.Anything, "acct-4", "u1").Return(account, nil)

		summary, err := svc.GetProgressSummary(context.Background(), "u1")

		require.NoError(t, err)
		assert.Equal(t, domain.FreshnessUnknown, summary.Goals[0].Freshness)
	})

	t.Run("portfolio totals computed correctly across multiple goals", func(t *testing.T) {
		goalRepo := &mockSavingsProgressGoalRepo{}
		contribRepo := &mockSavingsProgressContribRepo{}
		accountRepo := &mockSavingsProgressAccountRepo{}
		svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

		goals := []*domain.SavingsGoal{
			{ID: "ga", UserID: "u1", Name: "A", CurrentAmount: 500.0, TargetAmount: 1000.0, ProgressBasis: domain.ProgressBasisContributions, AccountID: "a"},
			{ID: "gb", UserID: "u1", Name: "B", CurrentAmount: 200.0, TargetAmount: 500.0, ProgressBasis: domain.ProgressBasisContributions, AccountID: "b"},
		}

		goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return(goals, nil)
		contribRepo.On("SumConfirmedByGoal", mock.Anything, "ga").Return(500.0, nil)
		contribRepo.On("SumConfirmedByGoal", mock.Anything, "gb").Return(200.0, nil)

		summary, err := svc.GetProgressSummary(context.Background(), "u1")

		require.NoError(t, err)
		assert.Equal(t, 700.0, summary.TotalSaved)  // 500 + 200
		assert.Equal(t, 1500.0, summary.TotalTarget) // 1000 + 500
		// 700/1500*100 = 46.67
		assert.Equal(t, 46.67, summary.OverallPercentComplete)
		assert.Equal(t, 2, summary.GoalCount)
		assert.Equal(t, 0, summary.CompletedGoalCount)
	})
}

// ─── unit tests for helpers ───────────────────────────────────────────────────

func TestClampNonNegative(t *testing.T) {
	assert.Equal(t, 0.0, clampNonNegative(-100.0))
	assert.Equal(t, 0.0, clampNonNegative(0.0))
	assert.Equal(t, 100.0, clampNonNegative(100.0))
}

func TestRoundToCents(t *testing.T) {
	assert.Equal(t, 1.23, roundToCents(1.234))
	assert.Equal(t, 1.24, roundToCents(1.235))
	assert.Equal(t, 0.0, roundToCents(0.0))
	assert.Equal(t, 333.33, roundToCents(333.3333333))
}
