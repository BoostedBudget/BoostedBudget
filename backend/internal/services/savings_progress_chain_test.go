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
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
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
	gotUIDs  []string
}

func (s *progressChainResolverStub) ListActiveByUser(context.Context, string) ([]domain.Account, error) {
	return nil, nil
}

func (s *progressChainResolverStub) ChainAccountIDs(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *progressChainResolverStub) ResolveActiveID(ctx context.Context, userAccountID, userID string) (string, error) {
	s.mu.Lock()
	s.gotUAIDs = append(s.gotUAIDs, userAccountID)
	s.gotUIDs = append(s.gotUIDs, userID)
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

// userIDCalls returns the userID each ResolveActiveID call was scoped to, in order — used to
// pin that resolveBasisAccount threads the REQUESTER's id through (#1569 R11 T2), not the
// goal's own owner.
func (s *progressChainResolverStub) userIDCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.gotUIDs...)
}

// assertNoCallTo fails if the mock recorded any call to the named method.
//
// ⚠ testify's AssertNotCalled(t, "Method") with NO argument matchers asserts only that the
// method was never called with an EMPTY argument list — for a method that always takes
// arguments it can never fail, so it silently passes even when the call happened. This
// helper matches on the method name, which is what these freeze tests actually mean.
//
// #1569 task 19: the implementation has been PROMOTED to internal/testsupport/mockassert so that
// packages other than this one can use it — it was the only correct spelling of this assertion in
// the tree, and it was reachable only from `package services`. This wrapper stays so the ~20 call
// sites here read the same; new code outside this package should call mockassert.NoCallTo directly.
// TestNoNewNoMatcherAssertNotCalled stops new no-matcher AssertNotCalled calls being added anywhere.
func assertNoCallTo(t *testing.T, m *mock.Mock, method string) {
	t.Helper()
	mockassert.NoCallTo(t, m, method)
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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	_, recomputeErr := svc.RecomputeGoal(context.Background(), goal)
	require.NoError(t, recomputeErr)

	args, called := callArgsTo(&goalRepo.Mock, "UpdateCurrentAmount")
	require.True(t, called, "a re-linked goal must resume recomputation, not stay frozen")
	// #1625 shifted the amount to index 3: the basis it was computed for is now bound into the
	// statement, so it is argument 2.
	assert.Equal(t, domain.ProgressBasisBalance, args.Get(2))
	assert.Equal(t, 4200.00, args.Get(3), "must be the LIVE account's balance, not the frozen 3500")
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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	_, recomputeErr := svc.RecomputeGoal(context.Background(), goal)
	require.NoError(t, recomputeErr)

	args, called := callArgsTo(&goalRepo.Mock, "UpdateCurrentAmount")
	require.True(t, called, "a re-linked account_percentage goal must resume recomputation")
	// 4000 * 0.25 = 1000 (the frozen row would give 800 * 0.25 = 200)
	assert.Equal(t, domain.ProgressBasisAccountPercentage, args.Get(2))
	assert.Equal(t, 1000.00, args.Get(3))
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

	// current_amount (1000) is deliberately different from the frozen balance (3500) so that
	// "did not move" and "recomputed from the frozen row" are distinguishable outcomes.
	goal := makeGoal("g1", "u1", "acc-old", domain.ProgressBasisBalance, 10000, 1000)
	goal.UserAccountID = strPtr1569("uaid-1")

	frozen := 3500.00
	terminal := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozen,
		Status: domain.AccountStatusRemoved, UpdatedAt: time.Now().Add(-90 * 24 * time.Hour),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(terminal, nil).Maybe()
	// Registered as .Maybe() so a regression RECORDS the write instead of panicking on an
	// unexpected mock call — AssertNotCalled below then fails as a real assertion.
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	goalRepo.On("MarkCompleted", mock.Anything, mock.Anything).Return(nil).Maybe()

	_, recomputeErr := svc.RecomputeGoal(context.Background(), goal)
	require.NoError(t, recomputeErr)

	assertNoCallTo(t, &goalRepo.Mock, "UpdateCurrentAmount")
	assertNoCallTo(t, &goalRepo.Mock, "MarkCompleted")
	assert.Equal(t, 1000.0, goal.CurrentAmount, "frozen goal keeps its last computed amount, not the retained row's 3500")
}

