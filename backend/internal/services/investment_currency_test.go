package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func ccy(s string) *string { return &s }

func holdingIn(id, currency string, instValue float64) domain.Holding {
	v := instValue
	h := domain.Holding{
		ID:               id,
		UserID:           "user-abc",
		AccountID:        "acct-1",
		SecurityID:       "sec-" + id,
		Quantity:         1,
		InstitutionValue: &v,
	}
	if currency != "" {
		h.CurrencyCode = ccy(currency)
	}
	return h
}

// Issue #1631 M8a: GetPortfolioSummary adds institution_value across every holding with no regard
// for the currency each one is denominated in. With no FX rate source in this system that sum
// cannot be corrected — so it must at least be LABELLED, because the failure mode being fixed is
// not "the number is hard to compute" but "the number is silently wrong".
func TestInvestmentService_GetPortfolioSummary_DeclaresItsCurrency(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"

	tests := []struct {
		name      string
		holdings  []domain.Holding
		wantCcy   *string
		wantMixed bool
	}{
		{
			name:     "a single-currency portfolio names its currency",
			holdings: []domain.Holding{holdingIn("h1", "USD", 100), holdingIn("h2", "USD", 50)},
			wantCcy:  ccy("USD"),
		},
		{
			// The pre-#1631 state of the entire table. It must NOT read as mixed: nothing about
			// the data changed, only our willingness to record a currency.
			name:     "a portfolio that states no currency is not mixed",
			holdings: []domain.Holding{holdingIn("h1", "", 100), holdingIn("h2", "", 50)},
			wantCcy:  nil,
		},
		{
			name:      "two currencies are declared mixed and the total names neither",
			holdings:  []domain.Holding{holdingIn("h1", "USD", 100), holdingIn("h2", "EUR", 50)},
			wantCcy:   nil,
			wantMixed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			holdingRepo := new(MockHoldingRepository)
			snapshotRepo := new(MockPortfolioSnapshotRepository)
			invTxnRepo := new(MockInvestmentTransactionRepository)
			svc := NewInvestmentService(holdingRepo, snapshotRepo, invTxnRepo)

			holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).
				Return(tt.holdings, nil)
			snapshotRepo.On("GetLatestBefore", ctx, userID, time.Now().UTC().Truncate(24*time.Hour)).
				Return((*domain.PortfolioSnapshot)(nil), nil)

			summary, err := svc.GetPortfolioSummary(ctx, userID)
			require.NoError(t, err)

			// The sum itself is unchanged — this task does not silently alter anyone's totals.
			assert.InDelta(t, 150.0, summary.TotalValue, 1e-9)
			assert.Equal(t, tt.wantMixed, summary.IsMixedCurrency)
			if tt.wantCcy == nil {
				assert.Nil(t, summary.CurrencyCode)
			} else {
				require.NotNil(t, summary.CurrencyCode)
				assert.Equal(t, *tt.wantCcy, *summary.CurrencyCode)
			}
		})
	}
}

// A prior snapshot recorded in one currency cannot be subtracted from a live total in another.
// The snapshot records the currency its totals are in precisely so this check is possible.
func TestInvestmentService_GetPortfolioSummary_RefusesADayChangeAcrossCurrencies(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"

	priorGated := 90.0
	newPrior := func(currency *string) *domain.PortfolioSnapshot {
		return &domain.PortfolioSnapshot{
			UserID:                   userID,
			SnapshotDate:             time.Now().UTC().AddDate(0, 0, -1),
			TotalValue:               90,
			TotalValueInvestmentOnly: &priorGated,
			CurrencyCode:             currency,
		}
	}

	t.Run("the same currency on both sides still computes", func(t *testing.T) {
		holdingRepo := new(MockHoldingRepository)
		snapshotRepo := new(MockPortfolioSnapshotRepository)
		svc := NewInvestmentService(holdingRepo, snapshotRepo, new(MockInvestmentTransactionRepository))

		holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).
			Return([]domain.Holding{holdingIn("h1", "GBP", 100)}, nil)
		snapshotRepo.On("GetLatestBefore", ctx, userID, time.Now().UTC().Truncate(24*time.Hour)).Return(newPrior(ccy("GBP")), nil)

		summary, err := svc.GetPortfolioSummary(ctx, userID)
		require.NoError(t, err)
		require.NotNil(t, summary.DayChange)
		assert.InDelta(t, 10.0, *summary.DayChange, 1e-9)
	})

	t.Run("a different currency on the prior snapshot refuses the day change", func(t *testing.T) {
		holdingRepo := new(MockHoldingRepository)
		snapshotRepo := new(MockPortfolioSnapshotRepository)
		svc := NewInvestmentService(holdingRepo, snapshotRepo, new(MockInvestmentTransactionRepository))

		holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).
			Return([]domain.Holding{holdingIn("h1", "GBP", 100)}, nil)
		snapshotRepo.On("GetLatestBefore", ctx, userID, time.Now().UTC().Truncate(24*time.Hour)).Return(newPrior(ccy("USD")), nil)

		summary, err := svc.GetPortfolioSummary(ctx, userID)
		require.NoError(t, err)
		assert.Nil(t, summary.DayChange, "100 GBP minus 90 USD is not a quantity of money")
		assert.Nil(t, summary.DayChangePct)
	})

	t.Run("an unrecorded snapshot currency does not suppress the day change", func(t *testing.T) {
		// Every snapshot written before migration 000197 has currency_code NULL. Refusing on
		// unknown would blank the day change for the whole existing user base.
		holdingRepo := new(MockHoldingRepository)
		snapshotRepo := new(MockPortfolioSnapshotRepository)
		svc := NewInvestmentService(holdingRepo, snapshotRepo, new(MockInvestmentTransactionRepository))

		holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).
			Return([]domain.Holding{holdingIn("h1", "GBP", 100)}, nil)
		snapshotRepo.On("GetLatestBefore", ctx, userID, time.Now().UTC().Truncate(24*time.Hour)).Return(newPrior(nil), nil)

		summary, err := svc.GetPortfolioSummary(ctx, userID)
		require.NoError(t, err)
		require.NotNil(t, summary.DayChange)
	})
}

