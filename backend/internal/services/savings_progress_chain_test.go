package services

// Issue #1569 (Task 13): a balance-basis savings goal must follow its account chain to the
// LIVE member instead of freezing forever on the terminal (replaced/removed) row it was
// created against.
//
// The two halves are tested separately and both matter:
//   - an ordinary re-link RESUMES recomputation from the live account's balance;
//   - a chain with no live member STILL FREEZES (no recompute from a frozen balance, no
//     drop to zero) — the guard added earlier on this branch keeps its correct behaviour.

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// progressChainResolverStub is a call-recording AccountChainResolver stub. It is separate from
// boost_execution_chain_test.go's mockAccountChainResolver because these tests need to assert
// that ResolveActiveID was NOT called on the degraded path.
type progressChainResolverStub struct {
	mu       sync.Mutex
	resolve  func(ctx context.Context, userAccountID string) (string, error)
	gotUAIDs []string
}

func (s *progressChainResolverStub) ListActiveByUser(context.Context, string) ([]domain.Account, error) {
	return nil, nil
}

func (s *progressChainResolverStub) ChainAccountIDs(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *progressChainResolverStub) ResolveActiveID(ctx context.Context, userAccountID string) (string, error) {
	s.mu.Lock()
	s.gotUAIDs = append(s.gotUAIDs, userAccountID)
	s.mu.Unlock()
	if s.resolve != nil {
		return s.resolve(ctx, userAccountID)
	}
	return "", errors.New("ResolveActiveID not configured")
}

func (s *progressChainResolverStub) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.gotUAIDs...)
}

// assertNoCallTo fails if the mock recorded any call to the named method.
//
// ⚠ testify's AssertNotCalled(t, "Method") with NO argument matchers asserts only that the
// method was never called with an EMPTY argument list — for a method that always takes
// arguments it can never fail, so it silently passes even when the call happened. This
// helper matches on the method name, which is what these freeze tests actually mean.
func assertNoCallTo(t *testing.T, m *mock.Mock, method string) {
	t.Helper()
	for _, c := range m.Calls {
		if c.Method == method {
			t.Fatalf("expected no call to %s, but it was called with %v", method, c.Arguments)
		}
	}
}

// callArgsTo returns the arguments of the first recorded call to the named method, and
// whether such a call happened. Used instead of a strict mock expectation so that a
// regression fails on a value assertion (4200 vs 3500) rather than panicking inside testify
// on an unexpected call — a panic proves only that something differed, not what.
func callArgsTo(m *mock.Mock, method string) (mock.Arguments, bool) {
	for _, c := range m.Calls {
		if c.Method == method {
			return c.Arguments, true
		}
	}
	return nil, false
}

func newChainProgressService(
	goalRepo *mockSavingsProgressGoalRepo,
	contribRepo *mockSavingsProgressContribRepo,
	accountRepo *mockSavingsProgressAccountRepo,
	resolver AccountChainResolver,
) SavingsProgressService {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewSavingsProgressService(goalRepo, contribRepo, accountRepo, resolver, logger)
}

// ─── half 1: an ordinary re-link resumes recomputation ───────────────────────

// TestSavingsProgress_Balance_Relink_FollowsChainToLiveAccount pins the defect this task
// fixes: the goal row still points at the replaced account, but recompute must read the
// LIVE account's balance instead of freezing.
func TestSavingsProgress_Balance_Relink_FollowsChainToLiveAccount(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(_ context.Context, uaid string) (string, error) {
			if uaid == "uaid-1" {
				return "acc-new", nil
			}
			return "", ErrNoActiveAccount
		},
	}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	goal := makeGoal("g1", "u1", "acc-old", domain.ProgressBasisBalance, 10000, 3500)
	goal.UserAccountID = strPtr1569("uaid-1")

	liveBalance := 4200.00
	live := &domain.Account{
		ID: "acc-new", UserID: "u1", CurrentBalance: &liveBalance,
		Status: domain.AccountStatusActive, UpdatedAt: time.Now(),
	}
	frozenBalance := 3500.00
	terminal := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozenBalance,
		Status: domain.AccountStatusReplaced, UpdatedAt: time.Now().Add(-90 * 24 * time.Hour),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-new", "u1").Return(live, nil).Maybe()
	// The terminal row is reachable (.Maybe()) on purpose: a regression that ignores the chain
	// then recomputes 3500 and fails the value assertion below, instead of panicking in testify.
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(terminal, nil).Maybe()
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	require.NoError(t, svc.RecomputeGoal(context.Background(), goal))

	args, called := callArgsTo(&goalRepo.Mock, "UpdateCurrentAmount")
	require.True(t, called, "a re-linked goal must resume recomputation, not stay frozen")
	assert.Equal(t, 4200.00, args.Get(2), "must be the LIVE account's balance, not the frozen 3500")
	assert.Equal(t, []string{"uaid-1"}, resolver.calls(), "chain must be resolved from the goal's stable user_account_id")
	assertNoAccountRead(t, &accountRepo.Mock, "acc-old")
}

