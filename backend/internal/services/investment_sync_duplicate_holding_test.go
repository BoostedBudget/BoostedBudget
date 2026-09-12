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

// This file is the service-side half of the 2026-09-11 production DLQ defect (webhook_processing
// job 9: five identical `unique_account_security` violations replacing holdings for one account).
//
// holdings is UNIQUE (account_id, security_id), so a payload carrying the same security twice for
// one account cannot be written at all — every attempt fails identically, forever, which is
// exactly the observed signature. Two ways that payload arises: Plaid lists the security twice for
// the account, or two plaid_security_ids resolve to one securities row. Both land here, as two
// domain.Holding rows with the same SecurityID.
//
// What a duplicate MEANS is the whole question, and Plaid answers it in its own SDK
// (plaid/model_holding.go):
//
//	CostBasis — "The total cost basis of the holding (e.g., the total amount spent to acquire all
//	assets currently in the holding)."
//	Quantity  — "The total quantity of the asset held, as reported by the financial institution."
//
// One Holding row is therefore the COMPLETE current position for its (account, security) pair —
// which is why the table is unique on that pair in the first place. Two rows for one pair are the
// same position stated twice, NOT two halves of it, so any arithmetic across them (summing most of
// all) doubles the user's position and overstates every portfolio total and allocation percentage
// that reads it. These tests pin the opposite behaviour: take the first row, discard the rest, do
// no arithmetic at all, and say out loud what was discarded.

// TestInvestmentSyncService_SyncHoldings_IdenticalDuplicateCollapsesWithValuesUnchanged is the
// likely real case — a provider repeating one row. Collapsing it must be lossless: the surviving
// row is byte-for-byte the row Plaid sent.
func TestInvestmentSyncService_SyncHoldings_IdenticalDuplicateCollapsesWithValuesUnchanged(t *testing.T) {
	f := newDuplicateHoldingFixture(t)

	position := makePlaidHolding(f.plaidAccountID, f.plaidSecurityID, 10.0, 165.0, 1650.0, 1500.0)
	written, result := f.sync(t, position, position)

	require.Len(t, written, 1, "the repeated position must reach the repository as ONE row")
	assert.Equal(t, f.internalSecurityID, written[0].SecurityID)
	assert.InDelta(t, 10.0, written[0].Quantity, 0.0001, "quantity is the TOTAL position: it must not change")
	require.NotNil(t, written[0].InstitutionValue)
	assert.InDelta(t, 1650.0, *written[0].InstitutionValue, 0.0001, "institution_value must not change")
	require.NotNil(t, written[0].CostBasis)
	assert.InDelta(t, 1500.0, *written[0].CostBasis, 0.0001, "cost_basis is the TOTAL paid: it must not change")
	require.NotNil(t, written[0].InstitutionPrice)
	assert.InDelta(t, 165.0, *written[0].InstitutionPrice, 0.0001, "institution_price must not change")
	assert.Equal(t, 1, result.HoldingsReplaced, "the collapsed row is one holding, not two")

	out := f.logs.String()
	assert.Contains(t, out, "discarded a duplicate holding", "the collapse must be logged, not silent")
	assert.Contains(t, out, f.plaidSecurityID, "the log must name the colliding security")
	assert.Contains(t, out, f.internalAccountID, "the log must name the account")
	assert.Contains(t, out, `"rows_seen_for_this_security":2`,
		"the log must state how many rows collapsed; got: %s", out)
	assert.Contains(t, out, `"differences":[]`,
		"an identical duplicate is a lossless no-op and must be visible as one; got: %s", out)
}