// Issue #1631 M8a: Plaid returns iso_currency_code / unofficial_currency_code on EVERY security,
// holding and investment transaction, and all three mappers dropped both on the floor. The test
// fixtures in this package had been setting IsoCurrencyCode all along, which is a good measure of
// how invisible the omission was — the data was there, nothing read it.
func TestPlaidMappers_ThreadTheCurrencyThrough(t *testing.T) {
	t.Run("security takes the ISO code", func(t *testing.T) {
		sec := makePlaidSecurity("plaid-sec-ccy-1", "Vanguard FTSE", "VEU")
		out := mapPlaidSecuritiesToDomain([]plaid.Security{sec}, nil)
		require.Len(t, out, 1)
		require.NotNil(t, out[0].CurrencyCode)
		assert.Equal(t, "USD", *out[0].CurrencyCode)
	})

	t.Run("security falls back to the unofficial code", func(t *testing.T) {
		// Plaid guarantees exactly one of the two is non-null, and the unofficial one covers
		// cryptocurrencies. Dropping it — as accounts.currency_code does — would record a crypto
		// position as having NO currency, and unknown is defined to merge silently into any
		// portfolio's sum. That is the exact defect this task exists to close, so the fallback is
		// load-bearing rather than tidiness.
		sec := makePlaidSecurity("plaid-sec-ccy-2", "Bitcoin", "BTC")
		sec.IsoCurrencyCode = plaidNilStr()
		sec.UnofficialCurrencyCode = plaidNullStr("BTC")
		out := mapPlaidSecuritiesToDomain([]plaid.Security{sec}, nil)
		require.Len(t, out, 1)
		require.NotNil(t, out[0].CurrencyCode)
		assert.Equal(t, "BTC", *out[0].CurrencyCode)
	})

	t.Run("security with neither code stays unknown", func(t *testing.T) {
		sec := makePlaidSecurity("plaid-sec-ccy-3", "Mystery", "MYST")
		sec.IsoCurrencyCode = plaidNilStr()
		sec.UnofficialCurrencyCode = plaidNilStr()
		out := mapPlaidSecuritiesToDomain([]plaid.Security{sec}, nil)
		require.Len(t, out, 1)
		assert.Nil(t, out[0].CurrencyCode, "absent must be recorded as absent, never defaulted")
	})

	t.Run("a blank code is not a currency", func(t *testing.T) {
		sec := makePlaidSecurity("plaid-sec-ccy-4", "Blank", "BLNK")
		sec.IsoCurrencyCode = plaidNullStr("   ")
		sec.UnofficialCurrencyCode = plaidNilStr()
		out := mapPlaidSecuritiesToDomain([]plaid.Security{sec}, nil)
		require.Len(t, out, 1)
		assert.Nil(t, out[0].CurrencyCode)
	})

	t.Run("investment transaction takes its own code", func(t *testing.T) {
		pt := makePlaidInvTxn("txn-ccy-1", "plaid-acct-1", "plaid-sec-1", 100, "2026-01-02")
		pt.IsoCurrencyCode = plaidNilStr()
		pt.UnofficialCurrencyCode = plaidNullStr("ETH")
		out, drops := mapPlaidInvestmentTransactions(
			[]plaid.InvestmentTransaction{pt},
			"user-1",
			map[string]string{"plaid-acct-1": "acct-1"},
			map[string]*string{},
			map[string]struct{}{},
			map[string]string{"plaid-sec-1": "sec-1"},
			nil,
		)
		require.Len(t, out, 1, "drops: %+v", drops)
		require.NotNil(t, out[0].CurrencyCode)
		assert.Equal(t, "ETH", *out[0].CurrencyCode)
	})
}

