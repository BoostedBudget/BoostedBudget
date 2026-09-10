//go:build integration

package repository

// Integration tests for the investment stack (#138 gap fixes).
// These tests execute SQL against a real Postgres instance and catch bugs that
// pgxmock cannot detect (type/NULL/ambiguous-column errors, UNION keyset semantics).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... \
//	  -run 'TestInvestment_|TestTransactions_' -tags integration -v
//
// Note: banking chain-expansion tests use the TestTransactions_ prefix.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ── fixture helper ─────────────────────────────────────────────────────────────

// seedInvestmentFixture creates a minimal user + linked_item + investment account + security.
// All rows land in db (typically a pgx.Tx) so they roll back with it.
// The prefix disambiguates seeds across tests.
// The security is seeded with ticker_symbol='TSEC' and name='Test Security'.
// Returns (userID, linkedItemID, accountID, securityID).
func seedInvestmentFixture(ctx context.Context, t *testing.T, db DB, prefix string) (
	userID, linkedItemID, accountID, secID uuid.UUID,
) {
	t.Helper()
	userID = uuid.New()
	linkedItemID = uuid.New()
	accountID = uuid.New()
	secID = uuid.New()

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix+"-"+userID.String(), prefix+"-"+userID.String()+"@example.com")
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, prefix+"-item-"+linkedItemID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Test Brokerage', 'investment', 'active')
	`, accountID, linkedItemID, userID, prefix+"-acct-"+accountID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol) VALUES ($1, $2, 'Test Security', 'TSEC')
	`, secID, prefix+"-sec-"+secID.String())
	require.NoError(t, err)

	return
}

// ── GAP F: activity-type predicate must not hit ambiguous column ─────────────

// TestInvestment_ActivityTypeFilter_NoAmbiguousColumn_Integration proves that
// querying with an activity_types filter does not raise "column reference is
// ambiguous" even though both investment_transactions (alias it) and accounts
// (alias a) have `type` and `subtype` columns. If GAP F were not fixed, the
// query would return ERROR: column reference "type" is ambiguous → 500.
func TestInvestment_ActivityTypeFilter_NoAmbiguousColumn_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-gapf")

	txnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id, type, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'buy', 'Buy AAPL', 1500.00, '2024-03-15')
	`, txnID, userID, accountID, secID, "plaid-gapf-"+txnID.String())
	require.NoError(t, err, "seed buy transaction")

	repo := NewInvestmentTransactionRepository(tx)
	filters := domain.InvestmentTransactionFilters{
		ActivityTypes: []string{"buy"},
	}
	// Must NOT return ERROR: column reference "type" is ambiguous
	txns, _, err := repo.GetByUserIDKeyset(ctx, userID.String(), filters, nil, 10)
	require.NoError(t, err, "GetByUserIDKeyset with activity_types must not error (proves GAP F fix)")
	require.Len(t, txns, 1, "expected the seeded buy transaction")
	assert.Equal(t, "buy", txns[0].Type)
}

// ── #1305: ticker_symbol and security_name must propagate through GetByUserIDKeyset ──

// TestInvestment_GetByUserIDKeyset_PopulatesSecurityFields_Integration proves that
// ticker_symbol and security_name from the securities JOIN are threaded onto the
// returned domain.InvestmentTransaction structs, and remain nil for cash transactions.
// pgxmock cannot catch column/type/NULL errors on the real query — this guards that.
func TestInvestment_GetByUserIDKeyset_PopulatesSecurityFields_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-1305")

	// Seed a security-linked buy transaction.
	secTxnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id, type, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'buy', 'Buy TSEC', 250.00, '2025-01-10')
	`, secTxnID, userID, accountID, secID, "plaid-1305-sec-"+secTxnID.String())
	require.NoError(t, err)

	// Seed a cash transaction (no security_id → LEFT JOIN produces NULLs).
	cashTxnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, plaid_investment_transaction_id, type, name, amount, date)
		VALUES ($1, $2, $3, $4, 'cash', 'Cash Distribution', 10.00, '2025-01-09')
	`, cashTxnID, userID, accountID, "plaid-1305-cash-"+cashTxnID.String())
	require.NoError(t, err)

	repo := NewInvestmentTransactionRepository(tx)
	txns, _, err := repo.GetByUserIDKeyset(ctx, userID.String(), domain.InvestmentTransactionFilters{}, nil, 10)
	require.NoError(t, err)
	require.Len(t, txns, 2, "expected both seeded transactions")

	// Find each transaction by type.
	var secTxn, cashTxn *domain.InvestmentTransaction
	for i := range txns {
		switch txns[i].Type {
		case "buy":
			secTxn = &txns[i]
		case "cash":
			cashTxn = &txns[i]
		}
	}

	require.NotNil(t, secTxn, "security-linked buy transaction must be returned")
	require.NotNil(t, secTxn.TickerSymbol, "ticker_symbol must be populated for security-linked txn")
	assert.Equal(t, "TSEC", *secTxn.TickerSymbol)
	require.NotNil(t, secTxn.SecurityName, "security_name must be populated for security-linked txn")
	assert.Equal(t, "Test Security", *secTxn.SecurityName)

	require.NotNil(t, cashTxn, "cash transaction must be returned")
	assert.Nil(t, cashTxn.TickerSymbol, "NULL ticker_symbol (no security) must remain nil")
	assert.Nil(t, cashTxn.SecurityName, "NULL security_name (no security) must remain nil")
}

// ── GAP B: partial-NULL cost_basis must not inflate gain/loss ────────────────

// TestInvestment_Snapshot_PartialNullCostBasis_Integration proves that the
// snapshot Upsert computes gain/loss only over holdings with a known cost_basis.
// A holding with NULL cost_basis must NOT be counted as 100% gain.
func TestInvestment_Snapshot_PartialNullCostBasis_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-gapb")

	sec2 := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO securities (id, plaid_security_id, name) VALUES ($1, $2, 'GOOG')`,
		sec2, "gapb-sec2-"+sec2.String())
	require.NoError(t, err)

	// Holding A: value=1000, cost_basis=800 → known gain=200
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value, cost_basis)
		VALUES ($1, $2, $3, $4, 10, 1000.00, 800.00)
	`, uuid.New(), userID, accountID, secID)
	require.NoError(t, err)

	// Holding B: value=500, cost_basis=NULL — must NOT add 500 to gain/loss
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value)
		VALUES ($1, $2, $3, $4, 5, 500.00)
	`, uuid.New(), userID, accountID, sec2)
	require.NoError(t, err)

	repo := NewPortfolioSnapshotRepository(tx)
	// Issue #1569, Task 6 (Fix A): snapshotDate must be a day the account's account_status_history
	// records as live, not an arbitrary fixed calendar date — the account seeded above was created
	// (and its history interval opened) just now, so a hardcoded past date would correctly resolve
	// to "not live yet" and fail for a reason unrelated to what this test is proving.
	snapshotDate := time.Now().UTC().Truncate(24 * time.Hour)
	snap, err := repo.Upsert(ctx, userID.String(), snapshotDate)
	require.NoError(t, err, "Upsert must succeed")

	assert.InDelta(t, 1500.0, snap.TotalValue, 0.01, "total_value must include all holdings (1000+500)")
	assert.InDelta(t, 800.0, snap.TotalCostBasis, 0.01, "total_cost_basis must be known-only")
	// gain = known_value(1000) - known_cost(800) = 200, NOT 1500-800=700
	assert.InDelta(t, 200.0, snap.TotalGainLoss, 0.01, "total_gain_loss must exclude NULL-basis holding (proves GAP B)")
	require.NotNil(t, snap.TotalGainLossPct)
	assert.InDelta(t, 25.0, *snap.TotalGainLossPct, 0.01, "pct = 200/800*100 = 25%")
}

// ── GAP C: snapshot Upsert excludes inactive-account holdings ────────────────

