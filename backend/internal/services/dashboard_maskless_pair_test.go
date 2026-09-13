package services

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

// ── #1569 T10-FIX item 2: a TERMINAL maskless sibling must not be counted through its live twin ──
//
// Round 10's snapshotAccount precedence ("a live chain representative wins") is right for a re-link
// PREDECESSOR and wrong for a maskless SIBLING that was simply closed. The two are not
// distinguishable by chain key — which is the trap this area has fallen into five times — but they
// ARE distinguishable by the terminal member's own status: 'replaced' is written by the re-link and
// the CreateBatch backstop and by nothing else, while a closed sibling is 'removed'/'expired'.
//
// The consequences of getting it wrong are two, and both are money:
//   - the CURRENT total counts the closed sibling's frozen balance, which
//     TestGetBalanceSummary_Baseline_RequestsLivenessAtBaselineCutoff already forbids for a
//     non-maskless account ("the removed account's frozen snapshot stays out of the CURRENT total").
//     It leaked back in here only because the closed sibling shares the live one's chain key.
//   - it is counted under the LIVE twin's TYPE. A maskless pair can be a depository and a credit
//     card, so a liability is reported as an asset.
func masklessClosedSiblingAccounts(userID string) (active []domain.Account, liveAtCutoff []domain.Account) {
	ua := masklessUA
	live := domain.Account{ID: "acct-checking", UserID: userID, Name: "Checking", Type: "depository",
		Status: domain.AccountStatusActive, UserAccountID: &ua}
	closed := domain.Account{ID: "acct-card", UserID: userID, Name: "Card", Type: "credit",
		Status: domain.AccountStatusRemoved, UserAccountID: &ua}
	return []domain.Account{live}, []domain.Account{live, closed}
}

func masklessClosedSiblingSnapshots(userID string, at time.Time, card, checking float64) []domain.BalanceSnapshot {
	ua := masklessUA
	return []domain.BalanceSnapshot{
		{ID: "s-card-" + at.String(), AccountID: "acct-card", UserAccountID: &ua,
			UserID: userID, CurrentBalance: &card, SnapshotAt: at},
		{ID: "s-checking-" + at.String(), AccountID: "acct-checking", UserAccountID: &ua,
			UserID: userID, CurrentBalance: &checking, SnapshotAt: at.Add(time.Minute)},
	}
}

func TestGetBalanceSummary_MasklessPair_ClosedSiblingIsNotCountedThroughTheLiveTwin(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-maskless-closed-sibling"
	now := time.Now().UTC()
	baselineDay := now.AddDate(0, 0, -30).Truncate(24 * time.Hour)

	activeAccounts, liveAtCutoff := masklessClosedSiblingAccounts(userID)

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeAccounts, nil)
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(asOfBaselineOpts(defaultBaselineCutoff()))).Return(liveAtCutoff, nil).Maybe()
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).
		Return(masklessClosedSiblingSnapshots(userID, now.Add(-time.Hour), 100, 400), nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return(masklessClosedSiblingSnapshots(userID, baselineDay.Add(9*time.Hour), 100, 400), nil)

	result, err := NewDashboardService(mockSnapshotRepo, mockAccountRepo).
		GetBalanceSummary(ctx, userID, nil)
	require.NoError(t, err)

	assert.InDelta(t, 400.00, result.TotalBalance, 0.001,
		"the closed sibling's frozen $100 must stay out of the CURRENT total; 500 is it being "+
			"counted through the live twin's chain key")
	assert.Nil(t, result.ByType["credit"],
		"a closed credit sibling contributes nothing to the current breakdown")
	require.NotNil(t, result.ByType["depository"])
	require.Len(t, result.ByType["depository"].Accounts, 1,
		"the live twin must be reported once, as itself")
	assert.InDelta(t, 400.00, result.ByType["depository"].Balance, 0.001,
		"$500 here is the credit sibling's balance reported as a depository asset")

	// The baseline is the OTHER universe on purpose: at the cutoff the card was still live, so its
	// $100 raw debt contributes -$100 there — against its OWN type, not the twin's. Removing that
	// debt from the tracked universe raises the signed balance by $100.
	require.NotNil(t, result.PeriodComparison)
	assert.InDelta(t, 300.00, result.PeriodComparison.PreviousBalance, 0.001,
		"the baseline universe includes the account that was live at the cutoff")
}

