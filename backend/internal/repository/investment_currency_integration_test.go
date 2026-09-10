//go:build integration

package repository

// Issue #1631 M8a. Every investment aggregate was a currency-blind sum: nothing in the domain
// recorded a currency, so two positions reported in different currencies were added together as
// though they were the same unit. There is no FX rate source anywhere in this system, so these
// tests pin the only honest behaviours available — DECLARE the mixed case on an aggregate, and
// REFUSE the per-row figure whose two operands are in different units.
//
// The fixtures deliberately use tiny round numbers: what is being pinned is which currency label
// (or refusal) comes out, not the arithmetic, which is unchanged by this task.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// seedCurrencyHolding inserts one holding with an explicit currency, and its own security row so
// the security currency can be set independently of the holding's.
func seedCurrencyHolding(
	ctx context.Context, t *testing.T, db DB,
	userID, accountID uuid.UUID,
	label string, value float64, holdingCcy, securityCcy *string, closePrice *float64,
) uuid.UUID {
	t.Helper()
	secID := uuid.New()
	_, err := db.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, type, close_price, currency_code)
		VALUES ($1, $2, $3, $4, 'equity', $5, $6)
	`, secID, "ccy-sec-"+secID.String(), "Security "+label, label, closePrice, securityCcy)
	require.NoError(t, err)

	holdingID := uuid.New()
	_, err = db.Exec(ctx, `
		INSERT INTO holdings
			(id, user_id, account_id, security_id, quantity, cost_basis, institution_value,
			 institution_price, currency_code)
		VALUES ($1, $2, $3, $4, 1, $5, $6, $7, $8)
	`, holdingID, userID, accountID, secID, value/2, value, value, holdingCcy)
	require.NoError(t, err)
	return holdingID
}

func ccyPtr(s string) *string { return &s }

func TestInvestmentAnalytics_CurrencyAwareAggregates_Integration(t *testing.T) {
	tests := []struct {
		name      string
		codes     []*string
		wantCcy   *string
		wantMixed bool
	}{
		{
			name:  "one stated currency is named",
			codes: []*string{ccyPtr("USD"), ccyPtr("USD")}, wantCcy: ccyPtr("USD"),
		},
		{
			// The pre-migration-000197 state of the whole table. It must not read as mixed.
			name:  "no stated currency is unknown, not mixed",
			codes: []*string{nil, nil}, wantCcy: nil,
		},
		{
			name:  "an unstated currency does not conflict with a stated one",
			codes: []*string{nil, ccyPtr("EUR")}, wantCcy: ccyPtr("EUR"),
		},
		{
			name:  "two stated currencies are mixed and named as neither",
			codes: []*string{ccyPtr("USD"), ccyPtr("EUR")}, wantCcy: nil, wantMixed: true,
		},
		{
			// Provider casing must not invent a third currency.
			name:  "casing is normalised before comparison",
			codes: []*string{ccyPtr("usd"), ccyPtr("USD")}, wantCcy: ccyPtr("USD"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := connectTestDB(t)
			ctx := context.Background()
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer tx.Rollback(ctx) //nolint:errcheck

			userID, _, accountID, _ := seedInvestmentFixture(ctx, t, tx, "ccy-agg")
			for i, code := range tt.codes {
				seedCurrencyHolding(ctx, t, tx, userID, accountID,
					string(rune('A'+i)), 100, code, code, nil)
			}

			assertCcy := func(t *testing.T, surface string, got *string, mixed bool) {
				t.Helper()
				assert.Equal(t, tt.wantMixed, mixed, "%s: mixed verdict", surface)
				if tt.wantCcy == nil {
					assert.Nil(t, got, "%s: must not name a currency", surface)
				} else {
					require.NotNil(t, got, "%s: must name its currency", surface)
					assert.Equal(t, *tt.wantCcy, *got, "%s: currency", surface)
				}
			}

			analytics := NewInvestmentAnalyticsRepository(tx)

			alloc, err := analytics.AllocationBreakdown(ctx, userID.String(), domain.AllocationOptions{})
			require.NoError(t, err)
			assertCcy(t, "AllocationBreakdown", alloc.CurrencyCode, alloc.IsMixedCurrency)

			gl, err := analytics.UnrealizedGainLoss(ctx, userID.String())
			require.NoError(t, err)
			assertCcy(t, "UnrealizedGainLoss", gl.CurrencyCode, gl.IsMixedCurrency)

			// portfolio_snapshots records the currency its totals are in — or NULL when it cannot
			// state one, which is the ONLY thing that lets GetPortfolioSummary refuse a
			// cross-currency day change.
			snapRepo := NewPortfolioSnapshotRepository(tx)
			snap, err := snapRepo.Upsert(ctx, userID.String(), time.Now().UTC().Truncate(24*time.Hour))
			require.NoError(t, err)
			if tt.wantCcy == nil {
				assert.Nil(t, snap.CurrencyCode, "a mixed or unknown total must not claim a currency")
			} else {
				require.NotNil(t, snap.CurrencyCode)
				assert.Equal(t, *tt.wantCcy, *snap.CurrencyCode)
			}
		})
	}
}

// TopMovers computes the day change in SQL rather than through domain.Holding.DayChange, so the
// currency refusal has to be repeated there. If it were not, the two surfaces would disagree
// about the same holding — the investments page showing no day change while the top-movers widget
// ranked it by one.
func TestInvestmentAnalytics_TopMovers_ExcludesACurrencyMismatchedHolding_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, _ := seedInvestmentFixture(ctx, t, tx, "ccy-movers")

	close90 := 90.0
	// Matched currencies: a real +10 mover.
	seedCurrencyHolding(ctx, t, tx, userID, accountID, "MATCH", 100,
		ccyPtr("GBP"), ccyPtr("GBP"), &close90)
	// Mismatched: institution_price in GBP, close_price in USD. Its "day change" is a unit error.
	seedCurrencyHolding(ctx, t, tx, userID, accountID, "MISMATCH", 100,
		ccyPtr("GBP"), ccyPtr("USD"), &close90)
	// Unknown on one side: must still be ranked, or the whole pre-#1631 user base loses the widget.
	seedCurrencyHolding(ctx, t, tx, userID, accountID, "UNKNOWN", 100,
		nil, ccyPtr("USD"), &close90)

	movers, err := NewInvestmentAnalyticsRepository(tx).TopMovers(ctx, userID.String(), 10)
	require.NoError(t, err)

	tickers := map[string]bool{}
	for _, g := range movers.Gainers {
		require.NotNil(t, g.TickerSymbol)
		tickers[*g.TickerSymbol] = true
	}
	assert.True(t, tickers["MATCH"], "a same-currency mover must be ranked")
	assert.True(t, tickers["UNKNOWN"], "an unstated currency must not suppress the figure")
	assert.False(t, tickers["MISMATCH"],
		"a holding whose price and close price are in different currencies has no day change to rank")
}

// The holding round-trip: both currencies must survive the read, because DayChange needs to
// compare them. Reading only one — or neither, as before this change — makes the mismatch
// invisible to the domain layer that is supposed to refuse it.
func TestHolding_GetByUserIDWithSecurities_ReadsBothCurrencies_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, _ := seedInvestmentFixture(ctx, t, tx, "ccy-read")
	close90 := 90.0
	seedCurrencyHolding(ctx, t, tx, userID, accountID, "FX", 100,
		ccyPtr("GBP"), ccyPtr("USD"), &close90)

	holdings, err := NewHoldingRepository(tx).GetByUserIDWithSecurities(ctx, userID.String(), nil)
	require.NoError(t, err)
	require.Len(t, holdings, 1)

	h := holdings[0]
	require.NotNil(t, h.CurrencyCode)
	assert.Equal(t, "GBP", *h.CurrencyCode, "the position currency")
	require.NotNil(t, h.Security)
	require.NotNil(t, h.Security.CurrencyCode)
	assert.Equal(t, "USD", *h.Security.CurrencyCode, "the close-price currency")

	assert.True(t, h.HasCurrencyMismatch())
	assert.Nil(t, h.DayChange(), "the domain must refuse the cross-currency subtraction")
}

// holding_snapshots freezes the currency alongside the values, so a historical position is stated
// in the currency it was reported in at the time rather than in whatever the live row says later.
func TestHoldingSnapshots_CaptureFreezesTheCurrency_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "ccy-hist")
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings
			(id, user_id, account_id, security_id, quantity, cost_basis, institution_value,
			 institution_price, currency_code)
		VALUES ($1, $2, $3, $4, 2, 80, 100, 50, 'CHF')
	`, uuid.New(), userID, accountID, secID)
	require.NoError(t, err)

	day, err := NewHoldingSnapshotRepository(tx).CaptureForUser(ctx, userID.String())
	require.NoError(t, err)

	rows, err := NewHoldingSnapshotRepository(tx).GetForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].CurrencyCode)
	assert.Equal(t, "CHF", *rows[0].CurrencyCode)

	// The live holding changing its reported currency must NOT rewrite the captured history.
	_, err = tx.Exec(ctx, `UPDATE holdings SET currency_code = 'USD' WHERE user_id = $1`, userID)
	require.NoError(t, err)
	rows, err = NewHoldingSnapshotRepository(tx).GetForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].CurrencyCode)
	assert.Equal(t, "CHF", *rows[0].CurrencyCode,
		"a captured row keeps the currency it was captured with until a new capture replaces it")
}

