package services

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// #1569 round 11 item 1 — GetBalanceSummary and GetNetWorth are separate, uncached HTTP endpoints
// that a single dashboard page load fires together, and in personal scope they read
// byte-identical repository arguments for three of their four reads (current snapshots, current
// accounts, baseline snapshots — see dashboard_service.go's dashboardSFDo doc comment). This test
// MEASURES the fix rather than asserting it exists: it fires both endpoints CONCURRENTLY (the
// realistic shape of a dashboard page load, where the frontend's two widgets issue their fetches
// together) with each mocked repository method deliberately slow enough to guarantee the two
// requests genuinely overlap, then asserts the exact call count on each shared repository method.
//
// BEFORE this fix, the two endpoints called the repository directly and independently: each of the
// three shared reads was issued ONCE PER ENDPOINT, i.e. 2 calls per shared read, 6 total across the
// two endpoints. That is a fact about the removed code (each call site was a direct,
// unconditional s.balanceSnapshotRepo.X / s.accountRepo.Y invocation — see the diff), not something
// this test can re-measure without reverting the fix; it is asserted here as the documented
// baseline this test's AFTER numbers are compared against.
//
// AFTER: singleflight.Group.Do coalesces the genuinely concurrent overlap onto one execution per
// shared read, so this test asserts 1 call each (3 total) for the two concurrent requests, not 2
// each (6 total).
func TestDashboard_ConcurrentBalanceSummaryAndNetWorth_DedupesSharedRepositoryReads(t *testing.T) {
	ctx := context.Background()
	userID := "u-concurrent-dedup"

	balance := 1000.00
	prevBalance := 900.00
	currentCutoff := defaultCurrentCutoff()
	baselineCutoff := defaultBaselineCutoff()

	snapshots := []domain.BalanceSnapshot{
		{ID: "s1", AccountID: "acct-1", UserID: userID, CurrentBalance: &balance, SnapshotAt: time.Now().UTC()},
	}
	previousSnapshots := []domain.BalanceSnapshot{
		{ID: "p1", AccountID: "acct-1", UserID: userID, CurrentBalance: &prevBalance, SnapshotAt: time.Now().UTC().AddDate(0, 0, -30)},
	}
	accounts := []domain.Account{
		{ID: "acct-1", UserID: userID, Name: "Checking", Type: "depository", Status: domain.AccountStatusActive},
	}

	// A short, deliberate delay on each shared repository method: long enough that the second
	// goroutine's call is reliably still in flight (and therefore coalesced by singleflight)
	// before the first goroutine's call returns, short enough to keep the test fast.
	const overlapDelay = 30 * time.Millisecond

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, currentCutoff).
		Run(func(mock.Arguments) { time.Sleep(overlapDelay) }).
		Return(snapshots, nil).Once()
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, baselineCutoff).
		Run(func(mock.Arguments) { time.Sleep(overlapDelay) }).
		Return(previousSnapshots, nil).Once()
	mockAccountRepo.On("GetByUserID", ctx, userID).
		Run(func(mock.Arguments) { time.Sleep(overlapDelay) }).
		Return(accounts, nil).Once()

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	var wg sync.WaitGroup
	wg.Add(2)
	var summaryErr, netWorthErr error
	go func() {
		defer wg.Done()
		_, summaryErr = service.GetBalanceSummary(ctx, userID, nil)
	}()
	go func() {
		defer wg.Done()
		_, netWorthErr = service.GetNetWorth(ctx, userID, nil)
	}()
	wg.Wait()

	require.NoError(t, summaryErr)
	require.NoError(t, netWorthErr)

	// The `.Once()` expectations above already fail loudly (a testify "mock: I don't know what to
	// return" panic) if either shared method is called a SECOND time — AssertExpectations confirms
	// every expectation was satisfied at all (i.e. dedup didn't accidentally drop a call to zero).
	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
	mockSnapshotRepo.AssertNumberOfCalls(t, "GetSnapshotsAsOfForUser", 2) // one per cutoff, NOT one per endpoint
	mockAccountRepo.AssertNumberOfCalls(t, "GetByUserID", 1)              // NOT one per endpoint
}

