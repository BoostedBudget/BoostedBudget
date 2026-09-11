package services

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestInvestmentSyncService_SyncHoldings_MergesADuplicatedSecurityInOnePayload is the
// service-side half of the 2026-09-11 production DLQ defect (webhook_processing job 9: five
// identical `unique_account_security` violations replacing holdings for one account).
//
// holdings is UNIQUE (account_id, security_id), so a payload carrying the same security twice for
// one account cannot be written at all — every attempt fails identically, forever, which is
// exactly the observed signature. Two ways that payload arises: Plaid lists the security twice
// for the account, or two plaid_security_ids resolve to one securities row. Both land here, as
// two domain.Holding rows with the same SecurityID.
//
// The merge sums, and that is the whole point: quantity, institution_value and cost_basis are
// TOTALS in this codebase (cost_basis is the total paid, not a per-share figure), so two genuine
// lots of one security add up. institution_price is a PER-SHARE figure and must NOT be summed —
// doubling a share price would silently misstate the position.
func TestInvestmentSyncService_SyncHoldings_MergesADuplicatedSecurityInOnePayload(t *testing.T) {
	svc, deps, logs := setupInvestmentSyncServiceWithLogger(t)
	ctx := context.Background()

	plaidAccountID := "plaid-acct-001"
	internalAccountID := uuid.New().String()
	plaidSecurityID := "plaid-sec-001"
	internalSecurityID := uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-001", userID)

	sec := makePlaidSecurity(plaidSecurityID, "Apple Inc.", "AAPL")
	// The same security, twice, on the same account — two lots.
	lotA := makePlaidHolding(plaidAccountID, plaidSecurityID, 10.0, 165.0, 1650.0, 1500.0)
	lotB := makePlaidHolding(plaidAccountID, plaidSecurityID, 4.0, 165.0, 660.0, 600.0)

	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{lotA, lotB},
		[]plaid.Security{sec},
		makeMinimalPlaidItem(),
		"req-001",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, linkedItem.AccessToken).Return(holdingsResp, nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{
		plaidSecurityID: internalSecurityID,
	}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	var written []domain.Holding
	deps.holdingRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.Anything).
		Run(func(args mock.Arguments) {
			written = args.Get(3).([]domain.Holding)
		}).Return(nil)

	result, err := svc.SyncHoldings(ctx, linkedItem)
	require.NoError(t, err)

	require.Len(t, written, 1, "the two lots of one security must reach the repository as ONE row")
	assert.Equal(t, internalSecurityID, written[0].SecurityID)
	assert.InDelta(t, 14.0, written[0].Quantity, 0.0001, "quantity is a total: the lots sum")
	require.NotNil(t, written[0].InstitutionValue)
	assert.InDelta(t, 2310.0, *written[0].InstitutionValue, 0.0001, "institution_value is a total: the lots sum")
	require.NotNil(t, written[0].CostBasis)
	assert.InDelta(t, 2100.0, *written[0].CostBasis, 0.0001, "cost_basis is a TOTAL in this codebase: the lots sum")
	require.NotNil(t, written[0].InstitutionPrice)
	assert.InDelta(t, 165.0, *written[0].InstitutionPrice, 0.0001, "institution_price is per-share and must NOT be summed")

	assert.Equal(t, 1, result.HoldingsReplaced, "the merged row is one holding, not two")

	out := logs.String()
	assert.Contains(t, out, "merged duplicate holdings", "the merge must be logged, not silent")
	assert.Contains(t, out, plaidSecurityID, "the log must name the colliding security")
	assert.Contains(t, out, internalAccountID, "the log must name the account")
	assert.True(t, strings.Contains(out, `"plaid_holdings_in_payload":2`) ||
		strings.Contains(out, `plaid_holdings_in_payload=2`),
		"the log must state how many holdings the payload carried for the account; got: %s", out)
}

// setupInvestmentSyncServiceWithLogger mirrors setupInvestmentSyncService but wires a capturing
// logger, because the diagnostics are half of the fix: five identical dead-lettered attempts with
// no extra detail were part of what made the production defect unactionable.
func setupInvestmentSyncServiceWithLogger(t *testing.T) (InvestmentSyncService, investmentSyncDeps, *bytes.Buffer) {
	t.Helper()
	_, deps := setupInvestmentSyncService(t)
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc := NewInvestmentSyncService(
		deps.webhookRepo,
		deps.linkedItemRepo,
		deps.accountRepo,
		deps.securityRepo,
		deps.holdingRepo,
		deps.invTxnRepo,
		deps.snapshotRepo,
		deps.userAccountRepo,
		deps.plaidSvc,
		logger,
	)
	return svc, deps, buf
}