// TestSavingsProgress_Balance_ChainHasTwoLiveMembers_StillFreezes covers #1569 R11 T2: two
// maskless accounts at one institution can share a user_account_id (migration 000100's
// NULLS NOT DISTINCT unique index), so the chain resolver refuses to guess and returns
// ErrAmbiguousActiveAccount. The goal must follow the SAME degradation philosophy as
// "no live member" — freeze on the physical row — rather than silently recomputing from
// whichever live sibling ResolveActiveID used to pick.
//
// This test's resolver stub short-circuits straight to ErrAmbiguousActiveAccount (as the real
// AccountChainResolver does once it sees len(accounts) > 1) — it does not construct sibling
// Account values, because resolveBasisAccount never reads them: on this error it falls straight
// through to the physical row without touching whatever the two live siblings' balances are.
// The real repository-level "len > 1" behaviour, with two siblings holding DIFFERENT balances,
// is covered separately by TestAccountChainResolver_ResolveActiveID's ambiguous case.
func TestSavingsProgress_Balance_ChainHasTwoLiveMembers_StillFreezes(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(context.Context, string) (string, error) { return "", ErrAmbiguousActiveAccount },
	}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	// current_amount (1000) differs from the physical row's frozen balance (3500), so "did not
	// move" and "recomputed from the physical row" are distinguishable outcomes.
	goal := makeGoal("g1", "u1", "acc-old", domain.ProgressBasisBalance, 10000, 1000)
	goal.UserAccountID = strPtr1569("uaid-1")

	frozen := 3500.00
	physicalRow := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozen,
		Status: domain.AccountStatusReplaced, UpdatedAt: time.Now().Add(-90 * 24 * time.Hour),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(physicalRow, nil)
	// Registered as .Maybe() so a regression that guesses a sibling RECORDS the write instead
	// of panicking on an unexpected mock call.
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	goalRepo.On("MarkCompleted", mock.Anything, mock.Anything).Return(nil).Maybe()

	_, recomputeErr := svc.RecomputeGoal(context.Background(), goal)
	require.NoError(t, recomputeErr)

	assertNoCallTo(t, &goalRepo.Mock, "UpdateCurrentAmount")
	assertNoCallTo(t, &goalRepo.Mock, "MarkCompleted")
	assert.Equal(t, 1000.0, goal.CurrentAmount,
		"an ambiguous chain freezes the goal — it must not recompute from the physical row's 3500")
}

// TestSavingsProgress_Summary_ChainHasTwoLiveMembers_ReportsDisconnected is the freshness half
// of the ambiguous-chain test: the API must report basis_account_disconnected, the same signal
// a fully-terminal chain produces, so the UI explains the frozen number.
func TestSavingsProgress_Summary_ChainHasTwoLiveMembers_ReportsDisconnected(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(context.Context, string) (string, error) { return "", ErrAmbiguousActiveAccount },
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
	physicalRow := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozen,
		Status: domain.AccountStatusReplaced,
	}

	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{goal}, nil)
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(0.0, nil)
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(physicalRow, nil)

	summary, err := svc.GetProgressSummary(context.Background(), "u1")

	require.NoError(t, err)
	require.Len(t, summary.Goals, 1)
	assert.Equal(t, domain.FreshnessBasisAccountDisconnected, summary.Goals[0].Freshness)
}

