//go:build integration

package repository

// Issue #1569 Task 20 (S12). securities.UpsertBatch overwrote close_price / institution_price with
// whatever the current sync happened to carry, NULL included.
//
// WHY THAT IS WORSE THAN AN ORDINARY OVERWRITE. securities is the one table in this schema that is
// NOT user-scoped — "Securities are shared across users", says the interface it implements. So one
// user's sync returning a payload with no close_price for AAPL erased the last known price of AAPL
// for EVERY user holding it, and every holding valuation that reads it went blank at once. Nothing
// re-derives it: the price is gone until some other sync happens to carry one.
//
// The last known price of a security IS history — it is the only thing that can value a holding
// when today's payload omits one. Ticker, type, ISIN and CUSIP are not: they are refetchable
// identity metadata, they are what the provider is authoritative about, and blanking one destroys
// no financial record. Only the former is retained; see the repository's own comment for the line.
//
// Must be an integration test. pgxmock replays a canned result for matched query text and can say
// nothing about what COALESCE did to a row.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// truncateToDate drops the clock from a timestamp so a fixture value compares equal to what a
// Postgres DATE column returns. #1631 changed domain.Security.ClosePriceAsOf from *string to
// *time.Time, because the column is a DATE and pgx cannot scan one into a string.
func truncateToDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func TestSecurityRepository_UpsertBatch_NullPriceDoesNotEraseTheLastKnownOne_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck // always rolled back on the shared integration DB

	repo := NewSecurityRepository(tx)
	plaidID := fmt.Sprintf("sec-s12-%d", time.Now().UnixNano())

	closeAsOf := truncateToDate(time.Now().AddDate(0, 0, -1))
	instAsOf := time.Now().Add(-2 * time.Hour)
	closePrice := 187.42
	instPrice := 187.55
	ticker := "AAPL"

	// Sync 1: the provider carries prices.
	_, err = repo.UpsertBatch(ctx, []domain.Security{{
		PlaidSecurityID:      plaidID,
		Name:                 "Apple Inc.",
		TickerSymbol:         &ticker,
		ClosePrice:           &closePrice,
		ClosePriceAsOf:       &closeAsOf,
		InstitutionPrice:     &instPrice,
		InstitutionPriceAsOf: &instAsOf,
	}})
	require.NoError(t, err)

	var gotClose, gotInst *float64
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT close_price, institution_price FROM securities WHERE plaid_security_id = $1`, plaidID,
	).Scan(&gotClose, &gotInst))
	require.NotNil(t, gotClose, "arrangement: the first sync must actually have stored a price")
	require.NotNil(t, gotInst)

	// Sync 2: the SAME security, carrying no prices at all. This is the write that used to erase
	// them for every user in the system.
	_, err = repo.UpsertBatch(ctx, []domain.Security{{
		PlaidSecurityID: plaidID,
		Name:            "Apple Inc.",
		TickerSymbol:    &ticker,
	}})
	require.NoError(t, err)

	var afterClose, afterInst *float64
	var afterCloseAsOf *time.Time
	var afterInstAsOf *time.Time
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT close_price, close_price_as_of, institution_price, institution_price_as_of
		FROM securities WHERE plaid_security_id = $1`, plaidID,
	).Scan(&afterClose, &afterCloseAsOf, &afterInst, &afterInstAsOf))

	require.NotNil(t, afterClose, "a sync with no close_price must not erase the last known one — "+
		"securities is shared across ALL users, so this blanks every holding valuation of it")
	assert.InDelta(t, closePrice, *afterClose, 1e-9)
	require.NotNil(t, afterInst, "same for institution_price")
	assert.InDelta(t, instPrice, *afterInst, 1e-9)

	// The as_of must travel WITH its price. Retaining the price while letting its timestamp go NULL
	// (or vice versa) would produce a figure nothing can date, which is the same defect this issue
	// closed for account balances.
	require.NotNil(t, afterCloseAsOf, "close_price_as_of must be retained alongside close_price")
	require.NotNil(t, afterInstAsOf, "institution_price_as_of must be retained alongside institution_price")
}