// assertNoAccountRead fails if the account repo was asked for the given account id.
func assertNoAccountRead(t *testing.T, m *mock.Mock, accountID string) {
	t.Helper()
	for _, c := range m.Calls {
		if c.Method == "GetByIDAndUser" && len(c.Arguments) > 1 && c.Arguments.Get(1) == accountID {
			t.Errorf("expected no read of account %s (the frozen terminal row), but it was read", accountID)
		}
	}
}

// TestSavingsProgress_AccountPercentage_Relink_FollowsChainToLiveAccount is the same fix for
// the account_percentage basis, which shares the balance-reading code path.
func TestSavingsProgress_AccountPercentage_Relink_FollowsChainToLiveAccount(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(_ context.Context, string2 string) (string, error) { return "acc-new", nil },
	}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	pct := 0.25
	goal := makeGoal("g1", "u1", "acc-old", domain.ProgressBasisAccountPercentage, 5000, 100)
	goal.AccountPercentage = &pct
	goal.UserAccountID = strPtr1569("uaid-1")

	liveBalance := 4000.00
	live := &domain.Account{
		ID: "acc-new", UserID: "u1", CurrentBalance: &liveBalance,
		Status: domain.AccountStatusActive, UpdatedAt: time.Now(),
	}
	frozenBalance := 800.00
	terminal := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozenBalance,
		Status: domain.AccountStatusReplaced, UpdatedAt: time.Now().Add(-90 * 24 * time.Hour),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-new", "u1").Return(live, nil).Maybe()
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(terminal, nil).Maybe()
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	require.NoError(t, svc.RecomputeGoal(context.Background(), goal))

	args, called := callArgsTo(&goalRepo.Mock, "UpdateCurrentAmount")
	require.True(t, called, "a re-linked account_percentage goal must resume recomputation")
	// 4000 * 0.25 = 1000 (the frozen row would give 800 * 0.25 = 200)
	assert.Equal(t, 1000.00, args.Get(2))
	assertNoAccountRead(t, &accountRepo.Mock, "acc-old")
}

// ─── half 2: no live member in the chain still freezes ───────────────────────

// TestSavingsProgress_Balance_ChainHasNoLiveMember_StillFreezes covers the user who
// disconnected and did NOT re-link. current_amount must neither be recomputed from the
// frozen balance nor dropped to zero.
func TestSavingsProgress_Balance_ChainHasNoLiveMember_StillFreezes(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(context.Context, string) (string, error) { return "", ErrNoActiveAccount },
	}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	goal := makeGoal("g1", "u1", "acc-old", domain.ProgressBasisBalance, 10000, 3500)
	goal.UserAccountID = strPtr1569("uaid-1")

	frozen := 3500.00
	terminal := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozen,
		Status: domain.AccountStatusRemoved, UpdatedAt: time.Now().Add(-90 * 24 * time.Hour),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(terminal, nil).Maybe()
	// Registered as .Maybe() so a regression RECORDS the write instead of panicking on an
	// unexpected mock call — AssertNotCalled below then fails as a real assertion.
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	goalRepo.On("MarkCompleted", mock.Anything, mock.Anything).Return(nil).Maybe()

	require.NoError(t, svc.RecomputeGoal(context.Background(), goal))

	assertNoCallTo(t, &goalRepo.Mock, "UpdateCurrentAmount")
	assertNoCallTo(t, &goalRepo.Mock, "MarkCompleted")
	assert.Equal(t, 3500.0, goal.CurrentAmount, "frozen goal keeps its last computed amount")
}

