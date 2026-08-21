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

// TestGetBalanceSummary_PeriodComparison_AppliesAccountTypeFilterToBaseline pins that the 30-day
// comparison baseline is filtered the same way the current total is. Issue #1537 round 9.
//
// THE BUG. The current-balance loop applies BOTH a chain-aware accountMap lookup and the
// AccountTypes filter (dashboard_service.go, "Apply account type filter if specified"). The
// previousBalance loop applied NEITHER — it summed every snapshot it was handed. So with a type
// filter active, changeAmount and changePercent compared a FILTERED current against an UNFILTERED
// baseline, and the reported change was arbitrary: here a real +50% is reported as +150%.
//
// WHY IT SURVIVED. TestGetBalanceSummary_WithAccountTypeFilter, the test whose name says it covers
// this, stubs GetByUserIDAndDateRange to return an EMPTY slice — so the baseline loop never runs and
// the asymmetry is invisible. A filter test that never populates the thing being filtered is the
// same vacuity this branch has found repeatedly; this test exists to close that specific hole, which
// is why it asserts on the baseline rather than on the total.
func TestGetBalanceSummary_PeriodComparison_AppliesAccountTypeFilterToBaseline(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-id"

	curDep, curCredit := 1500.00, -500.00
	prevDep, prevCredit := 1000.00, -400.00

	current := []domain.BalanceSnapshot{
		{ID: "cur-dep", AccountID: "account-1", UserID: userID, CurrentBalance: &curDep},
		{ID: "cur-cred", AccountID: "account-2", UserID: userID, CurrentBalance: &curCredit},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "prev-dep", AccountID: "account-1", UserID: userID, CurrentBalance: &prevDep,
			SnapshotAt: time.Now().AddDate(0, 0, -30)},
		{ID: "prev-cred", AccountID: "account-2", UserID: userID, CurrentBalance: &prevCredit,
			SnapshotAt: time.Now().AddDate(0, 0, -30)},
	}
	accounts := []domain.Account{
		{ID: "account-1", UserID: userID, Name: "Checking", Type: "depository"},
		{ID: "account-2", UserID: userID, Name: "Credit Card", Type: "credit"},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(current, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID,
		mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(previous, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, userID,
		&domain.DashboardFilters{AccountTypes: []string{"depository"}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison, "a populated previous period must produce a comparison")

	assert.Equal(t, 1500.00, result.TotalBalance, "current total must be depository-only")
	assert.Equal(t, 1000.00, result.PeriodComparison.PreviousBalance,
		"the baseline must be depository-only too. Summing the credit account (-400) into it makes "+
			"the baseline 600 and turns a real +50%% into +150%%.")
	assert.InDelta(t, 500.00, result.PeriodComparison.ChangeAmount, 0.001)
	assert.InDelta(t, 50.0, result.PeriodComparison.ChangePercent, 0.001)
}

// TestGetBalanceSummaryHousehold_PeriodComparison_AppliesAccountTypeFilterToBaseline is the household
// twin of the test above. It exists separately because the two code paths had the SAME defect for
// DIFFERENT reasons and fixing one would not have fixed the other: the personal loop at least
// deduped chain keys, while the household loop summed every row with no accountMap lookup at all —
// and a comment above the household snapshot fetch explicitly justified that raw sum (on the grounds
// that only one account map was then needed), so a reader had a stated reason to leave it alone.
//
// Distinguishing current from baseline needs MatchedBy on the asOf argument: both fetches go through
// GetSnapshotsAsOfForHousehold, and the existing household tests stub it with AnythingOfType, which
// returns the same slice for both and so cannot see this bug at all.
func TestGetBalanceSummaryHousehold_PeriodComparison_AppliesAccountTypeFilterToBaseline(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}

	curDep, curCredit := 1000.00, -300.00
	prevDep, prevCredit := 800.00, -200.00
	now := time.Now().UTC()

	current := []domain.BalanceSnapshot{
		{ID: "cur-dep", AccountID: "acct-dep", UserID: "owner-1", CurrentBalance: &curDep, SnapshotAt: now},
		{ID: "cur-cred", AccountID: "acct-cred", UserID: "member-2", CurrentBalance: &curCredit, SnapshotAt: now},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "prev-dep", AccountID: "acct-dep", UserID: "owner-1", CurrentBalance: &prevDep,
			SnapshotAt: now.AddDate(0, 0, -30)},
		{ID: "prev-cred", AccountID: "acct-cred", UserID: "member-2", CurrentBalance: &prevCredit,
			SnapshotAt: now.AddDate(0, 0, -30)},
	}
	accounts := []domain.Account{
		{ID: "acct-dep", UserID: "owner-1", Name: "Owner Checking", Type: "depository"},
		{ID: "acct-cred", UserID: "member-2", Name: "Member Card", Type: "credit"},
	}

	cutoff := now.AddDate(0, 0, -15) // anything between the two fetch points separates them
	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope,
		mock.MatchedBy(func(asOf time.Time) bool { return asOf.After(cutoff) })).Return(current, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope,
		mock.MatchedBy(func(asOf time.Time) bool { return !asOf.After(cutoff) })).Return(previous, nil)
	mockAccountRepo.On("GetForHouseholdScope", ctx, requestingUserID, scope,
		mock.AnythingOfType("time.Time")).Return(accounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, requestingUserID, &domain.DashboardFilters{
		AccountTypes:   []string{"depository"},
		HouseholdScope: scope,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, 1000.00, result.TotalBalance, "current household total must be depository-only")
	assert.Equal(t, 800.00, result.PeriodComparison.PreviousBalance,
		"the household baseline must be depository-only too. Summing the credit card (-200) makes it "+
			"600 and turns a real +25%% into +66%%.")
	assert.InDelta(t, 200.00, result.PeriodComparison.ChangeAmount, 0.001)
	assert.InDelta(t, 25.0, result.PeriodComparison.ChangePercent, 0.001)
}

