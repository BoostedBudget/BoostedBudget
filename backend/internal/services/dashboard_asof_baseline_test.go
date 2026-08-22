package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// ─── #1569: the comparison baseline asks for RECORDED liveness, not every status ─────────────
//
// #1537 built the baseline's historical account universe out of AllStatuses — "every account that
// exists in any status today". That was the best approximation available then, and it is wrong in
// one direction the tests of that round could not see: an account OPENED AFTER the baseline
// instant is in the all-statuses universe too. It only failed to corrupt the number because the
// balance always comes from a snapshot, and an account with no snapshot before the cutoff
// contributes nothing. That is luck, not a guarantee — the universe is still a lie, and any future
// read that consults it for anything but a type lookup inherits the lie.
//
// account_status_history (migration 000151) records exactly when each account was live, so the
// baseline can now ask the real question. These tests pin the REQUEST shape — that the service
// asks for liveness at the baseline cutoff, with no all-statuses fallback left behind. The
// end-to-end proof that the predicate selects the right rows is the integration suite, which runs
// it against a real table.

// asOfBaselineOpts matches the exact AccountFilterOptions the personal baseline must send.
func asOfBaselineOpts(wantAsOf time.Time) func(repository.AccountFilterOptions) bool {
	return func(opts repository.AccountFilterOptions) bool {
		return opts.AsOf != nil && opts.AsOf.Equal(wantAsOf) &&
			opts.IncludeHidden && !opts.AllStatuses && !opts.ReplacedOnly && !opts.IncludeReplaced
	}
}

// TestGetBalanceSummary_Baseline_RequestsLivenessAtBaselineCutoff pins that the personal
// balance-summary baseline resolves its account universe with AsOf = the SAME cutoff its baseline
// snapshot read used, and no longer with AllStatuses. A mismatch between those two instants is the
// #1369 G1 defect in personal clothing: the snapshots would come from one day and the universe
// that interprets them from another.
func TestGetBalanceSummary_Baseline_RequestsLivenessAtBaselineCutoff(t *testing.T) {
	ctx := context.Background()
	userID := "u-asof-summary"

	cur, prevActive, prevGone := 1000.00, 1000.00, 1000.00
	thirtyDaysAgo := time.Now().UTC().AddDate(0, 0, -30)

	current := []domain.BalanceSnapshot{
		{ID: "cur", AccountID: "acct-active", UserID: userID, CurrentBalance: &cur},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "p1", AccountID: "acct-active", UserID: userID, CurrentBalance: &prevActive, SnapshotAt: thirtyDaysAgo},
		{ID: "p2", AccountID: "acct-gone", UserID: userID, CurrentBalance: &prevGone, SnapshotAt: thirtyDaysAgo},
	}
	activeAccounts := []domain.Account{
		{ID: "acct-active", UserID: userID, Name: "Checking", Type: "depository", Status: domain.AccountStatusActive},
	}
	// What the as-of read returns: the account that was LIVE at the cutoff, including the one
	// since removed. An account opened after the cutoff is simply not in this result — that
	// exclusion is the repository's job and is proven against a real table in the integration suite.
	liveAtCutoff := append([]domain.Account{}, activeAccounts...)
	liveAtCutoff = append(liveAtCutoff,
		domain.Account{ID: "acct-gone", UserID: userID, Name: "Old Checking", Type: "depository",
			Status: domain.AccountStatusRemoved})

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(current, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return(previous, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeAccounts, nil)
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(asOfBaselineOpts(defaultBaselineCutoff()))).Return(liveAtCutoff, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)
	result, err := service.GetBalanceSummary(ctx, userID, nil)
	require.NoError(t, err)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, 1000.00, result.TotalBalance, "the removed account's frozen snapshot stays out of the CURRENT total")
	assert.Equal(t, 2000.00, result.PeriodComparison.PreviousBalance)
	assert.InDelta(t, -50.0, result.PeriodComparison.ChangePercent, 0.001)
	mockAccountRepo.AssertExpectations(t)
}