// TestInvestment_Snapshot_ExcludesInactiveAccounts_Integration proves that the
// snapshot Upsert excludes holdings on inactive (replaced) accounts, consistent with every
// INVESTMENT read path — holding_repository.go:204/363/415, investment_analytics_repository.go:83/
// 187/376/481/578, investment_transaction_repository.go:151, portfolio_snapshot_repository.go:82 —
// each of which joins accounts with `a.status = 'active'`.
//
// SCOPED TO THE INVESTMENT DOMAIN on purpose. This comment used to say "consistent with all read
// paths that JOIN accounts WHERE status='active'", which read as a universal about the codebase and
// is not one: after #1561 no spending/cash-flow analytics join carries a status predicate at all
// (#1109 had already removed the personal-scope ones), and the household balance read carries no
// status FILTER either.
//
// ⚠ THE PRECEDING CLAIM WAS RETRACTED BY #1626 E2, AND THE LINE NUMBER WITH IT. It read
// "GetAggregatedHistoryForHousehold expresses status as a DISTINCT ON ordering preference rather
// than a filter (balance_snapshot_repository.go:325)". Both halves were false, and the second is
// why it is retracted rather than re-pointed: #1569 T10.2 had already deleted that ordering
// preference — the ORDER BY term reading (a.status = 'active') DESC NULLS LAST — and replaced the
// chain-wide join with a.id = bs.account_id, so no status expression of any kind remained in that
// method, while the cited line had drifted into the PERSONAL statement's carry-forward, a different
// method entirely. Re-anchored by NAMING the constructs instead of counting lines to them: the two
// places accounts.status is now mentioned anywhere near this read are
// aggregatedHistoryHouseholdSQLBase's carried-row leg (a_carry.status = 'active', which gates an
// INFERENCE about a day nobody reported rather than the visibility of any retained row) and
// accountLiveOnDateClause. Neither is a read filter. Investment reads are the remaining family that
// genuinely filters, which is why this snapshot matching them is worth pinning.
func TestInvestment_Snapshot_ExcludesInactiveAccounts_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, activeAcct, secID := seedInvestmentFixture(ctx, t, tx, "inv-gapc")

	inactiveAcct := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, replaced_at)
		VALUES ($1, $2, $3, $4, 'Old Brokerage', 'investment', 'replaced', now())
	`, inactiveAcct, linkedItemID, userID, "plaid-replaced-"+inactiveAcct.String())
	require.NoError(t, err, "seed inactive account")

	// Active account: value=2000, cost_basis=1500
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value, cost_basis)
		VALUES ($1, $2, $3, $4, 20, 2000.00, 1500.00)
	`, uuid.New(), userID, activeAcct, secID)
	require.NoError(t, err)

	// Inactive account: value=3000 — must NOT be included in snapshot
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value, cost_basis)
		VALUES ($1, $2, $3, $4, 30, 3000.00, 2500.00)
	`, uuid.New(), userID, inactiveAcct, secID)
	require.NoError(t, err)

	repo := NewPortfolioSnapshotRepository(tx)
	// See the #1569 Task 6 comment above: snapshotDate must fall within the seeded account's
	// recorded-live window rather than an arbitrary fixed calendar date.
	snapshotDate := time.Now().UTC().Truncate(24 * time.Hour)
	snap, err := repo.Upsert(ctx, userID.String(), snapshotDate)
	require.NoError(t, err)

	// Only active account's holdings should be counted (proves GAP C fix)
	assert.InDelta(t, 2000.0, snap.TotalValue, 0.01, "Upsert must exclude inactive account holdings (proves GAP C)")
	assert.InDelta(t, 1500.0, snap.TotalCostBasis, 0.01)
}

// ── GAP B (analytics): UnrealizedGainLoss excludes NULL-basis holdings ──────

// TestInvestment_UnrealizedGainLoss_NullBasis_Integration proves that
// UnrealizedGainLoss excludes NULL-cost_basis holdings from gain/loss figures.
func TestInvestment_UnrealizedGainLoss_NullBasis_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-gapb2")

	sec2 := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO securities (id, plaid_security_id, name) VALUES ($1, $2, 'AGG')`,
		sec2, "gapb2-sec2-"+sec2.String())
	require.NoError(t, err)

	// Holding with known cost_basis: value=2000, cost_basis=1600 → gain=400
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value, cost_basis)
		VALUES ($1, $2, $3, $4, 10, 2000.00, 1600.00)
	`, uuid.New(), userID, accountID, secID)
	require.NoError(t, err)

	// Holding with NULL cost_basis: value=1200 — must NOT count as 1200 gain
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value)
		VALUES ($1, $2, $3, $4, 6, 1200.00)
	`, uuid.New(), userID, accountID, sec2)
	require.NoError(t, err)

	repo := NewInvestmentAnalyticsRepository(tx)
	result, err := repo.UnrealizedGainLoss(ctx, userID.String())
	require.NoError(t, err)

	// TotalValue covers all holdings: 2000 + 1200 = 3200
	assert.InDelta(t, 3200.0, result.TotalValue, 0.01, "TotalValue must include all holdings")
	// TotalCostBasis known-only: 1600
	assert.InDelta(t, 1600.0, result.TotalCostBasis, 0.01, "TotalCostBasis must be known-only")
	// gain = known_value(2000) - known_basis(1600) = 400, NOT 3200-1600=1600
	assert.InDelta(t, 400.0, result.TotalUnrealizedGainLoss, 0.01, "gain/loss must exclude NULL-basis holding (proves GAP B)")
	require.NotNil(t, result.TotalUnrealizedGainLossPct)
	assert.InDelta(t, 25.0, *result.TotalUnrealizedGainLossPct, 0.01, "pct = 400/1600*100 = 25%")
}

// TestInvestment_UnrealizedGainLoss_NullInstitutionValue_Integration proves that
// UnrealizedGainLoss excludes holdings with cost_basis SET but institution_value NULL
// from gain/loss figures. Such a holding must NOT become a phantom loss. — Round-6 R6-2 RED→GREEN
func TestInvestment_UnrealizedGainLoss_NullInstitutionValue_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-nullvalue")

	sec2 := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO securities (id, plaid_security_id, name) VALUES ($1, $2, 'BOND')`,
		sec2, "r62-sec2-"+sec2.String())
	require.NoError(t, err)

	// Holding A: both non-nil → contributes fully to gain/loss (value=2000, basis=1600 → gain=400)
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value, cost_basis)
		VALUES ($1, $2, $3, $4, 10, 2000.00, 1600.00)
	`, uuid.New(), userID, accountID, secID)
	require.NoError(t, err)

	// Holding B: cost_basis SET, institution_value NULL → must NOT become phantom loss
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, cost_basis)
		VALUES ($1, $2, $3, $4, 5, 500.00)
	`, uuid.New(), userID, accountID, sec2)
	require.NoError(t, err)

	repo := NewInvestmentAnalyticsRepository(tx)
	result, err := repo.UnrealizedGainLoss(ctx, userID.String())
	require.NoError(t, err)

	// TotalValue: only A contributes (B has NULL institution_value)
	assert.InDelta(t, 2000.0, result.TotalValue, 0.01, "TotalValue must only sum non-nil institution_value")
	// TotalCostBasis: all non-nil cost_basis — A(1600) + B(500) = 2100.
	// Matches GetPortfolioSummary + snapshot Upsert (all-non-null anchor). — R9-1 fix
	assert.InDelta(t, 2100.0, result.TotalCostBasis, 0.01, "TotalCostBasis must include all non-nil cost_basis (R9-1)")
	// gain = A only: 2000-1600 = 400, NOT 2000-(1600+500) = -100 phantom loss.
	// The gain/loss FIGURE uses the known-pair FILTER (both non-null), so B is excluded.
	assert.InDelta(t, 400.0, result.TotalUnrealizedGainLoss, 0.01,
		"gain/loss must NOT be phantom-loss due to NULL institution_value holding")
	require.NotNil(t, result.TotalUnrealizedGainLossPct)
	// pct denominator = known-pair cost (1600, not 2100): 400/1600*100 = 25%
	assert.InDelta(t, 25.0, *result.TotalUnrealizedGainLossPct, 0.01, "pct denominator = known-pair cost (1600)")
}

// ── GAP E: unified UNION page boundary — cross-source same-date ──────────────

// TestInvestment_UnifiedFeed_PageBoundary_Integration proves that the unified
// transaction feed (UNION ALL banking + investment) correctly pages across records
// that share the same sort_date. Cross-source same-date rows must not be
// skipped or duplicated at the page boundary.
//
// Setup: 2 banking + 2 investment transactions all on 2024-05-15.
// With limit=2, page 1 returns 2 rows; page 2 returns 2 rows. Zero dupes.
func TestInvestment_UnifiedFeed_PageBoundary_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// Seed the investment account (also seeds user + linked_item)
	userID, existingLinkedItemID, investAcctID, secID := seedInvestmentFixture(ctx, t, tx, "inv-gape")

	// Seed a separate linked_item + depository account for banking transactions
	bankLinkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, bankLinkedItemID, userID, "bank-item-gape-"+bankLinkedItemID.String())
	require.NoError(t, err)

	bankingAcctID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Checking', 'depository', 'active')
	`, bankingAcctID, bankLinkedItemID, userID, "plaid-bank-gape-"+bankingAcctID.String())
	require.NoError(t, err)

	sameDate := "2024-05-15"

	// 2 banking transactions on the same date
	for i := range 2 {
		txnID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions
				(id, user_id, account_id, linked_item_id, source,
				 external_transaction_id, external_transaction_id_blind_idx,
				 amount, date, name, pfc_primary, pfc_detailed, pending, created_at, updated_at)
			VALUES
				($1, $2, $3, $4, 'plaid', $5, $6, $7, $8::date, $9,
				 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_GROCERIES', false, now(), now())
		`, txnID, userID, bankingAcctID, bankLinkedItemID,
			fmt.Sprintf("bank-ext-gape-%d-%s", i, txnID),
			fmt.Sprintf("bank-blind-gape-%d-%s", i, txnID),
			float64(i+1)*100, sameDate,
			fmt.Sprintf("Banking txn %d", i),
		)
		require.NoError(t, err, "seed banking transaction %d", i)
	}

	// 2 investment transactions on the same date
	for i := range 2 {
		invTxnID := uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO investment_transactions
				(id, user_id, account_id, security_id, plaid_investment_transaction_id,
				 type, name, amount, date)
			VALUES ($1, $2, $3, $4, $5, 'buy', $6, $7, $8::date)
		`, invTxnID, userID, investAcctID, secID,
			fmt.Sprintf("plaid-inv-gape-%d-%s", i, invTxnID),
			fmt.Sprintf("Buy VTI %d", i),
			float64(i+1)*500, sameDate,
		)
		require.NoError(t, err, "seed investment transaction %d", i)
	}

	// Suppress unused variable warning for existingLinkedItemID (used for seeding)
	_ = existingLinkedItemID

	repo := NewTransactionRepository(tx, enc)
	filters := domain.TransactionFilters{
		SortBy:                    "date",
		IncludeInvestmentActivity: true,
	}

	// Page 1: limit=2 → exactly 2 rows, with a next cursor
	page1, cursor1, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, nil, 2)
	require.NoError(t, err, "page 1 must not error")
	require.Len(t, page1, 2, "page 1 must have exactly 2 items")
	require.NotNil(t, cursor1, "page 1 must return a next cursor")

	// Page 2: continue from cursor → exactly 2 rows, no next cursor
	page2, cursor2, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, cursor1, 2)
	require.NoError(t, err, "page 2 must not error")
	require.Len(t, page2, 2, "page 2 must have exactly 2 items")
	assert.Nil(t, cursor2, "page 2 must be the last page (nil cursor)")

	// Verify no skips/dupes across both pages
	allItems := append(page1, page2...)
	require.Len(t, allItems, 4, "total items across both pages must be 4")

	seenIDs := make(map[string]struct{}, 4)
	for _, item := range allItems {
		_, dup := seenIDs[item.ID]
		assert.False(t, dup, "duplicate item ID %s at page boundary (proves GAP E fix)", item.ID)
		seenIDs[item.ID] = struct{}{}
	}

	// Both sources must be present
	var bankCount, invCount int
	for _, item := range allItems {
		switch item.Source {
		case domain.FeedSourceBanking:
			bankCount++
		case domain.FeedSourceInvestment:
			invCount++
		}
	}
	assert.Equal(t, 2, bankCount, "expected 2 banking items across both pages")
	assert.Equal(t, 2, invCount, "expected 2 investment items across both pages")
}

