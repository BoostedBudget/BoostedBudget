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
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(current, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return(previous, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, userID,
		&domain.DashboardFilters{AccountTypes: []string{"depository"}})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison, "a populated previous period must produce a comparison")

	assert.Equal(t, 1500.00, result.TotalBalance, "current total must be depository-only")
	assert.Equal(t, 1000.00, result.PeriodComparison.PreviousBalance,
		"the baseline must be depository-only too. Including the raw -400 credit overpayment "+
			"would raise the signed baseline to 1400 and understate the real +50%% change.")
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
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope,
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
		"the household baseline must be depository-only too. Including the raw -200 credit overpayment "+
			"would raise the signed baseline to 1000 and hide the real +25%% change.")
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
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(current, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeAccounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return(previous, nil)
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(func(opts repository.AccountFilterOptions) bool {
			// #1569: AsOf at the BASELINE cutoff, not AllStatuses. Both reach 'expired' (which
			// IncludeReplaced would miss, producing status IN ('active','replaced')), but only
			// AsOf answers the question actually being asked — "was this account live on that
			// day" — and only AsOf also excludes accounts opened since. Pinning the instant is
			// the point: an as-of read at the WRONG instant is the same lie in a new place.
			// IncludeHidden must be true to match GetByUserID, which does not filter is_hidden.
			return opts.AsOf != nil && opts.AsOf.Equal(defaultBaselineCutoff()) &&
				opts.IncludeHidden && !opts.ReplacedOnly && !opts.AllStatuses
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
			"too would make it 2400 (the raw -400 is an overpayment) and re-break the type filter.")
	assert.InDelta(t, -1000.00, result.PeriodComparison.ChangeAmount, 0.001)
	assert.InDelta(t, -50.0, result.PeriodComparison.ChangePercent, 0.001)
}

// TestGetBalanceSummaryHousehold_PeriodComparison_BaselineIncludesSinceInactiveAccounts is the
// household twin. GetForHouseholdScopeActiveNow filters `WHERE a.status = 'active'` REGARDLESS of its asOf
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
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope,
		mock.MatchedBy(isCurrent)).Return(activeAccounts, nil)
	// The historical household map is resolved AT THE PREVIOUS CUTOFF, matching the
	// baseline snapshot fetch's asOf — the joint-account fingerprint winner must be picked
	// with the same bound on both sides (Issue #1369 gap-closure G1), and since #1569 that
	// same instant is what decides which accounts were live at all.
	mockAccountRepo.On("GetForHouseholdScopeLiveAsOf", ctx, requestingUserID, scope,
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
// and built two instruments to do it (historicalAccountMap and an all-statuses household read,
// since superseded by GetForHouseholdScopeLiveAsOf) —
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
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(current, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeAccounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return(previous, nil)
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(func(opts repository.AccountFilterOptions) bool {
			return opts.AsOf != nil && opts.AsOf.Equal(defaultBaselineCutoff()) &&
				opts.IncludeHidden && !opts.ReplacedOnly && !opts.AllStatuses
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
// (GetForHouseholdScopeActiveNow(..., previousCutoff), per #1369 gap-closure G1) — but that method filters
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
	// Active-only at BOTH cutoffs — that is what GetForHouseholdScopeActiveNow really returns.
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope,
		mock.MatchedBy(isCurrent)).Return(activeAccounts, nil)
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope,
		mock.MatchedBy(isPrevious)).Return(activeAccounts, nil)
	mockAccountRepo.On("GetForHouseholdScopeLiveAsOf", ctx, requestingUserID, scope,
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
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(current, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return(previous, nil)

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
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope,
		mock.MatchedBy(isCurrent)).Return(accounts, nil)
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope,
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

	mockAccountRepo.AssertNotCalled(t, "GetForHouseholdScopeLiveAsOf",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// ─── #1537 T69: the personal 30-day comparison never ran in production ──────────
//
// F1. Both personal baselines read GetByUserIDAndDateRange(ctx, userID, previousDate, previousDate)
// — the SAME instant for both bounds — against SQL that filters
// `snapshot_at >= $2 AND snapshot_at <= $3`. That window demands snapshot_at be EXACTLY midnight
// UTC. The only production writer stores wall-clock time (transaction_sync_service.go,
// `now := time.Now().UTC()`), so the window matches nothing, len(previousSnapshots) > 0 is false,
// and PeriodComparison is nil for EVERY personal request in production. Three rounds of arithmetic
// fixes landed inside a block that does not execute.
//
// WHY IT SURVIVED. Every pre-existing personal stub passes mock.AnythingOfType("time.Time") for
// BOTH bounds, so the mock returns rows for a zero-width window and the defect is invisible. The
// two tests below model the SQL instead: the row comes back only if the window the service actually
// requests CONTAINS the snapshot's instant. Two per-argument matchers on one expectation compose
// into exactly that joint predicate.
func TestGetBalanceSummary_PeriodComparison_FindsWallClockBaselineSnapshot(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-wallclock-baseline"

	// A snapshot written the way production writes them: 30 days ago at 14:32:07, not midnight.
	baselineAt := time.Now().UTC().AddDate(0, 0, -30).Truncate(24 * time.Hour).
		Add(14*time.Hour + 32*time.Minute + 7*time.Second)

	cur, prev := 700.00, 500.00
	current := []domain.BalanceSnapshot{
		{ID: "cur", AccountID: "acct-1", UserID: userID, CurrentBalance: &cur, SnapshotAt: time.Now().UTC()},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "prev", AccountID: "acct-1", UserID: userID, CurrentBalance: &prev, SnapshotAt: baselineAt},
	}
	accounts := []domain.Account{
		{ID: "acct-1", UserID: userID, Name: "Checking", Type: "depository", Status: domain.AccountStatusActive},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).
		Return(current, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	// Models the baseline read's SQL — `snapshot_at <= $2` — instead of accepting any cutoff: the
	// row comes back only if the cutoff the service actually asks for is at or after the snapshot's
	// wall-clock instant. testify tries expectations in order, so the exact current-cutoff
	// expectation above claims the current read and this one only ever sees the baseline read.
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID,
		mock.MatchedBy(func(asOf time.Time) bool { return !baselineAt.After(asOf) }),
	).Return(previous, nil).Maybe()
	// Any earlier cutoff finds nothing — which is what a midnight-anchored cutoff does.
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID,
		mock.AnythingOfType("time.Time"),
	).Return([]domain.BalanceSnapshot{}, nil).Maybe()

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, userID, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison,
		"a wall-clock baseline snapshot 30 days old MUST produce a comparison. A zero-width "+
			"[midnight, midnight] window finds nothing, so this is nil for every real user.")
	assert.Equal(t, 500.00, result.PeriodComparison.PreviousBalance)
	assert.InDelta(t, 200.00, result.PeriodComparison.ChangeAmount, 0.001)
	assert.InDelta(t, 40.0, result.PeriodComparison.ChangePercent, 0.001)
}

// TestGetNetWorth_PeriodComparison_FindsWallClockBaselineSnapshot is the net-worth twin: the same
// zero-width window is read at dashboard_service.go's other personal baseline, so this path is
// equally dead in production.
func TestGetNetWorth_PeriodComparison_FindsWallClockBaselineSnapshot(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-nw-wallclock-baseline"

	baselineAt := time.Now().UTC().AddDate(0, 0, -30).Truncate(24 * time.Hour).
		Add(14*time.Hour + 32*time.Minute + 7*time.Second)

	cur, prev := 700.00, 500.00
	current := []domain.BalanceSnapshot{
		{ID: "cur", AccountID: "acct-1", UserID: userID, CurrentBalance: &cur, SnapshotAt: time.Now().UTC()},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "prev", AccountID: "acct-1", UserID: userID, CurrentBalance: &prev, SnapshotAt: baselineAt},
	}
	accounts := []domain.Account{
		{ID: "acct-1", UserID: userID, Name: "Checking", Type: "depository", Status: domain.AccountStatusActive},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).
		Return(current, nil)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	// Models `snapshot_at <= $2`; see the balance-summary twin above.
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID,
		mock.MatchedBy(func(asOf time.Time) bool { return !baselineAt.After(asOf) }),
	).Return(previous, nil).Maybe()
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID,
		mock.AnythingOfType("time.Time"),
	).Return([]domain.BalanceSnapshot{}, nil).Maybe()

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, userID, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison,
		"a wall-clock baseline snapshot 30 days old MUST produce a net-worth comparison")
	assert.Equal(t, 500.00, result.PeriodComparison.PreviousNetWorth)
	assert.InDelta(t, 200.00, result.PeriodComparison.NetWorthChange, 0.001)
	assert.InDelta(t, 40.0, result.PeriodComparison.NetWorthChangePercent, 0.001)
}