// TestGetBalanceSummary_PeriodComparison_BaselineIncludesSinceInactiveAccounts pins the OTHER half
// of the period-comparison contract, the half round 9's fix broke. Issue #1537 round 10.
//
// THE DEFECT ROUND 9 INTRODUCED. Round 9 made the baseline apply the current total's chain-aware
// accountMap — but that map is built from GetByUserID, which is active-only
// (`WHERE user_id = $1 AND status = 'active'`), while the snapshot query filters on user_id alone.
// So a snapshot whose account has since gone 'replaced' or 'expired' with no active chain successor
// has no map entry and was silently dropped from the baseline. A $1,000 account disconnected 30 days
// ago therefore produced baseline $0, current $0, and the divide-by-zero guard reported
// change_percent = 0 — "no change" — for what is in truth a $1,000 decrease.
//
// THE PRODUCT RULE. Net worth history includes historical balances (a snapshot is a fact about a
// point in time); the CURRENT total includes only linked/active accounts. The two sides of the
// comparison therefore use deliberately different account universes, which is exactly what makes a
// disconnection surface as a real decrease. The field is PreviousBalance, not "previous balance of
// the accounts that happen to still be active".
//
// This test carries a type filter on purpose: it must hold simultaneously with
// TestGetBalanceSummary_PeriodComparison_AppliesAccountTypeFilterToBaseline above. A baseline that
// includes the since-expired CREDIT account (-400) would satisfy "include inactive history" while
// re-breaking the type filter; one that drops the since-expired DEPOSITORY account ($1,000) would
// satisfy the type filter while re-breaking history. Only 2000 satisfies both.
func TestGetBalanceSummary_PeriodComparison_BaselineIncludesSinceInactiveAccounts(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-inactive-baseline"

	curActive := 1000.00
	prevActive, prevGoneDep, prevGoneCredit := 1000.00, 1000.00, -400.00
	thirtyDaysAgo := time.Now().UTC().AddDate(0, 0, -30)

	current := []domain.BalanceSnapshot{
		{ID: "cur-active", AccountID: "acct-active", UserID: userID, CurrentBalance: &curActive},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "prev-active", AccountID: "acct-active", UserID: userID,
			CurrentBalance: &prevActive, SnapshotAt: thirtyDaysAgo},
		{ID: "prev-gone-dep", AccountID: "acct-gone-dep", UserID: userID,
			CurrentBalance: &prevGoneDep, SnapshotAt: thirtyDaysAgo},
		{ID: "prev-gone-credit", AccountID: "acct-gone-credit", UserID: userID,
			CurrentBalance: &prevGoneCredit, SnapshotAt: thirtyDaysAgo},
	}

	// GetByUserID is active-only — the two disconnected accounts are simply absent.
	activeAccounts := []domain.Account{
		{ID: "acct-active", UserID: userID, Name: "Checking", Type: "depository",
			Status: domain.AccountStatusActive},
	}
	// The all-statuses universe is what supplies the since-inactive accounts' TYPE, so the
	// type filter can still be applied to them. 'expired' is what ExpireAccountsByFingerprint
	// writes for a fully-disconnected chain, so it is the status this defect is really about.
	allStatusAccounts := append([]domain.Account{}, activeAccounts...)
	allStatusAccounts = append(allStatusAccounts,
		domain.Account{ID: "acct-gone-dep", UserID: userID, Name: "Old Checking", Type: "depository",
			Status: domain.AccountStatusExpired},
		domain.Account{ID: "acct-gone-credit", UserID: userID, Name: "Old Card", Type: "credit",
			Status: domain.AccountStatusExpired},
	)

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(current, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeAccounts, nil)
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID,
		mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(previous, nil)
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(func(opts repository.AccountFilterOptions) bool {
			// AllStatuses is the only option that reaches 'expired'; IncludeReplaced would
			// produce status IN ('active','replaced') and miss it entirely. IncludeHidden
			// must be true to match GetByUserID, which does not filter is_hidden at all.
			return opts.AllStatuses && opts.IncludeHidden && !opts.ReplacedOnly
		})).Return(allStatusAccounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, userID,
		&domain.DashboardFilters{AccountTypes: []string{"depository"}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, 1000.00, result.TotalBalance, "current total stays active-only")
	assert.Equal(t, 2000.00, result.PeriodComparison.PreviousBalance,
		"the baseline is a HISTORICAL figure: the since-expired depository account's $1,000 was "+
			"really there 30 days ago and must be counted. Dropping it makes the baseline 1000 and "+
			"reports 'no change' for a 50%% decrease. Counting the since-expired CREDIT account "+
			"too would make it 1600 and re-break the type filter.")
	assert.InDelta(t, -1000.00, result.PeriodComparison.ChangeAmount, 0.001)
	assert.InDelta(t, -50.0, result.PeriodComparison.ChangePercent, 0.001)
}

