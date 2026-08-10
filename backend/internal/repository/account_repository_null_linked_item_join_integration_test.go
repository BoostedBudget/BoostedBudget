//go:build integration

package repository

// Integration tests for Issue #1444 (Sites A and B): linked_items.status (and, for Site A,
// linked_items.institution_name) are nullable columns scanned via a LEFT/INNER JOIN into bare
// (non-pointer) Go string fields on domain.Account / FingerprintOwnership. pgxmock cannot
// reproduce the real pgx failure mode here — a mocked row's nil cell just yields the Go zero
// value with no error, because pgxmock's fake driver does not run the same NULL-vs-non-pointer-
// destination check the real pgx scan plan does. A genuine NULL, seeded via raw SQL against a
// real database, is required for an honest RED (see the coordinator's clarification on this
// task: "since linked_items.status is confirmed nullable in the live schema, you can seed a
// genuine NULL with raw SQL to get an honest RED").
//
// Since migration 000126 (Issue #1444) added a NOT NULL constraint to linked_items.status, both
// tests below transactionally DROP that constraint first (rolled back with the rest of the
// transaction — the real schema is never permanently altered) so a genuinely NULL status can
// still be constructed against a fully migrated database.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run 'TestAccountRepository_GetAccountsWithInstitutionFiltered_NullLinkedItemJoinColumns_Integration|TestAccountRepository_CheckFingerprintOwnership_NullItemStatus_Integration' -tags integration -v

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestAccountRepository_GetAccountsWithInstitutionFiltered_NullLinkedItemJoinColumns_Integration
// is Site A's RED test: a linked_items row with a genuinely NULL status (and NULL
// institution_name, the column's actual schema default — migrations/000006_create_linked_items_
// table.up.sql declares it VARCHAR(255) with no NOT NULL and no DEFAULT) must not fail the scan
// in GetAccountsWithInstitutionFiltered, and the NULL status must normalize to "active" per the
// established rule (normalizeLinkedItemStatus's doc comment).
func TestAccountRepository_GetAccountsWithInstitutionFiltered_NullLinkedItemJoinColumns_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	_, err = tx.Exec(ctx, "ALTER TABLE linked_items ALTER COLUMN status DROP NOT NULL")
	require.NoError(t, err)

	userID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|1444-sitea-"+userID.String(), "1444-sitea-"+userID.String()+"@example.com")
	require.NoError(t, err)

	// Genuinely NULL status AND NULL institution_name — neither column is set here, and status
	// is explicitly forced to NULL to override the column's 'active' default.
	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
		VALUES ($1, $2, $3, 'enc-test-token', NULL)
	`, linkedItemID, userID, "1444-sitea-item-"+linkedItemID.String())
	require.NoError(t, err)

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	acct := domain.Account{
		ID:             uuid.New().String(),
		LinkedItemID:   linkedItemID.String(),
		UserID:         userID.String(),
		PlaidAccountID: "1444-sitea-acc-" + uuid.New().String(),
		Name:           "Checking",
		Type:           "depository",
		CurrencyCode:   "USD",
	}
	require.NoError(t, repo.Create(ctx, &acct))

	accounts, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID.String(), AccountFilterOptions{})
	require.NoError(t, err, "a NULL l.status/l.institution_name must not fail the scan")
	require.Len(t, accounts, 1)
	assert.Equal(t, "active", accounts[0].LinkedItemStatus, "a NULL linked_item status must be treated as active")
	assert.Equal(t, "", accounts[0].InstitutionName, "a NULL institution_name must surface as the empty string, not fail the scan")
}

// TestAccountRepository_CheckFingerprintOwnership_NullItemStatus_Integration is Site B's RED
// test: CheckFingerprintOwnership backs the Issue #1010 cross-user Plaid-item uniqueness gate
// (account_service.go's LinkAccounts) — a genuinely NULL li.status must not fail the scan, and
// must normalize to domain.LinkedItemStatusActive.
func TestAccountRepository_CheckFingerprintOwnership_NullItemStatus_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck
	_, err = tx.Exec(ctx, "ALTER TABLE linked_items ALTER COLUMN status DROP NOT NULL")
	require.NoError(t, err)

	ownerID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, ownerID, "test|1444-siteb-owner-"+ownerID.String(), "1444-siteb-owner-"+ownerID.String()+"@example.com")
	require.NoError(t, err)

	requesterID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, requesterID, "test|1444-siteb-req-"+requesterID.String(), "1444-siteb-req-"+requesterID.String()+"@example.com")
	require.NoError(t, err)

	linkedItemID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, status)
		VALUES ($1, $2, $3, 'enc-test-token', NULL)
	`, linkedItemID, ownerID, "1444-siteb-item-"+linkedItemID.String())
	require.NoError(t, err)

	fingerprint := "1444-siteb-fingerprint-" + uuid.New().String()
	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	acct := domain.Account{
		ID:                          uuid.New().String(),
		LinkedItemID:                linkedItemID.String(),
		UserID:                      ownerID.String(),
		PlaidAccountID:              "1444-siteb-acc-" + uuid.New().String(),
		Name:                        "Checking",
		Type:                        "depository",
		CurrencyCode:                "USD",
		FallbackFingerprintBlindIdx: &fingerprint,
	}
	require.NoError(t, repo.Create(ctx, &acct))

	ownership, err := repo.CheckFingerprintOwnership(ctx, fingerprint, requesterID.String())
	require.NoError(t, err, "a NULL li.status must not fail the scan")
	require.NotNil(t, ownership)
	assert.Equal(t, ownerID.String(), ownership.UserID)
	assert.Equal(t, domain.LinkedItemStatusActive, ownership.ItemStatus, "a NULL linked_item status must be treated as active")
}