// ── R2-4: activity_types filter must exclude banking rows ────────────────────

// TestInvestment_UnifiedFeed_ActivityTypesExcludesBanking_Integration proves that
// when activity_types is set in the unified feed, banking rows are excluded entirely.
// Before the R2-4 fix, banking rows leaked through because only the investment branch
// had an activity filter — the banking branch was unaffected and returned all its rows.
func TestInvestment_UnifiedFeed_ActivityTypesExcludesBanking_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, investAcctID, secID := seedInvestmentFixture(ctx, t, tx, "inv-r24")

	// Seed a separate banking account
	bankLinkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, bankLinkedItemID, userID, "bank-item-r24-"+bankLinkedItemID.String())
	require.NoError(t, err)

	bankAcctID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Checking R24', 'depository', 'active')
	`, bankAcctID, bankLinkedItemID, userID, "plaid-bank-r24-"+bankAcctID.String())
	require.NoError(t, err)

	sameDate := "2024-04-10"

	// 1 banking transaction
	bankTxnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source,
			 external_transaction_id, external_transaction_id_blind_idx,
			 amount, date, name, pfc_primary, pfc_detailed, pending, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, 'plaid', $5, $6, 50.00, $7::date, 'Coffee',
			 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_COFFEE_SHOP', false, now(), now())
	`, bankTxnID, userID, bankAcctID, bankLinkedItemID,
		"bank-ext-r24-"+bankTxnID.String(), "bank-blind-r24-"+bankTxnID.String(), sameDate)
	require.NoError(t, err)

	// 1 investment "buy" transaction
	invTxnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id,
			 type, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'buy', 'Buy VTI', 1000.00, $6::date)
	`, invTxnID, userID, investAcctID, secID, "plaid-buy-r24-"+invTxnID.String(), sameDate)
	require.NoError(t, err)

	repo := NewTransactionRepository(tx, enc)
	filters := domain.TransactionFilters{
		SortBy:                    "date",
		IncludeInvestmentActivity: true,
		ActivityTypes:             []string{"buy"},
	}

	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, nil, 10)
	require.NoError(t, err, "query must not error")

	// Only the investment row (buy) must be returned — banking must be excluded (R2-4 fix)
	require.Len(t, items, 1, "only the investment buy transaction should be returned when activity_types filter is active")
	assert.Equal(t, domain.FeedSourceInvestment, items[0].Source, "result must be the investment row")
	assert.Equal(t, invTxnID.String(), items[0].ID, "result must be the buy investment transaction")
}

// ── R2-1: user_account_id re-link propagation ───────────────────────────────

// TestInvestment_UserAccountID_SurvivesRelink_Integration proves that:
//  1. UpsertBatch populates user_account_id at INSERT time (GAP D fix).
//  2. A second UpsertBatch with the same plaid_investment_transaction_id and a
//     non-nil UserAccountID backfills user_account_id on pre-fix NULL rows (R2-5 bulk-backfill).
//  3. GetByUserIDKeyset filtered by a NEW account_id (post-relink) still returns the
//     pre-relink transactions via chain-expansion through user_account_id.
func TestInvestment_UserAccountID_SurvivesRelink_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountA, secID := seedInvestmentFixture(ctx, t, tx, "inv-r21")

	// Insert a user_accounts row (stable reference surviving re-links).
	// user_accounts.id is the FK target for accounts.user_account_id (000031/000033).
	userAcctID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, 'ins-test-r21')
	`, userAcctID, userID)
	require.NoError(t, err)

	// Set user_account_id on account A (the stable chain key)
	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = $1 WHERE id = $2`,
		userAcctID, accountA)
	require.NoError(t, err)

	userAcctIDStr := userAcctID.String()
	secIDStr := secID.String()
	plaidTxnID := "plaid-r21-relink-001"

	// Insert a transaction on account A with user_account_id set (GAP D path)
	txnID := uuid.New()
	repo := NewInvestmentTransactionRepository(tx)
	inserted, err := repo.UpsertBatch(ctx, []domain.InvestmentTransaction{{
		ID:                           txnID.String(),
		UserID:                       userID.String(),
		AccountID:                    accountA.String(),
		UserAccountID:                &userAcctIDStr,
		SecurityID:                   &secIDStr,
		PlaidInvestmentTransactionID: plaidTxnID,
		Type:                         "buy",
		Name:                         "Buy VTI pre-relink",
		Amount:                       1200.00,
		Date:                         mustParseDate("2024-03-01"),
	}})
	require.NoError(t, err)
	assert.Equal(t, 1, inserted)

	// Verify user_account_id was stored at INSERT time (not nil)
	var storedUAID *string
	err = tx.QueryRow(ctx,
		`SELECT user_account_id::text FROM investment_transactions WHERE plaid_investment_transaction_id = $1`,
		plaidTxnID).Scan(&storedUAID)
	require.NoError(t, err, "row must exist")
	require.NotNil(t, storedUAID, "user_account_id must be non-nil after UpsertBatch (GAP D)")
	assert.Equal(t, userAcctIDStr, *storedUAID, "user_account_id must match the account's user_account_id")

	// Simulate a Plaid re-link: create account B with the same user_account_id; mark A replaced
	accountB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Relinked Brokerage', 'investment', 'active', $5)
	`, accountB, linkedItemID, userID, "plaid-relink-acct-r21-"+accountB.String(), userAcctID)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = now() WHERE id = $1`, accountA)
	require.NoError(t, err)

	// GetByUserIDKeyset filtered by account B (the new post-relink account)
	// must still return the pre-relink transaction via chain-expansion
	accountBStr := accountB.String()
	filters := domain.InvestmentTransactionFilters{
		AccountID: &accountBStr,
	}
	txns, _, err := repo.GetByUserIDKeyset(ctx, userID.String(), filters, nil, 10)
	require.NoError(t, err)
	require.Len(t, txns, 1, "pre-relink transaction must be returned via chain-expansion after re-link")
	assert.Equal(t, txnID.String(), txns[0].ID, "must return the pre-relink transaction ID")

	// ── Backfill test (R2-5): row with NULL user_account_id gets repaired in one batch ──

	// Insert a second row with NULL user_account_id (simulates a pre-fix row)
	txnID2 := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id, type, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'buy', 'Pre-fix row', 800.00, '2024-01-15')
	`, txnID2, userID, accountA, secID, "plaid-r21-prefixrow")
	require.NoError(t, err)

	// UpsertBatch with the same plaid IDs and non-nil UserAccountID — bulk backfill must repair both NULL rows
	txnID3 := uuid.New()
	_, err = repo.UpsertBatch(ctx, []domain.InvestmentTransaction{
		{
			ID:                           txnID3.String(), // new txn (will conflict → skip)
			UserID:                       userID.String(),
			AccountID:                    accountA.String(),
			UserAccountID:                &userAcctIDStr,
			SecurityID:                   &secIDStr,
			PlaidInvestmentTransactionID: "plaid-r21-prefixrow", // same plaid ID → conflict, do nothing
			Type:                         "buy",
			Name:                         "Pre-fix row",
			Amount:                       800.00,
			Date:                         mustParseDate("2024-01-15"),
		},
	})
	require.NoError(t, err)

	// Verify the NULL-user_account_id row was backfilled
	var backfilledUAID *string
	err = tx.QueryRow(ctx,
		`SELECT user_account_id::text FROM investment_transactions WHERE plaid_investment_transaction_id = 'plaid-r21-prefixrow'`,
	).Scan(&backfilledUAID)
	require.NoError(t, err)
	require.NotNil(t, backfilledUAID, "backfill must repair NULL user_account_id (R2-5)")
	assert.Equal(t, userAcctIDStr, *backfilledUAID)
}

// ── R2-2: banking-only filters must zero investment rows ─────────────────────

// TestInvestment_UnifiedFeed_BankingOnlyFilter_ExcludesInvestment_Integration proves
// that banking-only filters (Pending, MerchantID) constrain the result to banking rows
// only and return zero investment rows. This locks the GAP G fix.
// Second assertion: AccountIDs (a both-branch filter) still returns investment rows
// for the matching account.
func TestInvestment_UnifiedFeed_BankingOnlyFilter_ExcludesInvestment_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, investAcctID, secID := seedInvestmentFixture(ctx, t, tx, "inv-r22")

	// Seed banking account
	bankLinkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, bankLinkedItemID, userID, "bank-item-r22-"+bankLinkedItemID.String())
	require.NoError(t, err)

	bankAcctID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Checking R22', 'depository', 'active')
	`, bankAcctID, bankLinkedItemID, userID, "plaid-bank-r22-"+bankAcctID.String())
	require.NoError(t, err)

	sameDate := "2024-03-20"

	// 1 pending banking transaction
	bankTxnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source,
			 external_transaction_id, external_transaction_id_blind_idx,
			 amount, date, name, pfc_primary, pfc_detailed, pending, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, 'plaid', $5, $6, 30.00, $7::date, 'Starbucks',
			 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_COFFEE_SHOP', true, now(), now())
	`, bankTxnID, userID, bankAcctID, bankLinkedItemID,
		"bank-ext-r22-"+bankTxnID.String(), "bank-blind-r22-"+bankTxnID.String(), sameDate)
	require.NoError(t, err)

	// 1 investment transaction
	invTxnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id,
			 type, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'cash', 'Dividend', 25.00, $6::date)
	`, invTxnID, userID, investAcctID, secID, "plaid-div-r22-"+invTxnID.String(), sameDate)
	require.NoError(t, err)

	repo := NewTransactionRepository(tx, enc)

	// ── Assertion 1: Pending=true filter → only banking row, zero investment ──
	pendingTrue := true
	filters := domain.TransactionFilters{
		SortBy:                    "date",
		IncludeInvestmentActivity: true,
		Pending:                   &pendingTrue,
	}
	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, nil, 10)
	require.NoError(t, err, "Pending filter must not error")
	require.Len(t, items, 1, "only the pending banking transaction should be returned")
	assert.Equal(t, domain.FeedSourceBanking, items[0].Source, "result must be the banking row (proves GAP G fix)")

	// ── Assertion 1b: MerchantID filter → only banking row, zero investment ──
	merchantID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO merchants (id, name) VALUES ($1, $2)
	`, merchantID, "TestCo-R22-"+merchantID.String())
	require.NoError(t, err)
	merchantIDStr := merchantID.String()
	filtersMerchant := domain.TransactionFilters{
		SortBy:                    "date",
		IncludeInvestmentActivity: true,
		MerchantID:                &merchantIDStr,
	}
	itemsMerchant, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filtersMerchant, nil, 10)
	require.NoError(t, err, "MerchantID filter must not error")
	// Merchant filter returns zero rows (no txn has that merchant), confirming investment branch zeroed
	assert.Empty(t, itemsMerchant, "MerchantID filter must zero investment rows (proves transaction_repository.go:2552-2557 branch)")

	// ── Assertion 2: AccountIDs (both-branch filter) returns investment rows ──
	investAcctIDStr := investAcctID.String()
	filters2 := domain.TransactionFilters{
		SortBy:                    "date",
		IncludeInvestmentActivity: true,
		AccountIDs:                []string{investAcctIDStr},
	}
	items2, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters2, nil, 10)
	require.NoError(t, err, "AccountIDs filter must not error")
	require.Len(t, items2, 1, "only the investment transaction for that account should be returned")
	assert.Equal(t, domain.FeedSourceInvestment, items2[0].Source, "AccountIDs must return investment rows")
}

