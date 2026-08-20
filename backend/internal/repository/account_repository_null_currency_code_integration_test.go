//go:build integration

package repository

// Integration tests for Issue #1444 ("Grep for the same shape elsewhere"): accounts.currency_code
// is a nullable VARCHAR(3) column (migration 000007_create_accounts_table.up.sql:22 — DEFAULT
// 'USD' but no NOT NULL constraint), and (for GetAccountsWithInstitution specifically)
// linked_items.institution_name is likewise nullable (migration 000006:14). Both are scanned into
// bare (non-pointer) domain.Account string fields. This is the same shape as the
// linked_items.status/institution_id/institution_name bugs already fixed elsewhere in this
// package, and the same shape Phase 2 (see account_repository_null_linked_item_join_integration_
// test.go) already proved for GetAccountsWithInstitutionFiltered's institution_name/status —
// currency_code was left unfixed there and in every other currency_code-scanning query in this
// file.
//
// Every production writer (account_service.go's convertPlaidAccount defaults CurrencyCode to
// "USD" and only overwrites it with Plaid's non-nil ISO code) happens to never leave currency_code
// NULL, so this is a genuinely LATENT risk, not a currently-firing bug — reproduced here via a raw
// SQL UPDATE that bypasses the repository layer, exactly as the sibling tests in this package do.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestAccountRepository_NullCurrencyCode -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// createAccountNullCurrencyFixtureAccount inserts acct through CreateBatch — the only
// production insert path (AccountRepository.Create was removed in #1565) — and copies the
// generated ID back onto acct so callers can reference it in follow-up SQL.
func createAccountNullCurrencyFixtureAccount(t *testing.T, ctx context.Context, repo AccountRepository, acct *domain.Account) {
	t.Helper()
	batch := []domain.Account{*acct}
	require.NoError(t, repo.CreateBatch(ctx, batch))
	acct.ID = batch[0].ID
}

// newAccountNullCurrencyTestTx begins a transaction, registers a rollback, and returns an
// AccountRepository bound to it — no residue is left in the shared dev DB.
func newAccountNullCurrencyTestTx(t *testing.T, ctx context.Context) (pgx.Tx, AccountRepository) {
	t.Helper()
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	return tx, NewAccountRepository(tx, enc)
}

// createAccountNullCurrencyFixtureUser inserts a bare user row via raw SQL (mirroring the
// lightweight style already used by account_repository_null_linked_item_join_integration_test.go).
func createAccountNullCurrencyFixtureUser(t *testing.T, ctx context.Context, tx pgx.Tx, suffix string) string {
	t.Helper()
	var userID string
	err := tx.QueryRow(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES (gen_random_uuid(), $1, $2, 'premium', 'active', 'iad')
		RETURNING id::text
	`, "test|1444-acct-"+suffix, "1444-acct-"+suffix+"@example.com").Scan(&userID)
	require.NoError(t, err)
	return userID
}

// createAccountNullCurrencyFixtureLinkedItem inserts a linked_items row with a real (non-NULL)
// institution_name, so the GetAccountsWithInstitution test can independently null it out afterward.
func createAccountNullCurrencyFixtureLinkedItem(t *testing.T, ctx context.Context, tx pgx.Tx, userID, suffix string) string {
	t.Helper()
	var linkedItemID string
	err := tx.QueryRow(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, institution_name)
		VALUES (gen_random_uuid(), $1, $2, 'enc-test-token', 'Test Bank')
		RETURNING id::text
	`, userID, "1444-acct-item-"+suffix).Scan(&linkedItemID)
	require.NoError(t, err)
	return linkedItemID
}