// ─── honest degradation when the chain reference is absent ───────────────────

// TestSavingsProgress_Balance_NilUserAccountID_DegradesToPhysicalRow_AndFreezes covers rows
// written before user_account_id was populated in production. Resolution must not crash and
// must not silently reintroduce the frozen-balance bug: with no chain reference, the terminal
// physical row is all we have and the goal still freezes.
func TestSavingsProgress_Balance_NilUserAccountID_DegradesToPhysicalRow_AndFreezes(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	goal := makeGoal("g1", "u1", "acc-old", domain.ProgressBasisBalance, 10000, 3500)
	goal.UserAccountID = nil // legacy row

	frozen := 3500.00
	terminal := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozen,
		Status: domain.AccountStatusReplaced, UpdatedAt: time.Now(),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(terminal, nil)
	// See the .Maybe() note in the no-live-member test above.
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	goalRepo.On("MarkCompleted", mock.Anything, mock.Anything).Return(nil).Maybe()

	require.NoError(t, svc.RecomputeGoal(context.Background(), goal))

	assert.Empty(t, resolver.calls(), "no chain reference to resolve — resolver must not be called")
	assertNoCallTo(t, &goalRepo.Mock, "UpdateCurrentAmount")
	assertNoCallTo(t, &goalRepo.Mock, "MarkCompleted")
}