// TestGetNetWorth_MasklessPair_ClosedCreditSiblingIsNotAnAsset is the assertion that makes item 2 a
// money defect rather than a labelling one: resolving the closed card to its depository twin adds a
// LIABILITY into TotalAssets.
func TestGetNetWorth_MasklessPair_ClosedCreditSiblingIsNotAnAsset(t *testing.T) {
	ctx := context.Background()
	userID := "test-user-maskless-closed-networth"
	now := time.Now().UTC()
	baselineDay := now.AddDate(0, 0, -30).Truncate(24 * time.Hour)

	activeAccounts, liveAtCutoff := masklessClosedSiblingAccounts(userID)

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockAccountRepo.On("GetByUserID", ctx, userID).Return(activeAccounts, nil)
	mockAccountRepo.On("GetAccountsWithInstitutionFiltered", ctx, userID,
		mock.MatchedBy(asOfBaselineOpts(defaultBaselineCutoff()))).Return(liveAtCutoff, nil).Maybe()
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultCurrentCutoff()).
		Return(masklessClosedSiblingSnapshots(userID, now.Add(-time.Hour), 100, 400), nil)
	mockSnapshotRepo.On("GetSnapshotsAsOfForUser", ctx, userID, defaultBaselineCutoff()).
		Return(masklessClosedSiblingSnapshots(userID, baselineDay.Add(9*time.Hour), 100, 400), nil)

	result, err := NewDashboardService(mockSnapshotRepo, mockAccountRepo).
		GetNetWorth(ctx, userID, nil)
	require.NoError(t, err)

	assert.InDelta(t, 400.00, result.TotalAssets, 0.001,
		"500 is the closed CREDIT sibling counted as a depository asset")
	assert.InDelta(t, 0.00, result.TotalLiabilities, 0.001,
		"the card is closed; it is neither an asset nor a current liability")
}

// TestSnapshotAccount_ReplacedPredecessorStillResolvesToItsLiveSuccessor guards the OTHER side of
// the same precedence, the one worth $300 in
// TestDashboard_PastAsOfDate_MergeDirection_DecidesInclusionUnderATypeFilter. Narrowing the
// live-representative rule to genuine predecessors must not switch it off for them.
func TestSnapshotAccount_ReplacedPredecessorStillResolvesToItsLiveSuccessor(t *testing.T) {
	ua := masklessUA
	successor := domain.Account{ID: "acct-new", Name: "New Checking", Type: "depository",
		Status: domain.AccountStatusActive, UserAccountID: &ua}
	predecessor := domain.Account{ID: "acct-old", Name: "Old Brokerage", Type: "investment",
		Status: domain.AccountStatusReplaced, UserAccountID: &ua}

	currentMap := buildChainAwareAccountMap([]domain.Account{successor})
	asOfMap := buildChainAwareAccountMap([]domain.Account{predecessor})
	mergeMissingAccounts(currentMap, asOfMap)

	snapshot := domain.BalanceSnapshot{ID: "s-old", AccountID: "acct-old", UserAccountID: &ua}
	resolved, ok := snapshotAccount(snapshot, currentMap)
	require.True(t, ok)
	assert.Equal(t, "acct-new", resolved.ID,
		"a 'replaced' predecessor is interpreted by its successor — the add-only merge direction")

	closed := predecessor
	closed.Status = domain.AccountStatusRemoved
	closedMap := buildChainAwareAccountMap([]domain.Account{successor})
	mergeMissingAccounts(closedMap, buildChainAwareAccountMap([]domain.Account{closed}))
	resolvedClosed, ok := snapshotAccount(
		domain.BalanceSnapshot{ID: "s-closed", AccountID: "acct-old", UserAccountID: &ua}, closedMap)
	require.True(t, ok)
	assert.Equal(t, "acct-old", resolvedClosed.ID,
		"a CLOSED sibling is not a predecessor and belongs to itself")
	assert.Equal(t, "investment", resolvedClosed.Type,
		"...including its own type, which decides whether an AccountTypes filter counts the money")
}

