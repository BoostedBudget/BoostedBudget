package repository

// pgxmock SQL-shape guard for GetByUserIDKeysetUnified chain-expansion.
//
// This test does NOT require a real database (no //go:build integration tag).
// It locks the SQL shape expected after the Task 2 fix so that the fix is
// detectable without DATABASE_URL.
//
// On current (pre-fix) code these assertions FAIL — that is the expected RED
// state for issue #1304 Task 1.
//
// Run with:
//
//	go test ./internal/repository/... -run TestUnifiedFeed_ChainExpansion_SQLShape -v

import (
	"context"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// unifiedFeedColumns lists the column names returned by GetByUserIDKeysetUnified's
// SELECT.  The order must match the scan loop in transaction_repository.go.
// Banking columns first (40 total), then investment-only columns.
func unifiedFeedColumns() []string {
	return []string{
		// core
		"id", "user_id", "account_id",
		"sort_date",
		"source_disc",
		"date", "amount", "name",
		"user_account_id", "linked_item_id",
		// banking-only
		"authorized_date",
		"merchant_name", "merchant_id",
		"data_source",
		"external_transaction_id",
		"pfc_primary", "pfc_detailed", "pfc_confidence_level", "pfc_category_icon_url",
		"pfc_primary_override", "pfc_detailed_override",
		"custom_category_override_id",
		"payment_channel",
		"pending",
		"user_notes",
		"category_override_kind",
		"effective_pfc_primary", "effective_pfc_detailed", "effective_category_name",
		"reviewed", "reviewed_at",
		"tags",
		// investment-only
		"security_id", "ticker_symbol", "security_name",
		"type", "subtype",
		"quantity", "price", "fees",
	}
}

// makeUnifiedFeedRow builds a minimal pgxmock row for an investment feed row.
func makeUnifiedFeedRow(userID, accountID string) []interface{} {
	src := "investment"
	return []interface{}{
		"txn-id-1",        // id
		userID,            // user_id
		accountID,         // account_id
		nil,               // sort_date (time.Time)
		src,               // source_disc
		nil,               // date
		100.00,            // amount
		"Buy AAPL",        // name
		(*string)(nil),    // user_account_id
		(*string)(nil),    // linked_item_id
		(*string)(nil),    // authorized_date
		(*string)(nil),    // merchant_name
		(*string)(nil),    // merchant_id
		(*string)(nil),    // data_source
		(*string)(nil),    // external_transaction_id
		(*string)(nil),    // pfc_primary
		(*string)(nil),    // pfc_detailed
		(*string)(nil),    // pfc_confidence_level
		(*string)(nil),    // pfc_category_icon_url
		(*string)(nil),    // pfc_primary_override
		(*string)(nil),    // pfc_detailed_override
		(*string)(nil),    // custom_category_override_id
		(*string)(nil),    // payment_channel
		(*bool)(nil),      // pending
		(*string)(nil),    // user_notes
		(*string)(nil),    // category_override_kind
		(*string)(nil),    // effective_pfc_primary
		(*string)(nil),    // effective_pfc_detailed
		(*string)(nil),    // effective_category_name
		(*bool)(nil),      // reviewed
		(*time.Time)(nil), // reviewed_at
		(*[]byte)(nil),    // tags (jsonb)
		(*string)(nil),    // security_id
		(*string)(nil),    // ticker_symbol
		(*string)(nil),    // security_name
		(*string)(nil),    // type (investment)
		(*string)(nil),    // subtype (investment)
		(*float64)(nil),   // quantity
		(*float64)(nil),   // price
		(*float64)(nil),   // fees
	}
}

// TestUnifiedFeed_ChainExpansion_SQLShape verifies that GetByUserIDKeysetUnified
// issues a query containing the EXISTS-based chain-expansion predicates.
//
// RED state (pre-fix): assertions for chain_a.status='active', it.user_account_id
// IS NOT NULL, and t.user_account_id IS NOT NULL will FAIL because the current
// code uses a direct JOIN rather than an EXISTS subquery.
//
// GREEN state (post-fix, Task 2): all three predicates will be present and this
// test will pass.
func TestUnifiedFeed_ChainExpansion_SQLShape(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewTransactionRepository(mock, enc)
	ctx := context.Background()
	userID := "user-shape-1"

	rows := pgxmock.NewRows(unifiedFeedColumns()).
		AddRow(makeUnifiedFeedRow(userID, "acct-shape-1")...)

	// After Task 2 the query must contain all three chain-expansion predicates.
	// On current code (pre-fix) these will NOT appear — ExpectQuery will fail to
	// match and the test will error, confirming RED state.
	//
	// We assert all three in a single combined regex so a single test failure
	// shows which predicates are absent.
	// The first-page whole-set aggregate now precedes the page query (#1626 Phase 2). pgxmock is
	// ORDERED, so this expectation must come first and must be pinned to the aggregate's own shape —
	// the aggregate WRAPS the page query, so a pattern matching the page would match the aggregate
	// too and this test would silently stop inspecting the statement it names.
	expectFeedTotalsAggregate(mock, 0, 0, pgxmock.AnyArg())
	mock.ExpectQuery(`chain_a\.status = 'active'`).
		WithArgs(anyArgs(2)...). // userID + limit+1
		WillReturnRows(rows)

	_, _, _, err = repo.GetByUserIDKeysetUnified(
		ctx,
		userID,
		domain.TransactionFilters{
			IncludeInvestmentActivity: true,
		},
		nil,
		10,
	)
	// We do NOT assert err==nil here because on pre-fix code the ExpectQuery regex
	// won't match and pgxmock returns "call to Query was not expected".
	// The test failure produced by ExpectationsWereMet is the RED signal.
	_ = err

	// Assert the expected query was issued (fails on pre-fix code that lacks the
	// chain_a.status='active' pattern).
	assert.NoError(t, mock.ExpectationsWereMet(),
		"GetByUserIDKeysetUnified must emit a query containing the EXISTS chain-expansion pattern chain_a.status='active'")
}

// TestUnifiedFeed_InvestmentChainPredicate_SQLShape verifies the investment-branch
// chain predicate: it.user_account_id IS NOT NULL.
// This will FAIL on pre-fix code (RED) and pass after Task 2 (GREEN).
func TestUnifiedFeed_InvestmentChainPredicate_SQLShape(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewTransactionRepository(mock, enc)
	ctx := context.Background()
	userID := "user-inv-chain-1"

	rows := pgxmock.NewRows(unifiedFeedColumns()).
		AddRow(makeUnifiedFeedRow(userID, "acct-inv-chain-1")...)

	// The first-page whole-set aggregate now precedes the page query (#1626 Phase 2). pgxmock is
	// ORDERED, so this expectation must come first and must be pinned to the aggregate's own shape —
	// the aggregate WRAPS the page query, so a pattern matching the page would match the aggregate
	// too and this test would silently stop inspecting the statement it names.
	expectFeedTotalsAggregate(mock, 0, 0, pgxmock.AnyArg())
	mock.ExpectQuery(`it\.user_account_id IS NOT NULL`).
		WithArgs(anyArgs(2)...).
		WillReturnRows(rows)

	_, _, _, err = repo.GetByUserIDKeysetUnified(
		ctx,
		userID,
		domain.TransactionFilters{
			IncludeInvestmentActivity: true,
		},
		nil,
		10,
	)
	_ = err

	assert.NoError(t, mock.ExpectationsWereMet(),
		"GetByUserIDKeysetUnified must include 'it.user_account_id IS NOT NULL' in the investment branch")
}

// TestUnifiedFeed_BankingChainPredicate_SQLShape verifies the banking-branch
// chain predicate: t.user_account_id IS NOT NULL.
// This will FAIL on pre-fix code (RED) and pass after Task 2 (GREEN).
func TestUnifiedFeed_BankingChainPredicate_SQLShape(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewTransactionRepository(mock, enc)
	ctx := context.Background()
	userID := "user-bank-chain-1"

	rows := pgxmock.NewRows(unifiedFeedColumns()).
		AddRow(makeUnifiedFeedRow(userID, "acct-bank-chain-1")...)

	// The first-page whole-set aggregate now precedes the page query (#1626 Phase 2). pgxmock is
	// ORDERED, so this expectation must come first and must be pinned to the aggregate's own shape —
	// the aggregate WRAPS the page query, so a pattern matching the page would match the aggregate
	// too and this test would silently stop inspecting the statement it names.
	expectFeedTotalsAggregate(mock, 0, 0, pgxmock.AnyArg())
	mock.ExpectQuery(`t\.user_account_id IS NOT NULL`).
		WithArgs(anyArgs(2)...).
		WillReturnRows(rows)

	_, _, _, err = repo.GetByUserIDKeysetUnified(
		ctx,
		userID,
		domain.TransactionFilters{},
		nil,
		10,
	)
	_ = err

	assert.NoError(t, mock.ExpectationsWereMet(),
		"GetByUserIDKeysetUnified must include 't.user_account_id IS NOT NULL' in the banking branch")
}

// TestUnifiedFeed_NoActiveJoin_SQLShape asserts that the old inner-JOIN pattern
// (pre-#1304 fix) is NOT present in the emitted query.
//
// Technique: register an ExpectQuery whose regex ONLY matches if the old JOIN
// string is present.  After the call, assert that ExpectationsWereMet returns
// an error — meaning the expected (old-JOIN) regex was never satisfied, proving
// the JOIN was absent.  This is the standard pgxmock idiom for "assert absent."
func TestUnifiedFeed_NoActiveJoin_SQLShape(t *testing.T) {
	ctx := context.Background()

	// --- banking branch ---
	t.Run("banking branch omits old active-account JOIN", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		enc := newTestFieldEncryptor(t)
		repo := NewTransactionRepository(mock, enc)

		rows := pgxmock.NewRows(unifiedFeedColumns()).
			AddRow(makeUnifiedFeedRow("user-no-join-1", "acct-no-join-1")...)

		// This regex would only match if the old JOIN-based active-account filter
		// were present in the banking branch.  It must NOT match post-fix.
		// The first-page whole-set aggregate now precedes the page query (#1626 Phase 2). pgxmock is
		// ORDERED, so this expectation must come first and must be pinned to the aggregate's own shape —
		// the aggregate WRAPS the page query, so a pattern matching the page would match the aggregate
		// too and this test would silently stop inspecting the statement it names.
		expectFeedTotalsAggregate(mock, 0, 0, pgxmock.AnyArg())
		mock.ExpectQuery(`JOIN accounts a ON a\.id = t\.account_id AND a\.status = .active.`).
			WithArgs(anyArgs(2)...).
			WillReturnRows(rows)

		_, _, _, _ = repo.GetByUserIDKeysetUnified(
			ctx,
			"user-no-join-1",
			domain.TransactionFilters{},
			nil,
			10,
		)

		// ExpectationsWereMet must return an error: the old-JOIN expectation was
		// never satisfied because the query no longer contains that JOIN.
		require.Error(t, mock.ExpectationsWereMet(),
			"old active-account JOIN should be absent from the banking branch (chain-expansion fix #1304)")
	})

	// --- investment branch ---
	t.Run("investment branch omits old active-account JOIN", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		enc := newTestFieldEncryptor(t)
		repo := NewTransactionRepository(mock, enc)

		rows := pgxmock.NewRows(unifiedFeedColumns()).
			AddRow(makeUnifiedFeedRow("user-no-join-2", "acct-no-join-2")...)

		// Would match the old JOIN in the investment branch.  Must not match post-fix.
		// The first-page whole-set aggregate now precedes the page query (#1626 Phase 2). pgxmock is
		// ORDERED, so this expectation must come first and must be pinned to the aggregate's own shape —
		// the aggregate WRAPS the page query, so a pattern matching the page would match the aggregate
		// too and this test would silently stop inspecting the statement it names.
		expectFeedTotalsAggregate(mock, 0, 0, pgxmock.AnyArg())
		mock.ExpectQuery(`JOIN accounts a ON a\.id = it\.account_id AND a\.status = .active.`).
			WithArgs(anyArgs(2)...).
			WillReturnRows(rows)

		_, _, _, _ = repo.GetByUserIDKeysetUnified(
			ctx,
			"user-no-join-2",
			domain.TransactionFilters{IncludeInvestmentActivity: true},
			nil,
			10,
		)

		require.Error(t, mock.ExpectationsWereMet(),
			"old active-account JOIN should be absent from the investment branch (chain-expansion fix #1304)")
	})
}