// TestSavingsProgress_Balance_NilUserAccountID_ActiveAccount_StillRecomputes is the other
// side of honest degradation: a legacy row whose physical account is still active must keep
// working exactly as it did before this change.
func TestSavingsProgress_Balance_NilUserAccountID_ActiveAccount_StillRecomputes(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	goal := makeGoal("g1", "u1", "acc-old", domain.ProgressBasisBalance, 10000, 0)
	goal.UserAccountID = nil

	bal := 1234.56
	live := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &bal,
		Status: domain.AccountStatusActive, UpdatedAt: time.Now(),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(live, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", 1234.56, mock.Anything, mock.Anything).Return(nil)

	require.NoError(t, svc.RecomputeGoal(context.Background(), goal))

	assert.Empty(t, resolver.calls())
	goalRepo.AssertExpectations(t)
}

// TestSavingsProgress_Balance_EmptyUserAccountID_DegradesToPhysicalRow covers the empty-string
// (not nil) chain reference — ResolveActiveID would reject it with ErrEmptyUserAccountID, so
// resolution must skip it rather than turn it into an error the caller logs every run.
func TestSavingsProgress_Balance_EmptyUserAccountID_DegradesToPhysicalRow(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	goal := makeGoal("g1", "u1", "acc-old", domain.ProgressBasisBalance, 10000, 0)
	goal.UserAccountID = strPtr1569("")

	bal := 500.00
	live := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &bal,
		Status: domain.AccountStatusActive, UpdatedAt: time.Now(),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(live, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", 500.00, mock.Anything, mock.Anything).Return(nil)

	require.NoError(t, svc.RecomputeGoal(context.Background(), goal))

	assert.Empty(t, resolver.calls(), "an empty user_account_id is not a chain reference")
	goalRepo.AssertExpectations(t)
}

// frozenForTransportTest backs the fallback account the transport-error test registers as
// .Maybe() — if resolution wrongly degraded to the physical row, recompute would succeed and
// the require.Error below would fail as an assertion rather than a nil deref.
var frozenForTransportTest = 3500.00

// TestSavingsProgress_Balance_ResolverTransportError_IsReported ensures an infrastructure
// failure from the resolver is surfaced as an error rather than being mistaken for
// "the chain has no live member" (which would silently freeze a healthy goal).
func TestSavingsProgress_Balance_ResolverTransportError_IsReported(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(context.Context, string) (string, error) {
			return "", errors.New("connection refused")
		},
	}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	goal := makeGoal("g1", "u1", "acc-old", domain.ProgressBasisBalance, 10000, 3500)
	goal.UserAccountID = strPtr1569("uaid-1")

	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	accountRepo.On("GetByIDAndUser", mock.Anything, mock.Anything, "u1").Return(&domain.Account{
		ID: "acc-old", UserID: "u1", Status: domain.AccountStatusActive, CurrentBalance: &frozenForTransportTest,
	}, nil).Maybe()

	err := svc.RecomputeGoal(context.Background(), goal)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
	assertNoCallTo(t, &goalRepo.Mock, "UpdateCurrentAmount")
}

// ─── the same resolution must drive the summary's freshness signal ───────────

// TestSavingsProgress_Summary_Relink_ReportsLiveBalanceNotDisconnected pins that the API's
// freshness flag agrees with recompute: after an ordinary re-link the goal is NOT
// basis_account_disconnected, and linked_balance is the live account's.
func TestSavingsProgress_Summary_Relink_ReportsLiveBalanceNotDisconnected(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(context.Context, string) (string, error) { return "acc-new", nil },
	}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	recent := time.Now().Add(-1 * time.Hour)
	goal := &domain.SavingsGoal{
		ID: "g1", UserID: "u1", Name: "Emergency Fund",
		CurrentAmount: 4200, TargetAmount: 10000,
		ProgressBasis: domain.ProgressBasisBalance,
		AccountID:     "acc-old",
		UserAccountID: strPtr1569("uaid-1"),
		BalanceAsOf:   &recent,
	}
	liveBalance := 4200.00
	live := &domain.Account{
		ID: "acc-new", UserID: "u1", CurrentBalance: &liveBalance,
		Status: domain.AccountStatusActive,
	}

	frozenBalance := 3500.00
	terminal := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozenBalance,
		Status: domain.AccountStatusRemoved,
	}

	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{goal}, nil)
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(0.0, nil)
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-new", "u1").Return(live, nil).Maybe()
	// Reachable on purpose — see the note in the balance re-link test.
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(terminal, nil).Maybe()

	summary, err := svc.GetProgressSummary(context.Background(), "u1")

	require.NoError(t, err)
	require.Len(t, summary.Goals, 1)
	assert.Equal(t, domain.FreshnessFresh, summary.Goals[0].Freshness,
		"a re-linked goal is fresh, not basis_account_disconnected")
	require.NotNil(t, summary.Goals[0].LinkedBalance)
	assert.Equal(t, 4200.00, *summary.Goals[0].LinkedBalance)
}

// TestSavingsProgress_Summary_ChainHasNoLiveMember_ReportsDisconnected keeps the other half
// of the freshness contract: a chain with no live member is still reported as disconnected,
// which is the only signal an API consumer has to explain the frozen number.
func TestSavingsProgress_Summary_ChainHasNoLiveMember_ReportsDisconnected(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(context.Context, string) (string, error) { return "", ErrNoActiveAccount },
	}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	recent := time.Now().Add(-1 * time.Hour)
	goal := &domain.SavingsGoal{
		ID: "g1", UserID: "u1", Name: "Emergency Fund",
		CurrentAmount: 3500, TargetAmount: 10000,
		ProgressBasis: domain.ProgressBasisBalance,
		AccountID:     "acc-old",
		UserAccountID: strPtr1569("uaid-1"),
		BalanceAsOf:   &recent,
	}
	frozen := 3500.00
	terminal := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozen,
		Status: domain.AccountStatusRemoved,
	}

	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{goal}, nil)
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(0.0, nil)
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(terminal, nil)

	summary, err := svc.GetProgressSummary(context.Background(), "u1")

	require.NoError(t, err)
	require.Len(t, summary.Goals, 1)
	assert.Equal(t, domain.FreshnessBasisAccountDisconnected, summary.Goals[0].Freshness)
}

// strPtr1569 is a local string-pointer helper (the package already has a ptrStr in
// transaction_service_test.go).
func strPtr1569(s string) *string { return &s }