// TestGetBalanceSummary_NoAsOfDate_CountsSnapshotsTakenEarlierToday is the backward-compatibility
// half of #1537 T69. Bounding the current read by a cutoff where there was previously no bound at
// all (GetLatestByUserID) can only ever REMOVE rows, so the cutoff has to be the end of the day, not
// asOfDate itself: `snapshot_at <= midnight-today` would silently drop every snapshot taken so far
// today — the ones a user syncing at 09:00 actually cares about — and report yesterday's balance as
// current. The stub models the SQL rather than accepting any cutoff, so a midnight cutoff fails here
// instead of passing.
func TestGetBalanceSummary_NoAsOfDate_CountsSnapshotsTakenEarlierToday(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-today-0900"

	todayAt0900 := time.Now().UTC().Truncate(24 * time.Hour).Add(9 * time.Hour)
	yesterday := time.Now().UTC().Truncate(24 * time.Hour).Add(-3 * time.Hour)

	today, stale := 700.00, 100.00
	accounts := []domain.Account{
		{ID: "acct-1", UserID: userID, Name: "Checking", Type: "depository", Status: domain.AccountStatusActive},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	// Latest row at or before the requested cutoff: today's 09:00 sync if the cutoff reaches it,
	// otherwise yesterday's.
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID,
		mock.MatchedBy(func(asOf time.Time) bool { return !todayAt0900.After(asOf) }),
	).Return([]domain.BalanceSnapshot{
		{ID: "today", AccountID: "acct-1", UserID: userID, CurrentBalance: &today, SnapshotAt: todayAt0900},
	}, nil).Maybe()
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID,
		mock.AnythingOfType("time.Time"),
	).Return([]domain.BalanceSnapshot{
		{ID: "stale", AccountID: "acct-1", UserID: userID, CurrentBalance: &stale, SnapshotAt: yesterday},
	}, nil).Maybe()

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, userID, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 700.00, result.TotalBalance,
		"a snapshot taken at 09:00 today must still count as current. A midnight cutoff reports "+
			"yesterday's 100 instead — a regression the unbounded GetLatestByUserID could not have.")
}