// TestGetBalanceSummaryHousehold_PeriodComparison_BaselineIncludesSinceInactiveAccounts is the
// household twin. GetForHouseholdScope filters `WHERE a.status = 'active'` REGARDLESS of its asOf
// argument, so passing the previous cutoff to it does not help by itself — the household path needs
// its own all-statuses account source. Issue #1537 round 10.
func TestGetBalanceSummaryHousehold_PeriodComparison_BaselineIncludesSinceInactiveAccounts(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}

	now := time.Now().UTC()
	curActive := 1000.00
	prevActive, prevGoneDep, prevGoneCredit := 1000.00, 1000.00, -400.00

	current := []domain.BalanceSnapshot{
		{ID: "cur-active", AccountID: "acct-active", UserID: "owner-1", CurrentBalance: &curActive, SnapshotAt: now},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "prev-active", AccountID: "acct-active", UserID: "owner-1",
			CurrentBalance: &prevActive, SnapshotAt: now.AddDate(0, 0, -30)},
		{ID: "prev-gone-dep", AccountID: "acct-gone-dep", UserID: "member-2",
			CurrentBalance: &prevGoneDep, SnapshotAt: now.AddDate(0, 0, -30)},
		{ID: "prev-gone-credit", AccountID: "acct-gone-credit", UserID: "member-2",
			CurrentBalance: &prevGoneCredit, SnapshotAt: now.AddDate(0, 0, -30)},
	}
	activeAccounts := []domain.Account{
		{ID: "acct-active", UserID: "owner-1", Name: "Owner Checking", Type: "depository",
			Status: domain.AccountStatusActive},
	}
	allStatusAccounts := append([]domain.Account{}, activeAccounts...)
	allStatusAccounts = append(allStatusAccounts,
		domain.Account{ID: "acct-gone-dep", UserID: "member-2", Name: "Member Old Checking",
			Type: "depository", Status: domain.AccountStatusExpired},
		domain.Account{ID: "acct-gone-credit", UserID: "member-2", Name: "Member Old Card",
			Type: "credit", Status: domain.AccountStatusExpired},
	)

	cutoff := now.AddDate(0, 0, -15)
	isCurrent := func(asOf time.Time) bool { return asOf.After(cutoff) }
	isPrevious := func(asOf time.Time) bool { return !asOf.After(cutoff) }

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope,
		mock.MatchedBy(isCurrent)).Return(current, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope,
		mock.MatchedBy(isPrevious)).Return(previous, nil)
	mockAccountRepo.On("GetForHouseholdScope", ctx, requestingUserID, scope,
		mock.MatchedBy(isCurrent)).Return(activeAccounts, nil)
	// The all-statuses household map is resolved AT THE PREVIOUS CUTOFF, matching the
	// baseline snapshot fetch's asOf — the joint-account fingerprint winner must be picked
	// with the same bound on both sides (Issue #1369 gap-closure G1).
	mockAccountRepo.On("GetForHouseholdScopeAllStatuses", ctx, requestingUserID, scope,
		mock.MatchedBy(isPrevious)).Return(allStatusAccounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, requestingUserID, &domain.DashboardFilters{
		AccountTypes:   []string{"depository"},
		HouseholdScope: scope,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, 1000.00, result.TotalBalance, "current household total stays active-only")
	assert.Equal(t, 2000.00, result.PeriodComparison.PreviousBalance,
		"the household baseline must include the member's since-expired depository account "+
			"($1,000) and still exclude their since-expired credit card (-400)")
	assert.InDelta(t, -1000.00, result.PeriodComparison.ChangeAmount, 0.001)
	assert.InDelta(t, -50.0, result.PeriodComparison.ChangePercent, 0.001)
}

