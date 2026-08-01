//go:build integration

package repository_test

// Integration test for Issue #1444 ("Grep for the same shape elsewhere"): accounts.currency_code
// is nullable (migration 000007_create_accounts_table.up.sql:22 — DEFAULT 'USD' but no NOT NULL
// constraint) and is scanned into a bare (non-pointer) domain.Account.CurrencyCode field in
// GetForHouseholdScope, same as every other currency_code-scanning query in this package (see
// account_repository_null_currency_code_integration_test.go for the other 8 sites). This test
// lives in the household-scoped external test package (package repository_test) to reuse the
// setupTwoMemberSharedHousehold fixture helpers already established for
// TestAccountRepository_GetForHouseholdScope_* in account_repository_household_integration_test.go.
//
// The fixture setup (setupTwoMemberSharedHousehold and friends) runs directly against the shared
// pool, matching this file's own established convention (each helper registers its own t.Cleanup
// delete). But the NULL-out mutation itself is scoped to its own transaction and rolled back at the
// end (Codex adversarial review: a raw pool.Exec UPDATE with no Begin/Rollback left the mutation
// committed against the shared dev DB for however long it took the fixture's own cleanup hooks to
// cascade-delete the row) — GetForHouseholdScope is called through the SAME transaction so it still
// observes its own uncommitted write (Postgres read-your-own-writes), and the rollback guarantees
// zero residue regardless of cleanup ordering.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestAccountRepository_NullCurrencyCode_GetForHouseholdScope -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccountRepository_NullCurrencyCode_GetForHouseholdScope(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	owner, member, hh, ownerAcct, _ := setupTwoMemberSharedHousehold(t, pool)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx, `UPDATE accounts SET currency_code = NULL WHERE id = $1`, ownerAcct.AccountID)
	require.NoError(t, err)

	repo := repository.NewAccountRepository(tx, newTestFieldEncryptor(t))
	accounts, err := repo.GetForHouseholdScope(ctx, owner, &domain.HouseholdScope{
		HouseholdID:   hh,
		MemberUserIDs: []string{owner, member},
	}, time.Now().UTC())
	require.NoError(t, err, "a NULL currency_code row must not fail the scan")

	found := false
	for _, a := range accounts {
		if a.ID == ownerAcct.AccountID {
			found = true
			assert.Equal(t, "USD", a.CurrencyCode, "NULL currency_code should surface as the 'USD' default")
		}
	}
	assert.True(t, found, "owner's account (with the NULL currency_code) must still be returned")
}