// TestSavingsProgress_ResolveActiveID_ScopedToRequester_NotGoalOwner pins that
// resolveBasisAccount threads the REQUESTER's id into ResolveActiveID (#1569 R11 T2's tenancy
// fix), the same ownership boundary already enforced on the account read a few lines later.
func TestSavingsProgress_ResolveActiveID_ScopedToRequester_NotGoalOwner(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(context.Context, string) (string, error) { return "acc-live", nil },
	}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	goal := &domain.SavingsGoal{
		ID: "g1", UserID: "other-owner", Name: "Not Mine",
		CurrentAmount: 1000, TargetAmount: 10000,
		ProgressBasis: domain.ProgressBasisBalance,
		AccountID:     "acc-old", UserAccountID: strPtr1569("uaid-1"),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-live", "u1").Return(nil, ErrAccountNotFoundForProgress).Maybe()

	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{goal}, nil)
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(0.0, nil)

	_, err := svc.GetProgressSummary(context.Background(), "u1")
	require.NoError(t, err)

	for _, uid := range resolver.userIDCalls() {
		assert.Equal(t, "u1", uid, "ResolveActiveID must be scoped to the requester, not goal.UserID")
	}
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

	goal := makeGoal("g1", "u1", "acc-old", domain.ProgressBasisBalance, 10000, 1000)
	goal.UserAccountID = nil // legacy row

	frozen := 3500.00
	terminal := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozen,
		Status: domain.AccountStatusReplaced, UpdatedAt: time.Now(),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(terminal, nil)
	// See the .Maybe() note in the no-live-member test above.
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	goalRepo.On("MarkCompleted", mock.Anything, mock.Anything).Return(nil).Maybe()

	_, recomputeErr := svc.RecomputeGoal(context.Background(), goal)
	require.NoError(t, recomputeErr)

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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 1234.56, mock.Anything, mock.Anything).Return(nil)

	_, recomputeErr := svc.RecomputeGoal(context.Background(), goal)
	require.NoError(t, recomputeErr)

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
	goalRepo.On("UpdateCurrentAmount", mock.Anything, "g1", mock.Anything, 500.00, mock.Anything, mock.Anything).Return(nil)

	_, recomputeErr := svc.RecomputeGoal(context.Background(), goal)
	require.NoError(t, recomputeErr)

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

	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	accountRepo.On("GetByIDAndUser", mock.Anything, mock.Anything, "u1").Return(&domain.Account{
		ID: "acc-old", UserID: "u1", Status: domain.AccountStatusActive, CurrentBalance: &frozenForTransportTest,
	}, nil).Maybe()

	_, err := svc.RecomputeGoal(context.Background(), goal)

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
	// #1569 A4: CompletedGoalCount is now counted outside the active universe.
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
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
	// #1569 A4: CompletedGoalCount is now counted outside the active universe.
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
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

// ─── I1: the account_percentage basis needs BOTH halves, same as balance ─────
//
// Round 1 shipped only half (a) for account_percentage — the re-link test. Disabling the
// freeze guard in the account_percentage branch of RecomputeGoal left the entire unit AND
// integration suite green, so the guard that stops an account_percentage goal recomputing
// from a frozen balance was pinned by nothing.

// TestSavingsProgress_AccountPercentage_ChainHasNoLiveMember_StillFreezes mirrors the
// balance-basis freeze test: a percent-of-balance goal whose chain has no live member must not
// recompute from the retained row's frozen balance, and must not drop to zero.
func TestSavingsProgress_AccountPercentage_ChainHasNoLiveMember_StillFreezes(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(context.Context, string) (string, error) { return "", ErrNoActiveAccount },
	}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	pct := 0.25
	// current_amount (1000) differs from what the frozen row would produce (3500 * 0.25 = 875),
	// so "did not move" and "recomputed from the frozen row" are distinguishable.
	goal := makeGoal("g1", "u1", "acc-old", domain.ProgressBasisAccountPercentage, 10000, 1000)
	goal.AccountPercentage = &pct
	goal.UserAccountID = strPtr1569("uaid-1")

	frozen := 3500.00
	terminal := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozen,
		Status: domain.AccountStatusRemoved, UpdatedAt: time.Now().Add(-90 * 24 * time.Hour),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(terminal, nil).Maybe()
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	goalRepo.On("MarkCompleted", mock.Anything, mock.Anything).Return(nil).Maybe()

	_, recomputeErr := svc.RecomputeGoal(context.Background(), goal)
	require.NoError(t, recomputeErr)

	assertNoCallTo(t, &goalRepo.Mock, "UpdateCurrentAmount")
	assertNoCallTo(t, &goalRepo.Mock, "MarkCompleted")
	assert.Equal(t, 1000.0, goal.CurrentAmount, "frozen goal keeps its last computed amount, not 3500 * 0.25")
}