// The holdings mapper is inline in SyncHoldings rather than a standalone function, so its share
// of #1631 M8a is pinned through the sync. The currency read here must be the HOLDING's, not the
// security's: Plaid prices a foreign-currency security in USD while the institution reports the
// position in its own currency, and taking the security's would erase the very mismatch the
// domain layer then refuses a day change over.
func TestInvestmentSyncService_SyncHoldings_ThreadsTheHoldingCurrency(t *testing.T) {
	svc, deps := setupInvestmentSyncService(t)
	ctx := context.Background()

	plaidAccountID := "plaid-acct-ccy"
	internalAccountID := uuid.New().String()
	plaidSecurityID := "plaid-sec-ccy"
	internalSecurityID := "internal-ccy-" + uuid.New().String()
	userID := uuid.New().String()
	linkedItem := makeLinkedItem("item-ccy", userID)

	sec := makePlaidSecurity(plaidSecurityID, "Foreign Thing plc", "FGN")
	sec.IsoCurrencyCode = plaidNullStr("USD") // Plaid prices the security in USD ...
	holding := makePlaidHolding(plaidAccountID, plaidSecurityID, 5.0, 165.0, 825.0, 750.0)
	holding.IsoCurrencyCode = plaidNullStr("GBP") // ... while the institution reports GBP.

	holdingsResp := plaid.NewInvestmentsHoldingsGetResponse(
		[]plaid.AccountBase{},
		[]plaid.Holding{holding},
		[]plaid.Security{sec},
		makeMinimalPlaidItem(),
		"req-ccy",
	)
	deps.plaidSvc.On("InvestmentsHoldingsGet", ctx, "tok").Return(holdingsResp, nil)
	deps.securityRepo.On("UpsertBatch", ctx, mock.Anything).
		Return(map[string]string{plaidSecurityID: internalSecurityID}, nil)
	deps.accountRepo.On("GetByLinkedItemID", ctx, linkedItem.ID).Return([]domain.Account{
		{ID: internalAccountID, PlaidAccountID: plaidAccountID, UserID: userID},
	}, nil)

	var captured []domain.Holding
	deps.holdingRepo.On("ReplaceForAccount", ctx, internalAccountID, userID, mock.Anything).
		Run(func(args mock.Arguments) {
			captured = args.Get(3).([]domain.Holding)
		}).Return(nil)

	_, err := svc.SyncHoldings(ctx, linkedItem)
	require.NoError(t, err)

	require.Len(t, captured, 1)
	require.NotNil(t, captured[0].CurrencyCode)
	assert.Equal(t, "GBP", *captured[0].CurrencyCode,
		"the position currency comes from the holding, not from the security's price currency")
}