// ── R4-2: dedicated endpoint zeros on all-unrecognized activity_types ────────

// TestInvestment_DedicatedFeed_UnrecognizedActivityType_ReturnsZero_Integration
// proves that GetByUserIDKeyset returns ZERO rows (not all rows) when the
// activity_types filter contains only unrecognized values. Before R4-2 the
// empty predicate was silently skipped, returning all investment transactions.
func TestInvestment_DedicatedFeed_UnrecognizedActivityType_ReturnsZero_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-r42")

	// Seed one buy transaction so there are rows to potentially return.
	txnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id, type, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'buy', 'Buy AAPL R42', 2000.00, '2024-07-01')
	`, txnID, userID, accountID, secID, "plaid-r42-buy-"+txnID.String())
	require.NoError(t, err)

	repo := NewInvestmentTransactionRepository(tx)
	filters := domain.InvestmentTransactionFilters{
		ActivityTypes: []string{"garbage_type_not_in_enum"},
	}

	// With R4-2 fix: all-unrecognized activity_types → FALSE condition → zero rows.
	// Without the fix: empty clause skipped → all rows returned (the seeded buy txn).
	txns, _, err := repo.GetByUserIDKeyset(ctx, userID.String(), filters, nil, 10)
	require.NoError(t, err, "must not error on unrecognized activity types")
	assert.Empty(t, txns, "all-unrecognized activity_types must return zero rows (proves R4-2 fix)")
}

// ── R7-B: IncomeSummary must report positive income for Plaid-negative amounts ──

// TestInvestment_IncomeSummary_NegativePlaidAmounts_Integration proves that
// IncomeSummary reports positive (cash-magnitude) income even though Plaid stores
// dividend and interest amounts as NEGATIVE values (cash credited convention:
// positive = cash debited, negative = cash credited). Before R7-B the query used
// SUM(it.amount) → negative total_income, corrupting /portfolio/income responses.
//
// This test seeds transactions with Plaid-realistic negative amounts and asserts
// the output is positive — something pgxmock tests cannot verify because they feed
// fabricated positive values.
func TestInvestment_IncomeSummary_NegativePlaidAmounts_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-r7b")

	// Seed income transactions with Plaid-realistic NEGATIVE amounts.
	// dividend: $200 cash credit → Plaid amount = -200
	divID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id,
			 type, subtype, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'cash', 'dividend', 'AAPL Dividend', -200.00, '2024-06-15')
	`, divID, userID, accountID, secID, "plaid-r7b-div-"+divID.String())
	require.NoError(t, err)

	// interest: $50 cash credit → Plaid amount = -50
	intID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id,
			 type, subtype, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'cash', 'interest', 'Money Market Interest', -50.00, '2024-06-20')
	`, intID, userID, accountID, secID, "plaid-r7b-int-"+intID.String())
	require.NoError(t, err)

	// dividend reinvestment: $30 reinvested → Plaid amount = -30
	reinvID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id,
			 type, subtype, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'cash', 'dividend reinvestment', 'DRIP Reinvestment', -30.00, '2024-06-25')
	`, reinvID, userID, accountID, secID, "plaid-r7b-reinv-"+reinvID.String())
	require.NoError(t, err)

	repo := NewInvestmentAnalyticsRepository(tx)
	start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 30, 23, 59, 59, 0, time.UTC)

	result, err := repo.IncomeSummary(ctx, userID.String(), start, end)
	require.NoError(t, err)
	require.NotNil(t, result)

	// TotalIncome must be POSITIVE (R7-B fix: negate the Plaid cash-credit sign).
	// Without the fix: SUM(-200 + -50 + -30) = -280 → this assertion fails.
	assert.InDelta(t, 280.0, result.TotalIncome, 0.01,
		"TotalIncome must be positive +280 (Plaid stores income as negative cash credits)")
	assert.Len(t, result.ByType, 3, "must have 3 income buckets")

	// Each per-type TotalAmount must also be positive
	for _, bt := range result.ByType {
		assert.Greater(t, bt.TotalAmount, 0.0,
			"ByType[%s].TotalAmount must be positive (proves R7-B sign fix)", bt.IncomeType)
	}
}

// ── R4-3: trade-type partition round-trip (regression guard) ─────────────────

// TestInvestment_TradeTypePartition_RoundTrip_Integration proves that every value
// NormalizeTradeType can return has a matching TradeTypeToSQLPredicate bucket that
// actually returns the corresponding row. This locks the display↔filter partition
// against future regressions — R3-1 (transfer silently excluded from 'other')
// slipped three prior adversarial review rounds because no end-to-end test
// exercised this agreement against real SQL.
func TestInvestment_TradeTypePartition_RoundTrip_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-r43")
	userIDStr := userID.String()

	// Each entry represents one bucket — one representative (type, subtype) that
	// NormalizeTradeType maps to the expected display value.
	type bucketCase struct {
		plaidID  string
		sqlType  string
		sqlSub   *string // nil → no subtype column
		expected string  // display value = NormalizeTradeType result = filter bucket
	}
	subPtr := func(s string) *string { return &s }
	cases := []bucketCase{
		{plaidID: "r43-buy", sqlType: "buy", sqlSub: nil, expected: "buy"},
		{plaidID: "r43-sell", sqlType: "sell", sqlSub: nil, expected: "sell"},
		{plaidID: "r43-div", sqlType: "cash", sqlSub: subPtr("dividend"), expected: "dividend"},
		{plaidID: "r43-reinv", sqlType: "cash", sqlSub: subPtr("dividend reinvestment"), expected: "reinvest"},
		{plaidID: "r43-fee-type", sqlType: "fee", sqlSub: nil, expected: "fee"},
		{plaidID: "r43-fee-sub", sqlType: "cash", sqlSub: subPtr("management fee"), expected: "fee"},
		{plaidID: "r43-cash", sqlType: "cash", sqlSub: subPtr("deposit"), expected: "cash"},
		{plaidID: "r43-transfer", sqlType: "transfer", sqlSub: nil, expected: "other"},
	}

	// Verify NormalizeTradeType agrees with each case, and seed the transaction.
	for _, c := range cases {
		// Confirm NormalizeTradeType returns the expected display value (display side).
		got := domain.NormalizeTradeType(c.sqlType, c.sqlSub)
		require.Equal(t, c.expected, got,
			"NormalizeTradeType(%q, %v) must return %q (display side of partition)",
			c.sqlType, c.sqlSub, c.expected)

		// Seed the transaction.
		txnID := uuid.New()
		var seedErr error
		if c.sqlSub != nil {
			_, seedErr = tx.Exec(ctx, `
				INSERT INTO investment_transactions
					(id, user_id, account_id, security_id, plaid_investment_transaction_id,
					 type, subtype, name, amount, date)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 100.00, '2024-08-01')
			`, txnID, userID, accountID, secID, c.plaidID+"-"+txnID.String(),
				c.sqlType, *c.sqlSub, "R43 "+c.plaidID)
		} else {
			_, seedErr = tx.Exec(ctx, `
				INSERT INTO investment_transactions
					(id, user_id, account_id, security_id, plaid_investment_transaction_id,
					 type, name, amount, date)
				VALUES ($1, $2, $3, $4, $5, $6, $7, 100.00, '2024-08-01')
			`, txnID, userID, accountID, secID, c.plaidID+"-"+txnID.String(),
				c.sqlType, "R43 "+c.plaidID)
		}
		require.NoError(t, seedErr, "seed %s transaction", c.plaidID)
	}

	repo := NewInvestmentTransactionRepository(tx)

	// For each unique bucket, call GetByUserIDKeyset with [bucket] and verify the
	// representative row(s) are returned (filter side of partition). Uniquify to avoid
	// running "fee" twice (it has two representatives: fee-type and fee-sub).
	seen := make(map[string]bool)
	for _, c := range cases {
		if seen[c.expected] {
			continue
		}
		seen[c.expected] = true

		filters := domain.InvestmentTransactionFilters{
			ActivityTypes: []string{c.expected},
		}
		txns, _, filterErr := repo.GetByUserIDKeyset(ctx, userIDStr, filters, nil, 20)
		require.NoError(t, filterErr, "GetByUserIDKeyset with activity_types=[%s] must not error", c.expected)

		// Assert at least one row was returned — specifically that the row whose
		// NormalizeTradeType maps to this bucket is reachable by the filter.
		require.NotEmpty(t, txns,
			"activity_types=[%s] must return at least the %s-representative row "+
				"(partition round-trip: NormalizeTradeType agreed but SQL filter excludes it — regression of R3-1)",
			c.expected, c.plaidID)
	}
}

// mustParseDate parses a "YYYY-MM-DD" string into time.Time. Panics on error (test-only helper).
func mustParseDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// ── R8-1: TopMovers UNION ORDER BY must not use expressions (Postgres restriction) ──