// TestGetNetWorth_PeriodComparison_BaselineIncludesSinceInactiveAccounts is the NET WORTH twin of
// TestGetBalanceSummary_PeriodComparison_BaselineIncludesSinceInactiveAccounts. Issue #1537.
//
// Round 10 fixed the historical-universe defect in GetBalanceSummary and getBalanceSummaryHousehold
// and built two instruments to do it (historicalAccountMap and GetForHouseholdScopeAllStatuses) —
// and then did not apply either to the two net-worth paths, which have the identical defect. This
// test covers the personal one: GetNetWorth's baseline loop reuses the accountMap built from
// GetByUserID (`status = 'active'`, and a "now"-shaped map at that) and `continue`s on a miss, so a
// chain with no active member left 30 days ago contributes nothing and PreviousNetWorth /
// PreviousAssets / PreviousLiabilities all understate.
//
// WHY IT IS USER-VISIBLE, not merely internally inconsistent: GetAggregatedHistory
// (balance_snapshot_repository.go) LEFT JOINs accounts with NO status predicate, so the net-worth
// history CHART already counts the since-expired account. Unfixed, the chart draws the drop while
// the KPI tile above it reads 0.00% — two widgets on one dashboard disagreeing about the same 30
// days.
func TestGetNetWorth_PeriodComparison_BaselineIncludesSinceInactiveAccounts(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-nw-inactive-baseline"

	curActive := 1000.00
	prevActive, prevGoneDep, prevGoneCredit := 1000.00, 1000.00, -400.00
	thirtyDaysAgo := time.Now().UTC().AddDate(0, 0, -30)

	current := []domain.BalanceSnapshot{
		{ID: "cur-active", AccountID: "acct-active", UserID: userID,
			CurrentBalance: &curActive, SnapshotAt: time.Now().UTC()},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "prev-active", AccountID: "acct-active", UserID: userID,
			CurrentBalance: &prevActive, SnapshotAt: thirtyDaysAgo},
		{ID: "prev-gone-dep", AccountID: "acct-gone-dep", UserID: userID,
			CurrentBalance: &prevGoneDep, SnapshotAt: thirtyDaysAgo},
		{ID: "prev-gone-credit", AccountID: "acct-gone-credit", UserID: userID,
			CurrentBalance: &prevGoneCredit, SnapshotAt: thirtyDaysAgo},
	}

	// GetByUserID is active-only — the two disconnected accounts are simply absent.
	activeAccounts := []domain.Account{
		{ID: "acct-active", UserID: userID, Name: "Checking", Type: "depository",
			Status: domain.AccountStatusActive},
	}
	// The all-statuses universe supplies the since-inactive accounts' TYPE only, which is what
	// decides assets vs liabilities. The BALANCE always comes from the snapshot row.
	allStatusAccounts := append([]domain.Account{}, activeAccounts...)
	allStatusAccounts = append(allStatusAccounts,
		domain.Account{ID: "acct-gone-dep", UserID: userID, Name: "Old Checking", Type: "depository",
			Status: domain.AccountStatusExpired},
		domain.Account{ID: "acct-gone-credit", UserID: userID, Name: "Old Card", Type: "credit",
			Status: domain.AccountStatusExpired},
	)

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(current, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeAccounts, nil)
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID,
		mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(previous, nil)
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(func(opts repository.AccountFilterOptions) bool {
			return opts.AllStatuses && opts.IncludeHidden && !opts.ReplacedOnly
		})).Return(allStatusAccounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, userID, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, 1000.00, result.NetWorth, "the CURRENT figure stays active-only")
	assert.Equal(t, 2000.00, result.PeriodComparison.PreviousAssets,
		"the since-expired depository account's $1,000 was really there 30 days ago")
	assert.Equal(t, 400.00, result.PeriodComparison.PreviousLiabilities,
		"the since-expired card's $400 balance was really owed 30 days ago")
	assert.Equal(t, 1600.00, result.PeriodComparison.PreviousNetWorth)
	assert.InDelta(t, -600.00, result.PeriodComparison.NetWorthChange, 0.001,
		"dropping the disconnected chain reports a $600 decline as $0 change")
	assert.InDelta(t, -37.5, result.PeriodComparison.NetWorthChangePercent, 0.001)
}