// TestSavingsProgress_AccountPercentage_NilUserAccountID_DegradesToPhysicalRow_AndFreezes is
// the account_percentage twin of the balance-basis legacy-row test.
func TestSavingsProgress_AccountPercentage_NilUserAccountID_DegradesToPhysicalRow_AndFreezes(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	pct := 0.25
	goal := makeGoal("g1", "u1", "acc-old", domain.ProgressBasisAccountPercentage, 10000, 1000)
	goal.AccountPercentage = &pct
	goal.UserAccountID = nil

	frozen := 3500.00
	terminal := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozen,
		Status: domain.AccountStatusReplaced, UpdatedAt: time.Now(),
	}
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(terminal, nil)
	goalRepo.On("UpdateCurrentAmount", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	goalRepo.On("MarkCompleted", mock.Anything, mock.Anything).Return(nil).Maybe()

	_, recomputeErr := svc.RecomputeGoal(context.Background(), goal)
	require.NoError(t, recomputeErr)

	assert.Empty(t, resolver.calls(), "no chain reference to resolve — resolver must not be called")
	assertNoCallTo(t, &goalRepo.Mock, "UpdateCurrentAmount")
	assertNoCallTo(t, &goalRepo.Mock, "MarkCompleted")
}

// TestSavingsProgress_Summary_AccountPercentage_ChainHasNoLiveMember_ReportsDisconnected pins
// the freshness half for account_percentage — without it, the new badge could silently stop
// appearing for percent-of-balance goals.
func TestSavingsProgress_Summary_AccountPercentage_ChainHasNoLiveMember_ReportsDisconnected(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(context.Context, string) (string, error) { return "", ErrNoActiveAccount },
	}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	pct := 0.25
	recent := time.Now().Add(-1 * time.Hour)
	goal := &domain.SavingsGoal{
		ID: "g1", UserID: "u1", Name: "Shared Pot",
		CurrentAmount: 1000, TargetAmount: 10000,
		ProgressBasis: domain.ProgressBasisAccountPercentage, AccountPercentage: &pct,
		AccountID: "acc-old", UserAccountID: strPtr1569("uaid-1"), BalanceAsOf: &recent,
	}
	frozen := 3500.00
	terminal := &domain.Account{
		ID: "acc-old", UserID: "u1", CurrentBalance: &frozen, Status: domain.AccountStatusRemoved,
	}

	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{goal}, nil)
	// #1569 A4: CompletedGoalCount is now counted outside the active universe.
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(0.0, nil)
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-old", "u1").Return(terminal, nil)

	summary, err := svc.GetProgressSummary(context.Background(), "u1")

	require.NoError(t, err)
	require.Len(t, summary.Goals, 1)
	assert.Equal(t, domain.FreshnessBasisAccountDisconnected, summary.Goals[0].Freshness)
}

// ─── M1: a resolver blip must not be reported as freshness ───────────────────