// The allocation endpoint has FOUR SQL shapes (grouped/by-account x with/without the cash and
// liabilities UNION branches) and each one had to grow a currency aggregate. Three of them are
// unreachable through the default options, so this exercises all four — a malformed aggregate in
// an optional branch would otherwise only surface for a user who ticked the box.
//
// The cash and liabilities branches read accounts.currency_code, which carries DEFAULT 'USD'
// (migration 000007). That is why a USD holdings portfolio plus a bank account reads USD rather
// than mixed here, and why a genuinely foreign bank account WOULD raise the flag.
func TestInvestmentAnalytics_AllocationCurrency_AllFourShapes_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountID, _ := seedInvestmentFixture(ctx, t, tx, "ccy-alloc")
	seedCurrencyHolding(ctx, t, tx, userID, accountID, "USDH", 100, ccyPtr("USD"), ccyPtr("USD"), nil)

	// A depository account in a DIFFERENT currency: the cash branch must make the total mixed.
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status,
		                      current_balance, currency_code)
		VALUES ($1, $2, $3, $4, 'Euro Checking', 'depository', 'active', 500, 'EUR')
	`, uuid.New(), linkedItemID, userID, "ccy-alloc-eur-"+uuid.NewString())
	require.NoError(t, err)

	repo := NewInvestmentAnalyticsRepository(tx)

	for _, tc := range []struct {
		name      string
		opts      domain.AllocationOptions
		wantCcy   *string
		wantMixed bool
	}{
		{"grouped, holdings only", domain.AllocationOptions{}, ccyPtr("USD"), false},
		{"grouped, with cash", domain.AllocationOptions{Include: []string{"cash"}}, nil, true},
		{"by account, holdings only",
			domain.AllocationOptions{GroupBy: domain.AllocationGroupByAccount}, ccyPtr("USD"), false},
		{"by account, with cash and liabilities",
			domain.AllocationOptions{
				GroupBy: domain.AllocationGroupByAccount,
				Include: []string{"cash", "liabilities"},
			}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.AllocationBreakdown(ctx, userID.String(), tc.opts)
			require.NoError(t, err, "the currency aggregate must be valid SQL in every branch")
			assert.Equal(t, tc.wantMixed, got.IsMixedCurrency)
			if tc.wantCcy == nil {
				assert.Nil(t, got.CurrencyCode)
			} else {
				require.NotNil(t, got.CurrencyCode)
				assert.Equal(t, *tc.wantCcy, *got.CurrencyCode)
			}
		})
	}
}