// TestGetNetWorthHousehold_PeriodComparison_BaselineIncludesSinceInactiveAccounts is the household
// twin. getNetWorthHousehold already resolves its baseline map at the CORRECT asOf
// (GetForHouseholdScope(..., previousCutoff), per #1369 gap-closure G1) — but that method filters
// `a.status = 'active'` REGARDLESS of its asOf, so the right point in time over the wrong status
// universe still drops a member's since-expired account. Issue #1537.
func TestGetNetWorthHousehold_PeriodComparison_BaselineIncludesSinceInactiveAccounts(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}

	now := time.Now().UTC()
	curActive := 1000.00
	prevActive, prevGoneDep, prevGoneCredit := 1000.00, 1000.00, -400.00

	current := []domain.BalanceSnapshot{
		{ID: "cur-active", AccountID: "acct-active", UserID: "owner-1",
			CurrentBalance: &curActive, SnapshotAt: now},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "prev-active", AccountID: "acct-active", UserID: "owner-1",
			CurrentBalance: &prevActive, SnapshotAt: now.AddDate(0, 0, -30)},
		{ID: "prev-gone-dep", AccountID: "acct-gone-dep", UserID: "member-2",
			CurrentBalance: &prevGoneDep, SnapshotAt: now.AddDate(0, 0, -30)},
		{ID: "prev-gone-credit", AccountID: "acct-gone-credit", UserID: "member-2",
			CurrentBalance: &prevGoneCredit, SnapshotAt: now.AddDate(0, 0, -30)},
	}
	activeAccounts := []domain.Account{
		{ID: "acct-active", UserID: "owner-1", Name: "Owner Checking", Type: "depository",
			Status: domain.AccountStatusActive},
	}
	allStatusAccounts := append([]domain.Account{}, activeAccounts...)
	allStatusAccounts = append(allStatusAccounts,
		domain.Account{ID: "acct-gone-dep", UserID: "member-2", Name: "Member Old Checking",
			Type: "depository", Status: domain.AccountStatusExpired},
		domain.Account{ID: "acct-gone-credit", UserID: "member-2", Name: "Member Old Card",
			Type: "credit", Status: domain.AccountStatusExpired},
	)

	// Distinguish the "now" call from the "~30-days-ago" call on time.Since, exactly as
	// TestGetNetWorth_Household_PreviousPeriod_JointAccountResolvesViaMatchingAsOf does — an
	// AnythingOfType stub returns the same slice for both and cannot see this defect at all.
	isCurrent := func(tm time.Time) bool { return time.Since(tm) < time.Minute }
	isPrevious := func(tm time.Time) bool {
		d := time.Since(tm)
		return d > 29*24*time.Hour && d < 31*24*time.Hour
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope,
		mock.MatchedBy(isCurrent)).Return(current, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope,
		mock.MatchedBy(isPrevious)).Return(previous, nil)
	// Active-only at BOTH cutoffs — that is what GetForHouseholdScope really returns.
	mockAccountRepo.On("GetForHouseholdScope", ctx, requestingUserID, scope,
		mock.MatchedBy(isCurrent)).Return(activeAccounts, nil)
	mockAccountRepo.On("GetForHouseholdScope", ctx, requestingUserID, scope,
		mock.MatchedBy(isPrevious)).Return(activeAccounts, nil)
	mockAccountRepo.On("GetForHouseholdScopeAllStatuses", ctx, requestingUserID, scope,
		mock.MatchedBy(isPrevious)).Return(allStatusAccounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, requestingUserID,
		&domain.DashboardFilters{HouseholdScope: scope})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, 1000.00, result.NetWorth, "the CURRENT household figure stays active-only")
	assert.Equal(t, 2000.00, result.PeriodComparison.PreviousAssets,
		"the member's since-expired depository account's $1,000 must be counted")
	assert.Equal(t, 400.00, result.PeriodComparison.PreviousLiabilities,
		"the member's since-expired card's $400 must be counted")
	assert.Equal(t, 1600.00, result.PeriodComparison.PreviousNetWorth)
	assert.InDelta(t, -600.00, result.PeriodComparison.NetWorthChange, 0.001)
	assert.InDelta(t, -37.5, result.PeriodComparison.NetWorthChangePercent, 0.001)
}

