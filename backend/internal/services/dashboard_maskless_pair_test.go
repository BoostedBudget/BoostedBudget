package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dashboard_maskless_pair_test.go — #1569 T10.2, the SERVICE half of the maskless-pair fix.
//
// Fixing the repository alone is not enough and the residue points the OTHER way, which is what
// makes it worth its own file. Once the reads return both maskless accounts:
//
//   - the CURRENT loops resolve a snapshot through accountMap[snapshotChainKey], and both accounts
//     share one user_account_id — so buildChainAwareAccountMap's last-writer-wins entry decided
//     which account's NAME and ID were reported for BOTH, arbitrarily, by slice order;
//   - the BASELINE loops dedup on the RESOLVED account's chain key, so they still collapsed the
//     pair to one row. Current 500 against baseline 400 is not a halving — it is a FABRICATED
//     +25% on a user whose balance did not move.
//
// Both are pinned here. The re-link direction (a chain must still collapse) is pinned by the
// existing TestGetBalanceSummary_PeriodComparison_DedupKeepsLatestInChain and its net-worth twin,
// whose fixtures give the map only the surviving member.

const masklessUA = "ua-maskless-shared"

func masklessAccounts(userID string) []domain.Account {
	ua := masklessUA
	return []domain.Account{
		{ID: "acct-brokerage", UserID: userID, Name: "Brokerage", Type: "investment",
			Status: domain.AccountStatusActive, UserAccountID: &ua},
		{ID: "acct-ira", UserID: userID, Name: "IRA", Type: "investment",
			Status: domain.AccountStatusActive, UserAccountID: &ua},
	}
}

func masklessSnapshots(userID string, at time.Time, brokerage, ira float64) []domain.BalanceSnapshot {
	ua := masklessUA
	return []domain.BalanceSnapshot{
		{ID: "s-brokerage-" + at.String(), AccountID: "acct-brokerage", UserAccountID: &ua,
			UserID: userID, CurrentBalance: &brokerage, SnapshotAt: at},
		{ID: "s-ira-" + at.String(), AccountID: "acct-ira", UserAccountID: &ua,
			UserID: userID, CurrentBalance: &ira, SnapshotAt: at.Add(time.Minute)},
	}
}

func TestGetBalanceSummary_MasklessPair_CountsBothAndComparesLikeForLike(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-maskless-summary"
	now := time.Now().UTC()
	baselineDay := now.AddDate(0, 0, -30).Truncate(24 * time.Hour)

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(masklessAccounts(userID), nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).
		Return(masklessSnapshots(userID, now.Add(-time.Hour), 100, 400), nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return(masklessSnapshots(userID, baselineDay.Add(9*time.Hour), 100, 400), nil)

	result, err := NewDashboardService(mockSnapshotRepo, mockAccountRepo).
		GetBalanceSummary(ctx, userID, nil)
	require.NoError(t, err)

	assert.InDelta(t, 500.00, result.TotalBalance, 0.001,
		"both maskless accounts are live; 400 is the halving")
	require.NotNil(t, result.PeriodComparison)
	assert.InDelta(t, 500.00, result.PeriodComparison.PreviousBalance, 0.001,
		"the baseline must count the pair the same way the current total does. 400 here is the "+
			"baseline seen-set collapsing the pair on its shared user_account_id, which invents a "+
			"+25% move on a balance that did not change")
	assert.InDelta(t, 0.00, result.PeriodComparison.ChangePercent, 0.001)

	// Each account must be reported as ITSELF. Resolving both snapshots through the shared
	// user_account_id key made this list two copies of whichever account happened to be written
	// into the map last.
	byType := result.ByType["investment"]
	require.NotNil(t, byType)
	require.Len(t, byType.Accounts, 2)
	ids := map[string]float64{}
	for _, a := range byType.Accounts {
		ids[a.ID] = a.Balance
	}
	assert.InDelta(t, 100.00, ids["acct-brokerage"], 0.001, "the brokerage row must be the brokerage")
	assert.InDelta(t, 400.00, ids["acct-ira"], 0.001, "the IRA row must be the IRA")
}

func TestGetNetWorth_MasklessPair_CountsBothAndComparesLikeForLike(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-maskless-networth"
	now := time.Now().UTC()
	baselineDay := now.AddDate(0, 0, -30).Truncate(24 * time.Hour)

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(masklessAccounts(userID), nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).
		Return(masklessSnapshots(userID, now.Add(-time.Hour), 100, 400), nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return(masklessSnapshots(userID, baselineDay.Add(9*time.Hour), 100, 400), nil)

	result, err := NewDashboardService(mockSnapshotRepo, mockAccountRepo).
		GetNetWorth(ctx, userID, nil)
	require.NoError(t, err)

	assert.InDelta(t, 500.00, result.TotalAssets, 0.001, "both maskless accounts are assets")
	require.NotNil(t, result.PeriodComparison)
	assert.InDelta(t, 500.00, result.PeriodComparison.PreviousAssets, 0.001,
		"the net-worth baseline has its own seen-set and its own copy of the same defect")
	assert.InDelta(t, 0.00, result.PeriodComparison.NetWorthChange, 0.001)
}

// TestBuildChainAwareAccountMap_MasklessPairEntryIsDeterministic pins the map itself.
//
// The shared user_account_id key can only hold ONE account, and which one used to depend on slice
// order — so the same two accounts in the other order produced a different NAME and a different
// TYPE on every read that falls back to the chain key. Nothing chose that; it was whichever row
// the range loop wrote last. The choice is now stated: an active member beats a terminal one, and
// the lowest id breaks the remaining tie.
func TestBuildChainAwareAccountMap_MasklessPairEntryIsDeterministic(t *testing.T) {
	ua := masklessUA
	brokerage := domain.Account{ID: "acct-brokerage", Name: "Brokerage", Type: "investment",
		Status: domain.AccountStatusActive, UserAccountID: &ua}
	ira := domain.Account{ID: "acct-ira", Name: "IRA", Type: "investment",
		Status: domain.AccountStatusActive, UserAccountID: &ua}

	forward := buildChainAwareAccountMap([]domain.Account{brokerage, ira})
	reversed := buildChainAwareAccountMap([]domain.Account{ira, brokerage})
	assert.Equal(t, forward[ua].ID, reversed[ua].ID,
		"the chain-key entry must not depend on the order the accounts arrived in")
	assert.Equal(t, "acct-brokerage", forward[ua].ID, "lowest id wins among equally-live members")

	// ...and a live member always beats a terminal one, whichever order they arrive in.
	replaced := brokerage
	replaced.Status = domain.AccountStatusReplaced
	assert.Equal(t, "acct-ira",
		buildChainAwareAccountMap([]domain.Account{replaced, ira})[ua].ID,
		"a replaced member must not shadow the live one just because it sorts first")
	assert.Equal(t, "acct-ira",
		buildChainAwareAccountMap([]domain.Account{ira, replaced})[ua].ID)

	// Each account is still reachable by its OWN id — that is what lets a read resolve a snapshot
	// to the account it actually belongs to instead of to the chain's representative.
	assert.Equal(t, "Brokerage", forward["acct-brokerage"].Name)
	assert.Equal(t, "IRA", forward["acct-ira"].Name)
}