// #1631 M8a fix round 2, I-1. portfolio_snapshots.currency_code NULL was overloaded — it meant
// "mixed" AND it meant "nobody stated one" — and CurrenciesConflict treats an unknown currency as
// NO conflict. So a MIXED prior snapshot slipped straight past the guard:
//
//	Day 1  holdings USD + GBP  -> snapshot currency_code NULL, total a currency-blind sum
//	Day 2  the GBP sleeve is sold; live holdings all USD -> live currency "USD", not mixed
//	-> day_change = USD_today - (USD+GBP)_yesterday, returned as a plain number, on a response
//	   whose is_mixed_currency is FALSE.
//
// That is an unlabelled cross-currency subtraction on the very surface this design gave the
// STRICTER treatment to, which is worse than the flagged aggregates it sits beside. The snapshot
// now records mixedness in its own column so the guard can see it.
func TestInvestmentService_GetPortfolioSummary_RefusesADayChangeAgainstAMixedPriorSnapshot(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"
	priorGated := 90.0

	newPrior := func(currency *string, mixed bool) *domain.PortfolioSnapshot {
		return &domain.PortfolioSnapshot{
			UserID:                   userID,
			SnapshotDate:             time.Now().UTC().AddDate(0, 0, -1),
			TotalValue:               90,
			TotalValueInvestmentOnly: &priorGated,
			CurrencyCode:             currency,
			IsMixedCurrency:          mixed,
		}
	}

	summaryWith := func(t *testing.T, prior *domain.PortfolioSnapshot, live []domain.Holding) *domain.PortfolioSummary {
		t.Helper()
		holdingRepo := new(MockHoldingRepository)
		snapshotRepo := new(MockPortfolioSnapshotRepository)
		svc := NewInvestmentService(holdingRepo, snapshotRepo, new(MockInvestmentTransactionRepository))
		holdingRepo.On("GetByUserIDWithSecurities", ctx, userID, (*string)(nil)).Return(live, nil)
		snapshotRepo.On("GetLatestBefore", ctx, userID, time.Now().UTC().Truncate(24*time.Hour)).
			Return(prior, nil)
		summary, err := svc.GetPortfolioSummary(ctx, userID)
		require.NoError(t, err)
		return summary
	}

	t.Run("mixed prior against a single-currency today is refused", func(t *testing.T) {
		// The exact scenario above. The old guard saw (NULL, "USD") and computed.
		s := summaryWith(t, newPrior(nil, true), []domain.Holding{holdingIn("h1", "USD", 100)})
		assert.Nil(t, s.DayChange,
			"a currency-blind prior total cannot be subtracted from a USD total")
		assert.Nil(t, s.DayChangePct)
		assert.False(t, s.IsMixedCurrency, "today really is single-currency; only the PRIOR was mixed")
	})

	t.Run("mixed prior against a mixed today is also refused", func(t *testing.T) {
		// Two currency-blind sums are not comparable either: the mix can change composition
		// between the two days, so their difference is not a quantity of anything.
		s := summaryWith(t, newPrior(nil, true), []domain.Holding{
			holdingIn("h1", "USD", 100), holdingIn("h2", "EUR", 50),
		})
		assert.Nil(t, s.DayChange)
		assert.True(t, s.IsMixedCurrency)
	})

	t.Run("an unlabelled but NOT mixed prior still computes", func(t *testing.T) {
		// (NULL, false) is every snapshot written before migration 000197. Refusing on these
		// would blank the day change for the whole existing user base, which is the same
		// unknown-is-not-a-currency rule the rest of this task turns on.
		s := summaryWith(t, newPrior(nil, false), []domain.Holding{holdingIn("h1", "USD", 100)})
		require.NotNil(t, s.DayChange)
		assert.InDelta(t, 10.0, *s.DayChange, 1e-9)
	})
}

// #1631 M8a fix round 2, I-2. Migration 000197 gave portfolio_snapshots a currency and
// GetPortfolioHistory then threw it away: PortfolioHistoryPoint was date + total_value, so a chart
// could plot a series in which some days are currency-blind sums and others are single-currency
// totals, with no label and no flag anywhere on the response. That is the same defect already
// closed for PortfolioSummary, AllocationBreakdown, GainLossSummary, IncomeSummary and
// HoldingSnapshotHistory — this was the one investment surface still missing it.
//
// The verdict is RANGE-level, not per-point, deliberately: a line chart has one y-axis with one
// unit, so a series whose points disagree is broken as a whole rather than point by point, and a
// per-point currency would invite a client to render a per-point symbol as if the points were
// comparable. They are not, and there is no FX rate source here to make them so.
func TestInvestmentService_GetPortfolioHistory_DeclaresTheSeriesCurrency(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)

	snap := func(day int, total float64, code *string, mixed bool) domain.PortfolioSnapshot {
		return domain.PortfolioSnapshot{
			UserID:          userID,
			SnapshotDate:    time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC),
			TotalValue:      total,
			CurrencyCode:    code,
			IsMixedCurrency: mixed,
		}
	}

	tests := []struct {
		name      string
		snaps     []domain.PortfolioSnapshot
		wantCcy   *string
		wantMixed bool
	}{
		{
			name:    "a series of one currency names it",
			snaps:   []domain.PortfolioSnapshot{snap(1, 100, ccy("USD"), false), snap(2, 110, ccy("USD"), false)},
			wantCcy: ccy("USD"),
		},
		{
			// Every snapshot written before migration 000197. Unknown is not a currency, so this
			// must not read as mixed or the whole existing user base gets a warning about data
			// that has not changed.
			name:  "a series that states no currency is unknown, not mixed",
			snaps: []domain.PortfolioSnapshot{snap(1, 100, nil, false), snap(2, 110, nil, false)},
		},
		{
			name:      "two currencies across the range make the SERIES mixed",
			snaps:     []domain.PortfolioSnapshot{snap(1, 100, ccy("USD"), false), snap(2, 110, ccy("EUR"), false)},
			wantMixed: true,
		},
		{
			// The case only migration 000198 can express: the day itself was a currency-blind
			// sum. One such day poisons the series even though no two days disagree.
			name:      "a single currency-blind day makes the series mixed",
			snaps:     []domain.PortfolioSnapshot{snap(1, 100, ccy("USD"), false), snap(2, 110, nil, true)},
			wantMixed: true,
		},
		{
			name:    "an unstated day alongside a stated one does not conflict",
			snaps:   []domain.PortfolioSnapshot{snap(1, 100, nil, false), snap(2, 110, ccy("GBP"), false)},
			wantCcy: ccy("GBP"),
		},
		{
			name: "an empty range states nothing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshotRepo := new(MockPortfolioSnapshotRepository)
			svc := NewInvestmentService(new(MockHoldingRepository), snapshotRepo,
				new(MockInvestmentTransactionRepository))
			snapshotRepo.On("GetByUserDateRange", ctx, userID, start, end).Return(tt.snaps, nil)

			history, err := svc.GetPortfolioHistory(ctx, userID, start, end)
			require.NoError(t, err)

			// The points themselves are unchanged — this task does not alter anyone's series.
			require.Len(t, history.Points, len(tt.snaps))
			for i := range tt.snaps {
				assert.Equal(t, tt.snaps[i].SnapshotDate.Format("2006-01-02"), history.Points[i].Date)
				assert.InDelta(t, tt.snaps[i].TotalValue, history.Points[i].TotalValue, 1e-9)
			}

			assert.Equal(t, tt.wantMixed, history.IsMixedCurrency)
			if tt.wantCcy == nil {
				assert.Nil(t, history.CurrencyCode)
			} else {
				require.NotNil(t, history.CurrencyCode)
				assert.Equal(t, *tt.wantCcy, *history.CurrencyCode)
			}
		})
	}
}