func TestSecurityRepository_UpsertBatch_ANewPriceStillWins_Integration(t *testing.T) {
	// The discriminating case. A repository that simply never updated prices would satisfy the test
	// above and would freeze every valuation in the product.
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

	repo := NewSecurityRepository(tx)
	plaidID := fmt.Sprintf("sec-s12-move-%d", time.Now().UnixNano())

	oldPrice := 100.00
	oldAsOf := truncateToDate(time.Now().AddDate(0, 0, -3))
	newPrice := 111.11
	newAsOf := truncateToDate(time.Now().AddDate(0, 0, -1))

	_, err = repo.UpsertBatch(ctx, []domain.Security{{
		PlaidSecurityID: plaidID, Name: "Mover", ClosePrice: &oldPrice, ClosePriceAsOf: &oldAsOf,
	}})
	require.NoError(t, err)
	_, err = repo.UpsertBatch(ctx, []domain.Security{{
		PlaidSecurityID: plaidID, Name: "Mover", ClosePrice: &newPrice, ClosePriceAsOf: &newAsOf,
	}})
	require.NoError(t, err)

	var got *float64
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT close_price FROM securities WHERE plaid_security_id = $1`, plaidID).Scan(&got))
	require.NotNil(t, got)
	assert.InDelta(t, newPrice, *got, 1e-9, "a price the provider DID send must still win")

	// Identity metadata is NOT retained, deliberately, and this pins the distinction rather than
	// leaving it to the comment. A ticker is refetchable, the provider is authoritative about it,
	// and blanking one destroys no financial record — unlike a last known price, which is the only
	// thing that can value a holding when today's payload omits one.
	tickerA := "MOVE"
	_, err = repo.UpsertBatch(ctx, []domain.Security{{
		PlaidSecurityID: plaidID, Name: "Mover", TickerSymbol: &tickerA,
	}})
	require.NoError(t, err)
	_, err = repo.UpsertBatch(ctx, []domain.Security{{
		PlaidSecurityID: plaidID, Name: "Mover",
	}})
	require.NoError(t, err)
	var gotTicker *string
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT ticker_symbol FROM securities WHERE plaid_security_id = $1`, plaidID).Scan(&gotTicker))
	assert.Nil(t, gotTicker, "ticker_symbol is provider-authoritative identity, not retained history")
}

// The pairing claim, falsified separately. Retaining the price while letting a fresh timestamp
// through would stamp today's date onto last week's figure — a number nothing can date, which is
// the same defect this issue closed for account balances.
//
// This case exists because a per-column `COALESCE(EXCLUDED.close_price_as_of, ...)` — the obvious
// spelling, and one a reviewer would wave through — passes every other test in this file. It was
// found by mutating the CASE into that COALESCE and watching the suite stay green.
func TestSecurityRepository_UpsertBatch_AsOfTravelsWithItsPrice_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck // always rolled back

	repo := NewSecurityRepository(tx)
	plaidID := fmt.Sprintf("sec-s12-pair-%d", time.Now().UnixNano())

	oldPrice := 100.00
	oldAsOf := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	staleButFresherAsOf := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	instPrice := 100.50
	oldInstAsOf := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	fresherInstAsOf := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	_, err = repo.UpsertBatch(ctx, []domain.Security{{
		PlaidSecurityID: plaidID, Name: "Pairing",
		ClosePrice: &oldPrice, ClosePriceAsOf: &oldAsOf,
		InstitutionPrice: &instPrice, InstitutionPriceAsOf: &oldInstAsOf,
	}})
	require.NoError(t, err)

	// A payload carrying timestamps but NO prices.
	_, err = repo.UpsertBatch(ctx, []domain.Security{{
		PlaidSecurityID: plaidID, Name: "Pairing",
		ClosePriceAsOf: &staleButFresherAsOf, InstitutionPriceAsOf: &fresherInstAsOf,
	}})
	require.NoError(t, err)

	var gotPrice *float64
	var gotAsOf *time.Time
	var gotInstAsOf *time.Time
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT close_price, close_price_as_of, institution_price_as_of
		FROM securities WHERE plaid_security_id = $1`, plaidID).Scan(&gotPrice, &gotAsOf, &gotInstAsOf))

	require.NotNil(t, gotPrice)
	assert.InDelta(t, oldPrice, *gotPrice, 1e-9)
	require.NotNil(t, gotAsOf)
	assert.Equal(t, "2026-01-05", gotAsOf.Format("2006-01-02"),
		"the retained price must keep ITS OWN date — 2026-08-20 here would date a January figure to August")
	require.NotNil(t, gotInstAsOf)
	assert.Equal(t, oldInstAsOf.UTC(), gotInstAsOf.UTC(),
		"institution_price_as_of must move with institution_price for the same reason")
}
