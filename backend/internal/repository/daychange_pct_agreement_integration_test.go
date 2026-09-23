//go:build integration

package repository

// Issue #1631 M7: DayChangePct is computed TWICE — once in Go (domain.Holding.DayChangePct, behind
// ListHoldings and the portfolio summary) and once in SQL (TopMovers). Both divided by the CURRENT
// value instead of the prior close, and both were fixed. This test exists so they cannot drift
// apart again: it drives the SQL path against real Postgres and compares the answer to the Go path
// over the same numbers.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDayChangePct_GoAndSQLAgree_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-1631-dcp")

	// Prior close 100, now 105 → a +5% day. institution_value is the CURRENT value (10 x 105),
	// which is precisely the wrong denominator: it would yield 4.76%.
	const (
		quantity   = 10.0
		priorClose = 100.0
		nowPrice   = 105.0
	)
	currentValue := quantity * nowPrice

	_, err = tx.Exec(ctx, `
		UPDATE securities SET type = 'etf', close_price = $2 WHERE id = $1
	`, secID, priorClose)
	require.NoError(t, err)

	holdingID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings
			(id, user_id, account_id, security_id, quantity, cost_basis, institution_value, institution_price)
		VALUES ($1, $2, $3, $4, $5, 800.00, $6, $7)
	`, holdingID, userID, accountID, secID, quantity, currentValue, nowPrice)
	require.NoError(t, err)

	// Go path.
	holdings, err := NewHoldingRepository(tx).GetByUserIDWithSecurities(ctx, userID.String(), nil, nil)
	require.NoError(t, err)
	require.Len(t, holdings, 1)
	goPct := holdings[0].DayChangePct()
	require.NotNil(t, goPct)
	assert.InDelta(t, 5.0, *goPct, 0.0001, "Go path: a +5%% day must report 5%%, not 4.76%%")

	// SQL path.
	movers, err := NewInvestmentAnalyticsRepository(tx).TopMovers(ctx, userID.String(), 5)
	require.NoError(t, err)
	require.NotNil(t, movers)
	require.Len(t, movers.Gainers, 1, "the holding gained, so it must be a gainer")
	sqlPct := movers.Gainers[0].DayChangePct
	require.NotNil(t, sqlPct)
	assert.InDelta(t, 5.0, *sqlPct, 0.0001, "SQL path: a +5%% day must report 5%%, not 4.76%%")

	// The invariant that outlives both numbers.
	assert.InDelta(t, *goPct, *sqlPct, 0.0001,
		"the Go and SQL day-change percentages must agree; two implementations of one figure is "+
			"how #1631's original defect survived in both places")

	// And the absolute day change itself, which was always correct — pinned so a denominator fix
	// cannot quietly become a numerator change.
	require.NotNil(t, movers.Gainers[0].DayChange)
	assert.InDelta(t, quantity*(nowPrice-priorClose), *movers.Gainers[0].DayChange, 0.0001)
	dc := holdings[0].DayChange()
	require.NotNil(t, dc)
	assert.InDelta(t, quantity*(nowPrice-priorClose), *dc, 0.0001)
}
