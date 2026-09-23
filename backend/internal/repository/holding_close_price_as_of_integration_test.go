//go:build integration

package repository

// Issue #1631 M1. GetPortfolioSummary and ListHoldings both 500 in production with:
//
//	can't scan into dest[19] (col: close_price_as_of):
//	cannot scan date (OID 1082) in binary format into **string
//
// securities.close_price_as_of is DATE (migration 000051) while domain.Security.ClosePriceAsOf was
// *string, so GetByUserIDWithSecurities scanned a date through **string. pgx v5 has no binary-format
// plan for that pair.
//
// The reason no existing test caught it is the reason this file exists: pgx RESCUES the NULL case
// (scanPlanFail.Scan has an explicit null escape hatch), and every prior fixture — the two
// incidental integration callers included — left close_price_as_of NULL. Only a NON-NULL value
// reaches the failing scan plan. In production 21 of 34 securities carry one.
//
// So the assertion that matters is not "the call succeeds" but "the call succeeds WITH A VALUE
// PRESENT". A test that seeds NULL passes against the bug.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHolding_GetByUserIDWithSecurities_NonNullClosePriceAsOf_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-1631-cpao")

	// The production shape: a security priced with a real prior-close DATE.
	const closeAsOf = "2026-09-09"
	_, err = tx.Exec(ctx, `
		UPDATE securities
		SET type = 'etf', close_price = 96.68, close_price_as_of = $2::date,
		    institution_price_as_of = '2026-09-09T00:00:00Z'
		WHERE id = $1
	`, secID, closeAsOf)
	require.NoError(t, err, "seed a NON-NULL close_price_as_of — a NULL one cannot reproduce the bug")

	holdingID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings
			(id, user_id, account_id, security_id, quantity, cost_basis, institution_value, institution_price)
		VALUES ($1, $2, $3, $4, 208.388, 20597.95, 20146.95, 96.68)
	`, holdingID, userID, accountID, secID)
	require.NoError(t, err, "seed holding")

	repo := NewHoldingRepository(tx)

	holdings, err := repo.GetByUserIDWithSecurities(ctx, userID.String(), nil, nil)
	require.NoError(t, err,
		"GetByUserIDWithSecurities must scan a DATE close_price_as_of; a scan-plan error here is "+
			"the #1631 500 on /investments/portfolio/summary and /investments/holdings")

	require.Len(t, holdings, 1, "the seeded holding must come back")
	h := holdings[0]
	require.NotNil(t, h.Security, "security metadata must be joined")

	// Pin the VALUE, not just the absence of an error. Selecting the column as ::text, or dropping
	// it from the SELECT, would also stop the 500 — and would silently discard the only field that
	// can tell DayChange how stale its close price is (#1631 M7).
	require.NotNil(t, h.Security.ClosePriceAsOf, "close_price_as_of must round-trip, not vanish")
	expected, err := time.Parse("2006-01-02", closeAsOf)
	require.NoError(t, err)
	assert.Equal(t, expected, h.Security.ClosePriceAsOf.UTC(),
		"the DATE must round-trip as the same calendar day")

	assert.Equal(t, 208.388, h.Quantity)
	require.NotNil(t, h.Security.ClosePrice)
	assert.InDelta(t, 96.68, *h.Security.ClosePrice, 0.0001)
}

// TestHolding_GetByUserIDWithSecurities_NullClosePriceAsOf_Integration is the companion that proves
// the NULL path still works — and, read alongside the test above, documents why CI was green while
// production 500'd. This one passes both before and after the fix; that is the point.
func TestHolding_GetByUserIDWithSecurities_NullClosePriceAsOf_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-1631-cpao-null")

	holdingID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity)
		VALUES ($1, $2, $3, $4, 1.5)
	`, holdingID, userID, accountID, secID)
	require.NoError(t, err)

	holdings, err := NewHoldingRepository(tx).GetByUserIDWithSecurities(ctx, userID.String(), nil, nil)
	require.NoError(t, err)
	require.Len(t, holdings, 1)
	require.NotNil(t, holdings[0].Security)
	assert.Nil(t, holdings[0].Security.ClosePriceAsOf,
		"an absent prior close must stay absent rather than becoming a zero date")
}
