package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// #1631 gap 1, the DB-edge half. domain/date_only_wire_test.go pins the wire SHAPE of the
// date-only fields; this file pins that the READ PATHS actually populate them, which is where the
// investment precedent (InvestmentTransaction.DateDisplay) puts its own equivalent test.
//
// ⚠ WHY pgxmock AND NOT AN INTEGRATION TEST. The claim here is about Go code — that the scan
// finalizer and the unified scan loop call PopulateDateDisplay — not about anything Postgres
// computes. pgxmock hands invented rows into the real scan path, which is exactly the right
// instrument for that claim. (The opposite holds for the SQL-shape questions in this package,
// which is why those live in integration tests.)
//
// ⚠ Every assertion is Equal against the exact calendar day. assert.Contains(got, "2026-01-02")
// would pass for "2026-01-02T00:00:00Z" as well and would therefore prove nothing.

// makeDateOnlyKeysetRow mirrors makeKeysetRow but sets authorized_date, so the nil and non-nil
// halves of AuthorizedDateDisplay are both exercised. The order is transactionSelectColumns.
func makeDateOnlyKeysetRow(id, userID string, txDate time.Time, authorizedDate *time.Time) []interface{} {
	return []interface{}{
		id, userID, "account-1", "item-1",
		"plaid", nil, nil,
		-10.00, txDate, authorizedDate, "Test Merchant", nil, nil,
		nil, nil, nil, nil,
		nil, false, nil, txDate, txDate,
		nil, nil,
		nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		false, nil,
		nil, nil, nil,
	}
}

// TestTransactionKeyset_PopulatesDateOnlyDisplayFields covers the scanTransactions path, which
// reaches clients through getTransactions (banking-only), getTransactionByID,
// updateTransactionTags, updateTransactionCategory and getCategoryTransactions — every one of
// them finalized by postScanTransactionState.
func TestTransactionKeyset_PopulatesDateOnlyDisplayFields(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()
	userID := "user-date-only-1"

	posted := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	authorized := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	expectFeedTotalsAggregate(mock, 2, -20.00, userID)
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(makeDateOnlyKeysetRow("00000000-0000-0000-0000-000000000001", userID, posted, &authorized)...).
		AddRow(makeDateOnlyKeysetRow("00000000-0000-0000-0000-000000000002", userID, posted, nil)...)
	mock.ExpectQuery(`(?is)FROM transactions t`).
		WithArgs(userID, 3).
		WillReturnRows(rows)

	txs, _, _, err := repo.GetByUserIDKeyset(ctx, userID, domain.TransactionFilters{}, nil, 2)
	require.NoError(t, err)
	require.Len(t, txs, 2)

	assert.Equal(t, "2026-01-02", txs[0].DateDisplay)
	require.NotNil(t, txs[0].AuthorizedDateDisplay)
	assert.Equal(t, "2026-01-01", *txs[0].AuthorizedDateDisplay)

	// The time.Time fields must SURVIVE — buildKeysetCursor reads Date and AuthorizedDate as
	// time.Time, so a fix that retyped them instead of adding siblings would break pagination.
	assert.Equal(t, posted, txs[0].Date)
	require.NotNil(t, txs[0].AuthorizedDate)
	assert.Equal(t, authorized, *txs[0].AuthorizedDate)

	// A NULL authorized_date must stay nil, not become the empty string: the UI renders
	// authorized_date || date and an empty string wins that fallback.
	assert.Equal(t, "2026-01-02", txs[1].DateDisplay)
	assert.Nil(t, txs[1].AuthorizedDateDisplay)

	require.NoError(t, mock.ExpectationsWereMet())
}