// TestSavingsProgress_Summary_ResolverTransportError_ReportsUnknownNotFresh covers the
// summary side of the transport-error distinction. Round 1 discarded the error with
// `if err == nil`, so during a database blip a permanently-frozen goal reported `fresh` from
// its stale balance_as_of and the disconnected badge silently vanished.
func TestSavingsProgress_Summary_ResolverTransportError_ReportsUnknownNotFresh(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(context.Context, string) (string, error) {
			return "", errors.New("connection refused")
		},
	}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	recent := time.Now().Add(-1 * time.Hour) // would otherwise compute as `fresh`
	goal := &domain.SavingsGoal{
		ID: "g1", UserID: "u1", Name: "Emergency Fund",
		CurrentAmount: 1000, TargetAmount: 10000,
		ProgressBasis: domain.ProgressBasisBalance,
		AccountID:     "acc-old", UserAccountID: strPtr1569("uaid-1"), BalanceAsOf: &recent,
	}
	bal := 3500.00
	accountRepo.On("GetByIDAndUser", mock.Anything, mock.Anything, mock.Anything).Return(
		&domain.Account{ID: "acc-old", UserID: "u1", CurrentBalance: &bal, Status: domain.AccountStatusActive}, nil).Maybe()
	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{goal}, nil)
	// #1569 A4: CompletedGoalCount is now counted outside the active universe.
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(0.0, nil)

	summary, err := svc.GetProgressSummary(context.Background(), "u1")

	require.NoError(t, err, "one unresolvable goal must not fail the whole summary")
	require.Len(t, summary.Goals, 1)
	assert.Equal(t, domain.FreshnessUnknown, summary.Goals[0].Freshness,
		"a resolution failure is not evidence of freshness")
	assert.Nil(t, summary.Goals[0].LinkedBalance,
		"no balance may be reported from an account we failed to resolve")
}

// ─── I3: the account read stays scoped to the REQUESTER ──────────────────────

// TestSavingsProgress_Summary_AccountReadIsScopedToRequesterNotGoalOwner pins the ownership
// gate on the read that populates LinkedBalance (a financial amount). Scoping it to
// goal.UserID would compare the goal against its own owner, so the check could never fail
// closed; the pre-#1569 code passed the requester's id and that must survive.
func TestSavingsProgress_Summary_AccountReadIsScopedToRequesterNotGoalOwner(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	resolver := &progressChainResolverStub{
		resolve: func(context.Context, string) (string, error) { return "acc-live", nil },
	}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, resolver)

	// A goal owned by someone else reaching the summary of requester "u1" (the shape a future
	// household view would produce). The account read must be scoped to "u1".
	goal := &domain.SavingsGoal{
		ID: "g1", UserID: "other-owner", Name: "Not Mine",
		CurrentAmount: 1000, TargetAmount: 10000,
		ProgressBasis: domain.ProgressBasisBalance,
		AccountID:     "acc-old", UserAccountID: strPtr1569("uaid-1"),
	}
	othersBalance := 9999.00
	// Registered ONLY for the goal owner: if the code scopes to goal.UserID the read succeeds
	// and leaks the balance; scoped to the requester it must miss.
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-live", "other-owner").
		Return(&domain.Account{ID: "acc-live", UserID: "other-owner", CurrentBalance: &othersBalance, Status: domain.AccountStatusActive}, nil).Maybe()
	accountRepo.On("GetByIDAndUser", mock.Anything, "acc-live", "u1").
		Return(nil, ErrAccountNotFoundForProgress).Maybe()

	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{goal}, nil)
	// #1569 A4: CompletedGoalCount is now counted outside the active universe.
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, nil).Maybe()
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g1").Return(0.0, nil)

	summary, err := svc.GetProgressSummary(context.Background(), "u1")

	require.NoError(t, err)
	require.Len(t, summary.Goals, 1)
	assert.Nil(t, summary.Goals[0].LinkedBalance,
		"an account the requester does not own must not have its balance reported")

	for _, c := range accountRepo.Calls {
		if c.Method == "GetByIDAndUser" {
			assert.Equal(t, "u1", c.Arguments.Get(2),
				"the account read must be scoped to the requester, not the goal's owner")
		}
	}
}

// ErrAccountNotFoundForProgress stands in for repository.ErrAccountNotFound without importing
// the repository package into this service-level test.
var ErrAccountNotFoundForProgress = errors.New("account not found")

// ─── M4: the summary's goal universe is active-only, deliberately ────────────

