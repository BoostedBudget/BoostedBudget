package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Issue #1640 / #1616: Plaid may return a holding for a cash-management account even though the
// account is stored as depository. That row must not enter the investment holdings replacement
// path. The per-chain exclusion preference is deliberately irrelevant to holdings: it governs
// investment transactions only.
func TestInvestmentSyncService_SyncHoldings_GatesByAccountTypeNotPreference(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-holdings-type-gate", userID)

	investmentPlaidID := "plaid-investment"
	depositoryPlaidID := "plaid-depository"
	unknownTypePlaidID := "plaid-unknown-type"
	investmentAccountID := uuid.New().String()
	depositoryAccountID := uuid.New().String()
	unknownTypeAccountID := uuid.New().String()
	securityID := "plaid-security"
	internalSecurityID := uuid.New().String()

	resp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{
			{AccountId: investmentPlaidID},
			{AccountId: depositoryPlaidID},
			{AccountId: unknownTypePlaidID},
		},
		[]plaid.Holding{
			makePlaidHolding(investmentPlaidID, securityID, 2, 100, 200, 150),
			makePlaidHolding(depositoryPlaidID, securityID, 500, 1, 500, 500),
			makePlaidHolding(unknownTypePlaidID, securityID, 600, 1, 600, 600),
		},
		[]plaid.Security{makePlaidSecurity(securityID, "Test security", "TEST")},
		makeMinimalPlaidItem(),
		"req-holdings-type-gate",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(resp, nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{securityID: internalSecurityID}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{
			ID: investmentAccountID, PlaidAccountID: investmentPlaidID, UserID: userID,
			Type: "investment", ExcludeInvestmentSync: true,
		},
		{
			ID: depositoryAccountID, PlaidAccountID: depositoryPlaidID, UserID: userID,
			Type: "depository", ExcludeInvestmentSync: false,
		},
		{
			ID: unknownTypeAccountID, PlaidAccountID: unknownTypePlaidID, UserID: userID,
			Type: "", ExcludeInvestmentSync: true,
		},
	}, nil)
	deps.holdingRepo.On("ReplaceForAccount", ctx, investmentAccountID, userID, mock.MatchedBy(func(holdings []domain.Holding) bool {
		return len(holdings) == 1 && holdings[0].AccountID == investmentAccountID
	})).Return(nil).Once()

	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.AccountsProcessed)
	assert.Equal(t, 1, result.HoldingsReplaced)
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.SkippedIncomplete)
	assert.Equal(t, 2, result.AccountsSkippedNonInvestment)
	assert.Equal(t, 2, result.HoldingsSkippedNonInvestment)
	deps.holdingRepo.AssertNotCalled(t, "ReplaceForAccount", ctx, depositoryAccountID, userID, mock.Anything)
	deps.holdingRepo.AssertNotCalled(t, "ReplaceForAccount", ctx, unknownTypeAccountID, userID, mock.Anything)
	deps.holdingRepo.AssertExpectations(t)
}