// createAccountNullCurrencyFixtureUserAccount inserts a minimal user_accounts row (required by
// the user_account_id-keyed methods).
func createAccountNullCurrencyFixtureUserAccount(t *testing.T, ctx context.Context, tx pgx.Tx, userID, suffix string) string {
	t.Helper()
	var userAccountID string
	err := tx.QueryRow(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, institution_id_blind_idx)
		VALUES (gen_random_uuid(), $1, 'enc_inst', $2)
		RETURNING id::text
	`, userID, "blind_inst_"+suffix).Scan(&userAccountID)
	require.NoError(t, err)
	return userAccountID
}

func TestAccountRepository_NullCurrencyCode_GetByLinkedItemID(t *testing.T) {
	ctx := context.Background()
	tx, repo := newAccountNullCurrencyTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createAccountNullCurrencyFixtureUser(t, ctx, tx, suffix)
	linkedItemID := createAccountNullCurrencyFixtureLinkedItem(t, ctx, tx, userID, suffix)

	acct := &domain.Account{
		LinkedItemID:   linkedItemID,
		UserID:         userID,
		PlaidAccountID: "1444-acct-plaid-" + suffix,
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	createAccountNullCurrencyFixtureAccount(t, ctx, repo, acct)
	_, err := tx.Exec(ctx, `UPDATE accounts SET currency_code = NULL WHERE id = $1`, acct.ID)
	require.NoError(t, err)

	accounts, err := repo.GetByLinkedItemID(ctx, linkedItemID)
	require.NoError(t, err, "a NULL currency_code row must not fail the scan")
	require.Len(t, accounts, 1)
	assert.Equal(t, "USD", accounts[0].CurrencyCode, "NULL currency_code should surface as the 'USD' default, matching the column's schema default")
}

func TestAccountRepository_NullCurrencyCode_GetByUserID(t *testing.T) {
	ctx := context.Background()
	tx, repo := newAccountNullCurrencyTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createAccountNullCurrencyFixtureUser(t, ctx, tx, suffix)
	linkedItemID := createAccountNullCurrencyFixtureLinkedItem(t, ctx, tx, userID, suffix)

	acct := &domain.Account{
		LinkedItemID:   linkedItemID,
		UserID:         userID,
		PlaidAccountID: "1444-acct-plaid-" + suffix,
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	createAccountNullCurrencyFixtureAccount(t, ctx, repo, acct)
	_, err := tx.Exec(ctx, `UPDATE accounts SET currency_code = NULL WHERE id = $1`, acct.ID)
	require.NoError(t, err)

	accounts, err := repo.GetByUserID(ctx, userID)
	require.NoError(t, err, "a NULL currency_code row must not fail the scan")
	require.Len(t, accounts, 1)
	assert.Equal(t, "USD", accounts[0].CurrencyCode, "NULL currency_code should surface as the 'USD' default")
}

func TestAccountRepository_NullCurrencyCodeAndInstitutionName_GetAccountsWithInstitution(t *testing.T) {
	ctx := context.Background()
	tx, repo := newAccountNullCurrencyTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createAccountNullCurrencyFixtureUser(t, ctx, tx, suffix)
	linkedItemID := createAccountNullCurrencyFixtureLinkedItem(t, ctx, tx, userID, suffix)

	acct := &domain.Account{
		LinkedItemID:   linkedItemID,
		UserID:         userID,
		PlaidAccountID: "1444-acct-plaid-" + suffix,
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	createAccountNullCurrencyFixtureAccount(t, ctx, repo, acct)
	_, err := tx.Exec(ctx, `UPDATE accounts SET currency_code = NULL WHERE id = $1`, acct.ID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE linked_items SET institution_name = NULL WHERE id = $1`, linkedItemID)
	require.NoError(t, err)

	accounts, err := repo.GetAccountsWithInstitution(ctx, userID)
	require.NoError(t, err, "a NULL currency_code/institution_name row must not fail the scan")
	require.Len(t, accounts, 1)
	assert.Equal(t, "USD", accounts[0].CurrencyCode, "NULL currency_code should surface as the 'USD' default")
	assert.Equal(t, "", accounts[0].InstitutionName, "NULL institution_name should surface as the empty string")
}