// TestInvestmentSyncService_SyncHoldings_DuplicateDifferingInQuantityKeepsTheFirst covers the
// contradictory case. The two rows claim different totals for one position; exactly one of them can
// be true and nothing here can tell which. First-wins is bounded-wrong (the answer is one of the
// two figures the provider actually sent); summing is unbounded-wrong.
func TestInvestmentSyncService_SyncHoldings_DuplicateDifferingInQuantityKeepsTheFirst(t *testing.T) {
	f := newDuplicateHoldingFixture(t)

	first := makePlaidHolding(f.plaidAccountID, f.plaidSecurityID, 10.0, 165.0, 1650.0, 1500.0)
	second := makePlaidHolding(f.plaidAccountID, f.plaidSecurityID, 4.0, 165.0, 660.0, 600.0)
	written, _ := f.sync(t, first, second)

	require.Len(t, written, 1)
	assert.InDelta(t, 10.0, written[0].Quantity, 0.0001, "the FIRST row's quantity survives; 14 would be the sum")
	require.NotNil(t, written[0].InstitutionValue)
	assert.InDelta(t, 1650.0, *written[0].InstitutionValue, 0.0001, "the first row's value survives; 2310 would be the sum")
	require.NotNil(t, written[0].CostBasis)
	assert.InDelta(t, 1500.0, *written[0].CostBasis, 0.0001, "the first row's basis survives; 2100 would be the sum")

	out := f.logs.String()
	assert.Contains(t, out, "discarded a duplicate holding")
	for _, want := range []string{"quantity", "10", "4", "institution_value", "1650", "660", "cost_basis", "1500", "600"} {
		assert.Contains(t, out, want,
			"the log must name what differed and BOTH values, so a contradiction reads as a contradiction; got: %s", out)
	}
}

// TestInvestmentSyncService_SyncHoldings_DuplicateDifferingInCurrencyIsNotSummed is Codex finding 2.
// Summing two differently-denominated figures and then labelling the result with whichever currency
// came first produces a number that means nothing AND defeats is_mixed_currency detection, which
// inspects persisted holding currency codes only. Taking one row whole keeps figure and unit
// together by construction.
func TestInvestmentSyncService_SyncHoldings_DuplicateDifferingInCurrencyIsNotSummed(t *testing.T) {
	f := newDuplicateHoldingFixture(t)

	usd := makePlaidHolding(f.plaidAccountID, f.plaidSecurityID, 10.0, 165.0, 1650.0, 1500.0)
	eur := makePlaidHolding(f.plaidAccountID, f.plaidSecurityID, 10.0, 152.0, 1520.0, 1400.0)
	eur.IsoCurrencyCode = plaidNullStr("EUR")
	written, _ := f.sync(t, usd, eur)

	require.Len(t, written, 1)
	require.NotNil(t, written[0].CurrencyCode)
	assert.Equal(t, "USD", *written[0].CurrencyCode, "the first row's currency survives")
	require.NotNil(t, written[0].InstitutionValue)
	assert.InDelta(t, 1650.0, *written[0].InstitutionValue, 0.0001,
		"the USD figure must stay whole: 3170 would be dollars and euros added together")
	require.NotNil(t, written[0].CostBasis)
	assert.InDelta(t, 1500.0, *written[0].CostBasis, 0.0001, "the USD basis must stay whole")

	out := f.logs.String()
	assert.Contains(t, out, "currency_code", "the currency contradiction must be named; got: %s", out)
	assert.Contains(t, out, "EUR", "the discarded currency must be visible; got: %s", out)
}

// TestInvestmentSyncService_SyncHoldings_DuplicateWithNilCostBasisFabricatesNoTotal is Codex
// finding 3. An absent cost basis is UNKNOWN, and adding it to a known figure used to return the
// known figure — so a position whose basis Plaid does not report acquired a "complete" basis and,
// with it, a fabricated unrealized gain. Taking one row whole means an unknown stays unknown.
func TestInvestmentSyncService_SyncHoldings_DuplicateWithNilCostBasisFabricatesNoTotal(t *testing.T) {
	f := newDuplicateHoldingFixture(t)

	unknownBasis := makePlaidHolding(f.plaidAccountID, f.plaidSecurityID, 10.0, 165.0, 1650.0, 0)
	unknownBasis.CostBasis = plaidNilFloat()
	knownBasis := makePlaidHolding(f.plaidAccountID, f.plaidSecurityID, 10.0, 165.0, 1650.0, 600.0)
	written, _ := f.sync(t, unknownBasis, knownBasis)

	require.Len(t, written, 1)
	assert.Nil(t, written[0].CostBasis,
		"the first row reported no cost basis; the duplicate's 600 must not be adopted as a complete total")

	out := f.logs.String()
	assert.Contains(t, out, "cost_basis", "the basis contradiction must be named; got: %s", out)
	assert.Contains(t, out, "600", "the discarded basis must be visible; got: %s", out)
}

// duplicateHoldingFixture holds the ids one SyncHoldings call needs, so each test above states only
// the holdings it is about.
type duplicateHoldingFixture struct {
	svc                InvestmentSyncService
	deps               investmentSyncDeps
	logs               *bytes.Buffer
	plaidAccountID     string
	internalAccountID  string
	plaidSecurityID    string
	internalSecurityID string
	userID             string
	linkedItem         *domain.LinkedItem
}

