//go:build integration

package repository

// TestTransactionRepository_CreateBatch_ConflictPreservesUserFields_Integration
// validates what pgxmock unit tests cannot: that the actual PostgreSQL upsert
// (a) returns xmax=0 (inserted=true) for fresh rows,
// (b) returns xmax!=0 (inserted=false) for conflict-updated rows, and
// (c) genuinely PRESERVES user enrichment fields (category_override_kind,
//     pfc_primary_override, user_notes, category_source) in the DB row when
//     ON CONFLICT DO UPDATE fires.
//
// This is the real-Postgres regression test for Issue #1213.
//
// Run with:
//   DATABASE_URL=postgres://... go test ./internal/repository/... \
//     -run TestTransactionRepository_CreateBatch_ConflictPreservesUserFields_Integration \
//     -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransactionRepository_CreateBatch_ConflictPreservesUserFields_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	// Wrap all seed data and the assertions in a transaction so it is rolled
	// back automatically — no test-data pollution in the integration DB.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	// ── seed prerequisites ────────────────────────────────────────────────────
	userID := uuid.New().String()
	linkedItemID := uuid.New().String()
	accountID := uuid.New().String()

	require.NoError(t, tx.Exec(ctx, `
		INSERT INTO users (id, auth0_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'us-east-1')
	`, userID, "test|"+userID, "integration-1213-"+userID+"@example.com").Close())

	require.NoError(t, tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "test-item-"+linkedItemID).Close())

	require.NoError(t, tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-acc-conflict-test', 'Test Checking', 'depository')
	`, accountID, linkedItemID, userID).Close())

	repo := NewTransactionRepository(tx, enc)

	// ── Step 1: fresh insert with user enrichment ─────────────────────────────
	// Use a unique external ID for this test run so there are no cross-test conflicts.
	externalID := "plaid-conflict-inttest-" + uuid.New().String()
	overrideKind := "pfc"
	overridePrimary := "FOOD_AND_DRINK"
	userNotes := "original coffee note"

	txnID := uuid.New().String()
	txn1 := domain.Transaction{
		ID:                    txnID,
		UserID:                userID,
		AccountID:             accountID,
		LinkedItemID:          linkedItemID,
		Source:                domain.SourcePlaid,
		ExternalTransactionID: &externalID,
		Amount:                -5.00,
		Date:                  time.Now().UTC().Truncate(24 * time.Hour),
		Name:                  "Starbucks",
		// User enrichment that must survive a conflict upsert:
		CategoryOverrideKind: &overrideKind,
		PFCPrimaryOverride:   &overridePrimary,
		CategorySourceField:  domain.CategorySourceRule,
		UserNotes:            &userNotes,
	}

	insertedIDs, err := repo.CreateBatch(ctx, []domain.Transaction{txn1})
	require.NoError(t, err, "initial insert must succeed")
	assert.True(t, insertedIDs[txnID],
		"fresh insert: xmax=0 → insertedIDs[id] must be true")

	// ── Step 2: conflict upsert — same arbiter key, different incoming fields ──
	// Simulates a re-sync of the same Plaid transaction. The incoming payload
	// has a different amount (legitimate Plaid update) and different rule-applied
	// category (should be ignored because ON CONFLICT DO UPDATE preserves them).
	externalIDCopy := externalID // same ExternalTransactionID → conflict fires
	differentKind := "custom"
	differentPrimary := "TRANSPORTATION"
	differentNotes := "DIFFERENT NOTES — should NOT appear in DB"

	txn2 := domain.Transaction{
		ID:                    uuid.New().String(), // different ID — arbiter key drives the conflict
		UserID:                userID,
		AccountID:             accountID,
		LinkedItemID:          linkedItemID,
		Source:                domain.SourcePlaid,
		ExternalTransactionID: &externalIDCopy,
		Amount:                -6.00, // amount is in the SET clause → updated (expected)
		Date:                  time.Now().UTC().Truncate(24 * time.Hour),
		Name:                  "Starbucks",
		// Incoming category/notes that must NOT overwrite the existing row's enrichment:
		CategoryOverrideKind: &differentKind,
		PFCPrimaryOverride:   &differentPrimary,
		CategorySourceField:  domain.CategorySourceRule,
		UserNotes:            &differentNotes,
	}

	insertedIDs2, err := repo.CreateBatch(ctx, []domain.Transaction{txn2})
	require.NoError(t, err, "conflict upsert must succeed without error")

	// The map only contains IDs where inserted=true (xmax=0).
	// A conflict-updated row has xmax!=0 so it must NOT appear in insertedIDs2.
	assert.Empty(t, insertedIDs2,
		"conflict row: xmax!=0 → insertedIDs must be empty (no fresh inserts)")
	assert.False(t, insertedIDs2[txnID],
		"conflict row: existing row ID must NOT be in insertedIDs")

	// ── Step 3: query DB and verify enrichment fields were preserved ───────────
	var storedKind, storedPrimary, storedNotes *string
	var storedSource string
	var storedAmount float64
	err = tx.QueryRow(ctx, `
		SELECT category_override_kind, pfc_primary_override, user_notes, category_source, amount
		FROM transactions WHERE id = $1
	`, txnID).Scan(&storedKind, &storedPrimary, &storedNotes, &storedSource, &storedAmount)
	require.NoError(t, err, "row must be present after upsert")

	// User enrichment: must still have original values.
	require.NotNil(t, storedKind)
	assert.Equal(t, "pfc", *storedKind,
		"category_override_kind: preserved from original insert, not overwritten by conflict")
	require.NotNil(t, storedPrimary)
	assert.Equal(t, "FOOD_AND_DRINK", *storedPrimary,
		"pfc_primary_override: preserved from original insert")
	require.NotNil(t, storedNotes)
	assert.Equal(t, userNotes, *storedNotes,
		"user_notes: preserved from original insert")
	assert.Equal(t, "rule", storedSource,
		"category_source: preserved from original insert")

	// Plaid fields: amount IS in the SET clause → should be updated.
	assert.InDelta(t, -6.00, storedAmount, 0.001,
		"amount: Plaid field in SET clause → updated by conflict DO UPDATE")
}