// TestDashboard_SequentialBalanceSummaryAndNetWorth_NoOverlapMeansNoDedup documents the
// mechanism's honest boundary: singleflight only coalesces calls that are ACTUALLY in flight
// together. Two requests that do not overlap in time each pay for their own repository read —
// this is not a bug, it is what distinguishes "coalesce concurrent duplicate work" from "cache the
// result," which item 1's brief specifically warned against reaching for.
func TestDashboard_SequentialBalanceSummaryAndNetWorth_NoOverlapMeansNoDedup(t *testing.T) {
	ctx := context.Background()
	userID := "u-sequential-no-dedup"

	balance := 1000.00
	prevBalance := 900.00
	currentCutoff := defaultCurrentCutoff()
	baselineCutoff := defaultBaselineCutoff()

	snapshots := []domain.BalanceSnapshot{
		{ID: "s1", AccountID: "acct-1", UserID: userID, CurrentBalance: &balance, SnapshotAt: time.Now().UTC()},
	}
	previousSnapshots := []domain.BalanceSnapshot{
		{ID: "p1", AccountID: "acct-1", UserID: userID, CurrentBalance: &prevBalance, SnapshotAt: time.Now().UTC().AddDate(0, 0, -30)},
	}
	accounts := []domain.Account{
		{ID: "acct-1", UserID: userID, Name: "Checking", Type: "depository", Status: domain.AccountStatusActive},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, currentCutoff).Return(snapshots, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, baselineCutoff).Return(previousSnapshots, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	_, err := service.GetBalanceSummary(ctx, userID, nil)
	require.NoError(t, err)
	_, err = service.GetNetWorth(ctx, userID, nil)
	require.NoError(t, err)

	mockSnapshotRepo.AssertNumberOfCalls(t, "GetSnapshotsAsOfForUser", 4) // 2 endpoints x 2 cutoffs, no overlap to coalesce
	mockAccountRepo.AssertNumberOfCalls(t, "GetByUserID", 2)              // 2 endpoints, no overlap to coalesce
}

// #1569 round 11 fix round 1, item 2 — a cancelled LEADER must not poison a healthy FOLLOWER.
//
// leaderFollowerSnapshotRepo lets the test control exactly which goroutine's Do call becomes the
// singleflight "leader": the FIRST call blocks (signalling `started`, then waiting on `proceed`),
// so any concurrent call for the same key made before `proceed` is closed necessarily joins as a
// "follower" per singleflight's own semantics (the in-flight entry isn't cleared until the
// leader's fn returns). Every call — leader or a later retry — returns ctx.Err() if its OWN ctx is
// already done, modelling what a real DB driver does under a cancelled context.
type leaderFollowerSnapshotRepo struct {
	*MockBalanceSnapshotRepository
	started chan struct{}
	proceed chan struct{}
	calls   int32
	result  []domain.BalanceSnapshot
}

func (r *leaderFollowerSnapshotRepo) GetSnapshotsAsOfForUser(ctx context.Context, userID string, asOf time.Time) ([]domain.BalanceSnapshot, error) {
	if atomic.AddInt32(&r.calls, 1) == 1 {
		close(r.started)
		<-r.proceed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.result, nil
}

// TestDashboard_FollowerSurvivesLeaderContextCancellation is the regression test for item 2: a
// follower whose OWN context is healthy must get a real result, never the leader's spurious
// context.Canceled, even though singleflight itself only runs the repository call once (under the
// leader's context) and hands that one call's outcome to every concurrent waiter.
func TestDashboard_FollowerSurvivesLeaderContextCancellation(t *testing.T) {
	userID := "u-leader-cancel"
	cutoff := defaultCurrentCutoff()
	balance := 500.00
	repo := &leaderFollowerSnapshotRepo{
		MockBalanceSnapshotRepository: new(MockBalanceSnapshotRepository),
		started:                       make(chan struct{}),
		proceed:                       make(chan struct{}),
		result: []domain.BalanceSnapshot{
			{ID: "s1", AccountID: "acct-1", UserID: userID, CurrentBalance: &balance},
		},
	}

	svc, ok := NewDashboardService(repo, new(MockAccountRepository)).(*dashboardService)
	require.True(t, ok, "test needs the concrete type to reach the unexported sf* method directly")

	leaderCtx, leaderCancel := context.WithCancel(context.Background())
	followerCtx := context.Background()

	type outcome struct {
		snapshots []domain.BalanceSnapshot
		err       error
	}
	leaderCh := make(chan outcome, 1)
	followerCh := make(chan outcome, 1)

	go func() {
		snaps, err := svc.sfSnapshotsAsOfForUser(leaderCtx, userID, cutoff)
		leaderCh <- outcome{snaps, err}
	}()
	<-repo.started // the leader's fn is now blocked inside the mocked repository call

	go func() {
		snaps, err := svc.sfSnapshotsAsOfForUser(followerCtx, userID, cutoff)
		followerCh <- outcome{snaps, err}
	}()
	// No deterministic external signal exists for "the follower's Do call has joined the
	// in-flight entry" short of instrumenting singleflight itself; a short sleep is the same
	// forced-overlap technique TestDashboard_ConcurrentBalanceSummaryAndNetWorth_DedupesSharedRepositoryReads
	// already uses above. The leader is still blocked on `proceed` (closed only below), so
	// singleflight's in-flight entry for this key cannot have cleared yet — the follower's Do
	// call, whenever the goroutine scheduler runs it within this window, joins it rather than
	// becoming a second leader.
	time.Sleep(50 * time.Millisecond)

	leaderCancel()      // the leader's OWN context is cancelled — a real AbortController/unmount
	close(repo.proceed) // release the blocked call; it observes the (now cancelled) leader ctx

	leaderOut := <-leaderCh
	followerOut := <-followerCh

	assert.Error(t, leaderOut.err, "the leader's own context really was cancelled — this is its own honest error")
	assert.True(t, errors.Is(leaderOut.err, context.Canceled))

	require.NoError(t, followerOut.err,
		"the follower's context was never cancelled — it must not inherit the leader's spurious error")
	assert.Equal(t, repo.result, followerOut.snapshots)

	assert.Equal(t, int32(2), atomic.LoadInt32(&repo.calls),
		"exactly two repository calls: the shared (leader) attempt, and the follower's solo retry — never a third")
}

// #1569 round 11 fix round 1, item 1 — household singleflight keys must fold in scope.MemberUserIDs.
//
// household_service.go's ResolveScope re-derives MemberUserIDs fresh from the database on every
// request, and analytics_repository.go's householdScopeArgs binds it as a literal SQL parameter
// into every household CTE these reads use — the result genuinely depends on the member set, so
// two concurrent requests naming the SAME household but a DIFFERENT member set (a membership
// change racing a read) must never collapse onto one coalesced result.

func TestHouseholdScopeKeyFragment_DifferentMemberSetsProduceDifferentFragments(t *testing.T) {
	a := &domain.HouseholdScope{HouseholdID: "hh1", MemberUserIDs: []string{"u1", "u2"}}
	b := &domain.HouseholdScope{HouseholdID: "hh1", MemberUserIDs: []string{"u1", "u2", "u3"}}
	assert.NotEqual(t, householdScopeKeyFragment(a), householdScopeKeyFragment(b),
		"a household ID alone must not be enough to key a household read — the member set changed")
}

func TestHouseholdScopeKeyFragment_OrderAndDuplicatesDoNotChangeFragment(t *testing.T) {
	a := &domain.HouseholdScope{HouseholdID: "hh1", MemberUserIDs: []string{"u2", "u1", "u1"}}
	b := &domain.HouseholdScope{HouseholdID: "hh1", MemberUserIDs: []string{"u1", "u2"}}
	assert.Equal(t, householdScopeKeyFragment(a), householdScopeKeyFragment(b),
		"order and incidental duplicates in MemberUserIDs must not fragment an otherwise-identical scope into separate keys")
}

// TestDashboard_ConcurrentHouseholdRequests_DifferentMemberSetsAreNotCoalesced is the behavioural
// (not just unit-level) proof: two concurrent GetBalanceSummary household calls for the SAME
// household but DIFFERENT member sets must each hit the repository — proving they were NOT
// coalesced onto one (wrong, for one of them) shared result. Before the item 1 fix, this test
// would have failed: both calls keyed identically on (userID, householdID, cutoff) and the second
// caller's mock expectation (a DIFFERENT scope pointer, matched by value) would never have been
// consulted, since singleflight would have handed it the first caller's result instead.
func TestDashboard_ConcurrentHouseholdRequests_DifferentMemberSetsAreNotCoalesced(t *testing.T) {
	userID := "u-household-membership-race"
	currentCutoff := defaultCurrentCutoff()
	baselineCutoff := defaultBaselineCutoff()

	scopeBefore := &domain.HouseholdScope{HouseholdID: "hh-race", MemberUserIDs: []string{"u-household-membership-race", "u-other-member"}}
	scopeAfter := &domain.HouseholdScope{HouseholdID: "hh-race", MemberUserIDs: []string{"u-household-membership-race", "u-other-member", "u-new-member"}}

	balance := 1000.00
	prevBalance := 900.00
	snapshotsBefore := []domain.BalanceSnapshot{{ID: "s-before", AccountID: "acct-1", UserID: userID, CurrentBalance: &balance, SnapshotAt: time.Now().UTC()}}
	snapshotsAfter := []domain.BalanceSnapshot{{ID: "s-after", AccountID: "acct-1", UserID: userID, CurrentBalance: &balance, SnapshotAt: time.Now().UTC()}}
	previousSnapshots := []domain.BalanceSnapshot{{ID: "p1", AccountID: "acct-1", UserID: userID, CurrentBalance: &prevBalance, SnapshotAt: time.Now().UTC().AddDate(0, 0, -30)}}
	accounts := []domain.Account{{ID: "acct-1", UserID: userID, Name: "Joint Checking", Type: "depository", Status: domain.AccountStatusActive}}

	const overlapDelay = 30 * time.Millisecond

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)

	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", mock.Anything, userID, scopeBefore, currentCutoff).
		Run(func(mock.Arguments) { time.Sleep(overlapDelay) }).
		Return(snapshotsBefore, nil).Once()
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", mock.Anything, userID, scopeAfter, currentCutoff).
		Run(func(mock.Arguments) { time.Sleep(overlapDelay) }).
		Return(snapshotsAfter, nil).Once()
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", mock.Anything, userID, scopeBefore, baselineCutoff).
		Return(previousSnapshots, nil).Once()
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", mock.Anything, userID, scopeAfter, baselineCutoff).
		Return(previousSnapshots, nil).Once()
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", mock.Anything, userID, scopeBefore, currentCutoff).
		Return(accounts, nil).Once()
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", mock.Anything, userID, scopeAfter, currentCutoff).
		Return(accounts, nil).Once()

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	var wg sync.WaitGroup
	wg.Add(2)
	var errBefore, errAfter error
	go func() {
		defer wg.Done()
		_, errBefore = service.GetBalanceSummary(context.Background(), userID, &domain.DashboardFilters{HouseholdScope: scopeBefore})
	}()
	go func() {
		defer wg.Done()
		_, errAfter = service.GetBalanceSummary(context.Background(), userID, &domain.DashboardFilters{HouseholdScope: scopeAfter})
	}()
	wg.Wait()

	require.NoError(t, errBefore)
	require.NoError(t, errAfter)

	// The `.Once()` expectations above are the real proof: if the two scopes had collapsed onto
	// one singleflight key, one of them would never have been called (a testify panic on an
	// unmatched call) instead of quietly succeeding.
	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}