// TestGetNetWorth_Baseline_RequestsLivenessAtBaselineCutoff is the net-worth twin.
func TestGetNetWorth_Baseline_RequestsLivenessAtBaselineCutoff(t *testing.T) {
	ctx := context.Background()
	userID := "u-asof-networth"

	cur, prevActive, prevGone := 1000.00, 1000.00, 1000.00
	thirtyDaysAgo := time.Now().UTC().AddDate(0, 0, -30)

	current := []domain.BalanceSnapshot{
		{ID: "cur", AccountID: "acct-active", UserID: userID, CurrentBalance: &cur},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "p1", AccountID: "acct-active", UserID: userID, CurrentBalance: &prevActive, SnapshotAt: thirtyDaysAgo},
		{ID: "p2", AccountID: "acct-gone", UserID: userID, CurrentBalance: &prevGone, SnapshotAt: thirtyDaysAgo},
	}
	activeAccounts := []domain.Account{
		{ID: "acct-active", UserID: userID, Name: "Checking", Type: "depository", Status: domain.AccountStatusActive},
	}
	liveAtCutoff := append([]domain.Account{}, activeAccounts...)
	liveAtCutoff = append(liveAtCutoff,
		domain.Account{ID: "acct-gone", UserID: userID, Name: "Old Checking", Type: "depository",
			Status: domain.AccountStatusRemoved})

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(current, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return(previous, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeAccounts, nil)
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(asOfBaselineOpts(defaultBaselineCutoff()))).Return(liveAtCutoff, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)
	result, err := service.GetNetWorth(ctx, userID, nil)
	require.NoError(t, err)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, 1000.00, result.NetWorth)
	assert.Equal(t, 2000.00, result.PeriodComparison.PreviousNetWorth)
	assert.InDelta(t, -1000.00, result.PeriodComparison.NetWorthChange, 0.001)
	assert.InDelta(t, -50.0, result.PeriodComparison.NetWorthChangePercent, 0.001)
	mockAccountRepo.AssertExpectations(t)
}

// TestGetNetWorth_CurrentSide_ExcludesRetainedRemovedAccount is the guard that must NOT be relaxed
// while fixing the baseline. Task 2 of #1569 stopped DELETING disconnected accounts, so a removed
// account now keeps its last balance snapshot forever. GetNetWorth's account map comes from
// GetByUserID (active-only) and skips any snapshot absent from it — that map is the sole thing
// keeping that frozen snapshot out of CURRENT net worth. Widen it to the historical universe and
// current net worth becomes "old bank frozen + new bank", permanently and silently.
//
// The two universes are deliberately different: history is resolved as-of a date, current is
// resolved from live accounts. This test exists so a future "make it consistent" refactor fails
// here instead of in production.
func TestGetNetWorth_CurrentSide_ExcludesRetainedRemovedAccount(t *testing.T) {
	ctx := context.Background()
	userID := "u-retained-removed"

	live, frozen := 250.00, 9999.00
	// Both snapshots are CURRENT (the removed account's last one was never deleted, and
	// GetSnapshotsAsOfForUser returns the latest at or before the cutoff regardless of status).
	current := []domain.BalanceSnapshot{
		{ID: "cur-live", AccountID: "acct-live", UserID: userID, CurrentBalance: &live},
		{ID: "cur-frozen", AccountID: "acct-removed", UserID: userID, CurrentBalance: &frozen},
	}
	activeAccounts := []domain.Account{
		{ID: "acct-live", UserID: userID, Name: "New Bank", Type: "depository", Status: domain.AccountStatusActive},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(current, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return([]domain.BalanceSnapshot{}, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeAccounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)
	result, err := service.GetNetWorth(ctx, userID, nil)
	require.NoError(t, err)

	assert.Equal(t, 250.00, result.NetWorth,
		"the retained removed account's frozen $9,999 must not appear in CURRENT net worth")
	assert.Equal(t, 250.00, result.TotalAssets)

	// And the current side must not have consulted the historical universe at all.
	mockAccountRepo.AssertNotCalled(t, "GetAccountsWithInstitutionFiltered",
		mock.Anything, mock.Anything, mock.Anything)
}

// TestGetBalanceSummaryHousehold_Baseline_RequestsLivenessAtBaselineCutoff pins the household
// balance-summary baseline on GetForHouseholdScopeLiveAsOf, at the baseline cutoff.
func TestGetBalanceSummaryHousehold_Baseline_RequestsLivenessAtBaselineCutoff(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}

	now := time.Now().UTC()
	cur, prevActive, prevGone := 1000.00, 1000.00, 1000.00
	current := []domain.BalanceSnapshot{
		{ID: "cur", AccountID: "acct-active", UserID: "owner-1", CurrentBalance: &cur, SnapshotAt: now},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "p1", AccountID: "acct-active", UserID: "owner-1", CurrentBalance: &prevActive, SnapshotAt: now.AddDate(0, 0, -30)},
		{ID: "p2", AccountID: "acct-gone", UserID: "member-2", CurrentBalance: &prevGone, SnapshotAt: now.AddDate(0, 0, -30)},
	}
	activeAccounts := []domain.Account{
		{ID: "acct-active", UserID: "owner-1", Name: "Owner Checking", Type: "depository", Status: domain.AccountStatusActive},
	}
	liveAtCutoff := append([]domain.Account{}, activeAccounts...)
	liveAtCutoff = append(liveAtCutoff,
		domain.Account{ID: "acct-gone", UserID: "member-2", Name: "Member Old Checking",
			Type: "depository", Status: domain.AccountStatusRemoved})

	cutoff := now.AddDate(0, 0, -15)
	isCurrent := func(asOf time.Time) bool { return asOf.After(cutoff) }
	isPrevious := func(asOf time.Time) bool { return !asOf.After(cutoff) }

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope, mock.MatchedBy(isCurrent)).Return(current, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope, mock.MatchedBy(isPrevious)).Return(previous, nil)
	mockAccountRepo.On("GetForHouseholdScope", ctx, requestingUserID, scope, mock.MatchedBy(isCurrent)).Return(activeAccounts, nil)
	mockAccountRepo.On("GetForHouseholdScopeLiveAsOf", ctx, requestingUserID, scope, mock.MatchedBy(isPrevious)).Return(liveAtCutoff, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)
	result, err := service.GetBalanceSummary(ctx, requestingUserID, &domain.DashboardFilters{HouseholdScope: scope})
	require.NoError(t, err)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, 1000.00, result.TotalBalance)
	assert.Equal(t, 2000.00, result.PeriodComparison.PreviousBalance)
	assert.InDelta(t, -50.0, result.PeriodComparison.ChangePercent, 0.001)
	mockAccountRepo.AssertNotCalled(t, "GetForHouseholdScopeAllStatuses",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockAccountRepo.AssertExpectations(t)
}

