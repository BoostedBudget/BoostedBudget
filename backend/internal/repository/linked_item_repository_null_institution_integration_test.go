//go:build integration

package repository

// Integration tests for Issue #1444 ("Grep for the same shape elsewhere"): linked_items.institution_id
// and linked_items.institution_name are nullable VARCHAR(255) columns (migration
// 000006_create_linked_items_table.up.sql:13-14 — no NOT NULL constraint was ever added), but
// domain.LinkedItem.InstitutionID/.InstitutionName are bare (non-pointer) string fields. Scanning a
// row whose institution_id/institution_name are genuinely NULL into those bare fields fails with
// "cannot scan NULL into *string" — the exact same shape as the linked_items.status bug fixed
// elsewhere in this file (see normalizeLinkedItemStatus's doc comment).
//
// pgxmock cannot reproduce this: it returns the Go zero value with no error regardless of what a
// nullable column "would" contain, so this must run against real Postgres. Every production writer
// (account_service.go's convertPlaidAccount for currency_code; LinkAccounts for institution fields)
// happens to never leave these columns NULL, so a NULL row here is a genuinely LATENT risk (a
// pre-migration-000053 row, a manual DB edit, or a future writer regression) — proven here via a raw
// SQL UPDATE that bypasses the repository layer entirely, exactly as
// TestPlaidCleanupService_Integration_NullStatusItem_ActuallyDisconnectedAndMarked did for the
// analogous status bug.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestLinkedItemRepository_NullInstitutionFields -tags integration -v

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

// createNullInstitutionFixture creates a fresh user + linked item (with real institution_id/name
// values, so encryption/blind-index computation is exercised exactly like production), then forces
// institution_id and institution_name to genuine SQL NULL via a raw UPDATE that bypasses the
// repository — reproducing a row no ordinary repository-layer fixture could ever produce (Create()
// only ever writes a Go string, empty or not, never a SQL NULL). Returns the created item (with its
// original in-memory InstitutionID plaintext, useful for GetByUserAndInstitution's blind-index
// lookup, since institution_id_blind_idx is a separate column left untouched by this NULL-out).
func createNullInstitutionFixture(t *testing.T, ctx context.Context, tx pgx.Tx, repo LinkedItemRepository, userID, suffix string) (item *domain.LinkedItem, plainInstitutionID string) {
	t.Helper()

	plainInstitutionID = "ins_test_" + suffix
	item = &domain.LinkedItem{
		UserID:          userID,
		ItemID:          "plaid-item-" + suffix,
		AccessToken:     "access-token-" + suffix,
		InstitutionID:   plainInstitutionID,
		InstitutionName: "Test Bank " + suffix,
		Provider:        "plaid",
	}
	require.NoError(t, repo.Create(ctx, item))

	_, err := tx.Exec(ctx, `UPDATE linked_items SET institution_id = NULL, institution_name = NULL WHERE id = $1`, item.ID)
	require.NoError(t, err)

	return item, plainInstitutionID
}