// TestGetNetWorth_NoAsOfDate_CountsSnapshotsTakenEarlierToday is the net-worth twin.
func TestGetNetWorth_NoAsOfDate_CountsSnapshotsTakenEarlierToday(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-nw-today-0900"

	todayAt0900 := time.Now().UTC().Truncate(24 * time.Hour).Add(9 * time.Hour)
	yesterday := time.Now().UTC().Truncate(24 * time.Hour).Add(-3 * time.Hour)

	today, stale := 700.00, 100.00
	accounts := []domain.Account{
		{ID: "acct-1", UserID: userID, Name: "Checking", Type: "depository", Status: domain.AccountStatusActive},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID,
		mock.MatchedBy(func(asOf time.Time) bool { return !todayAt0900.After(asOf) }),
	).Return([]domain.BalanceSnapshot{
		{ID: "today", AccountID: "acct-1", UserID: userID, CurrentBalance: &today, SnapshotAt: todayAt0900},
	}, nil).Maybe()
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID,
		mock.AnythingOfType("time.Time"),
	).Return([]domain.BalanceSnapshot{
		{ID: "stale", AccountID: "acct-1", UserID: userID, CurrentBalance: &stale, SnapshotAt: yesterday},
	}, nil).Maybe()

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, userID, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 700.00, result.NetWorth,
		"a snapshot taken at 09:00 today must still count as current net worth")
}