// TestInvestment_TopMovers_Integration verifies that the TopMovers query executes against
// real Postgres, correctly splits rows into gainers/losers, applies the intended per-group
// ordering (gainers DESC, losers most-negative-first), excludes NULL-close-price holdings,
// and respects the limit. The query wraps UNION ALL in a `combined` CTE to allow ORDER BY
// with a CASE expression (PostgreSQL forbids expressions in set-operation ORDER BY).
func TestInvestment_TopMovers_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, _ := seedInvestmentFixture(ctx, t, tx, "inv-r81")
	userIDStr := userID.String()

	// ── securities ─────────────────────────────────────────────────────────────

	// Two gainer securities: close=100; inst_price will be 120 and 110 respectively.
	gainSec1ID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, close_price) VALUES ($1, $2, 'Big Gainer', 'BGAIN', 100.00)`,
		gainSec1ID, "r81-bg-sec-"+gainSec1ID.String())
	require.NoError(t, err)

	gainSec2ID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, close_price) VALUES ($1, $2, 'Small Gainer', 'SGAIN', 100.00)`,
		gainSec2ID, "r81-sg-sec-"+gainSec2ID.String())
	require.NoError(t, err)

	// Two loser securities: close=200; inst_price will be 180 and 190 respectively.
	loseSec1ID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, close_price) VALUES ($1, $2, 'Big Loser', 'BLOSE', 200.00)`,
		loseSec1ID, "r81-bl-sec-"+loseSec1ID.String())
	require.NoError(t, err)

	loseSec2ID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, close_price) VALUES ($1, $2, 'Small Loser', 'SLOSE', 200.00)`,
		loseSec2ID, "r81-sl-sec-"+loseSec2ID.String())
	require.NoError(t, err)

	// Security with NULL close_price — its holding must be EXCLUDED.
	nullPriceSecID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO securities (id, plaid_security_id, name, ticker_symbol) VALUES ($1, $2, 'No Price Corp', 'NOPE')`,
		nullPriceSecID, "r81-null-sec-"+nullPriceSecID.String())
	require.NoError(t, err)

	// ── holdings ───────────────────────────────────────────────────────────────

	// Big gainer: qty=10, inst=120, close=100 → day_change=+200
	bigGainerID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_price, institution_value) VALUES ($1, $2, $3, $4, 10, 120.00, 1200.00)`,
		bigGainerID, userID, accountID, gainSec1ID)
	require.NoError(t, err)

	// Small gainer: qty=10, inst=110, close=100 → day_change=+100
	smallGainerID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_price, institution_value) VALUES ($1, $2, $3, $4, 10, 110.00, 1100.00)`,
		smallGainerID, userID, accountID, gainSec2ID)
	require.NoError(t, err)

	// Big loser: qty=5, inst=180, close=200 → day_change=-100
	bigLoserID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_price, institution_value) VALUES ($1, $2, $3, $4, 5, 180.00, 900.00)`,
		bigLoserID, userID, accountID, loseSec1ID)
	require.NoError(t, err)

	// Small loser: qty=5, inst=190, close=200 → day_change=-50
	smallLoserID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_price, institution_value) VALUES ($1, $2, $3, $4, 5, 190.00, 950.00)`,
		smallLoserID, userID, accountID, loseSec2ID)
	require.NoError(t, err)

	// Excluded: institution_price set but security has NULL close_price → must not appear.
	excludedHoldingID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_price, institution_value) VALUES ($1, $2, $3, $4, 20, 50.00, 1000.00)`,
		excludedHoldingID, userID, accountID, nullPriceSecID)
	require.NoError(t, err)

	repo := NewInvestmentAnalyticsRepository(tx)

	// ── basic: no SQL parse error, correct split ────────────────────────────────
	result, err := repo.TopMovers(ctx, userIDStr, 5)
	require.NoError(t, err, "TopMovers must not error (R8-1: UNION ORDER BY CASE fix)")
	require.NotNil(t, result)
	require.Len(t, result.Gainers, 2, "should have 2 gainers")
	require.Len(t, result.Losers, 2, "should have 2 losers")

	// ── ordering: gainers highest day_change first ──────────────────────────────
	require.NotNil(t, result.Gainers[0].DayChange)
	require.NotNil(t, result.Gainers[1].DayChange)
	assert.Equal(t, bigGainerID.String(), result.Gainers[0].HoldingID,
		"first gainer must be the big gainer (day_change=+200)")
	assert.Equal(t, smallGainerID.String(), result.Gainers[1].HoldingID,
		"second gainer must be the small gainer (day_change=+100)")
	assert.InDelta(t, 200.0, *result.Gainers[0].DayChange, 0.01, "big gainer day_change = 10*(120-100)=200")
	assert.InDelta(t, 100.0, *result.Gainers[1].DayChange, 0.01, "small gainer day_change = 10*(110-100)=100")

	// ── ordering: losers most-negative first ───────────────────────────────────
	require.NotNil(t, result.Losers[0].DayChange)
	require.NotNil(t, result.Losers[1].DayChange)
	assert.Equal(t, bigLoserID.String(), result.Losers[0].HoldingID,
		"first loser must be the big loser (day_change=-100)")
	assert.Equal(t, smallLoserID.String(), result.Losers[1].HoldingID,
		"second loser must be the small loser (day_change=-50)")
	assert.InDelta(t, -100.0, *result.Losers[0].DayChange, 0.01, "big loser day_change = 5*(180-200)=-100")
	assert.InDelta(t, -50.0, *result.Losers[1].DayChange, 0.01, "small loser day_change = 5*(190-200)=-50")

	// ── limit: limit=1 must return only the top mover per side ─────────────────
	limited, err := repo.TopMovers(ctx, userIDStr, 1)
	require.NoError(t, err)
	require.Len(t, limited.Gainers, 1, "limit=1 must return 1 gainer")
	require.Len(t, limited.Losers, 1, "limit=1 must return 1 loser")
	assert.Equal(t, bigGainerID.String(), limited.Gainers[0].HoldingID, "limit=1 gainer must be the big gainer")
	assert.Equal(t, bigLoserID.String(), limited.Losers[0].HoldingID, "limit=1 loser must be the big loser")

	// ── NULL-close-price holding must not appear in either list ────────────────
	// (Count assertions above already prove this since all 5 holdings are seeded,
	// but we assert by HoldingID to make the exclusion explicit and catch false-positive orderings.)
	allIDs := make([]string, 0, len(result.Gainers)+len(result.Losers))
	for _, g := range result.Gainers {
		allIDs = append(allIDs, g.HoldingID)
	}
	for _, l := range result.Losers {
		allIDs = append(allIDs, l.HoldingID)
	}
	assert.NotContains(t, allIDs, excludedHoldingID.String(),
		"null-close-price holding must be excluded from TopMovers results")
}

// ── Round-9 R9-3: AllocationBreakdown real-DB coverage (closes GAP-E class) ──

// TestInvestment_AllocationBreakdown_Integration is the first real-Postgres execution
// of the AllocationBreakdown query. It covers:
//   - Multi-bucket grouping by security type (stock + etf)
//   - NULL security.type → "unknown" bucket
//   - Inactive-account holdings excluded
//   - Per-bucket percentage = value / grandTotal * 100 (computed in Go post-total)
//   - ORDER BY total_value DESC ordering
//
// This locks the query against the GAP-E class (pgxmock-only coverage → silent
// SQL or semantics bug): R7-B (income sign), R8-1 (TopMovers UNION ORDER BY crash),
// and now AllocationBreakdown complete the coverage of all 4 analytics queries.
func TestInvestment_AllocationBreakdown_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountID, _ := seedInvestmentFixture(ctx, t, tx, "alloc")

	// ── additional securities with distinct types ──────────────────────────────
	// Two equity securities (different IDs) — needed because unique_account_security
	// prevents two holdings with the same (account_id, security_id).
	stockSecID1 := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO securities (id, plaid_security_id, name, type) VALUES ($1, $2, 'AAPL', 'equity')`,
		stockSecID1, "alloc-stock1-"+stockSecID1.String())
	require.NoError(t, err)

	stockSecID2 := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO securities (id, plaid_security_id, name, type) VALUES ($1, $2, 'MSFT', 'equity')`,
		stockSecID2, "alloc-stock2-"+stockSecID2.String())
	require.NoError(t, err)

	etfSecID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO securities (id, plaid_security_id, name, type) VALUES ($1, $2, 'SPY', 'etf')`,
		etfSecID, "alloc-etf-"+etfSecID.String())
	require.NoError(t, err)

	nullTypeSecID := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO securities (id, plaid_security_id, name) VALUES ($1, $2, 'PRIV')`,
		nullTypeSecID, "alloc-null-"+nullTypeSecID.String())
	require.NoError(t, err)

	// ── active-account holdings ────────────────────────────────────────────────
	// Equity bucket: two distinct securities × one holding each = 2 holdings, total 3000
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value)
		VALUES ($1, $2, $3, $4, 10, 2000.00)
	`, uuid.New(), userID, accountID, stockSecID1)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value)
		VALUES ($1, $2, $3, $4, 5, 1000.00)
	`, uuid.New(), userID, accountID, stockSecID2)
	require.NoError(t, err)

	// ETF: value 4000 (largest bucket → should come first)
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value)
		VALUES ($1, $2, $3, $4, 20, 4000.00)
	`, uuid.New(), userID, accountID, etfSecID)
	require.NoError(t, err)

	// NULL type: value 500
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value)
		VALUES ($1, $2, $3, $4, 1, 500.00)
	`, uuid.New(), userID, accountID, nullTypeSecID)
	require.NoError(t, err)

	// ── inactive-account holding (must be excluded) ────────────────────────────
	inactiveAccID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, replaced_at)
		VALUES ($1, $2, $3, $4, 'Old Brokerage', 'investment', 'replaced', now())
	`, inactiveAccID, linkedItemID, userID, "alloc-inactive-acct-"+inactiveAccID.String())
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity, institution_value)
		VALUES ($1, $2, $3, $4, 100, 99999.00)
	`, uuid.New(), userID, inactiveAccID, etfSecID)
	require.NoError(t, err)

	repo := NewInvestmentAnalyticsRepository(tx)
	// Pass empty options — default behavior (investment-only, security_type grouping, assets basis).
	result, err := repo.AllocationBreakdown(ctx, userID.String(), domain.AllocationOptions{})
	require.NoError(t, err)
	require.NotNil(t, result)

	// ── bucket count ──────────────────────────────────────────────────────────
	// 3 active buckets: "equity" (3000), "etf" (4000), "unknown" (500).
	// The inactive-account holding (99999) must not appear.
	require.Len(t, result.Buckets, 3, "expected 3 buckets: equity, etf, unknown")

	// ── TotalValue excludes inactive-account holdings ─────────────────────────
	const wantTotal = 7500.0 // 2000+1000+4000+500
	assert.InDelta(t, wantTotal, result.TotalValue, 0.01, "TotalValue must exclude inactive-account holdings")

	// ── ORDER BY total_value DESC: etf (4000) must come first ─────────────────
	assert.Equal(t, "etf", result.Buckets[0].SecurityType, "highest-value bucket must be first")
	assert.InDelta(t, 4000.0, result.Buckets[0].TotalValue, 0.01)
	assert.Equal(t, 1, result.Buckets[0].HoldingCount)

	assert.Equal(t, "equity", result.Buckets[1].SecurityType)
	assert.InDelta(t, 3000.0, result.Buckets[1].TotalValue, 0.01)
	assert.Equal(t, 2, result.Buckets[1].HoldingCount, "equity bucket must aggregate both holdings")

	// ── NULL type → "unknown" bucket ──────────────────────────────────────────
	assert.Equal(t, "unknown", result.Buckets[2].SecurityType, "NULL type must bucket as 'unknown'")
	assert.InDelta(t, 500.0, result.Buckets[2].TotalValue, 0.01)
	assert.Equal(t, 1, result.Buckets[2].HoldingCount)

	// ── percentage correctness ────────────────────────────────────────────────
	// etf:    4000/7500*100 ≈ 53.33%
	// equity: 3000/7500*100 = 40.00%
	// unknown: 500/7500*100 ≈  6.67%
	assert.InDelta(t, 53.33, result.Buckets[0].Percentage, 0.02, "etf percentage")
	assert.InDelta(t, 40.0, result.Buckets[1].Percentage, 0.02, "equity percentage")
	assert.InDelta(t, 6.67, result.Buckets[2].Percentage, 0.02, "unknown percentage")

	// Percentages must sum to ~100
	totalPct := result.Buckets[0].Percentage + result.Buckets[1].Percentage + result.Buckets[2].Percentage
	assert.InDelta(t, 100.0, totalPct, 0.1, "percentages must sum to 100")
}

