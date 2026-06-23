//go:build integration

// Package services provides a real-Postgres integration test for transaction search.
// This test guards against regressions of the #1307 sanitizer bug, which mutated search
// values in the service layer before they reached the repository. A repo-only test would
// pass even against the pre-fix code; only a service+real-DB path exercises the bug.
//
// Discriminating fixture:
//   - Row A: "Western Union Money Transfer" — matches the search term "Western Union Money"
//   - Row B: "Western Digital Store"        — negative control; must NOT appear in results
//
// Pre-fix behaviour: the sanitizer stripped "UNION " → "Western Money"; ILIKE '%Western Money%'
// matched neither row → 0 results → test fails. Post-fix: search forwarded verbatim → 1 result.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/services/... -tags integration -v \
//	  -run TestTransactionService_SearchPassthrough_Integration
package services

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// searchTestEncryptionService is a minimal EncryptionService for the search integration test.
// name/merchant_name are stored plaintext, so encryption is not exercised on the search columns;
// this service is required only to satisfy NewTransactionRepository's encryptor argument.
type searchTestEncryptionService struct{}

func (s *searchTestEncryptionService) Encrypt(plaintext string) (string, error) {
	return "TENC[" + plaintext + "]", nil
}

func (s *searchTestEncryptionService) Decrypt(ciphertext string) (string, error) {
	return strings.TrimSuffix(strings.TrimPrefix(ciphertext, "TENC["), "]"), nil
}

func newSearchTestFieldEncryptor(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	key := []byte("12345678901234567890123456789012") // 32-byte AES key
	enc, err := appcrypto.NewFieldEncryptor(&searchTestEncryptionService{}, key)
	require.NoError(t, err)
	return enc
}

// TestTransactionService_SearchPassthrough_Integration seeds two banking rows with distinct
// names and calls both service search paths with a term containing "UNION ". It asserts
// exactly the matching row is returned — proving the value was not sanitized before reaching SQL.
func TestTransactionService_SearchPassthrough_Integration(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()
	enc := newSearchTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// ── Seed prerequisites: user → linked_item → account ─────────────────────
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "int-search|"+userID, "int-search-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "search-item-"+linkedItemID)
	require.NoError(t, err, "seed linked_item")

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Search Test Checking', 'depository', 'active')
	`, accountID, linkedItemID, userID, "plaid-search-acc-"+accountID)
	require.NoError(t, err, "seed account")

	// ── Seed two banking rows with discriminating names ───────────────────────
	const txDate = "2024-06-15"
	rowAID := uuid.New().String()
	rowBID := uuid.New().String()

	for _, row := range []struct {
		id   string
		name string
	}{
		{rowAID, "Western Union Money Transfer"},
		{rowBID, "Western Digital Store"},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (
				id, user_id, account_id, linked_item_id, source,
				external_transaction_id, external_transaction_id_blind_idx,
				amount, date, name, pfc_primary, pfc_detailed,
				pending, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, 'plaid', $5, $6,
				-25.00, $7::date, $8,
				'FOOD_AND_DRINK', 'FOOD_AND_DRINK_RESTAURANT',
				false, now(), now()
			)
		`, row.id, userID, accountID, linkedItemID,
			"ext-"+row.id, "blind-"+row.id, txDate, row.name)
		require.NoError(t, err, "seed transaction name=%q", row.name)
	}

	// ── Wire service against the same in-flight transaction ───────────────────
	// Passing tx (pgx.Tx) as repository.DB keeps seeded rows visible to the queries.
	txRepo := repository.NewTransactionRepository(tx, enc)
	ccRepo := repository.NewCustomCategoryRepository(tx)
	svc := NewTransactionService(txRepo, ccRepo)

	search := "Western Union Money"

	// ── Assertion 1: offset-pagination path (ListTransactions) ───────────────
	t.Run("ListTransactions returns only matching row", func(t *testing.T) {
		filters := domain.TransactionFilters{
			Search: &search,
		}
		resp, err := svc.ListTransactions(ctx, userID, filters, 50, 0)
		require.NoError(t, err)
		require.Len(t, resp.Transactions, 1,
			"Search=%q must return exactly 1 row (row A); got %d", search, len(resp.Transactions))
		assert.Equal(t, rowAID, resp.Transactions[0].ID,
			"result must be the Western Union Money Transfer row")
	})

	// ── Assertion 2: cursor-pagination path (ListTransactionsByCursor) ────────
	t.Run("ListTransactionsByCursor returns only matching row", func(t *testing.T) {
		filters := domain.TransactionFilters{
			SortBy:  "date",
			SortDir: "desc",
			Search:  &search,
		}
		resp, err := svc.ListTransactionsByCursor(ctx, userID, filters, nil, 50)
		require.NoError(t, err)
		require.Len(t, resp.Transactions, 1,
			"Search=%q must return exactly 1 row (row A); got %d", search, len(resp.Transactions))
		assert.Equal(t, rowAID, resp.Transactions[0].ID,
			"result must be the Western Union Money Transfer row")
	})
}