// TestGetNetWorth_PeriodComparison_AppliesAccountTypeFilterToBaseline is the NET WORTH twin of
// TestGetBalanceSummary_PeriodComparison_AppliesAccountTypeFilterToBaseline. Issue #1537.
//
// Round 9 fixed the FILTER axis on the two balance-summary baselines and did not touch the two
// net-worth baselines, which have it identically: GetNetWorth's current loop applies
// filters.AccountTypes, its baseline loop applies nothing. So a type-filtered net-worth comparison
// measures a FILTERED current against an UNFILTERED baseline and the reported change is arbitrary:
// here a real +25% is reported as +150%.
//
// This test deliberately keeps every account ACTIVE so it is independent of the universe axis — it
// must fail for the filter reason alone, and no all-statuses read may be triggered.
func TestGetNetWorth_PeriodComparison_AppliesAccountTypeFilterToBaseline(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-nw-filtered-baseline"

	curDep, curCredit := 1000.00, -500.00
	prevDep, prevCredit := 800.00, -400.00
	thirtyDaysAgo := time.Now().UTC().AddDate(0, 0, -30)

	current := []domain.BalanceSnapshot{
		{ID: "cur-dep", AccountID: "acct-dep", UserID: userID,
			CurrentBalance: &curDep, SnapshotAt: time.Now().UTC()},
		{ID: "cur-cred", AccountID: "acct-cred", UserID: userID,
			CurrentBalance: &curCredit, SnapshotAt: time.Now().UTC()},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "prev-dep", AccountID: "acct-dep", UserID: userID,
			CurrentBalance: &prevDep, SnapshotAt: thirtyDaysAgo},
		{ID: "prev-cred", AccountID: "acct-cred", UserID: userID,
			CurrentBalance: &prevCredit, SnapshotAt: thirtyDaysAgo},
	}
	accounts := []domain.Account{
		{ID: "acct-dep", UserID: userID, Name: "Checking", Type: "depository",
			Status: domain.AccountStatusActive},
		{ID: "acct-cred", UserID: userID, Name: "Credit Card", Type: "credit",
			Status: domain.AccountStatusActive},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetLatestByUserID", ctx, userID).Return(current, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetByUserIDAndDateRange", ctx, userID,
		mock.AnythingOfType("time.Time"), mock.AnythingOfType("time.Time")).Return(previous, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, userID,
		&domain.DashboardFilters{AccountTypes: []string{"depository"}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, 1000.00, result.NetWorth, "the current figure is depository-only")
	assert.Equal(t, 0.00, result.TotalLiabilities, "the filtered-out card must not reach the current side")
	assert.Equal(t, 800.00, result.PeriodComparison.PreviousAssets, "the baseline is depository-only too")
	assert.Equal(t, 0.00, result.PeriodComparison.PreviousLiabilities,
		"counting the filtered-out card's $400 here makes the baseline net worth 400 and turns a "+
			"real +25%% into +150%%")
	assert.Equal(t, 800.00, result.PeriodComparison.PreviousNetWorth)
	assert.InDelta(t, 200.00, result.PeriodComparison.NetWorthChange, 0.001)
	assert.InDelta(t, 25.0, result.PeriodComparison.NetWorthChangePercent, 0.001)

	// The universe axis must not be dragged in: every account here is active, so the lazy
	// all-statuses gate must not fire.
	mockAccountRepo.AssertNotCalled(t, "GetAccountsWithInstitutionFiltered",
		mock.Anything, mock.Anything, mock.Anything)
}