// ── #1569 T10-FIX item 3: the household net-worth BASELINE classified by the shared chain key ──
//
// GetSnapshotsAsOfForHousehold returns PHYSICAL-account rows as of #1569 T10.2, but this one
// baseline loop still looked each snapshot up as `previousAccountMap[snapshotChainKey(snapshot)]` —
// the only remaining raw chain-key lookup in the file. A maskless depository and a maskless credit
// card share a user_accounts row by schema (migration 000100), so BOTH resolved to whichever of the
// two won the chain-key slot, and both were classified as that account's category.
//
// The total can look right while every part of it is wrong: $100 asset + $50 liability is a $50
// baseline net worth, but classified together they are either $150 of assets or $150 of
// liabilities — a $200 error in NetWorthChange either way, and asset/liability splits that do not
// describe the household at all.
func TestGetNetWorthHousehold_Baseline_MasklessPairKeepsEachAccountsOwnCategory(t *testing.T) {
	ctx := context.Background()
	requestingUserID := "owner-hh-maskless"
	scope := &domain.HouseholdScope{HouseholdID: "hh-maskless", MemberUserIDs: []string{requestingUserID}}
	ua := masklessUA
	now := time.Now().UTC()

	accounts := []domain.Account{
		{ID: "acct-hh-checking", UserID: requestingUserID, Name: "Checking", Type: "depository",
			Status: domain.AccountStatusActive, UserAccountID: &ua},
		{ID: "acct-hh-card", UserID: requestingUserID, Name: "Card", Type: "credit",
			Status: domain.AccountStatusActive, UserAccountID: &ua},
	}
	current := func(checking, card float64, at time.Time) []domain.BalanceSnapshot {
		return []domain.BalanceSnapshot{
			{ID: "hh-s-checking-" + at.String(), AccountID: "acct-hh-checking", UserAccountID: &ua,
				UserID: requestingUserID, CurrentBalance: &checking, SnapshotAt: at},
			{ID: "hh-s-card-" + at.String(), AccountID: "acct-hh-card", UserAccountID: &ua,
				UserID: requestingUserID, CurrentBalance: &card, SnapshotAt: at},
		}
	}

	mockSnapshotRepo := new(MockBalanceSnapshotRepository)
	mockAccountRepo := new(MockAccountRepository)
	mockSnapshotRepo.On("GetSnapshotsAsOfForHousehold", ctx, requestingUserID, scope,
		mock.AnythingOfType("time.Time")).Return(current(100, 50, now.Add(-time.Hour)), nil)
	mockAccountRepo.On("GetForHouseholdScopeActiveNow", ctx, requestingUserID, scope,
		mock.AnythingOfType("time.Time")).Return(accounts, nil)

	result, err := NewDashboardService(mockSnapshotRepo, mockAccountRepo).
		GetNetWorth(ctx, requestingUserID, &domain.DashboardFilters{HouseholdScope: scope})
	require.NoError(t, err)

	// The current leg already resolves per physical account; it is the control for the baseline.
	require.InDelta(t, 100.00, result.TotalAssets, 0.001)
	require.InDelta(t, 50.00, result.TotalLiabilities, 0.001)

	require.NotNil(t, result.PeriodComparison)
	assert.InDelta(t, 100.00, result.PeriodComparison.PreviousAssets, 0.001,
		"150 here is the credit card classified as a depository because it shares the chain key")
	assert.InDelta(t, 50.00, result.PeriodComparison.PreviousLiabilities, 0.001,
		"0 here is the same defect seen from the other side")
	assert.InDelta(t, 50.00, result.PeriodComparison.PreviousNetWorth, 0.001)
	assert.InDelta(t, 0.00, result.PeriodComparison.NetWorthChange, 0.001,
		"the household's balances did not move; any change here is fabricated by classification")
}