// TestGetBalanceSummary_ExplicitAsOfDate_AnchorsBothSides pins the ruling that as_of_date anchors
// BOTH sides of the response (#1537 T69, finding F2). Before this, the current figures came from the
// unbounded GetLatestByUserID and only the baseline moved with as_of_date — so asking for a date two
// months ago returned TODAY's total, labelled with that date, compared against a baseline 30 days
// before it. Every number in the response was real; the response as a whole described no interval
// that existed.
//
// The two cutoffs are asserted as EXACT values. That is the point: it is the only way to make a
// wrong cutoff (a zero-width window, a midnight-anchored bound, or a time.Now() that ignores the
// filter) fail rather than quietly match.
func TestGetBalanceSummary_ExplicitAsOfDate_AnchorsBothSides(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-explicit-asof"

	asOf := time.Now().UTC().AddDate(0, 0, -60).Truncate(24 * time.Hour)
	baselineDate := asOf.AddDate(0, 0, -30)

	atAsOf, atBaseline := 900.00, 600.00
	accounts := []domain.Account{
		{ID: "acct-1", UserID: userID, Name: "Checking", Type: "depository", Status: domain.AccountStatusActive},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, endOfDay(asOf)).Return(
		[]domain.BalanceSnapshot{{ID: "at-asof", AccountID: "acct-1", UserID: userID,
			CurrentBalance: &atAsOf, SnapshotAt: asOf.Add(11 * time.Hour)}}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, endOfDay(baselineDate)).Return(
		[]domain.BalanceSnapshot{{ID: "at-baseline", AccountID: "acct-1", UserID: userID,
			CurrentBalance: &atBaseline, SnapshotAt: baselineDate.Add(11 * time.Hour)}}, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, userID, &domain.DashboardFilters{AsOfDate: &asOf})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, asOf, result.AsOfDate)
	assert.Equal(t, 900.00, result.TotalBalance,
		"the CURRENT figure must be the balance as of the requested date, not today's")
	assert.Equal(t, 600.00, result.PeriodComparison.PreviousBalance)
	assert.InDelta(t, 300.00, result.PeriodComparison.ChangeAmount, 0.001)
	assert.InDelta(t, 50.0, result.PeriodComparison.ChangePercent, 0.001)
	mockSnapshotRepo.AssertExpectations(t)
}

// TestGetNetWorth_ExplicitAsOfDate_AnchorsBothSides is the net-worth twin.
func TestGetNetWorth_ExplicitAsOfDate_AnchorsBothSides(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-nw-explicit-asof"

	asOf := time.Now().UTC().AddDate(0, 0, -60).Truncate(24 * time.Hour)
	baselineDate := asOf.AddDate(0, 0, -30)

	atAsOf, atBaseline := 900.00, 600.00
	accounts := []domain.Account{
		{ID: "acct-1", UserID: userID, Name: "Checking", Type: "depository", Status: domain.AccountStatusActive},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, endOfDay(asOf)).Return(
		[]domain.BalanceSnapshot{{ID: "at-asof", AccountID: "acct-1", UserID: userID,
			CurrentBalance: &atAsOf, SnapshotAt: asOf.Add(11 * time.Hour)}}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, endOfDay(baselineDate)).Return(
		[]domain.BalanceSnapshot{{ID: "at-baseline", AccountID: "acct-1", UserID: userID,
			CurrentBalance: &atBaseline, SnapshotAt: baselineDate.Add(11 * time.Hour)}}, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, userID, &domain.DashboardFilters{AsOfDate: &asOf})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, asOf, result.AsOfDate)
	assert.Equal(t, 900.00, result.NetWorth,
		"the CURRENT net worth must be as of the requested date, not today")
	assert.Equal(t, 600.00, result.PeriodComparison.PreviousNetWorth)
	assert.InDelta(t, 300.00, result.PeriodComparison.NetWorthChange, 0.001)
	assert.InDelta(t, 50.0, result.PeriodComparison.NetWorthChangePercent, 0.001)
	mockSnapshotRepo.AssertExpectations(t)
}

// TestGetBalanceSummaryHousehold_ExplicitAsOfDate_AnchorsBothSides is the household twin, and it
// doubles as the ONLY coverage of endOfDay on a household path. Both household cutoffs are asserted
// as exact values, so replacing endOfDay(previousDate) with previousDate — a mutation that left the
// entire internal/services package green — now has nothing to match and fails. Dropping endOfDay is
// not cosmetic: `snapshot_at <= midnight` skips every snapshot taken during the baseline day itself.
func TestGetBalanceSummaryHousehold_ExplicitAsOfDate_AnchorsBothSides(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}

	asOf := time.Now().UTC().AddDate(0, 0, -60).Truncate(24 * time.Hour)
	baselineDate := asOf.AddDate(0, 0, -30)

	atAsOf, atBaseline := 900.00, 600.00
	accounts := []domain.Account{
		{ID: "acct-1", UserID: "owner-1", Name: "Checking", Type: "depository", Status: domain.AccountStatusActive},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	// A snapshot taken at 23:30 on each anchor day: only an end-of-day cutoff reaches it.
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope, endOfDay(asOf)).Return(
		[]domain.BalanceSnapshot{{ID: "at-asof", AccountID: "acct-1", UserID: "owner-1",
			CurrentBalance: &atAsOf, SnapshotAt: asOf.Add(23*time.Hour + 30*time.Minute)}}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope, endOfDay(baselineDate)).Return(
		[]domain.BalanceSnapshot{{ID: "at-baseline", AccountID: "acct-1", UserID: "owner-1",
			CurrentBalance: &atBaseline, SnapshotAt: baselineDate.Add(23*time.Hour + 30*time.Minute)}}, nil)
	// The account map must be resolved at the SAME cutoff as the current snapshot read.
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope, endOfDay(asOf)).Return(accounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, requestingUserID,
		&domain.DashboardFilters{AsOfDate: &asOf, HouseholdScope: scope})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, asOf, result.AsOfDate)
	assert.Equal(t, 900.00, result.TotalBalance,
		"the household current total must be as of the requested date, not time.Now()")
	assert.Equal(t, 600.00, result.PeriodComparison.PreviousBalance)
	assert.InDelta(t, 50.0, result.PeriodComparison.ChangePercent, 0.001)
	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