// TestGetNetWorthHousehold_PeriodComparison_AppliesAccountTypeFilterToBaseline is the household twin.
// The gap is present in BOTH net-worth paths — the household baseline loop applies no
// filters.AccountTypes either, even though its own current loop does. Issue #1537.
func TestGetNetWorthHousehold_PeriodComparison_AppliesAccountTypeFilterToBaseline(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}

	now := time.Now().UTC()
	curDep, curCredit := 1000.00, -500.00
	prevDep, prevCredit := 800.00, -400.00

	current := []domain.BalanceSnapshot{
		{ID: "cur-dep", AccountID: "acct-dep", UserID: "owner-1", CurrentBalance: &curDep, SnapshotAt: now},
		{ID: "cur-cred", AccountID: "acct-cred", UserID: "member-2", CurrentBalance: &curCredit, SnapshotAt: now},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "prev-dep", AccountID: "acct-dep", UserID: "owner-1",
			CurrentBalance: &prevDep, SnapshotAt: now.AddDate(0, 0, -30)},
		{ID: "prev-cred", AccountID: "acct-cred", UserID: "member-2",
			CurrentBalance: &prevCredit, SnapshotAt: now.AddDate(0, 0, -30)},
	}
	accounts := []domain.Account{
		{ID: "acct-dep", UserID: "owner-1", Name: "Owner Checking", Type: "depository",
			Status: domain.AccountStatusActive},
		{ID: "acct-cred", UserID: "member-2", Name: "Member Card", Type: "credit",
			Status: domain.AccountStatusActive},
	}

	isCurrent := func(tm time.Time) bool { return time.Since(tm) < time.Minute }
	isPrevious := func(tm time.Time) bool {
		d := time.Since(tm)
		return d > 29*24*time.Hour && d < 31*24*time.Hour
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope,
		mock.MatchedBy(isCurrent)).Return(current, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope,
		mock.MatchedBy(isPrevious)).Return(previous, nil)
	mockAccountRepo.On("GetForHouseholdScope", ctx, requestingUserID, scope,
		mock.MatchedBy(isCurrent)).Return(accounts, nil)
	mockAccountRepo.On("GetForHouseholdScope", ctx, requestingUserID, scope,
		mock.MatchedBy(isPrevious)).Return(accounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, requestingUserID, &domain.DashboardFilters{
		AccountTypes:   []string{"depository"},
		HouseholdScope: scope,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, 1000.00, result.NetWorth, "the current household figure is depository-only")
	assert.Equal(t, 0.00, result.TotalLiabilities)
	assert.Equal(t, 800.00, result.PeriodComparison.PreviousAssets)
	assert.Equal(t, 0.00, result.PeriodComparison.PreviousLiabilities,
		"the member's filtered-out card must not enter the household baseline either")
	assert.Equal(t, 800.00, result.PeriodComparison.PreviousNetWorth)
	assert.InDelta(t, 200.00, result.PeriodComparison.NetWorthChange, 0.001)
	assert.InDelta(t, 25.0, result.PeriodComparison.NetWorthChangePercent, 0.001)

	mockAccountRepo.AssertNotCalled(t, "GetForHouseholdScopeAllStatuses",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