// createNullInstitutionFixtureUser inserts a bare user row for fixtures in this file, mirroring
// createMarkDisconnectedFixtureUser's pattern but scoped to the caller's own transaction (rolled
// back by the caller, so no explicit cleanup is registered here).
func createNullInstitutionFixtureUser(t *testing.T, ctx context.Context, tx pgx.Tx, profileRepo ProfileRepository, suffix string) string {
	t.Helper()

	user := &domain.User{
		ClerkUserID:        "clerk_nullinst_" + suffix,
		Email:              "nullinst-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))
	return user.ID
}

// newNullInstitutionTestTx begins a transaction, registers a rollback, and returns repos bound to
// it. Every subtest below gets its own transaction so state never leaks between cases and the
// shared dev DB is left with no residue (per the task brief: "prefer pool.Begin + defer tx.Rollback
// so your tests leave no residue").
func newNullInstitutionTestTx(t *testing.T, ctx context.Context) (pgx.Tx, LinkedItemRepository, ProfileRepository) {
	t.Helper()
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	return tx, NewLinkedItemRepository(tx, enc), NewProfileRepository(tx, enc)
}

func TestLinkedItemRepository_NullInstitutionFields_GetByID(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newNullInstitutionTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createNullInstitutionFixtureUser(t, ctx, tx, profileRepo, suffix)
	fixture, _ := createNullInstitutionFixture(t, ctx, tx, repo, userID, suffix)

	got, err := repo.GetByID(ctx, fixture.ID)
	require.NoError(t, err, "a NULL institution_id/institution_name row must not fail the scan")
	assert.Equal(t, "", got.InstitutionID, "NULL institution_id should surface as the empty string")
	assert.Equal(t, "", got.InstitutionName, "NULL institution_name should surface as the empty string")
}

func TestLinkedItemRepository_NullInstitutionFields_GetByUserID(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newNullInstitutionTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createNullInstitutionFixtureUser(t, ctx, tx, profileRepo, suffix)
	fixture, _ := createNullInstitutionFixture(t, ctx, tx, repo, userID, suffix)

	items, err := repo.GetByUserID(ctx, userID)
	require.NoError(t, err, "a NULL institution_id/institution_name row must not fail the scan")
	found := findLinkedItemByID(t, items, fixture.ID)
	assert.Equal(t, "", found.InstitutionID, "NULL institution_id should surface as the empty string")
	assert.Equal(t, "", found.InstitutionName, "NULL institution_name should surface as the empty string")
}

func TestLinkedItemRepository_NullInstitutionFields_GetByItemID(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newNullInstitutionTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createNullInstitutionFixtureUser(t, ctx, tx, profileRepo, suffix)
	fixture, _ := createNullInstitutionFixture(t, ctx, tx, repo, userID, suffix)

	got, err := repo.GetByItemID(ctx, "plaid-item-"+suffix)
	require.NoError(t, err, "a NULL institution_id/institution_name row must not fail the scan")
	assert.Equal(t, fixture.ID, got.ID)
	assert.Equal(t, "", got.InstitutionID, "NULL institution_id should surface as the empty string")
	assert.Equal(t, "", got.InstitutionName, "NULL institution_name should surface as the empty string")
}

func TestLinkedItemRepository_NullInstitutionFields_GetByIDAndUser(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newNullInstitutionTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createNullInstitutionFixtureUser(t, ctx, tx, profileRepo, suffix)
	fixture, _ := createNullInstitutionFixture(t, ctx, tx, repo, userID, suffix)

	got, err := repo.GetByIDAndUser(ctx, fixture.ID, userID)
	require.NoError(t, err, "a NULL institution_id/institution_name row must not fail the scan")
	assert.Equal(t, "", got.InstitutionID, "NULL institution_id should surface as the empty string")
	assert.Equal(t, "", got.InstitutionName, "NULL institution_name should surface as the empty string")
}

func TestLinkedItemRepository_NullInstitutionFields_GetByIDForUpdate(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newNullInstitutionTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createNullInstitutionFixtureUser(t, ctx, tx, profileRepo, suffix)
	fixture, _ := createNullInstitutionFixture(t, ctx, tx, repo, userID, suffix)

	got, err := repo.GetByIDForUpdate(ctx, tx, fixture.ID)
	require.NoError(t, err, "a NULL institution_id/institution_name row must not fail the scan")
	assert.Equal(t, "", got.InstitutionID, "NULL institution_id should surface as the empty string")
	assert.Equal(t, "", got.InstitutionName, "NULL institution_name should surface as the empty string")
}

func TestLinkedItemRepository_NullInstitutionFields_GetByUserAndInstitution(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newNullInstitutionTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createNullInstitutionFixtureUser(t, ctx, tx, profileRepo, suffix)
	// institution_id_blind_idx is left untouched by the NULL-out (only the plaintext columns are
	// nulled), so a lookup by the ORIGINAL plaintext institution ID still matches this row via its
	// blind index — reproducing a NULL institution_id/name on a row this method actually reaches.
	fixture, plainInstitutionID := createNullInstitutionFixture(t, ctx, tx, repo, userID, suffix)

	got, err := repo.GetByUserAndInstitution(ctx, userID, plainInstitutionID, string(domain.LinkedItemStatusActive))
	require.NoError(t, err, "a NULL institution_id/institution_name row must not fail the scan")
	require.NotNil(t, got, "fixture item must be found via its blind index")
	assert.Equal(t, fixture.ID, got.ID)
	assert.Equal(t, "", got.InstitutionID, "NULL institution_id should surface as the empty string")
	assert.Equal(t, "", got.InstitutionName, "NULL institution_name should surface as the empty string")
}

func TestLinkedItemRepository_NullInstitutionFields_GetItemsForCleanup(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newNullInstitutionTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createNullInstitutionFixtureUser(t, ctx, tx, profileRepo, suffix)
	fixture, _ := createNullInstitutionFixture(t, ctx, tx, repo, userID, suffix)
	// No accounts exist for this item, so the "zero active accounts" cleanup-candidate branch
	// always matches regardless of errorDays/staleDays — no extra state needed.

	items, err := repo.GetItemsForCleanup(ctx, 30, 30)
	require.NoError(t, err, "a NULL institution_id/institution_name row must not fail the scan")
	found := findLinkedItemByID(t, items, fixture.ID)
	assert.Equal(t, "", found.InstitutionID, "NULL institution_id should surface as the empty string")
	assert.Equal(t, "", found.InstitutionName, "NULL institution_name should surface as the empty string")
}

func TestLinkedItemRepository_NullInstitutionFields_GetItemsPendingCleanup(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newNullInstitutionTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createNullInstitutionFixtureUser(t, ctx, tx, profileRepo, suffix)
	fixture, _ := createNullInstitutionFixture(t, ctx, tx, repo, userID, suffix)

	_, err := tx.Exec(ctx, `UPDATE linked_items SET cleanup_scheduled_at = NOW() - INTERVAL '1 day' WHERE id = $1`, fixture.ID)
	require.NoError(t, err)

	items, err := repo.GetItemsPendingCleanup(ctx)
	require.NoError(t, err, "a NULL institution_id/institution_name row must not fail the scan")
	found := findLinkedItemByID(t, items, fixture.ID)
	assert.Equal(t, "", found.InstitutionID, "NULL institution_id should surface as the empty string")
	assert.Equal(t, "", found.InstitutionName, "NULL institution_name should surface as the empty string")
}

func TestLinkedItemRepository_NullInstitutionFields_GetActiveItemsByUserID(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newNullInstitutionTestTx(t, ctx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createNullInstitutionFixtureUser(t, ctx, tx, profileRepo, suffix)
	fixture, _ := createNullInstitutionFixture(t, ctx, tx, repo, userID, suffix)

	items, err := repo.GetActiveItemsByUserID(ctx, userID)
	require.NoError(t, err, "a NULL institution_id/institution_name row must not fail the scan")
	found := findLinkedItemByID(t, items, fixture.ID)
	assert.Equal(t, "", found.InstitutionID, "NULL institution_id should surface as the empty string")
	assert.Equal(t, "", found.InstitutionName, "NULL institution_name should surface as the empty string")
}

// findLinkedItemByID locates want within got by ID, failing the test if absent.
func findLinkedItemByID(t *testing.T, got []domain.LinkedItem, id string) domain.LinkedItem {
	t.Helper()
	for _, it := range got {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("fixture item %s not found in result set", id)
	return domain.LinkedItem{}
}