// TestGetNetWorthHousehold_ExplicitAsOfDate_AnchorsBothSides is the household net-worth twin; it
// pins endOfDay on the second household path, which builds its own previous-cutoff account map.
func TestGetNetWorthHousehold_ExplicitAsOfDate_AnchorsBothSides(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-1"
	scope := &domain.HouseholdScope{HouseholdID: "hh-1", MemberUserIDs: []string{"owner-1", "member-2"}}

	asOf := time.Now().UTC().AddDate(0, 0, -60).Truncate(24 * time.Hour)
	baselineDate := asOf.AddDate(0, 0, -30)

	atAsOf, atBaseline := 900.00, 600.00
	accounts := []domain.Account{
		{ID: "acct-1", UserID: "owner-1", Name: "Checking", Type: "depository", Status: domain.AccountStatusActive},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope, endOfDay(asOf)).Return(
		[]domain.BalanceSnapshot{{ID: "at-asof", AccountID: "acct-1", UserID: "owner-1",
			CurrentBalance: &atAsOf, SnapshotAt: asOf.Add(23*time.Hour + 30*time.Minute)}}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope, endOfDay(baselineDate)).Return(
		[]domain.BalanceSnapshot{{ID: "at-baseline", AccountID: "acct-1", UserID: "owner-1",
			CurrentBalance: &atBaseline, SnapshotAt: baselineDate.Add(23*time.Hour + 30*time.Minute)}}, nil)
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope, endOfDay(asOf)).Return(accounts, nil)
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope, endOfDay(baselineDate)).Return(accounts, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, requestingUserID,
		&domain.DashboardFilters{AsOfDate: &asOf, HouseholdScope: scope})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.PeriodComparison)

	assert.Equal(t, asOf, result.AsOfDate)
	assert.Equal(t, 900.00, result.NetWorth)
	assert.Equal(t, 600.00, result.PeriodComparison.PreviousNetWorth)
	assert.InDelta(t, 50.0, result.PeriodComparison.NetWorthChangePercent, 0.001)
	mockSnapshotRepo.AssertExpectations(t)
	mockAccountRepo.AssertExpectations(t)
}

// TestEndOfDay pins the boundary arithmetic itself, which had NO test anywhere in internal/ despite
// four call sites depending on it. The last representable nanosecond of the day, not the next day's
// midnight: `snapshot_at <= endOfDay(d)` must include every instant on d and no instant on d+1.
func TestEndOfDay(t *testing.T) {
	day := time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC)

	for _, input := range []time.Time{
		day,
		day.Add(9 * time.Hour),
		day.Add(23*time.Hour + 59*time.Minute + 59*time.Second),
	} {
		assert.Equal(t, time.Date(2026, 3, 14, 23, 59, 59, 999999999, time.UTC), endOfDay(input),
			"endOfDay must normalise any instant on the day to that day's last nanosecond")
	}

	got := endOfDay(day)
	assert.True(t, got.After(day.Add(23*time.Hour+59*time.Minute+59*time.Second)),
		"must include a snapshot taken at 23:59:59 — the whole point of not using midnight")
	assert.True(t, got.Before(day.AddDate(0, 0, 1)),
		"must NOT reach the next day's midnight, or a `<=` cutoff would leak one instant of d+1")
}

// ─── #1537 T70: three mutants that survived the entire internal/services package ───