// TestSavingsProgress_Summary_UniverseIsActiveOnly pins the boundary behind the review's M4
// ("completed goals never get the disconnect badge"). It is deliberate, not an oversight:
//
//   - RecomputeUser — the only periodic recompute path — also reads GetActiveByUserID, so a
//     COMPLETED goal's current_amount is frozen whether or not its account is connected. The
//     card's existing "Completed" badge already explains why that number is static; adding
//     "Account disconnected" would attribute a general property to a specific cause.
//   - Widening the universe here is an API-semantics change to a live MCP/AI contract
//     (goal_count, total_saved, total_target, overall_percent_complete all shift), which is
//     out of scope for a re-link defect.
//
// This test exists so that widening it later is a deliberate act with a failing test attached,
// rather than an accident.
func TestSavingsProgress_Summary_UniverseIsActiveOnly(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, &progressChainResolverStub{})

	active := makeGoal("g-active", "u1", "acc-1", domain.ProgressBasisContributions, 1000, 250)
	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{active}, nil)
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g-active").Return(250.0, nil)
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(7, nil)

	summary, err := svc.GetProgressSummary(context.Background(), "u1")

	require.NoError(t, err)
	assertNoCallTo(t, &goalRepo.Mock, "GetByIDForUser")
	require.Len(t, summary.Goals, 1)
	assert.Equal(t, "g-active", summary.Goals[0].GoalID)

	// The universe stays active-only, but CompletedGoalCount no longer inherits that limit: it is
	// counted by its own query (Issue #1569 Task 20, A4). Seven completed goals, none of them in
	// `Goals`, and the count says seven.
	assert.Equal(t, 7, summary.CompletedGoalCount,
		"CompletedGoalCount is counted OUTSIDE the active universe — see A4")
}

// Issue #1569 Task 20 (A4). CompletedGoalCount was derived by counting IsCompleted over
// GetActiveByUserID's result, and that query's WHERE clause is `is_completed = false`. The count
// was therefore structurally zero for every user alive, and the field is REQUIRED in the JSON
// contract — so an MCP or LLM consumer asking "how many goals have I completed?" got a confident,
// well-typed, wrong 0. There is no frontend consumer, so the whole impact was on that contract.
//
// The old shape is what makes this test's arrangement look odd and is the point of it: the
// completed goal is NOT in the slice the repository returns, because it never could be. A fix that
// merely widened the active query would show up here as a changed Goals length.
func TestSavingsProgress_Summary_CompletedGoalCountIsNotStructurallyZero(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, &progressChainResolverStub{})

	active := makeGoal("g-active", "u1", "acc-1", domain.ProgressBasisContributions, 1000, 250)
	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{active}, nil)
	contribRepo.On("SumConfirmedByGoal", mock.Anything, "g-active").Return(250.0, nil)
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(3, nil)

	summary, err := svc.GetProgressSummary(context.Background(), "u1")

	require.NoError(t, err)
	assert.Equal(t, 3, summary.CompletedGoalCount, "the completed count must come from its own query")
	// GoalCount stays the ACTIVE count. Asserted so a future "fix" that merges the two universes
	// fails here rather than silently changing what every other total in this struct means.
	assert.Equal(t, 1, summary.GoalCount)
	assert.Len(t, summary.Goals, 1)
	assert.Equal(t, 250.0, summary.TotalSaved)
}

// A failure counting completed goals must PROPAGATE, not default to 0. Defaulting would restore
// the original defect on the one path where nobody would notice it, and a confidently wrong count
// is worse for an LLM consumer than an error it can report.
func TestSavingsProgress_Summary_CompletedCountFailurePropagates(t *testing.T) {
	goalRepo := &mockSavingsProgressGoalRepo{}
	contribRepo := &mockSavingsProgressContribRepo{}
	accountRepo := &mockSavingsProgressAccountRepo{}
	svc := newChainProgressService(goalRepo, contribRepo, accountRepo, &progressChainResolverStub{})

	active := makeGoal("g-active", "u1", "acc-1", domain.ProgressBasisContributions, 1000, 250)
	goalRepo.On("GetActiveByUserID", mock.Anything, "u1").Return([]*domain.SavingsGoal{active}, nil)
	goalRepo.On("CountCompletedByUserID", mock.Anything, "u1").Return(0, errors.New("db is down"))

	summary, err := svc.GetProgressSummary(context.Background(), "u1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "count completed goals")
	assert.Nil(t, summary)
}
