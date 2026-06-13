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
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+prefix+"-"+userID.String(), prefix+"-"+userID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, prefix+"-item-"+linkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Test Brokerage', 'investment', 'active')
	`, accountID, linkedItemID, userID, prefix+"-acct-"+accountID.String())
	require.NoError(t, err, "seed investment account")

	_, err = db.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name) VALUES ($1, $2, 'Test Security')
	`, secID, prefix+"-sec-"+secID.String())
	require.NoError(t, err, "seed security")

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
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Old Brokerage', 'investment', 'replaced')
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
				 'food_and_drink', 'groceries', false, now(), now())
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