// TestGetBalanceSummary_PeriodComparison_DedupKeepsLatestInChain pins WHICH row of a re-link chain
// survives dedup, not merely that dedup happens.
//
// WHY IT WAS UNPINNED. TestGetBalanceSummary_PreviousPeriodChainDedup gives both chain members the
// SAME balance (500 and 500) and leaves SnapshotAt at its zero value, so it proves the two rows
// collapse to one and says nothing about which one. Reversing the sort.Slice that orders
// previousSnapshots DESC — making dedup keep the OLDEST row per chain — left the whole package green.
// Distinct balances AND distinct instants are what make the choice observable.
func TestGetBalanceSummary_PeriodComparison_DedupKeepsLatestInChain(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-dedup-latest"
	stableUA := "ua-dedup-latest"

	cur, newer, older := 1000.00, 900.00, 400.00
	baselineDay := time.Now().UTC().AddDate(0, 0, -30).Truncate(24 * time.Hour)

	accounts := []domain.Account{
		{ID: "acct-new", UserID: userID, Name: "Checking", Type: "depository",
			Status: domain.AccountStatusActive, UserAccountID: &stableUA},
	}
	// Deliberately handed to the service OLDEST-FIRST, the order the repo's ORDER BY would give a
	// range read: if the service does not sort DESC before deduping, the $400 row wins.
	previous := []domain.BalanceSnapshot{
		{ID: "prev-older", AccountID: "acct-old", UserAccountID: &stableUA, UserID: userID,
			CurrentBalance: &older, SnapshotAt: baselineDay.Add(2 * time.Hour)},
		{ID: "prev-newer", AccountID: "acct-new", UserAccountID: &stableUA, UserID: userID,
			CurrentBalance: &newer, SnapshotAt: baselineDay.Add(20 * time.Hour)},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(
		[]domain.BalanceSnapshot{{ID: "cur", AccountID: "acct-new", UserAccountID: &stableUA,
			UserID: userID, CurrentBalance: &cur, SnapshotAt: time.Now().UTC()}}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return(previous, nil)
	// #1569 T10-FIX item 2: the baseline universe is resolved at the baseline cutoff, and the
	// predecessor is part of it. Supplying it is what makes this fixture exercise dedup at all —
	// a snapshot naming an account that is in NO universe is now dropped before dedup sees it, so
	// without this row the two assertions below would hold for the wrong reason.
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(asOfBaselineOpts(defaultBaselineCutoff()))).
		Return(append(append([]domain.Account{}, accounts...),
			domain.Account{ID: "acct-old", UserID: userID, Name: "Old Checking", Type: "depository",
				Status: domain.AccountStatusReplaced, UserAccountID: &stableUA}), nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, userID, nil)
	require.NoError(t, err)
	require.NotNil(t, result.PeriodComparison)
	assert.Equal(t, 900.00, result.PeriodComparison.PreviousBalance,
		"dedup must keep the chain's LATEST baseline snapshot (900), not its oldest (400). "+
			"Keeping 400 turns a real +11% into +150%.")
	assert.InDelta(t, 100.00, result.PeriodComparison.ChangeAmount, 0.001)
}

// TestGetNetWorth_PeriodComparison_DedupKeepsLatestInChain is the net-worth twin: that loop is a
// separate implementation with its own seen-set, and its dedup test had the same blind spot.
func TestGetNetWorth_PeriodComparison_DedupKeepsLatestInChain(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-nw-dedup-latest"
	stableUA := "ua-nw-dedup-latest"

	cur, newer, older := 1000.00, 900.00, 400.00
	baselineDay := time.Now().UTC().AddDate(0, 0, -30).Truncate(24 * time.Hour)

	accounts := []domain.Account{
		{ID: "acct-new", UserID: userID, Name: "Checking", Type: "depository",
			Status: domain.AccountStatusActive, UserAccountID: &stableUA},
	}
	previous := []domain.BalanceSnapshot{
		{ID: "prev-older", AccountID: "acct-old", UserAccountID: &stableUA, UserID: userID,
			CurrentBalance: &older, SnapshotAt: baselineDay.Add(2 * time.Hour)},
		{ID: "prev-newer", AccountID: "acct-new", UserAccountID: &stableUA, UserID: userID,
			CurrentBalance: &newer, SnapshotAt: baselineDay.Add(20 * time.Hour)},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(
		[]domain.BalanceSnapshot{{ID: "cur", AccountID: "acct-new", UserAccountID: &stableUA,
			UserID: userID, CurrentBalance: &cur, SnapshotAt: time.Now().UTC()}}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return(previous, nil)
	// See the balance-summary twin: the predecessor must be in the baseline universe for this
	// fixture to reach the dedup it is testing (#1569 T10-FIX item 2).
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(asOfBaselineOpts(defaultBaselineCutoff()))).
		Return(append(append([]domain.Account{}, accounts...),
			domain.Account{ID: "acct-old", UserID: userID, Name: "Old Checking", Type: "depository",
				Status: domain.AccountStatusReplaced, UserAccountID: &stableUA}), nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, userID, nil)
	require.NoError(t, err)
	require.NotNil(t, result.PeriodComparison)
	assert.Equal(t, 900.00, result.PeriodComparison.PreviousAssets,
		"dedup must keep the chain's LATEST baseline snapshot (900), not its oldest (400)")
	assert.InDelta(t, 100.00, result.PeriodComparison.NetWorthChange, 0.001)
}

// TestBaselineBalanceForComparison_ActiveAccountMapWinsOverHistorical pins the precedence that
// twelve lines of comment above baselineBalanceForComparison justify and no test asserted: the
// ACTIVE map is consulted first and the all-statuses historical map is only a fallback. Swapping the
// two left the entire package green.
//
// The disagreement below (one key, two types) is deliberately artificial — the comment argues it
// cannot arise in production, because chain members share a type and the fallback fingerprint
// includes the type. That argument is exactly what needs a guard: it is the reason nobody would
// notice if the precedence flipped, and it stops being true the moment anyone reads a NAME, an ID,
// or a status-dependent field from this lookup. The test states the contract directly so the flip
// is caught by the contract rather than by whatever product bug the flip eventually causes.
func TestBaselineBalanceForComparison_ActiveAccountMapWinsOverHistorical(t *testing.T) {
	balance := 1000.00
	snapshots := []domain.BalanceSnapshot{
		{ID: "s1", AccountID: "acct-1", CurrentBalance: &balance},
	}
	active := map[string]domain.Account{
		"acct-1": {ID: "acct-1", Type: "depository", Status: domain.AccountStatusActive},
	}
	historical := map[string]domain.Account{
		"acct-1": {ID: "acct-1", Type: "credit", Status: domain.AccountStatusExpired},
	}

	assert.Equal(t, 1000.00,
		baselineBalanceForComparison(snapshots, active, historical, []string{"depository"}, true),
		"the ACTIVE map must win: its account is depository, so a depository filter keeps the row. "+
			"If the historical map won, the row would read as credit and be filtered out to 0.")
	assert.Equal(t, 0.00,
		baselineBalanceForComparison(snapshots, active, historical, []string{"credit"}, true),
		"and the converse: a credit filter must reject the row, because the type that counts is the "+
			"ACTIVE map's depository, not the historical map's credit")
}

// ─── #1537 T71 (F4): a legacy NULL user_account_id snapshot double-counted the chain ───

// TestGetBalanceSummary_PeriodComparison_LegacyNullUserAccountIDDoesNotDoubleCount pins that one
// logical account contributes to the baseline once even when its snapshots disagree about which
// column identifies it.
//
// THE DEFECT. balance_snapshots.user_account_id is NULLABLE, its backfill was one-shot
// (migrations/000038) and the FK is ON DELETE SET NULL. So a chain can hold a legacy row keyed on a
// raw account_id AND a post-relink sibling keyed on the user_account_id. snapshotChainKey returns
// different keys for those two rows, so the seen-set did not collapse them — while the CURRENT loop
// counts the chain once, because it resolves through the chain-aware account map. The all-statuses
// historical map then resurrects the replaced member that the current loop correctly dropped, so the
// baseline counted $1,000 where the truth was $500: a real $500 → $700 move (+40%) was reported as
// −30%. The sign of the change was wrong, not just its size.
func TestGetBalanceSummary_PeriodComparison_LegacyNullUserAccountIDDoesNotDoubleCount(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-legacy-null-ua"
	stableUA := "ua-legacy-chain"

	cur, legacy, sibling := 700.00, 500.00, 500.00
	baselineDay := time.Now().UTC().AddDate(0, 0, -30).Truncate(24 * time.Hour)

	// Active universe: only the post-relink account. Its snapshot carries the user_account_id.
	activeAccounts := []domain.Account{
		{ID: "acct-new", UserID: userID, Name: "Checking", Type: "depository",
			Status: domain.AccountStatusActive, UserAccountID: &stableUA},
	}
	// All-statuses universe additionally holds the replaced member — same chain, same
	// user_account_id, which is what lets the two snapshot rows be recognised as one account.
	allStatusAccounts := append([]domain.Account{}, activeAccounts...)
	allStatusAccounts = append(allStatusAccounts,
		domain.Account{ID: "acct-old", UserID: userID, Name: "Checking (replaced)", Type: "depository",
			Status: domain.AccountStatusReplaced, UserAccountID: &stableUA})

	previous := []domain.BalanceSnapshot{
		// Legacy row: user_account_id IS NULL, so it keys on its raw (already replaced) account_id.
		{ID: "prev-legacy", AccountID: "acct-old", UserAccountID: nil, UserID: userID,
			CurrentBalance: &legacy, SnapshotAt: baselineDay.Add(3 * time.Hour)},
		// Post-relink sibling for the SAME logical account.
		{ID: "prev-sibling", AccountID: "acct-new", UserAccountID: &stableUA, UserID: userID,
			CurrentBalance: &sibling, SnapshotAt: baselineDay.Add(21 * time.Hour)},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeAccounts, nil)
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(func(opts repository.AccountFilterOptions) bool {
			return opts.AsOf != nil && opts.AsOf.Equal(defaultBaselineCutoff()) &&
				opts.IncludeHidden && !opts.AllStatuses
		})).Return(allStatusAccounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(
		[]domain.BalanceSnapshot{{ID: "cur", AccountID: "acct-new", UserAccountID: &stableUA,
			UserID: userID, CurrentBalance: &cur, SnapshotAt: time.Now().UTC()}}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return(previous, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetBalanceSummary(ctx, userID, nil)
	require.NoError(t, err)
	require.NotNil(t, result.PeriodComparison)
	assert.Equal(t, 500.00, result.PeriodComparison.PreviousBalance,
		"one logical account, one contribution. Counting the legacy NULL row separately makes the "+
			"baseline 1000 and reports a real +40% as −30%.")
	assert.InDelta(t, 200.00, result.PeriodComparison.ChangeAmount, 0.001)
	assert.InDelta(t, 40.0, result.PeriodComparison.ChangePercent, 0.001)
}

// TestGetNetWorth_PeriodComparison_LegacyNullUserAccountIDDoesNotDoubleCount is the net-worth twin;
// its baseline loop keeps its own seen-set and had the identical defect.
func TestGetNetWorth_PeriodComparison_LegacyNullUserAccountIDDoesNotDoubleCount(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-nw-legacy-null-ua"
	stableUA := "ua-nw-legacy-chain"

	cur, legacy, sibling := 700.00, 500.00, 500.00
	baselineDay := time.Now().UTC().AddDate(0, 0, -30).Truncate(24 * time.Hour)

	activeAccounts := []domain.Account{
		{ID: "acct-new", UserID: userID, Name: "Checking", Type: "depository",
			Status: domain.AccountStatusActive, UserAccountID: &stableUA},
	}
	allStatusAccounts := append([]domain.Account{}, activeAccounts...)
	allStatusAccounts = append(allStatusAccounts,
		domain.Account{ID: "acct-old", UserID: userID, Name: "Checking (replaced)", Type: "depository",
			Status: domain.AccountStatusReplaced, UserAccountID: &stableUA})

	previous := []domain.BalanceSnapshot{
		{ID: "prev-legacy", AccountID: "acct-old", UserAccountID: nil, UserID: userID,
			CurrentBalance: &legacy, SnapshotAt: baselineDay.Add(3 * time.Hour)},
		{ID: "prev-sibling", AccountID: "acct-new", UserAccountID: &stableUA, UserID: userID,
			CurrentBalance: &sibling, SnapshotAt: baselineDay.Add(21 * time.Hour)},
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeAccounts, nil)
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(func(opts repository.AccountFilterOptions) bool {
			return opts.AsOf != nil && opts.AsOf.Equal(defaultBaselineCutoff()) &&
				opts.IncludeHidden && !opts.AllStatuses
		})).Return(allStatusAccounts, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(
		[]domain.BalanceSnapshot{{ID: "cur", AccountID: "acct-new", UserAccountID: &stableUA,
			UserID: userID, CurrentBalance: &cur, SnapshotAt: time.Now().UTC()}}, nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return(previous, nil)

	service := NewDashboardService(mockSnapshotRepo, mockAccountRepo)

	result, err := service.GetNetWorth(ctx, userID, nil)
	require.NoError(t, err)
	require.NotNil(t, result.PeriodComparison)
	assert.Equal(t, 500.00, result.PeriodComparison.PreviousAssets,
		"one logical account, one contribution — counting the legacy NULL row separately makes the "+
			"baseline 1000 and flips the sign of the reported change")
	assert.InDelta(t, 200.00, result.PeriodComparison.NetWorthChange, 0.001)
	assert.InDelta(t, 40.0, result.PeriodComparison.NetWorthChangePercent, 0.001)
}