func TestInvestmentSyncService_SyncHoldings_NonInvestmentAbsentFromRosterIsNotIncomplete(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-non-investment-absent", userID)
	accountID := uuid.New().String()

	resp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{}, []plaid.Holding{}, []plaid.Security{}, makeMinimalPlaidItem(), "req-non-investment-absent",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(resp, nil)
	deps.securityRepo.On("UpsertBatch", ctx, []domain.Security{}).Return(map[string]string{}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{{
		ID: accountID, PlaidAccountID: "plaid-non-investment-absent", UserID: userID, Type: "depository",
	}}, nil)

	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.NoError(t, err)
	assert.Zero(t, result.AccountsProcessed)
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.SkippedIncomplete, "a non-investment account is inapplicable, not an incomplete response")
	assert.Equal(t, 1, result.AccountsSkippedNonInvestment)
	assert.Zero(t, result.HoldingsSkippedNonInvestment)
	deps.holdingRepo.AssertNotCalled(t, "ReplaceForAccount", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestInvestmentSyncService_SyncHoldings_NonInvestmentCountersAreOwnerScoped(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()
	userID := uuid.New().String()
	foreignUserID := uuid.New().String()
	linkedItem := makeLinkedItem("item-owner-scoped-skips", userID)
	ownedPlaidID := "plaid-owned-depository"
	foreignPlaidID := "plaid-foreign-depository"
	unknownPlaidID := "plaid-unknown-provider-account"
	securityID := "plaid-owner-scope-security"

	response := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{{AccountId: ownedPlaidID}, {AccountId: foreignPlaidID}, {AccountId: unknownPlaidID}},
		[]plaid.Holding{
			makePlaidHolding(ownedPlaidID, securityID, 1, 10, 10, 10),
			makePlaidHolding(foreignPlaidID, securityID, 2, 10, 20, 20),
			makePlaidHolding(unknownPlaidID, securityID, 3, 10, 30, 30),
		},
		[]plaid.Security{makePlaidSecurity(securityID, "Owner scope security", "OWNR")},
		makeMinimalPlaidItem(),
		"req-owner-scoped-skips",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(response, nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{securityID: uuid.New().String()}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: uuid.New().String(), PlaidAccountID: ownedPlaidID, UserID: userID, Type: "depository"},
		{ID: uuid.New().String(), PlaidAccountID: foreignPlaidID, UserID: foreignUserID, Type: "depository"},
	}, nil)

	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.AccountsSkippedNonInvestment)
	assert.Equal(t, 1, result.HoldingsSkippedNonInvestment)
	assert.Zero(t, result.AccountsProcessed)
	deps.holdingRepo.AssertNotCalled(t, "ReplaceForAccount", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestInvestmentSyncService_SyncHoldings_ReportsNonInvestmentSkipsToAnalytics(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-holdings-skip-analytics", userID)
	plaidAccountID := "plaid-depository-analytics"
	securityID := "plaid-security-analytics"

	resp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{{AccountId: plaidAccountID}},
		[]plaid.Holding{makePlaidHolding(plaidAccountID, securityID, 10, 20, 200, 150)},
		[]plaid.Security{makePlaidSecurity(securityID, "Analytics security", "ANLY")},
		makeMinimalPlaidItem(),
		"req-holdings-skip-analytics",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(resp, nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.MatchedBy(func(securities []domain.Security) bool {
		return len(securities) == 1 && securities[0].PlaidSecurityID == securityID
	})).Return(map[string]string{securityID: uuid.New().String()}, nil).Once()
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{{
		ID: uuid.New().String(), PlaidAccountID: plaidAccountID, UserID: userID, Type: "depository",
	}}, nil)
	analytics := &recordingHoldingsSyncAnalytics{}
	svc.SetAnalytics(analytics)

	result, err := svc.SyncHoldings(ctx, linkedItem)

	require.NoError(t, err)
	assert.Equal(t, 1, result.SecuritiesUpserted)
	assert.Equal(t, phclient.EventInvestmentHoldingsSynced, analytics.event)
	assert.Equal(t, 1, analytics.properties["accounts_skipped_non_investment"])
	assert.Equal(t, 1, analytics.properties["holdings_skipped_non_investment"])
	deps.securityRepo.AssertExpectations(t)
}

type recordingHoldingsSyncAnalytics struct {
	event      string
	properties map[string]interface{}
}

func (a *recordingHoldingsSyncAnalytics) Capture(_ string, event string, properties map[string]interface{}) {
	a.event = event
	a.properties = properties
}

func (*recordingHoldingsSyncAnalytics) Identify(string, map[string]interface{}) {}
func (*recordingHoldingsSyncAnalytics) Close()                                  {}

var _ phclient.Analytics = (*recordingHoldingsSyncAnalytics)(nil)