// ── R10-1: AC#6 — unified feed search must match investment ticker symbols ────
//
// TestInvestment_UnifiedFeed_SearchMatchesTicker_Integration locks AC#6
// ("search matches tickers") with a real-Postgres execution of the unified UNION
// feed's investment-branch search clause:
//
//	AND (it.name ILIKE $N OR sec.ticker_symbol ILIKE $N OR sec.name ILIKE $N)
//
// The SQL is correct on static inspection (single bound $N referenced by both
// UNION branches; LEFT JOIN securities is NULL-safe for cash rows), but this
// was the LAST acceptance-criterion behavior with zero test coverage — sitting
// in the exact GAP-E surface that produced HIGH bugs in R7-B, R8-1, and R9-1.
//
// This test proves:
//  1. Search="BGAIN" (ticker_symbol) → only the investment row is returned.
//  2. Search="Coffee" (banking txn name) → only the banking row is returned.
//  3. Search="Boosted" (security name prefix) → only the investment row is returned.
//
// The shared $N parameter binding is validated implicitly: if the banking branch
// accidentally picked up the ticker pattern (or the investment branch dropped it),
// the assertion counts would be wrong.
func TestInvestment_UnifiedFeed_SearchMatchesTicker_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, investAcctID, _ := seedInvestmentFixture(ctx, t, tx, "inv-r10")

	// ── Create a security with a distinctive ticker and security name ────────────
	bgainSecID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol)
		VALUES ($1, $2, 'Boosted Gain Fund', 'BGAIN')
	`, bgainSecID, "r10-bgain-"+bgainSecID.String())
	require.NoError(t, err)

	// ── Seed a depository account for banking transactions ───────────────────────
	bankLinkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, bankLinkedItemID, userID, "bank-item-r10-"+bankLinkedItemID.String())
	require.NoError(t, err)

	bankAcctID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Checking R10', 'depository', 'active')
	`, bankAcctID, bankLinkedItemID, userID, "plaid-bank-r10-"+bankAcctID.String())
	require.NoError(t, err)

	txnDate := "2024-09-15"

	// ── Seed the investment transaction (references BGAIN security) ─────────────
	invTxnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id,
			 type, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'buy', 'Boosted Gain Fund Trade', 1500.00, $6::date)
	`, invTxnID, userID, investAcctID, bgainSecID, "plaid-r10-buy-"+invTxnID.String(), txnDate)
	require.NoError(t, err)

	// ── Seed the banking transaction (name="Coffee", no investment fields) ───────
	bankTxnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source,
			 external_transaction_id, external_transaction_id_blind_idx,
			 amount, date, name, pfc_primary, pfc_detailed, pending, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, 'plaid', $5, $6, 5.00, $7::date, 'Coffee',
			 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_COFFEE_SHOP', false, now(), now())
	`, bankTxnID, userID, bankAcctID, bankLinkedItemID,
		"bank-ext-r10-"+bankTxnID.String(), "bank-blind-r10-"+bankTxnID.String(), txnDate)
	require.NoError(t, err)

	repo := NewTransactionRepository(tx, enc)

	// ── Assertion 1: ticker search "BGAIN" returns only the investment row ───────
	searchTicker := "BGAIN"
	filters1 := domain.TransactionFilters{
		SortBy:                    "date",
		IncludeInvestmentActivity: true,
		Search:                    &searchTicker,
	}
	items1, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters1, nil, 10)
	require.NoError(t, err, "search by ticker")
	require.Len(t, items1, 1, "Search=BGAIN must return exactly the investment row")
	assert.Equal(t, domain.FeedSourceInvestment, items1[0].Source,
		"Search=BGAIN result must be the investment row (ticker_symbol match)")
	assert.Equal(t, invTxnID.String(), items1[0].ID,
		"result ID must be the investment transaction ID")

	// ── Assertion 2: name search "Coffee" returns only the banking row ───────────
	searchName := "Coffee"
	filters2 := domain.TransactionFilters{
		SortBy:                    "date",
		IncludeInvestmentActivity: true,
		Search:                    &searchName,
	}
	items2, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters2, nil, 10)
	require.NoError(t, err, "search by banking name")
	require.Len(t, items2, 1, "Search=Coffee must return exactly the banking row")
	assert.Equal(t, domain.FeedSourceBanking, items2[0].Source,
		"Search=Coffee result must be the banking row (proves shared $N does not cross-contaminate branches)")
	assert.Equal(t, bankTxnID.String(), items2[0].ID,
		"result ID must be the banking transaction ID")

	// ── Assertion 3: security-name search "Boosted" also matches the investment row ─
	// This exercises the `sec.name ILIKE $N` path (distinct from ticker_symbol).
	searchSecName := "Boosted"
	filters3 := domain.TransactionFilters{
		SortBy:                    "date",
		IncludeInvestmentActivity: true,
		Search:                    &searchSecName,
	}
	items3, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters3, nil, 10)
	require.NoError(t, err, "search by security name")
	require.Len(t, items3, 1, "Search=Boosted must return exactly the investment row via sec.name")
	assert.Equal(t, domain.FeedSourceInvestment, items3[0].Source,
		"Search=Boosted must match security name and return the investment row")
}

// ── #1304: chain-expansion for unified feed (RED tests) ───────────────────────
//
// The three tests below lock the requirement that GetByUserIDKeysetUnified must
// return pre-relink rows after an account is marked status='replaced'.  On current
// code both UNION branches use a hard JOIN on accounts.status='active' which
// silently drops rows whose account_id points at a replaced account.
//
// These tests FAIL on current code (RED) and must PASS after the Task 2 fix.

// TestInvestment_UnifiedFeed_InvestmentSurvivesRelink_Integration proves that an
// investment transaction inserted on accountA (status='active') is still visible
// in the unified feed after accountA is replaced by accountB (re-link).
func TestInvestment_UnifiedFeed_InvestmentSurvivesRelink_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountA, secID := seedInvestmentFixture(ctx, t, tx, "inv-u1")

	// Create a stable user_accounts row (survives re-links)
	userAcctID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, 'ins-test-u1')
	`, userAcctID, userID)
	require.NoError(t, err)

	// Wire accountA to the stable user_account
	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = $1 WHERE id = $2`,
		userAcctID, accountA)
	require.NoError(t, err)

	// Insert an investment transaction on accountA with user_account_id set
	txnID := uuid.New()
	userAcctIDStr := userAcctID.String()
	secIDStr := secID.String()
	invRepo := NewInvestmentTransactionRepository(tx)
	inserted, err := invRepo.UpsertBatch(ctx, []domain.InvestmentTransaction{{
		ID:                           txnID.String(),
		UserID:                       userID.String(),
		AccountID:                    accountA.String(),
		UserAccountID:                &userAcctIDStr,
		SecurityID:                   &secIDStr,
		PlaidInvestmentTransactionID: "plaid-u1-unified-001",
		Type:                         "buy",
		Name:                         "Buy AAPL unified",
		Amount:                       1200.00,
		Date:                         mustParseDate("2024-03-01"),
	}})
	require.NoError(t, err)
	assert.Equal(t, 1, inserted)

	// Create accountB (the post-relink active account) with the same user_account_id
	accountB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Relinked Brokerage U1', 'investment', 'active', $5)
	`, accountB, linkedItemID, userID, "plaid-relink-u1-"+accountB.String(), userAcctID)
	require.NoError(t, err)

	// Mark accountA as replaced (simulates Plaid re-link)
	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = now() WHERE id = $1`, accountA)
	require.NoError(t, err)

	// Unified feed with no account filter — must still return the pre-relink txn
	repo := NewTransactionRepository(tx, enc)
	filters := domain.TransactionFilters{
		IncludeInvestmentActivity: true,
	}
	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, nil, 10)
	require.NoError(t, err, "GetByUserIDKeysetUnified must not error")
	require.Len(t, items, 1,
		"pre-relink investment transaction must appear in unified feed after re-link (chain-expansion RED)")
	assert.Equal(t, txnID.String(), items[0].ID,
		"returned item must be the pre-relink transaction")
	assert.Equal(t, domain.FeedSourceInvestment, items[0].Source)
}