// dateOnlyUnifiedColumns is the full scan order of GetByUserIDKeysetUnified.
//
// ⚠ Deliberately NOT unifiedFeedColumns(): that helper serves the SQL-SHAPE tests, which ignore
// the returned error on purpose and therefore never noticed that it omits deleted_at,
// deleted_reason, security_attached and security_is_cash_equivalent. A test that reads values back
// needs the complete list or the scan fails on argument count.
func dateOnlyUnifiedColumns() []string {
	return []string{
		"id", "user_id", "account_id",
		"sort_date", "source_disc",
		"date", "amount", "name",
		"user_account_id", "linked_item_id", "authorized_date",
		"merchant_name", "merchant_id", "data_source",
		"external_transaction_id",
		"pfc_primary", "pfc_detailed", "pfc_confidence_level", "pfc_category_icon_url",
		"pfc_primary_override", "pfc_detailed_override", "custom_category_override_id",
		"payment_channel", "pending", "user_notes",
		"category_override_kind",
		"effective_pfc_primary", "effective_pfc_detailed", "effective_category_name",
		"reviewed", "reviewed_at",
		"tags",
		"deleted_at", "deleted_reason",
		"security_id", "ticker_symbol", "security_name",
		"type", "subtype",
		"quantity", "price", "fees",
		"security_attached", "security_is_cash_equivalent",
	}
}

// makeDateOnlyUnifiedRow builds one banking row of the unified feed in dateOnlyUnifiedColumns
// order.
func makeDateOnlyUnifiedRow(userID, accountID string, sortDate, date time.Time, authorizedDate *time.Time) []interface{} {
	src := domain.FeedSourceBanking
	dataSource := "plaid"
	pending := false
	reviewed := false
	return []interface{}{
		uuid.New().String(), userID, &accountID,
		sortDate, src,
		date, -12.34, "Coffee",
		(*string)(nil), (*string)(nil), authorizedDate,
		(*string)(nil), (*string)(nil), &dataSource,
		(*string)(nil),
		(*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil),
		(*string)(nil), (*string)(nil), (*string)(nil),
		(*string)(nil), &pending, (*string)(nil),
		(*string)(nil),
		(*string)(nil), (*string)(nil), (*string)(nil),
		&reviewed, (*time.Time)(nil),
		([]byte)(nil),
		(*time.Time)(nil), (*string)(nil),
		(*string)(nil), (*string)(nil), (*string)(nil),
		(*string)(nil), (*string)(nil),
		(*float64)(nil), (*float64)(nil), (*float64)(nil),
		(*bool)(nil), (*bool)(nil),
	}
}

// TestUnifiedFeed_PopulatesDateOnlyDisplayFields covers the UNION scan loop — the default
// getTransactions response, and the one whose sort_date the V:2 cursor is built from.
func TestUnifiedFeed_PopulatesDateOnlyDisplayFields(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()
	userID := "user-date-only-unified"

	// sort_date is COALESCE(authorized_date, date), so the two differ here on purpose: a fix that
	// derived sort_date from date would still pass if they were equal.
	sortDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	date := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	authorized := sortDate

	expectFeedTotalsAggregate(mock, 1, -12.34, pgxmock.AnyArg())
	rows := pgxmock.NewRows(dateOnlyUnifiedColumns()).
		AddRow(makeDateOnlyUnifiedRow(userID, "acct-1", sortDate, date, &authorized)...)
	mock.ExpectQuery(`(?is)UNION ALL`).
		WithArgs(anyArgs(2)...).
		WillReturnRows(rows)

	items, _, _, err := repo.GetByUserIDKeysetUnified(
		ctx,
		userID,
		domain.TransactionFilters{IncludeInvestmentActivity: true},
		nil,
		10,
	)
	require.NoError(t, err)
	require.Len(t, items, 1)

	assert.Equal(t, "2026-01-01", items[0].SortDateDisplay)
	assert.Equal(t, "2026-01-02", items[0].DateDisplay)
	require.NotNil(t, items[0].AuthorizedDateDisplay)
	assert.Equal(t, "2026-01-01", *items[0].AuthorizedDateDisplay)

	// SortDate must survive as a time.Time: the V:2 cursor carries it as SVT and folds it via
	// time.DateOnly for SV.
	assert.Equal(t, sortDate, items[0].SortDate)

	require.NoError(t, mock.ExpectationsWereMet())
}