// TestGetNetWorthHousehold_Baseline_RequestsLivenessAtBaselineCutoff is the household net-worth twin.
func TestGetNetWorthHousehold_Baseline_RequestsLivenessAtBaselineCutoff(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}

	now := time.Now().UTC()
	cur, prevActive, prevGone := 1000.00, 1000.00, 1000.00
	current := []domain.BalanceSnapshot{
		{ID: "cur", AccountID: "acct-active", UserID: "owner-1", CurrentBalance: &cur, SnapshotAt: now},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "p1", AccountID: "acct-active", UserID: "owner-1", CurrentBalance: &prevActive, SnapshotAt: now.AddDate(0, 0, -30)},
		{ID: "p2", AccountID: "acct-gone", UserID: "member-2", CurrentBalance: &prevGone, SnapshotAt: now.AddDate(0, 0, -30)},
	}
	activeAccounts := []domain.Account{
		{ID: "acct-active", UserID: "owner-1", Name: "Owner Checking", Type: "depository", Status: domain.AccountStatusActive},
	}
	liveAtCutoff := append([]domain.Account{}, activeAccounts...)
	liveAtCutoff = append(liveAtCutoff,
		domain.Account{ID: "acct-gone", UserID: "member-2", Name: "Member Old Checking",
			Type: "depository", Status: domain.AccountStatusRemoved})

	cutoff := now.AddDate(0, 0, -15)
	isCurrent := func(asOf time.Time) bool { return asOf.After(cutoff) }
	isPrevious := func(asOf time.Time) bool { return !asOf.After(cutoff) }

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope, mock.MatchedBy(isCurrent)).Return(current, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope, mock.MatchedBy(isPrevious)).Return(previous, nil)
	mockAccountRepo.On("GetForHouseholdScope", ctx, requestingUserID, scope, mock.MatchedBy(isCurrent)).Return(activeAccounts, nil)
	mockAccountRepo.On("GetForHouseholdScope", ctx, requestingUserID, scope, mock.MatchedBy(isPrevious)).Return(activeAccounts, nil)
	mockAccountRepo.On("GetForHouseholdScopeLiveAsOf", ctx, requestingUserID, scope, mock.MatchedBy(isPrevious)).Return(liveAtCutoff, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)
	result, err := service.GetNetWorth(ctx, requestingUserID, &domain.DashboardFilters{HouseholdScope: scope})
	require.NoError(t, err)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, 1000.00, result.NetWorth)
	assert.Equal(t, 2000.00, result.PeriodComparison.PreviousNetWorth)
	assert.InDelta(t, -50.0, result.PeriodComparison.NetWorthChangePercent, 0.001)
	mockAccountRepo.AssertNotCalled(t, "GetForHouseholdScopeAllStatuses",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockAccountRepo.AssertExpectations(t)
}