// TestInvestment_UnifiedFeed_SurvivesRelink_WithAccountFilter_Integration proves
// that filtering the unified feed by the NEW (post-relink) accountB still returns
// the pre-relink investment transaction stored under accountA.
func TestInvestment_UnifiedFeed_SurvivesRelink_WithAccountFilter_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountA, secID := seedInvestmentFixture(ctx, t, tx, "inv-u2")

	// Stable user_accounts row
	userAcctID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, 'ins-test-u2')
	`, userAcctID, userID)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = $1 WHERE id = $2`,
		userAcctID, accountA)
	require.NoError(t, err)

	txnID := uuid.New()
	userAcctIDStr := userAcctID.String()
	secIDStr := secID.String()
	invRepo := NewInvestmentTransactionRepository(tx)
	inserted, err := invRepo.UpsertBatch(ctx, []domain.InvestmentTransaction{{
		ID:                           txnID.String(),
		UserID:                       userID.String(),
		AccountID:                    accountA.String(),
		UserAccountID:                &userAcctIDStr,
		SecurityID:                   &secIDStr,
		PlaidInvestmentTransactionID: "plaid-u2-unified-002",
		Type:                         "buy",
		Name:                         "Buy VTI unified acct filter",
		Amount:                       800.00,
		Date:                         mustParseDate("2024-04-01"),
	}})
	require.NoError(t, err)
	assert.Equal(t, 1, inserted)

	// Create accountB (post-relink active) with same user_account_id
	accountB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Relinked Brokerage U2', 'investment', 'active', $5)
	`, accountB, linkedItemID, userID, "plaid-relink-u2-"+accountB.String(), userAcctID)
	require.NoError(t, err)

	// Mark accountA replaced
	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = now() WHERE id = $1`, accountA)
	require.NoError(t, err)

	// Filter by accountB — chain-expansion must reach pre-relink txn on accountA
	accountBStr := accountB.String()
	filters := domain.TransactionFilters{
		IncludeInvestmentActivity: true,
		AccountID:                 &accountBStr,
	}
	repo := NewTransactionRepository(tx, enc)
	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, nil, 10)
	require.NoError(t, err, "GetByUserIDKeysetUnified with AccountID filter must not error")
	require.Len(t, items, 1,
		"pre-relink investment txn must be found via accountB filter (chain-expansion RED)")
	assert.Equal(t, txnID.String(), items[0].ID,
		"returned item must be the pre-relink transaction")
	assert.Equal(t, domain.FeedSourceInvestment, items[0].Source)
}

