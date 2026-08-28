package services

import (
	"context"
	"sync"
	"testing"
	"time"

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
