package services

import (
	"bytes"
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

func (m *mockSavingsProgressGoalRepo) UpdateCurrentAmount(ctx context.Context, goalID, basis string, currentAmount float64, lastCalculatedAt time.Time, balanceAsOf *time.Time) error {
	return m.Called(ctx, goalID, basis, currentAmount, lastCalculatedAt, balanceAsOf).Error(0)
}

func (m *mockSavingsProgressGoalRepo) MarkCompleted(ctx context.Context, goalID string) error {
	return m.Called(ctx, goalID).Error(0)
}

// Issue #1569 Task 20 (A4). Driven by testify like the rest of this mock, so a test that forgets
// to set an expectation FAILS rather than silently getting 0 — which is the exact value the
// defect produced, and would make a regression indistinguishable from a passing fix.
func (m *mockSavingsProgressGoalRepo) CountCompletedByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
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
	return NewSavingsProgressService(goalRepo, contribRepo, accountRepo, nil, logger)
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

// ─── manual basis ─────────────────────────────────────────────────────────────

// TestSavingsProgress_Manual_NoOp tests that manual basis is a no-op — no DB calls.
func TestSavingsProgress_Manual_NoOp(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisManual, 1000, 500)
	_, err := svc.RecomputeGoal(context.Background(), goal)

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
	_, err := svc.RecomputeGoal(context.Background(), goal)

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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 750.00, mock.Anything, (*time.Time)(nil)).Return(nil)

	_, err := svc.RecomputeGoal(context.Background(), goal)

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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 1000.00, mock.Anything, (*time.Time)(nil)).Return(nil)
	goalRepo.On("MarkCompleted", mock.Anything, "g1").Return(nil)

	_, err := svc.RecomputeGoal(context.Background(), goal)

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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 1200.00, mock.Anything, (*time.Time)(nil)).Return(nil)
	goalRepo.On("MarkCompleted", mock.Anything, "g1").Return(nil)

	_, err := svc.RecomputeGoal(context.Background(), goal)

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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 333.33, mock.Anything, (*time.Time)(nil)).Return(nil)

	_, err := svc.RecomputeGoal(context.Background(), goal)

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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 3500.00, mock.Anything, mock.Anything).Return(nil)

	_, err := svc.RecomputeGoal(context.Background(), goal)

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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 0.00, mock.Anything, mock.Anything).Return(nil)

	_, err := svc.RecomputeGoal(context.Background(), goal)

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

	_, err := svc.RecomputeGoal(context.Background(), goal)

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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 2000.00, mock.Anything,
		mock.MatchedBy(func(bao *time.Time) bool {
			return bao != nil && bao.Equal(syncedAt)
		}),
	).Return(nil)

	_, err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	goalRepo.AssertExpectations(t)
}

// TestSavingsProgress_Balance_DisconnectedAccount_SkipsRecompute tests that a balance-basis
// goal pointed at a retained (non-active) account freezes current_amount rather than
// recomputing from a frozen balance forever (Issue #1569).
func TestSavingsProgress_Balance_DisconnectedAccount_SkipsRecompute(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisBalance, 5000, 1000)
	balance := 3500.00
	account := &domain.Account{
		ID:             "acc1",
		UserID:         "u1",
		CurrentBalance: &balance,
		Status:         domain.AccountStatusRemoved,
		UpdatedAt:      time.Now(),
	}

	accountRepo.On("GetByIDAndUser", mock.Anything, "acc1", "u1").Return(account, nil)
	// Registered as .Maybe() so a regression records the write instead of panicking on an
	// unexpected mock call, letting assertNoCallTo below fail as a real assertion.
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	goalRepo.On("MarkCompleted", mock.Anything, mock.Anything).Return(nil).Maybe()

	_, err := svc.RecomputeGoal(context.Background(), goal)

	require.NoError(t, err)
	// Neither UpdateCurrentAmount nor MarkCompleted should be called — same freeze semantics
	// as the nil-balance case above.
	assertNoCallTo(t, &goalRepo.Mock, "UpdateCurrentAmount")
	assertNoCallTo(t, &goalRepo.Mock, "MarkCompleted")
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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 1200.00, mock.Anything, mock.Anything).Return(nil)

	_, err := svc.RecomputeGoal(context.Background(), goal)

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

	_, err := svc.RecomputeGoal(context.Background(), goal)

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

	_, err := svc.RecomputeGoal(context.Background(), goal)

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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 1000.00, mock.Anything, (*time.Time)(nil)).Return(nil).Once()
	goalRepo.On("MarkCompleted", mock.Anything, "g1").Return(nil).Once()

	_, recomputeErr := svc.RecomputeGoal(context.Background(), goal)
	require.NoError(t, recomputeErr)
	goalRepo.AssertCalled(t, "MarkCompleted", mock.Anything, "g1")

	// Second call: drops below target (balance_regression simulation).
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(400.00, nil).Once()
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 400.00, mock.Anything, (*time.Time)(nil)).Return(nil).Once()

	_, recomputeErr = svc.RecomputeGoal(context.Background(), goal)
	require.NoError(t, recomputeErr)
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
	// #1569 A4: CompletedGoalCount is now counted outside the active universe.
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g2").Return(800.00, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g2", mock.Anything, 800.00, mock.Anything, (*time.Time)(nil)).Return(nil)

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
	// #1569 A4: CompletedGoalCount is now counted outside the active universe.
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
	// g1 fails
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(0.0, errors.New("db error")).Once()
	// g2 succeeds
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g2").Return(500.00, nil).Once()
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g2", mock.Anything, 500.00, mock.Anything, (*time.Time)(nil)).Return(nil)

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
	// #1569 A4: CompletedGoalCount is now counted outside the active universe.
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()

	err := svc.RecomputeUser(context.Background(), "u1")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "get active goals")
}