func TestAccountRepository_NullCurrencyCode_GetByIDAndUser(t *testing.T) {
	ctx := context.Background()
	tx, repo := newAccountNullCurrencyTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createAccountNullCurrencyFixtureUser(t, ctx, tx, suffix)
	linkedItemID := createAccountNullCurrencyFixtureLinkedItem(t, ctx, tx, userID, suffix)

	acct := &domain.Account{
		LinkedItemID:   linkedItemID,
		UserID:         userID,
		PlaidAccountID: "1444-acct-plaid-" + suffix,
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	createAccountNullCurrencyFixtureAccount(t, ctx, repo, acct)
	_, err := tx.Exec(ctx, `UPDATE accounts SET currency_code = NULL WHERE id = $1`, acct.ID)
	require.NoError(t, err)

	got, err := repo.GetByIDAndUser(ctx, acct.ID, userID)
	require.NoError(t, err, "a NULL currency_code row must not fail the scan")
	assert.Equal(t, "USD", got.CurrencyCode, "NULL currency_code should surface as the 'USD' default")
}

func TestAccountRepository_NullCurrencyCode_GetByUserAccountIDAndUser(t *testing.T) {
	ctx := context.Background()
	tx, repo := newAccountNullCurrencyTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createAccountNullCurrencyFixtureUser(t, ctx, tx, suffix)
	linkedItemID := createAccountNullCurrencyFixtureLinkedItem(t, ctx, tx, userID, suffix)
	userAccountID := createAccountNullCurrencyFixtureUserAccount(t, ctx, tx, userID, suffix)

	acct := &domain.Account{
		LinkedItemID:   linkedItemID,
		UserID:         userID,
		UserAccountID:  &userAccountID,
		PlaidAccountID: "1444-acct-plaid-" + suffix,
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	createAccountNullCurrencyFixtureAccount(t, ctx, repo, acct)
	_, err := tx.Exec(ctx, `UPDATE accounts SET currency_code = NULL WHERE id = $1`, acct.ID)
	require.NoError(t, err)

	got, err := repo.GetByUserAccountIDAndUser(ctx, userAccountID, userID)
	require.NoError(t, err, "a NULL currency_code row must not fail the scan")
	assert.Equal(t, "USD", got.CurrencyCode, "NULL currency_code should surface as the 'USD' default")
}

func TestAccountRepository_NullCurrencyCode_GetAccountsWithInstitutionFiltered(t *testing.T) {
	ctx := context.Background()
	tx, repo := newAccountNullCurrencyTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createAccountNullCurrencyFixtureUser(t, ctx, tx, suffix)
	linkedItemID := createAccountNullCurrencyFixtureLinkedItem(t, ctx, tx, userID, suffix)

	acct := &domain.Account{
		LinkedItemID:   linkedItemID,
		UserID:         userID,
		PlaidAccountID: "1444-acct-plaid-" + suffix,
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	createAccountNullCurrencyFixtureAccount(t, ctx, repo, acct)
	_, err := tx.Exec(ctx, `UPDATE accounts SET currency_code = NULL WHERE id = $1`, acct.ID)
	require.NoError(t, err)

	accounts, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID, AccountFilterOptions{})
	require.NoError(t, err, "a NULL currency_code row must not fail the scan")
	require.Len(t, accounts, 1)
	assert.Equal(t, "USD", accounts[0].CurrencyCode, "NULL currency_code should surface as the 'USD' default")
}

func TestAccountRepository_NullCurrencyCode_GetByUserAccountAndStatus(t *testing.T) {
	ctx := context.Background()
	tx, repo := newAccountNullCurrencyTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createAccountNullCurrencyFixtureUser(t, ctx, tx, suffix)
	linkedItemID := createAccountNullCurrencyFixtureLinkedItem(t, ctx, tx, userID, suffix)
	userAccountID := createAccountNullCurrencyFixtureUserAccount(t, ctx, tx, userID, suffix)

	acct := &domain.Account{
		LinkedItemID:   linkedItemID,
		UserID:         userID,
		UserAccountID:  &userAccountID,
		PlaidAccountID: "1444-acct-plaid-" + suffix,
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	createAccountNullCurrencyFixtureAccount(t, ctx, repo, acct)
	_, err := tx.Exec(ctx, `UPDATE accounts SET currency_code = NULL WHERE id = $1`, acct.ID)
	require.NoError(t, err)

	accounts, err := repo.GetByUserAccountAndStatus(ctx, userAccountID, string(domain.AccountStatusActive))
	require.NoError(t, err, "a NULL currency_code row must not fail the scan")
	require.Len(t, accounts, 1)
	assert.Equal(t, "USD", accounts[0].CurrencyCode, "NULL currency_code should surface as the 'USD' default")
}

func TestAccountRepository_NullCurrencyCode_GetByUserAccountID(t *testing.T) {
	ctx := context.Background()
	tx, repo := newAccountNullCurrencyTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createAccountNullCurrencyFixtureUser(t, ctx, tx, suffix)
	linkedItemID := createAccountNullCurrencyFixtureLinkedItem(t, ctx, tx, userID, suffix)
	userAccountID := createAccountNullCurrencyFixtureUserAccount(t, ctx, tx, userID, suffix)

	acct := &domain.Account{
		LinkedItemID:   linkedItemID,
		UserID:         userID,
		UserAccountID:  &userAccountID,
		PlaidAccountID: "1444-acct-plaid-" + suffix,
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	createAccountNullCurrencyFixtureAccount(t, ctx, repo, acct)
	_, err := tx.Exec(ctx, `UPDATE accounts SET currency_code = NULL WHERE id = $1`, acct.ID)
	require.NoError(t, err)

	accounts, err := repo.GetByUserAccountID(ctx, userAccountID)
	require.NoError(t, err, "a NULL currency_code row must not fail the scan")
	require.Len(t, accounts, 1)
	assert.Equal(t, "USD", accounts[0].CurrencyCode, "NULL currency_code should surface as the 'USD' default")
}
