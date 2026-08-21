package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