// TestPlaidCurrencyCode_DropsAValueThatOverflowsTheColumn pins #1631 I5: currency_code is
// VARCHAR(16) (migration 000197) and Postgres HARD-ERRORS (22001) on an overlong value rather
// than truncating it — before this fix, a Plaid unofficial_currency_code over 16 bytes turned
// "failed to upsert securities on page N", aborting the whole page and wedging that item's
// investment sync on every subsequent run. plaidCurrencyCode must drop only the FIELD (return
// nil), never abort the caller, matching the principle parsePlaidDate states for its own field.
func TestPlaidCurrencyCode_DropsAValueThatOverflowsTheColumn(t *testing.T) {
	overlong := "THIS_CODE_IS_WAY_TOO_LONG_FOR_THE_COLUMN" // 41 bytes
	exactly16 := "EXACTLYSIXTEENC"                         // 15 bytes — pad below to hit 16 precisely
	exactly16 = exactly16 + "C"                            // 16 bytes exactly, must NOT be dropped

	require.Len(t, exactly16, 16, "arrangement: this fixture must be exactly at the column limit")
	require.Greater(t, len(overlong), 16, "arrangement: this fixture must exceed the column limit")

	t.Run("an ISO code within the limit is kept", func(t *testing.T) {
		got := plaidCurrencyCode(ccy("USD"), nil, nil, "test")
		require.NotNil(t, got)
		assert.Equal(t, "USD", *got)
	})

	t.Run("a value exactly at the 16-byte limit is kept", func(t *testing.T) {
		got := plaidCurrencyCode(ccy(exactly16), nil, nil, "test")
		require.NotNil(t, got)
		assert.Equal(t, exactly16, *got)
	})

	t.Run("an overlong iso code is dropped, not returned", func(t *testing.T) {
		got := plaidCurrencyCode(ccy(overlong), nil, nil, "test")
		assert.Nil(t, got, "an overlong value must be dropped (nil), never returned to overflow the column")
	})

	t.Run("an overlong iso code falls through to a valid unofficial code", func(t *testing.T) {
		got := plaidCurrencyCode(ccy(overlong), ccy("BTC"), nil, "test")
		require.NotNil(t, got, "the overlong ISO leg must not swallow a perfectly good fallback")
		assert.Equal(t, "BTC", *got)
	})

	t.Run("an overlong unofficial code is also dropped", func(t *testing.T) {
		got := plaidCurrencyCode(nil, ccy(overlong), nil, "test")
		assert.Nil(t, got)
	})

	t.Run("a nil logger does not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			plaidCurrencyCode(ccy(overlong), nil, nil, "test")
		})
	})
}