// TestTransactions_UnifiedFeed_BankingSurvivesRelink_Integration proves that a
// banking transaction (transactions table) on a replaced account is still returned
// by the unified feed when chain-expansion is applied to the banking branch.
// Also asserts that orphan rows (account_id=NULL and user_account_id=NULL) are excluded.
func TestTransactions_UnifiedFeed_BankingSurvivesRelink_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountA, _ := seedInvestmentFixture(ctx, t, tx, "inv-u3")

	// Stable user_accounts row
	userAcctID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, 'ins-test-u3')
	`, userAcctID, userID)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = $1 WHERE id = $2`,
		userAcctID, accountA)
	require.NoError(t, err)

	// Insert a banking transaction on accountA with user_account_id set
	bankTxnID := uuid.New()
	userAcctIDStr := userAcctID.String()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, user_account_id,
			 source, external_transaction_id, external_transaction_id_blind_idx,
			 amount, date, name, pfc_primary, pfc_detailed, pending, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5,
			 'plaid', $6, $7,
			 42.00, '2024-05-01', 'Coffee Relink U3',
			 'FOOD_AND_DRINK', 'FOOD_AND_DRINK_COFFEE_SHOP', false, now(), now())
	`, bankTxnID, userID, accountA, linkedItemID, userAcctID,
		"bank-ext-u3-"+bankTxnID.String(), "bank-blind-u3-"+bankTxnID.String())
	require.NoError(t, err)

	// Create accountB (post-relink) with same user_account_id
	accountB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Relinked Checking U3', 'depository', 'active', $5)
	`, accountB, linkedItemID, userID, "plaid-relink-u3-"+accountB.String(), userAcctIDStr)
	require.NoError(t, err)

	// Mark accountA replaced
	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = now() WHERE id = $1`, accountA)
	require.NoError(t, err)

	// IncludeInvestmentActivity gating happens in transaction_service, not in SQL.
	repo := NewTransactionRepository(tx, enc)
	filters := domain.TransactionFilters{
		IncludeInvestmentActivity: false,
	}
	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, nil, 10)
	require.NoError(t, err, "GetByUserIDKeysetUnified (banking-only) must not error")
	require.Len(t, items, 1,
		"pre-relink banking transaction must appear in unified feed after re-link (banking chain-expansion RED)")
	assert.Equal(t, bankTxnID.String(), items[0].ID,
		"returned item must be the pre-relink banking transaction")
	assert.Equal(t, domain.FeedSourceBanking, items[0].Source)

	// ── Sub-case: orphan row (no account_id linkage) must be excluded ─────────
	// Insert a banking row with account_id pointing to a replaced account and
	// user_account_id=NULL — this is a true orphan with no chain to follow.
	// The result count must remain 1.
	orphanTxnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id,
			 source, external_transaction_id, external_transaction_id_blind_idx,
			 amount, date, name, pending, created_at, updated_at)
		VALUES
			($1, $2, $3, $4,
			 'plaid', $5, $6,
			 7.00, '2024-05-02', 'Orphan Row U3', false, now(), now())
	`, orphanTxnID, userID, accountA, linkedItemID,
		"orphan-ext-u3-"+orphanTxnID.String(), "orphan-blind-u3-"+orphanTxnID.String())
	require.NoError(t, err)

	// Re-query — orphan must NOT be returned (user_account_id is NULL, no chain)
	items2, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, nil, 10)
	require.NoError(t, err, "re-query after orphan insert must not error")
	// Filter to count only the chained banking row.
	// On current code both rows are dropped (inner JOIN excludes replaced accountA),
	// so chainedCount will be 0 — the key RED failure for this sub-case.
	// After the fix, exactly 1 row (the chained one) must appear.
	chainedCount := 0
	for _, item := range items2 {
		if item.ID == bankTxnID.String() {
			chainedCount++
		}
	}
	assert.Equal(t, 1, chainedCount,
		"chained banking transaction (user_account_id set) must appear exactly once")
	for _, item := range items2 {
		assert.NotEqual(t, orphanTxnID.String(), item.ID,
			"orphan banking transaction (user_account_id=NULL on replaced account) must be excluded")
	}

	// ── Sub-case R11: NULL account_id + active sibling → INCLUDED ────────────
	// Migration 000035 (ON DELETE SET NULL) can produce a transactions row where
	// account_id IS NULL after the referenced account is hard-deleted, while
	// user_account_id still points at a live chain.  The first EXISTS leg
	// (EXISTS ... WHERE id = t.account_id AND status='active') evaluates to
	// UNKNOWN when account_id IS NULL (Postgres three-valued logic: id = NULL →
	// UNKNOWN, not TRUE), so the row falls through to the chain leg.
	// This sub-case asserts that fall-through include path is working, so a
	// future refactor (e.g. = ANY(...) or COALESCE) can't silently break it.
	nullAcctTxnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, user_account_id,
			 source, external_transaction_id, external_transaction_id_blind_idx,
			 amount, date, name, pending, created_at, updated_at)
		VALUES
			($1, $2, NULL, $3, $4,
			 'plaid', $5, $6,
			 11.00, '2024-05-04', 'Null-Acct Chained U3', false, now(), now())
	`, nullAcctTxnID, userID, linkedItemID, userAcctID,
		"ext-"+nullAcctTxnID.String(), "blind-"+nullAcctTxnID.String())
	require.NoError(t, err)

	// Re-query — null-acct chained row MUST be returned; orphan MUST NOT.
	items3, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, nil, 10)
	require.NoError(t, err, "re-query after null-acct-chained insert must not error")
	nullAcctFound := false
	for _, item := range items3 {
		if item.ID == nullAcctTxnID.String() {
			nullAcctFound = true
		}
		assert.NotEqual(t, orphanTxnID.String(), item.ID,
			"orphan (NULL user_account_id) must remain excluded after null-acct-chained insert")
	}
	assert.True(t, nullAcctFound,
		"banking transaction with account_id=NULL but user_account_id chained to an active account "+
			"must appear in unified feed (migration 000035 SET NULL path — R11)")
}

// TestTransactions_UnifiedFeed_NoDuplication_Integration verifies that EXISTS (semi-join)
// prevents row duplication when multiple active accounts share a user_account_id.
// A JOIN would produce multiple rows; this test asserts exactly 1 output row.
func TestTransactions_UnifiedFeed_NoDuplication_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountA, _ := seedInvestmentFixture(ctx, t, tx, "dedup-u4")

	// Stable chain key shared by three accounts.
	userAcctID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, 'ins-test-u4')
	`, userAcctID, userID)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `UPDATE accounts SET user_account_id = $1 WHERE id = $2`,
		userAcctID, accountA)
	require.NoError(t, err)

	// accountB — first active sibling (post-relink).
	accountB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Active Sibling B U4', 'depository', 'active', $5)
	`, accountB, linkedItemID, userID, "plaid-b-u4-"+accountB.String(), userAcctID)
	require.NoError(t, err)

	// accountC — second active sibling; under a JOIN both B and C would each match the
	// transaction's user_account_id, producing 2 output rows instead of 1.
	accountC := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Active Sibling C U4', 'depository', 'active', $5)
	`, accountC, linkedItemID, userID, "plaid-c-u4-"+accountC.String(), userAcctID)
	require.NoError(t, err)

	// Mark accountA replaced (the original account bearing the transaction).
	_, err = tx.Exec(ctx, `UPDATE accounts SET status = 'replaced', replaced_at = now() WHERE id = $1`, accountA)
	require.NoError(t, err)

	// Insert a single banking transaction on the replaced accountA.
	bankTxnID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, user_account_id,
			 source, external_transaction_id, external_transaction_id_blind_idx,
			 amount, date, name, pending, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5,
			 'plaid', $6, $7,
			 99.00, '2024-06-01', 'Dedup Guard U4', false, now(), now())
	`, bankTxnID, userID, accountA, linkedItemID, userAcctID,
		"dedup-ext-u4-"+bankTxnID.String(), "dedup-blind-u4-"+bankTxnID.String())
	require.NoError(t, err)

	repo := NewTransactionRepository(tx, enc)
	filters := domain.TransactionFilters{IncludeInvestmentActivity: false}
	items, _, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, nil, 10)
	require.NoError(t, err, "GetByUserIDKeysetUnified must not error")

	// Count how many times bankTxnID appears. Under a JOIN with 2 active siblings
	// this would be 2; EXISTS (semi-join) must yield exactly 1.
	count := 0
	for _, item := range items {
		if item.ID == bankTxnID.String() {
			count++
		}
	}
	assert.Equal(t, 1, count,
		"banking transaction must appear exactly once even when two active siblings share the "+
			"same user_account_id (EXISTS semi-join must not fan-out like a JOIN would — Issue #1304)")
}

// ── #1306: UpsertBatch ON CONFLICT DO UPDATE atomicity ───────────────────────

// TestInvestmentTransactionRepository_UpsertBatch_ConflictSemantics_Integration
// validates what pgxmock unit tests cannot: that the actual PostgreSQL
// ON CONFLICT DO UPDATE conflict handling correctly:
//
//	(a) returns inserted=1 for a fresh row (xmax::text::bigint=0),
//	(b) backfills user_account_id and returns inserted=0 when the existing row
//	    has NULL user_account_id and the incoming row has a non-NULL value,
//	(c) leaves an existing non-NULL user_account_id untouched and returns
//	    inserted=0 when the DO UPDATE WHERE clause is false.
//
// This is the real-Postgres regression test for Issue #1306.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... \
//	  -run TestInvestmentTransactionRepository_UpsertBatch_ConflictSemantics_Integration \
//	  -tags integration -v
func TestInvestmentTransactionRepository_UpsertBatch_ConflictSemantics_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-1306")

	// user_accounts row required for user_account_id FK.
	userAcctID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, 'ins-test-1306')
	`, userAcctID, userID)
	require.NoError(t, err)

	userAcctIDStr := userAcctID.String()
	secIDStr := secID.String()

	repo := NewInvestmentTransactionRepository(tx)

	// ── (a) Fresh insert → inserted=1, user_account_id stored ─────────────────
	plaidID1 := "plaid-1306-fresh-" + uuid.New().String()
	inserted, err := repo.UpsertBatch(ctx, []domain.InvestmentTransaction{{
		ID:                           uuid.New().String(),
		UserID:                       userID.String(),
		AccountID:                    accountID.String(),
		UserAccountID:                &userAcctIDStr,
		SecurityID:                   &secIDStr,
		PlaidInvestmentTransactionID: plaidID1,
		Type:                         "buy",
		Name:                         "Fresh insert",
		Amount:                       100.00,
		Date:                         mustParseDate("2024-01-01"),
	}})
	require.NoError(t, err)
	assert.Equal(t, 1, inserted, "(a) fresh insert must return inserted=1")

	var storedUAID *string
	err = tx.QueryRow(ctx,
		`SELECT user_account_id::text FROM investment_transactions WHERE plaid_investment_transaction_id = $1`,
		plaidID1).Scan(&storedUAID)
	require.NoError(t, err)
	require.NotNil(t, storedUAID, "(a) user_account_id must be stored on fresh insert")
	assert.Equal(t, userAcctIDStr, *storedUAID)

	// ── (b) Conflict, existing NULL, incoming non-NULL → backfill, inserted=0 ──
	plaidID2 := "plaid-1306-null-" + uuid.New().String()
	// Seed row directly with NULL user_account_id (simulates a pre-fix row).
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id, type, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'buy', 'Pre-fix NULL row', 200.00, '2024-01-02')
	`, uuid.New(), userID, accountID, secID, plaidID2)
	require.NoError(t, err)

	inserted, err = repo.UpsertBatch(ctx, []domain.InvestmentTransaction{{
		ID:                           uuid.New().String(),
		UserID:                       userID.String(),
		AccountID:                    accountID.String(),
		UserAccountID:                &userAcctIDStr, // non-NULL incoming → DO UPDATE fires
		SecurityID:                   &secIDStr,
		PlaidInvestmentTransactionID: plaidID2,
		Type:                         "buy",
		Name:                         "Pre-fix NULL row",
		Amount:                       200.00,
		Date:                         mustParseDate("2024-01-02"),
	}})
	require.NoError(t, err)
	assert.Equal(t, 0, inserted, "(b) conflict backfill must return inserted=0")

	err = tx.QueryRow(ctx,
		`SELECT user_account_id::text FROM investment_transactions WHERE plaid_investment_transaction_id = $1`,
		plaidID2).Scan(&storedUAID)
	require.NoError(t, err)
	require.NotNil(t, storedUAID, "(b) user_account_id must be backfilled after DO UPDATE fires")
	assert.Equal(t, userAcctIDStr, *storedUAID)

	// ── (c) Conflict, existing non-NULL → no-op, inserted=0, data unchanged ────
	// Re-insert row from (a) which already has user_account_id set; WHERE is false.
	inserted, err = repo.UpsertBatch(ctx, []domain.InvestmentTransaction{{
		ID:                           uuid.New().String(),
		UserID:                       userID.String(),
		AccountID:                    accountID.String(),
		UserAccountID:                &userAcctIDStr,
		SecurityID:                   &secIDStr,
		PlaidInvestmentTransactionID: plaidID1, // same plaid ID as (a) → conflict, non-NULL → no-op
		Type:                         "buy",
		Name:                         "Fresh insert",
		Amount:                       999.00, // different amount must NOT be stored
		Date:                         mustParseDate("2024-01-01"),
	}})
	require.NoError(t, err)
	assert.Equal(t, 0, inserted, "(c) no-op conflict must return inserted=0")

	var storedAmount float64
	err = tx.QueryRow(ctx,
		`SELECT amount FROM investment_transactions WHERE plaid_investment_transaction_id = $1`,
		plaidID1).Scan(&storedAmount)
	require.NoError(t, err)
	assert.InDelta(t, 100.00, storedAmount, 0.001, "(c) amount must not change on no-op conflict")
}

// TestInvestmentTransactionRepository_UpsertBatch_DuplicatePlaidIDInBatch_Integration
// locks the behavior when the SAME plaid_investment_transaction_id appears twice in a
// single UpsertBatch call. Because UpsertBatch issues one atomic INSERT ... ON CONFLICT
// DO UPDATE statement per slice element, the second occurrence is processed as a conflict
// against the row the first occurrence just inserted — so the end state is correct and
// more atomic than the deleted post-loop bulk UPDATE (Issue #1306, Codex Finding #1).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... \
//	  -run TestInvestmentTransactionRepository_UpsertBatch_DuplicatePlaidIDInBatch_Integration \
//	  -tags integration -v
func TestInvestmentTransactionRepository_UpsertBatch_DuplicatePlaidIDInBatch_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-1306-dup")

	userAcctID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, 'ins-test-1306-dup')
	`, userAcctID, userID)
	require.NoError(t, err)

	userAcctIDStr := userAcctID.String()
	secIDStr := secID.String()
	repo := NewInvestmentTransactionRepository(tx)

	// helper: build a txn with the given plaid_id and optional user_account_id
	mk := func(plaidID string, uaid *string) domain.InvestmentTransaction {
		return domain.InvestmentTransaction{
			ID:                           uuid.New().String(),
			UserID:                       userID.String(),
			AccountID:                    accountID.String(),
			UserAccountID:                uaid,
			SecurityID:                   &secIDStr,
			PlaidInvestmentTransactionID: plaidID,
			Type:                         "buy",
			Name:                         "dup-in-batch",
			Amount:                       100.00,
			Date:                         mustParseDate("2024-03-01"),
		}
	}
	readUAID := func(plaidID string) *string {
		var got *string
		e := tx.QueryRow(ctx,
			`SELECT user_account_id::text FROM investment_transactions WHERE plaid_investment_transaction_id = $1`,
			plaidID).Scan(&got)
		require.NoError(t, e)
		return got
	}

	// ── (a) order [nil, then non-NULL]: iter2 DO UPDATE backfills in-batch ─────
	plaidA := "plaid-1306-dup-a-" + uuid.New().String()
	insertedA, err := repo.UpsertBatch(ctx, []domain.InvestmentTransaction{
		mk(plaidA, nil),
		mk(plaidA, &userAcctIDStr),
	})
	require.NoError(t, err)
	assert.Equal(t, 1, insertedA, "(a) duplicate plaid_id in one batch counts as exactly one insert")
	gotA := readUAID(plaidA)
	require.NotNil(t, gotA, "(a) user_account_id must be backfilled by the in-batch conflict DO UPDATE")
	assert.Equal(t, userAcctIDStr, *gotA)

	// ── (b) order [non-NULL, then nil]: iter2 WHERE false → preserves value ────
	plaidB := "plaid-1306-dup-b-" + uuid.New().String()
	insertedB, err := repo.UpsertBatch(ctx, []domain.InvestmentTransaction{
		mk(plaidB, &userAcctIDStr),
		mk(plaidB, nil),
	})
	require.NoError(t, err)
	assert.Equal(t, 1, insertedB, "(b) duplicate plaid_id in one batch counts as exactly one insert")
	gotB := readUAID(plaidB)
	require.NotNil(t, gotB, "(b) incoming NULL must not clobber the non-NULL value set by the first row")
	assert.Equal(t, userAcctIDStr, *gotB)
}
