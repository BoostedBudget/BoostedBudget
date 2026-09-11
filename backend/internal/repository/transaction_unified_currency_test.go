package repository

// #1631 Gap A1 — the unified feed carries the investment currency, and ONLY on investment rows.
//
// Migration 000202 gave investment_transactions a currency_code, but the unified activity feed's
// investment arm never selected it, so `ActivityItem` reached the client with money fields and no
// unit. The one remaining surface that renders those figures — the investment detail drawer — had
// nothing to render them IN, and so asserted dollars it could not know.
//
// This is a pgxmock SQL/scan-shape test (no //go:build integration tag, no DATABASE_URL): it
// checks the two things that can actually go wrong here.
//
//  1. The investment arm selects the column, and the value reaches ActivityItem.CurrencyCode.
//  2. The BANKING arm does NOT fabricate one. `transactions` has no currency at all on this feed,
//     and an absent value is what the frontend's three-state model (named / mixed / unknown)
//     reads as "render as dollars, exactly as before" — correct for every row written before
//     000202. Stamping a code on a banking row would turn that absence into a confident wrong
//     answer, which is the defect this whole task exists to remove.
//
// It is also a COLUMN-ALIGNMENT guard for the feed. The two UNION arms and the rows.Scan must
// agree positionally; a column added to one arm only shifts every subsequent value silently (a
// ticker landing in security_name, a price landing in fees). These tests read values back, so a
// misalignment shows up as a hard scan error rather than as plausible-looking wrong data.

import (
	"context"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// unifiedFeedCurrencyRow builds one row in dateOnlyUnifiedColumns() order — the complete scan
// order of GetByUserIDKeysetUnified, reused rather than re-listed so a new column has ONE place to
// be declared and both files' scans fail together if it is missed. (unifiedFeedColumns() in
// transaction_unified_keyset_test.go is deliberately NOT that list: its consumers swallow the scan
// error because they only inspect the emitted SQL.)
//
// `currency` is applied verbatim, so a caller can hand it nil to model a banking row or a
// pre-000202 investment row.
func unifiedFeedCurrencyRow(id, userID, source string, currency *string, invType *string) []interface{} {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	acct := "acct-currency-1"
	qty := 1.5
	price := 201.25
	fees := 0.99
	attached := invType != nil
	return []interface{}{
		id, userID, &acct,
		now, source,
		now, 301.87, "Buy VOD",
		(*string)(nil), (*string)(nil), (*time.Time)(nil),
		(*string)(nil), (*string)(nil), (*string)(nil),
		(*string)(nil),
		(*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil),
		(*string)(nil), (*string)(nil), (*string)(nil),
		(*string)(nil), (*bool)(nil), (*string)(nil),
		(*string)(nil),
		(*string)(nil), (*string)(nil), (*string)(nil),
		(*bool)(nil), (*time.Time)(nil),
		([]byte)(nil),
		(*time.Time)(nil), (*string)(nil),
		(*string)(nil), (*string)(nil), (*string)(nil),
		invType, (*string)(nil),
		&qty, &price, &fees,
		&attached, (*bool)(nil),
		currency,
	}
}

func TestUnifiedFeed_InvestmentRowCarriesItsCurrency(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()
	userID := "user-currency-1"

	gbp := "GBP"
	buy := "buy"
	expectFeedTotalsAggregate(mock, 1, 301.87, anyArgs(1)...)
	mock.ExpectQuery(`(?is)it\.currency_code`).
		WithArgs(anyArgs(2)...).
		WillReturnRows(pgxmock.NewRows(dateOnlyUnifiedColumns()).
			AddRow(unifiedFeedCurrencyRow("inv-1", userID, "investment", &gbp, &buy)...))

	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID,
		domain.TransactionFilters{IncludeInvestmentActivity: true}, nil, 10)
	require.NoError(t, err,
		"the investment arm must select it.currency_code, and the scan must stay column-aligned")
	require.NoError(t, mock.ExpectationsWereMet())

	require.Len(t, items, 1)
	require.NotNil(t, items[0].CurrencyCode,
		"an investment row whose currency_code is set must carry it to the client — without it the "+
			"detail drawer has no unit to render price and fees in")
	assert.Equal(t, "GBP", *items[0].CurrencyCode)
}

func TestUnifiedFeed_BankingRowGainsNoFabricatedCurrency(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()
	userID := "user-currency-2"

	expectFeedTotalsAggregate(mock, 1, 301.87, anyArgs(1)...)
	mock.ExpectQuery(`(?is)UNION ALL`).
		WithArgs(anyArgs(2)...).
		WillReturnRows(pgxmock.NewRows(dateOnlyUnifiedColumns()).
			AddRow(unifiedFeedCurrencyRow("bank-1", userID, "banking", nil, nil)...))

	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID,
		domain.TransactionFilters{IncludeInvestmentActivity: true}, nil, 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Nil(t, items[0].CurrencyCode,
		"a banking row has no currency on this feed. Absence is the frontend's 'unknown' state, "+
			"which renders as dollars for the legacy base; a fabricated code would assert a unit "+
			"nothing established")
}