func newDuplicateHoldingFixture(t *testing.T) *duplicateHoldingFixture {
	t.Helper()
	svc, deps, logs := setupInvestmentSyncServiceWithLogger(t)
	userID := uuid.New().String()
	return &duplicateHoldingFixture{
		svc:                svc,
		deps:               deps,
		logs:               logs,
		plaidAccountID:     "plaid-acct-001",
		internalAccountID:  uuid.New().String(),
		plaidSecurityID:    "plaid-sec-001",
		internalSecurityID: uuid.New().String(),
		userID:             userID,
		linkedItem:         makeLinkedItem("item-001", userID),
	}
}

// sync runs SyncHoldings over holdings (all for the fixture's one account and one security) and
// returns the rows that actually reached the repository.
func (f *duplicateHoldingFixture) sync(t *testing.T, holdings ...plaid.Holding) ([]domain.Holding, *domain.HoldingSyncResult) {
	t.Helper()
	ctx := context.Background()

	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		holdings,
		[]plaid.Security{makePlaidSecurity(f.plaidSecurityID, "Apple Inc.", "AAPL")},
		makeMinimalPlaidItem(),
		"req-001",
	)
	f.deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, f.linkedItem.AccessToken).Return(holdingsResp, nil)
	f.deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).Return(map[string]string{
		f.plaidSecurityID: f.internalSecurityID,
	}, nil)
	f.deps.accountRepo.On("GetByLinkedItemID", ctx, f.linkedItem.ID).Return([]domain.Account{
		{ID: f.internalAccountID, PlaidAccountID: f.plaidAccountID, UserID: f.userID, Type: "investment"},
	}, nil)

	var written []domain.Holding
	f.deps.holdingRepo.On("ReplaceForAccount", ctx, f.internalAccountID, f.userID, mock.Anything).
		Run(func(args mock.Arguments) {
			written = args.Get(3).([]domain.Holding)
		}).Return(nil)

	result, err := f.svc.SyncHoldings(ctx, f.linkedItem)
	require.NoError(t, err)
	return written, result
}

// TestDescribeHoldingDuplicateDifferences_ReportsBothValuesPerField pins the difference report
// itself, including the two nil/absent asymmetries a plain != would get wrong.
func TestDescribeHoldingDuplicateDifferences_ReportsBothValuesPerField(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	s := func(v string) *string { return &v }

	t.Run("identical rows differ in nothing", func(t *testing.T) {
		h := domain.Holding{Quantity: 10, InstitutionValue: f(1650), CostBasis: f(1500), CurrencyCode: s("USD")}
		assert.Empty(t, describeHoldingDuplicateDifferences(h, h))
	})

	t.Run("a known value beside an absent one is a difference, not a match", func(t *testing.T) {
		kept := domain.Holding{Quantity: 10}
		discarded := domain.Holding{Quantity: 10, CostBasis: f(600)}
		diffs := describeHoldingDuplicateDifferences(kept, discarded)
		require.Len(t, diffs, 1)
		assert.Contains(t, diffs[0], "cost_basis")
		assert.Contains(t, diffs[0], "600")
		assert.Contains(t, strings.ToLower(diffs[0]), "absent")
	})

	t.Run("equal values behind different pointers are not a difference", func(t *testing.T) {
		kept := domain.Holding{InstitutionPrice: f(165)}
		discarded := domain.Holding{InstitutionPrice: f(165)}
		assert.Empty(t, describeHoldingDuplicateDifferences(kept, discarded))
	})

	t.Run("every differing field is reported with both values", func(t *testing.T) {
		kept := domain.Holding{Quantity: 10, InstitutionValue: f(1650), InstitutionPrice: f(165), CostBasis: f(1500), CurrencyCode: s("USD")}
		discarded := domain.Holding{Quantity: 4, InstitutionValue: f(660), InstitutionPrice: f(164), CostBasis: f(600), CurrencyCode: s("EUR")}
		joined := strings.Join(describeHoldingDuplicateDifferences(kept, discarded), " | ")
		for _, want := range []string{
			"quantity", "10", "4",
			"institution_value", "1650", "660",
			"institution_price", "165", "164",
			"cost_basis", "1500", "600",
			"currency_code", "USD", "EUR",
		} {
			assert.Contains(t, joined, want)
		}
	})
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
