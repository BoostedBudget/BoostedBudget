package services

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func floatPtr(value float64) *float64 { return &value }

func TestBalanceSummary_SignedContributions(t *testing.T) {
	for _, household := range []bool{false, true} {
		name := "personal"
		if household {
			name = "household"
		}
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name, accountType                                  string
				previous, current                                  *float64
				wantPrevious, wantCurrent, wantChange, wantPercent float64
			}{
				{"loan repayment", "loan", floatPtr(100000), floatPtr(90000), -100000, -90000, 10000, 10},
				{"credit worsening", "credit", floatPtr(100000), floatPtr(110000), -100000, -110000, -10000, -10},
				{"credit overpayment", "credit", floatPtr(100), floatPtr(-20), -100, 20, 120, 120},
				{"loan overpayment", "loan", floatPtr(-100), floatPtr(-120), 100, 120, 20, 20},
				{"zero debt", "credit", floatPtr(0), floatPtr(0), 0, 0, 0, 0},
				{"nil debt", "loan", nil, nil, 0, 0, 0, 0},
				{"zero baseline", "loan", floatPtr(0), floatPtr(100), 0, -100, -100, 0},
				{"overdraft", "depository", floatPtr(-100), floatPtr(-80), -100, -80, 20, 20},
				{"negative investment", "investment", floatPtr(-100), floatPtr(-120), -100, -120, -20, -20},
				{"other asset", "other", floatPtr(-100), floatPtr(20), -100, 20, 120, 120},
			} {
				t.Run(tc.name, func(t *testing.T) {
					accounts := []domain.Account{{ID: "account", Type: tc.accountType, Name: "Account"}}
					current := []domain.BalanceSnapshot{{AccountID: "account", CurrentBalance: tc.current}}
					previous := []domain.BalanceSnapshot{{AccountID: "account", CurrentBalance: tc.previous}}
					result := signedBalanceSummary(t, household, accounts, current, previous, nil)
					assert.Equal(t, tc.wantCurrent, result.TotalBalance)
					assert.Equal(t, tc.wantCurrent, result.ByType[tc.accountType].Balance)
					require.Len(t, result.ByType[tc.accountType].Accounts, 1)
					assert.Equal(t, tc.wantCurrent, result.ByType[tc.accountType].Accounts[0].Balance)
					if tc.wantCurrent == 0 {
						assert.False(t, math.Signbit(result.ByType[tc.accountType].Accounts[0].Balance))
					}
					require.NotNil(t, result.PeriodComparison)
					assert.Equal(t, tc.wantPrevious, result.PeriodComparison.PreviousBalance)
					assert.Equal(t, tc.wantChange, result.PeriodComparison.ChangeAmount)
					assert.InDelta(t, tc.wantPercent, result.PeriodComparison.ChangePercent, 0.0001)
				})
			}
		})
	}
}

func TestBalanceSummary_SignedBreakdownAndFilter(t *testing.T) {
	accounts := []domain.Account{{ID: "cash", Type: "depository"}, {ID: "card", Type: "credit"}, {ID: "loan", Type: "loan"}, {ID: "overpayment", Type: "credit"}}
	current := []domain.BalanceSnapshot{{AccountID: "cash", CurrentBalance: floatPtr(1000)}, {AccountID: "card", CurrentBalance: floatPtr(200)}, {AccountID: "loan", CurrentBalance: floatPtr(300)}, {AccountID: "overpayment", CurrentBalance: floatPtr(-50)}}
	previous := []domain.BalanceSnapshot{{AccountID: "cash", CurrentBalance: floatPtr(1000)}, {AccountID: "card", CurrentBalance: floatPtr(300)}, {AccountID: "loan", CurrentBalance: floatPtr(400)}, {AccountID: "overpayment", CurrentBalance: floatPtr(-50)}}
	for _, household := range []bool{false, true} {
		result := signedBalanceSummary(t, household, accounts, current, previous, nil)
		assert.Equal(t, 550.0, result.TotalBalance)
		assert.Equal(t, -150.0, result.ByType["credit"].Balance)
		assert.Equal(t, 2, result.ByType["credit"].AccountCount)
		var total float64
		for _, group := range result.ByType {
			var details float64
			for _, account := range group.Accounts {
				details += account.Balance
			}
			assert.Equal(t, group.Balance, details)
			total += group.Balance
		}
		assert.Equal(t, result.TotalBalance, total)
		require.NotNil(t, result.PeriodComparison)
		assert.Equal(t, 350.0, result.PeriodComparison.PreviousBalance)
		filtered := signedBalanceSummary(t, household, accounts, current, previous, []string{"credit", "loan"})
		assert.Equal(t, -450.0, filtered.TotalBalance)
		assert.NotContains(t, filtered.ByType, "depository")
		require.NotNil(t, filtered.PeriodComparison)
		assert.Equal(t, -650.0, filtered.PeriodComparison.PreviousBalance)
		assert.Equal(t, 200.0, filtered.PeriodComparison.ChangeAmount)
		assert.InDelta(t, 200.0/650*100, filtered.PeriodComparison.ChangePercent, 0.0001)
	}
}

func signedBalanceSummary(t *testing.T, household bool, accounts []domain.Account, current, previous []domain.BalanceSnapshot, types []string) *domain.BalanceSummaryResponse {
	t.Helper()
	ctx := context.Background()
	userID := "signed-summary-user"
	snapshots := new(MockBalanceSnapshotRepository)
	accountRepo := new(MockAccountRepository)
	filters := &domain.DashboardFilters{AccountTypes: types}
	if household {
		filters.HouseholdScope = &domain.HouseholdScope{HouseholdID: "household", MemberUserIDs: []string{userID}}
		snapshots.On("GetSnapshotsAsOfForHousehold", ctx, userID, filters.HouseholdScope, defaultCurrentCutoff()).Return(current, nil)
		snapshots.On("GetSnapshotsAsOfForHousehold", ctx, userID, filters.HouseholdScope, defaultBaselineCutoff()).Return(previous, nil)
		accountRepo.On("GetForHouseholdScopeActiveNow", ctx, userID, filters.HouseholdScope, defaultCurrentCutoff()).Return(accounts, nil)
	} else {
		snapshots.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).Return(current, nil)
		snapshots.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).Return(previous, nil)
		accountRepo.On("GetByUserID", ctx, userID).Return(accounts, nil)
	}
	result, err := NewDashboardService(snapshots, accountRepo).GetBalanceSummary(ctx, userID, filters)
	require.NoError(t, err)
	require.NotNil(t, result)
	snapshots.AssertExpectations(t)
	accountRepo.AssertExpectations(t)
	return result
}
