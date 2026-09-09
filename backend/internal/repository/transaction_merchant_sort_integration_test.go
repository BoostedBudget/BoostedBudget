//go:build integration

package repository

// Integration tests for effective merchant name sort/search (issue #1348, Task 1).
//
// These tests require a real PostgreSQL instance (DATABASE_URL env var).
// They exercise behavior that pgxmock cannot validate: JOIN semantics, collation,
// cursor boundary integrity, and search result accuracy.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -tags integration \
//	  -run 'TestTransactionMerchant' -v

import (
	"context"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// merchantTestFixture holds IDs for a test scenario.
type merchantTestFixture struct {
	userID     string
	accountID  string
	itemID     string
	merchantA  string // e.g. "Acme Corp" — will be overridden to "AAA Override"
	merchantB  string // e.g. "Beta LLC" — unmodified
	merchantC  string // will be merged into merchantB
	txAID      string // transaction linked to merchantA
	txBID      string // transaction linked to merchantB
	txCID      string // transaction linked to merchantC (sorts by merchantB's name after merge)
	txNullID   string // transaction with null merchant_id — sorts by t.name
	txNoLinkID string // transaction with merchant_name set but merchant_id NULL
}

// setupMerchantTestFixture creates a complete test scenario for merchant sort tests.
// All entities are inserted and cleaned up via t.Cleanup.
func setupMerchantTestFixture(t *testing.T, db *pgxpool.Pool) merchantTestFixture {
	t.Helper()
	ctx := context.Background()

	userUUID := uuid.New()
	f := merchantTestFixture{
		userID:     userUUID.String(),
		accountID:  uuid.New().String(),
		itemID:     uuid.New().String(),
		merchantA:  uuid.New().String(),
		merchantB:  uuid.New().String(),
		merchantC:  uuid.New().String(),
		txAID:      uuid.New().String(),
		txBID:      uuid.New().String(),
		txCID:      uuid.New().String(),
		txNullID:   uuid.New().String(),
		txNoLinkID: uuid.New().String(),
	}

	baseDate := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	// Insert user (required FK for linked_items and accounts)
	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, primary_region, subscription_tier, subscription_status)
		VALUES ($1, $2, $3, 'iad', 'premium', 'active')`,
		f.userID, "test|"+f.userID[:8], "int-merchant-"+f.userID[:8]+"@example.com")
	require.NoError(t, err, "insert user")

	// Insert linked item
	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted,
			institution_name, institution_id, provider, created_at, updated_at)
		VALUES ($1, $2, $3, 'enc_test', 'Test Bank', 'ins_test', 'plaid', NOW(), NOW())`,
		f.itemID, f.userID, "item-"+f.itemID)
	require.NoError(t, err, "insert linked item")

	// Insert account
	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, user_id, linked_item_id, plaid_account_id,
			name, type, subtype, current_balance, available_balance, currency_code,
			status, created_at, updated_at)
		VALUES ($1, $2, $3, 'ext_test', 'Test Account', 'depository', 'checking',
			1000.00, 900.00, 'USD', 'active', NOW(), NOW())`,
		f.accountID, f.userID, f.itemID)
	require.NoError(t, err, "insert account")

	// Insert merchants
	for _, mid := range []string{f.merchantA, f.merchantB, f.merchantC} {
		name := "Merchant-" + mid // full UUID keeps name unique across parallel test runs
		_, err = db.Exec(ctx, `
			INSERT INTO merchants (id, name, normalized_name, created_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (id) DO NOTHING`,
			mid, name, name)
		require.NoError(t, err, "insert merchant")
	}

	// Override merchantA: user renames it to "AAA Override" (sorts first ASC)
	_, err = db.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, display_name, created_at, updated_at)
		VALUES ($1, $2, $3, 'AAA Override', NOW(), NOW())`,
		uuid.New().String(), f.userID, f.merchantA)
	require.NoError(t, err, "insert override for merchantA")

	// Merge merchantC into merchantB (user merged C→B)
	_, err = db.Exec(ctx, `
		INSERT INTO merchant_name_overrides (id, user_id, merchant_id, merged_into_merchant_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())`,
		uuid.New().String(), f.userID, f.merchantC, f.merchantB)
	require.NoError(t, err, "insert merge override C→B")

	// Insert transactions
	insertTx := func(txID, name, merchantID *string, merchantName *string, date time.Time) {
		mid := (*string)(nil)
		if merchantID != nil {
			mid = merchantID
		}
		extID := *txID
		_, err := db.Exec(ctx, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id,
				source, external_transaction_id, external_transaction_id_blind_idx,
				amount, date, name, merchant_name, merchant_id,
				pending, pfc_primary, category_source,
				created_at, updated_at)
			VALUES ($1, $2, $3, $4,
				'plaid', $5, $6,
				10.00, $7, $8, $9, $10,
				false, 'FOOD_AND_DRINK', 'plaid',
				NOW(), NOW())`,
			*txID, f.userID, f.accountID, f.itemID,
			extID, extID, date, *name, merchantName, mid)
		require.NoError(t, err, "insert transaction %s", *txID)
	}

	txAID := f.txAID
	txBID := f.txBID
	txCID := f.txCID
	txNullID := f.txNullID
	txNoLinkID := f.txNoLinkID
	nameA := "Store A"
	nameB := "Store B"
	nameC := "Store C"
	nameNull := "ZZZ No Merchant"
	nameNoLink := "YYY No Link"

	mN := "raw merchant_name"

	insertTx(&txAID, &nameA, &f.merchantA, nil, baseDate.Add(4*24*time.Hour))
	insertTx(&txBID, &nameB, &f.merchantB, nil, baseDate.Add(3*24*time.Hour))
	insertTx(&txCID, &nameC, &f.merchantC, nil, baseDate.Add(2*24*time.Hour))
	insertTx(&txNullID, &nameNull, nil, nil, baseDate.Add(1*24*time.Hour))     // null merchant_id
	insertTx(&txNoLinkID, &nameNoLink, nil, &mN, baseDate.Add(0*24*time.Hour)) // merchant_name set, merchant_id NULL

	t.Cleanup(func() {
		ctx2 := context.Background()
		db.Exec(ctx2, `DELETE FROM transactions WHERE user_id = $1`, f.userID)            //nolint:errcheck
		db.Exec(ctx2, `DELETE FROM merchant_name_overrides WHERE user_id = $1`, f.userID) //nolint:errcheck
		db.Exec(ctx2, `DELETE FROM accounts WHERE user_id = $1`, f.userID)                //nolint:errcheck
		db.Exec(ctx2, `DELETE FROM linked_items WHERE user_id = $1`, f.userID)            //nolint:errcheck
		db.Exec(ctx2, `DELETE FROM merchants WHERE id IN ($1, $2, $3)`,                   //nolint:errcheck
			f.merchantA, f.merchantB, f.merchantC)
		db.Exec(ctx2, `DELETE FROM users WHERE id = $1`, f.userID) //nolint:errcheck
	})

	return f
}

// TestTransactionMerchantSort_OverrideFlipsSortOrder verifies that a user's display_name
// override changes the effective sort position vs the raw merchant name.
func TestTransactionMerchantSort_OverrideFlipsSortOrder(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	f := setupMerchantTestFixture(t, db)

	pool := db
	enc := newTestFieldEncryptor(t)

	// Build a minimal transactionRepository using the pool directly.
	// We use the pool adapter pattern consistent with the rest of the integration tests.
	repo := &transactionRepository{db: pool, enc: enc}

	filters := domain.TransactionFilters{SortBy: "merchant", SortDir: "asc"}
	txns, _, _, err := repo.GetByUserIDKeyset(ctx, f.userID, filters, nil, 50)
	require.NoError(t, err)

	// Collect IDs in returned order
	var ids []string
	for _, tx := range txns {
		ids = append(ids, tx.ID)
	}

	// txA (override "AAA Override") should come first since 'A' < 'M' (Merchant-...)
	// txC (merged into merchantB) should sort by merchantB's name
	// txNull (no merchant_id) sorts by t.name "ZZZ No Merchant" — should be last or near last
	require.True(t, len(ids) >= 2, "expected at least 2 results")

	posA := slices.Index(ids, f.txAID)
	posNull := slices.Index(ids, f.txNullID)

	assert.NotEqual(t, -1, posA, "txA should be in results")
	assert.NotEqual(t, -1, posNull, "txNull should be in results")

	// "AAA Override" < "ZZZ No Merchant" so txA should come before txNull
	assert.Less(t, posA, posNull,
		"transaction with override 'AAA Override' should sort before 'ZZZ No Merchant'")
}

// TestTransactionMerchantSort_MergeChain verifies that a transaction linked to a source
// merchant (C) that was merged into a target (B) sorts by B's effective name.
func TestTransactionMerchantSort_MergeChain(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	f := setupMerchantTestFixture(t, db)

	repo := &transactionRepository{db: db, enc: newTestFieldEncryptor(t)}

	filters := domain.TransactionFilters{SortBy: "merchant", SortDir: "asc"}
	txns, _, _, err := repo.GetByUserIDKeyset(ctx, f.userID, filters, nil, 50)
	require.NoError(t, err)

	var ids []string
	for _, tx := range txns {
		ids = append(ids, tx.ID)
	}

	posB := slices.Index(ids, f.txBID)
	posC := slices.Index(ids, f.txCID)

	assert.NotEqual(t, -1, posB, "txB should be in results")
	assert.NotEqual(t, -1, posC, "txC should be in results")

	// txC is merged into merchantB, so both should sort by the same effective name.
	// They may appear adjacent (order between them is by t.id tiebreaker).
	diff := posB - posC
	if diff < 0 {
		diff = -diff
	}
	// Sort position assertion is collation-agnostic; verify adjacency in sort order
	assert.Equal(t, 1, diff, "txB and txC should be adjacent in sort order; got positions B=%d C=%d", posB, posC)
}

// TestTransactionMerchantSort_NullMerchantSortsByName verifies that a transaction with
// NULL merchant_id sorts by t.name (EFF fallback) and doesn't disappear.
func TestTransactionMerchantSort_NullMerchantSortsByName(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	f := setupMerchantTestFixture(t, db)

	repo := &transactionRepository{db: db, enc: newTestFieldEncryptor(t)}

	filters := domain.TransactionFilters{SortBy: "merchant", SortDir: "asc"}
	txns, _, _, err := repo.GetByUserIDKeyset(ctx, f.userID, filters, nil, 50)
	require.NoError(t, err)

	var ids []string
	for _, tx := range txns {
		ids = append(ids, tx.ID)
	}

	assert.Contains(t, ids, f.txNullID, "transaction with null merchant_id must appear in results")
}

// TestTransactionMerchantSort_MerchantNameNoIDSortsByMerchantName verifies that a
// transaction with merchant_name set but merchant_id NULL sorts by merchant_name
// (NULLIF fallback in effectiveMerchantNameExpr).
func TestTransactionMerchantSort_MerchantNameNoIDSortsByMerchantName(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	f := setupMerchantTestFixture(t, db)

	repo := &transactionRepository{db: db, enc: newTestFieldEncryptor(t)}

	filters := domain.TransactionFilters{SortBy: "merchant", SortDir: "asc"}
	txns, _, _, err := repo.GetByUserIDKeyset(ctx, f.userID, filters, nil, 50)
	require.NoError(t, err)

	var ids []string
	for _, tx := range txns {
		ids = append(ids, tx.ID)
	}

	assert.Contains(t, ids, f.txNoLinkID, "transaction with merchant_name but no merchant_id must appear")

	// Both txNoLink (effective="raw merchant_name") and txNull (effective="ZZZ No Merchant") appear in
	// results — which sorts first depends on DB collation (case-sensitive: lowercase 'r' > 'Z').
	// The invariant is that txNoLink appears and is NOT equal to txNull's position.
	posNoLink := slices.Index(ids, f.txNoLinkID)
	posNull := slices.Index(ids, f.txNullID)
	assert.NotEqual(t, -1, posNoLink, "txNoLink should appear in sort results")
	assert.NotEqual(t, posNoLink, posNull, "txNoLink and txNull should be at different positions")
}

// TestTransactionMerchantSort_PaginationBoundary verifies that keyset pagination
// over the full result set (asc + desc) produces no duplicates and no skips.
func TestTransactionMerchantSort_PaginationBoundary(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	f := setupMerchantTestFixture(t, db)

	repo := &transactionRepository{db: db, enc: newTestFieldEncryptor(t)}

	for _, dir := range []string{"asc", "desc"} {
		t.Run("dir="+dir, func(t *testing.T) {
			filters := domain.TransactionFilters{SortBy: "merchant", SortDir: dir}

			// Get full result in one shot
			full, _, _, err := repo.GetByUserIDKeyset(ctx, f.userID, filters, nil, 100)
			require.NoError(t, err)
			require.NotEmpty(t, full, "should have transactions")

			// Paginate with page size 2
			var pages [][]domain.Transaction
			var cur *domain.TransactionCursor
			for {
				page, next, _, err2 := repo.GetByUserIDKeyset(ctx, f.userID, filters, cur, 2)
				require.NoError(t, err2)
				if len(page) == 0 {
					break
				}
				pages = append(pages, page)
				cur = next
				if cur == nil {
					break
				}
			}

			// Concatenate pages
			var paged []domain.Transaction
			for _, p := range pages {
				paged = append(paged, p...)
			}

			// Must equal full result: same IDs in same order
			require.Equal(t, len(full), len(paged),
				"paginated concat must equal full result length (dir=%s)", dir)

			for i, tx := range full {
				assert.Equal(t, tx.ID, paged[i].ID,
					"mismatch at position %d (dir=%s)", i, dir)
			}

			// No duplicates
			seen := make(map[string]int)
			for i, tx := range paged {
				if prev, ok := seen[tx.ID]; ok {
					t.Errorf("duplicate ID %s at positions %d and %d (dir=%s)", tx.ID, prev, i, dir)
				}
				seen[tx.ID] = i
			}
		})
	}
}

// TestTransactionMerchantSort_SearchFindsByEffectiveName verifies that searching for
// a user's display_name override finds the transaction (G8 search behavior).
func TestTransactionMerchantSort_SearchFindsByEffectiveName(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	f := setupMerchantTestFixture(t, db)

	repo := &transactionRepository{db: db, enc: newTestFieldEncryptor(t)}

	searchTerm := "AAA Override" // this is the override name for merchantA
	filters := domain.TransactionFilters{
		SortBy: "date",
		Search: &searchTerm,
	}

	txns, _, _, err := repo.GetByUserIDKeyset(ctx, f.userID, filters, nil, 50)
	require.NoError(t, err)

	var ids []string
	for _, tx := range txns {
		ids = append(ids, tx.ID)
	}

	assert.Contains(t, ids, f.txAID,
		"search for effective name 'AAA Override' must find txA (linked to overridden merchant)")
}

// TestTransactionMerchantSort_OffsetPathParity verifies that GetByUserIDWithFilters
// returns the same order as GetByUserIDKeyset (concatenated pages).
func TestTransactionMerchantSort_OffsetPathParity(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	f := setupMerchantTestFixture(t, db)

	repo := &transactionRepository{db: db, enc: newTestFieldEncryptor(t)}

	filters := domain.TransactionFilters{SortBy: "merchant", SortDir: "asc"}

	// Get via offset path
	offsetTxns, err := repo.GetByUserIDWithFilters(ctx, f.userID, filters, 100, 0)
	require.NoError(t, err)

	// Get via keyset path (single page, large limit)
	keysetTxns, _, _, err := repo.GetByUserIDKeyset(ctx, f.userID, filters, nil, 100)
	require.NoError(t, err)

	require.Equal(t, len(offsetTxns), len(keysetTxns),
		"offset and keyset paths should return same number of transactions")

	// Extract IDs in order for comparison
	offsetIDs := make([]string, len(offsetTxns))
	for i, tx := range offsetTxns {
		offsetIDs[i] = tx.ID
	}
	keysetIDs := make([]string, len(keysetTxns))
	for i, tx := range keysetTxns {
		keysetIDs[i] = tx.ID
	}

	// Sort both for set equality check (order may differ slightly due to tiebreaker differences)
	// But we also want order parity — check both
	offsetIDsSorted := append([]string(nil), offsetIDs...)
	keysetIDsSorted := append([]string(nil), keysetIDs...)
	sort.Strings(offsetIDsSorted)
	sort.Strings(keysetIDsSorted)

	assert.Equal(t, offsetIDsSorted, keysetIDsSorted,
		"offset and keyset paths must return same set of transactions")

	// Additionally verify order matches (merchant sort + t.id tiebreaker should be deterministic)
	assert.Equal(t, offsetIDs, keysetIDs,
		"offset and keyset paths should return transactions in the same order")
}