// ─── RecomputeGoalByID — R4-T2 ────────────────────────────────────────────────

// TestSavingsProgress_RecomputeGoalByID_CompletedGoal_UpdatesAmountKeepsCompletedAt verifies
// that RecomputeGoalByID (R3-2) recomputes current_amount on a goal that is ALREADY
// completed, while leaving completed_at sticky (Codex C5).
//
// Context: RecomputeUser loads goals via GetActiveByUserID which excludes completed goals.
// R3-2 introduced RecomputeGoalByID to use GetByIDForUser so that writes (log/confirm/
// delete) on completed goals still update current_amount. The completion itself is
// handled at the DB layer (MarkCompleted uses CASE WHEN completed_at IS NULL so it is
// idempotent) and the service calls MarkCompleted whenever amount >= target regardless
// of prior completion state.
//
// RED: would fail if the handler still called RecomputeUser (completed goal excluded).
// GREEN: passes because RecomputeGoalByID → GetByIDForUser → RecomputeGoal updates amount.
func TestSavingsProgress_RecomputeGoalByID_CompletedGoal_UpdatesAmountKeepsCompletedAt(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

	// Build a completed contributions-basis goal (current == target → was completed).
	completedGoal := makeGoal("g-done", "u1", "acc1", domain.ProgressBasisContributions, 1000, 1000)
	completedGoal.IsCompleted = true
	completedAt := time.Now().Add(-24 * time.Hour) // completed yesterday
	completedGoal.CompletedAt = &completedAt

	// GetByIDForUser must be called (not GetActiveByUserID) — that's the R3-2 fix.
	goalRepo.On("GetByIDForUser", mock.Anything, "g-done", "u1").Return(completedGoal, nil)

	// Suppose a new confirmed contribution pushes amount to 1200.
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g-done").Return(1200.00, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g-done", mock.Anything, 1200.00, mock.Anything, (*time.Time)(nil)).Return(nil)
	// MarkCompleted is called because 1200 >= 1000; DB layer (CASE WHEN completed_at IS NULL)
	// keeps completed_at sticky — this validates the service's sticky-completion call chain.
	goalRepo.On("MarkCompleted", mock.Anything, "g-done").Return(nil)

	err := svc.RecomputeGoalByID(context.Background(), "u1", "g-done")

	require.NoError(t, err)
	// Amount was updated.
	goalRepo.AssertCalled(t, "UpdateCurrentAmount", mock.Anything, "g-done", mock.Anything, 1200.00, mock.Anything, (*time.Time)(nil))
	// MarkCompleted was called (amount >= target); DB stickiness preserves the original completed_at.
	goalRepo.AssertCalled(t, "MarkCompleted", mock.Anything, "g-done")
	// GetByIDForUser was used, not GetActiveByUserID (which would exclude the completed goal).
	goalRepo.AssertCalled(t, "GetByIDForUser", mock.Anything, "g-done", "u1")
	goalRepo.AssertNotCalled(t, "GetActiveByUserID")
	contribRepo.AssertExpectations(t)
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
		// #1569 A4: CompletedGoalCount is now counted outside the active universe.
		goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()

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
		// #1569 A4: CompletedGoalCount is now counted outside the active universe.
		goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
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
		// #1569 A4: CompletedGoalCount is now counted outside the active universe.
		goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
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
		// #1569 A4: CompletedGoalCount is now counted outside the active universe.
		goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
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
		// #1569 A4: CompletedGoalCount is now counted outside the active universe.
		goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
		contribRepo.On("SumConfirmedByGoal", mock.Anything, "g4").Return(0.0, nil)
		accountRepo.On("GetByIDAndUser", mock.Anything, "acct-4", "u1").Return(account, nil)

		summary, err := svc.GetProgressSummary(context.Background(), "u1")

		require.NoError(t, err)
		assert.Equal(t, domain.FreshnessUnknown, summary.Goals[0].Freshness)
	})

	t.Run("balance basis: freshness basis_account_disconnected when linked account is retained", func(t *testing.T) {
		// Issue #1569: the account exists (soft-removed, not deleted) but is no longer active —
		// its current_balance will never update again. This must be surfaced distinctly from
		// FreshnessStale, which means "still updating, just not recently."
		goalRepo := &mockSavingsProgressGoalRepo{}
		contribRepo := &mockSavingsProgressContribRepo{}
		accountRepo := &mockSavingsProgressAccountRepo{}
		svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

		recentBalance := time.Now().Add(-1 * time.Hour)
		bal := 800.0
		goal := &domain.SavingsGoal{
			ID: "g5", UserID: "u1", Name: "Disconnected Goal",
			CurrentAmount: 800.0, TargetAmount: 2000.0,
			ProgressBasis: domain.ProgressBasisBalance, AccountID: "acct-5",
			BalanceAsOf: &recentBalance,
		}
		account := &domain.Account{
			ID: "acct-5", UserID: "u1", CurrentBalance: &bal,
			Status: domain.AccountStatusRemoved,
		}

		goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{goal}, nil)
		// #1569 A4: CompletedGoalCount is now counted outside the active universe.
		goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
		contribRepo.On("SumConfirmedByGoal", mock.Anything, "g5").Return(0.0, nil)
		accountRepo.On("GetByIDAndUser", mock.Anything, "acct-5", "u1").Return(account, nil)

		summary, err := svc.GetProgressSummary(context.Background(), "u1")

		require.NoError(t, err)
		require.Len(t, summary.Goals, 1)
		assert.Equal(t, domain.FreshnessBasisAccountDisconnected, summary.Goals[0].Freshness)
		goalRepo.AssertExpectations(t)
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
		// #1569 A4: CompletedGoalCount is now counted outside the active universe.
		goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
		contribRepo.On("SumConfirmedByGoal", mock.Anything, "ga").Return(500.0, nil)
		contribRepo.On("SumConfirmedByGoal", mock.Anything, "gb").Return(200.0, nil)

		summary, err := svc.GetProgressSummary(context.Background(), "u1")

		require.NoError(t, err)
		assert.Equal(t, 700.0, summary.TotalSaved)   // 500 + 200
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

// TestSavingsProgress_TerminalAccountWithNoBalance_FreezesOnBothBases is the shape the repository
// now ACTUALLY returns, on both bases.
//
// The two tests above hand-build a `domain.Account{Status: removed, CurrentBalance: &bal}`. After
// #1569 task 21 (item D) no repository read can produce that any more: a terminal account carries
// NO balance, so the real object is terminal AND nil. Those tests still pass and are still worth
// keeping — they pin that the STATUS alone is sufficient to freeze, independently of the balance —
// but neither of them exercises the object the code now actually receives, and a test suite whose
// every fixture is unreachable is a suite that has stopped watching.
//
// What must hold, and does: freeze, never zero, on both bases. The nil path and the terminal path
// agree, which is why item D reinforces task 13's freeze rather than colliding with it. The order
// of the two checks decides only which warning is logged, and is asserted for by
// TestSavingsProgress_TerminalAccountWithNoBalance_LogsTheDisconnection below.
func TestSavingsProgress_TerminalAccountWithNoBalance_FreezesOnBothBases(t *testing.T) {
	for _, tc := range []struct {
		name  string
		basis string
		goal  func() *domain.SavingsGoal
	}{
		{
			name:  "balance basis",
			basis: domain.ProgressBasisBalance,
			goal: func() *domain.SavingsGoal {
				return makeGoal("g1", "u1", "acc1", domain.ProgressBasisBalance, 5000, 1000)
			},
		},
		{
			name:  "account_percentage basis",
			basis: domain.ProgressBasisAccountPercentage,
			goal: func() *domain.SavingsGoal {
				g := makeGoal("g1", "u1", "acc1", domain.ProgressBasisAccountPercentage, 5000, 1000)
				pct := 0.5
				g.AccountPercentage = &pct
				return g
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			goalRepo := &mockSavingsProgressGoalRepo{}
			contribRepo := &mockSavingsProgressContribRepo{}
			accountRepo := &mockSavingsProgressAccountRepo{}
			svc := newTestProgressService(goalRepo, contribRepo, accountRepo)

			// Terminal AND balance-less — what GetByIDAndUser returns for a closed account now.
			account := &domain.Account{
				ID:               "acc1",
				UserID:           "u1",
				CurrentBalance:   nil,
				AvailableBalance: nil,
				Status:           domain.AccountStatusRemoved,
				UpdatedAt:        time.Now(),
			}

			accountRepo.On("GetByIDAndUser", mock.Anything, "acc1", "u1").Return(account, nil)
			goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			goalRepo.On("MarkCompleted", mock.Anything, mock.Anything).Return(nil).Maybe()

			_, err := svc.RecomputeGoal(context.Background(), tc.goal())

			require.NoError(t, err)
			assertNoCallTo(t, &goalRepo.Mock, "UpdateCurrentAmount")
			assertNoCallTo(t, &goalRepo.Mock, "MarkCompleted")
		})
	}
}

// TestSavingsProgress_TerminalAccountWithNoBalance_LogsTheDisconnection pins the ORDER of the two
// skip checks, which is the only thing item D could have quietly broken here.
//
// A closed account now satisfies BOTH conditions: it is disconnected, and (since item D) it carries
// no balance. The outcome is the same either way — freeze, never zero — so no behavioural test can
// tell the orderings apart, and the ordering would have flipped unnoticed. What differs is the
// operator-facing warning: "balance is nil ... (stale)" says a sync is late, and invites waiting for
// one that is never coming; "basis account is no longer active" says the account is gone. The
// specific condition must be checked before the general one.
func TestSavingsProgress_TerminalAccountWithNoBalance_LogsTheDisconnection(t *testing.T) {
	var logs bytes.Buffer
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc := NewSavingsProgressService(goalRepo, contribRepo, accountRepo, nil, logger)

	account := &domain.Account{
		ID: "acc1", UserID: "u1", CurrentBalance: nil, AvailableBalance: nil,
		Status: domain.AccountStatusRemoved, UpdatedAt: time.Now(),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc1", "u1").Return(account, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	goalRepo.On("MarkCompleted", mock.Anything, mock.Anything).Return(nil).Maybe()

	_, recomputeErr := svc.RecomputeGoal(context.Background(),
		makeGoal("g1", "u1", "acc1", domain.ProgressBasisBalance, 5000, 1000))
	require.NoError(t, recomputeErr)

	out := logs.String()
	assert.Contains(t, out, "basis account is no longer active",
		"a CLOSED account must be reported as closed, not as a late sync")
	assert.NotContains(t, out, "account balance is nil",
		"the nil check must not claim this one — since item D every closed account satisfies it too, "+
			"so a nil-first ordering silently relabels every disconnection as 'stale'")
}

// TestRecomputeGoal_ReportsThePersistedAmountEvenWhenMarkCompletedFails pins the two-write
// hazard. RecomputeGoal commits UpdateCurrentAmount and THEN calls MarkCompleted; when the
// second fails it used to return (0, err), so CreateGoal/UpdateGoal kept the pre-recompute
// figure while the row already held the new one. "Returned an error" and "changed nothing" are
// different states, and conflating them is what made the response disagree with the database.
//
// Reachable for the balance and account_percentage bases, whose computed value comes straight
// off an account balance and can meet the target immediately.
func TestRecomputeGoal_ReportsThePersistedAmountEvenWhenMarkCompletedFails(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisContributions, 100, 0)

	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(250.00, nil).Once()
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 250.00, mock.Anything, mock.Anything).
		Return(nil).Once()
	// The amount is already committed at this point; only the completion flag fails.
	goalRepo.On("MarkCompleted", mock.Anything, "g1").
		Return(errors.New("boom")).Once()

	svc := NewSavingsProgressService(goalRepo, contribRepo, nil, nil, testLogger())
	amount, err := svc.RecomputeGoal(context.Background(), goal)

	require.Error(t, err, "the MarkCompleted failure must still surface")
	assert.Equal(t, 250.00, amount,
		"the reported amount must be what the ROW holds (250), not 0 — the UpdateCurrentAmount write already committed")
}

// ...and when the FIRST write fails, nothing was persisted, so the prior amount is what the row
// still holds. Returning the new figure there would be the same bug pointing the other way.
func TestRecomputeGoal_ReportsThePriorAmountWhenNothingWasWritten(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}

	goal := makeGoal("g1", "u1", "acc1", domain.ProgressBasisContributions, 1000, 42)

	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(250.00, nil).Once()
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 250.00, mock.Anything, mock.Anything).
		Return(errors.New("boom")).Once()

	svc := NewSavingsProgressService(goalRepo, contribRepo, nil, nil, testLogger())
	amount, err := svc.RecomputeGoal(context.Background(), goal)

	require.Error(t, err)
	assert.Equal(t, 42.00, amount, "nothing was written, so the row still holds the prior amount")
}
