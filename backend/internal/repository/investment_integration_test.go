//go:build integration

package repository

// Integration tests for the investment stack (#138 gap fixes).
// These tests execute SQL against a real Postgres instance and catch bugs that
// pgxmock cannot detect (type/NULL/ambiguous-column errors, UNION keyset semantics).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... \
//	  -run TestInvestment_ -tags integration -v

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
		INSERT INTO securities (id, plaid_security_id, name) VALUES ($1, $2, 'Test Security')
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
	snap, err := repo.Upsert(ctx, userID.String(), time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))
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
// snapshot Upsert excludes holdings on inactive (replaced) accounts, consistent
// with all read paths that JOIN accounts WHERE status='active'.
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
	snap, err := repo.Upsert(ctx, userID.String(), time.Date(2024, 6, 2, 0, 0, 0, 0, time.UTC))
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
	// TotalCostBasis: only A's cost_basis (known-basis pair, FILTER semantics)
	assert.InDelta(t, 1600.0, result.TotalCostBasis, 0.01, "TotalCostBasis must only count known-basis pairs")
	// gain = A only: 2000-1600 = 400, NOT 2000-(1600+500) = -100 phantom loss
	assert.InDelta(t, 400.0, result.TotalUnrealizedGainLoss, 0.01,
		"gain/loss must NOT be phantom-loss due to NULL institution_value holding")
	require.NotNil(t, result.TotalUnrealizedGainLossPct)
	assert.InDelta(t, 25.0, *result.TotalUnrealizedGainLossPct, 0.01, "pct = 400/1600*100 = 25%")
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
	page1, cursor1, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, nil, 2)
	require.NoError(t, err, "page 1 must not error")
	require.Len(t, page1, 2, "page 1 must have exactly 2 items")
	require.NotNil(t, cursor1, "page 1 must return a next cursor")

	// Page 2: continue from cursor → exactly 2 rows, no next cursor
	page2, cursor2, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, cursor1, 2)
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

	items, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, nil, 10)
	require.NoError(t, err, "query must not error")

	// Only the investment row (buy) must be returned — banking must be excluded (R2-4 fix)
	require.Len(t, items, 1, "only the investment buy transaction should be returned when activity_types filter is active")
	assert.Equal(t, domain.FeedSourceInvestment, items[0].Source, "result must be the investment row")
	assert.Equal(t, invTxnID.String(), items[0].ID, "result must be the buy investment transaction")
}

// ── R2-1: user_account_id re-link propagation ───────────────────────────────

// TestInvestment_UserAccountID_SurvivesRelink_Integration proves that:
// 1. UpsertBatch populates user_account_id at INSERT time (GAP D fix).
// 2. A second UpsertBatch with the same plaid_investment_transaction_id and a
//    non-nil UserAccountID backfills user_account_id on pre-fix NULL rows (R2-5 bulk-backfill).
// 3. GetByUserIDKeyset filtered by a NEW account_id (post-relink) still returns the
//    pre-relink transactions via chain-expansion through user_account_id.
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
	items, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters, nil, 10)
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
	itemsMerchant, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filtersMerchant, nil, 10)
	require.NoError(t, err, "MerchantID filter must not error")
	// Merchant filter returns zero rows (no txn has that merchant), confirming investment branch zeroed
	assert.Empty(t, itemsMerchant, "MerchantID filter must zero investment rows (proves transaction_repository.go:2323-2328 branch)")

	// ── Assertion 2: AccountIDs (both-branch filter) returns investment rows ──
	investAcctIDStr := investAcctID.String()
	filters2 := domain.TransactionFilters{
		SortBy:                    "date",
		IncludeInvestmentActivity: true,
		AccountIDs:                []string{investAcctIDStr},
	}
	items2, _, err := repo.GetByUserIDKeysetUnified(ctx, userID.String(), filters2, nil, 10)
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
