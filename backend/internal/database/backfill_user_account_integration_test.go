//go:build integration

package database

// Integration tests for BackfillUserAccountIDs (Issue #1306 P2).
//
// Verifies that accounts.user_account_id is repaired for rows whose value was
// silently dropped by the pre-#1306-P1 CreateBatch INSERT bug, and that the cascade
// repairs downstream transactions/investment_transactions/holdings.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestBackfillUserAccountIDs -tags integration -v

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillUserAccountIDs_Integration(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	// ── Seed encrypted field values and blind indexes ─────────────────────────
	// Use the same encryptor as BackfillUserAccountIDs so decrypt→BlindIndex matches.
	suffix := uuid.New().String()[:8]
	plainInstID := "ins-p2-" + suffix
	plainMask := "5678"

	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)
	instBlind := enc.BlindIndex(plainInstID)
	maskBlind := enc.BlindIndex(plainMask)

	// ── Seed user ─────────────────────────────────────────────────────────────
	userID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|p2-"+userID, "p2-"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) })

	// ── Seed user_account with computed blind indexes ─────────────────────────
	// The backfill will look this row up by (user_id, institution_id_blind_idx, mask_blind_idx).
	uaID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, uaID, userID, encInstID, encMask, instBlind, maskBlind)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Exec(ctx, "DELETE FROM user_accounts WHERE id = $1", uaID) })

	// ── Seed linked_item with encrypted institution_id ────────────────────────
	liID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, institution_id)
		VALUES ($1, $2, $3, 'enc-test-token', $4)
	`, liID, userID, "item-p2-"+liID, encInstID)
	require.NoError(t, err)

	// ── Seed account with NULL user_account_id and encrypted mask ─────────────
	// This simulates an account created during the bug window.
	accID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, mask)
		VALUES ($1, $2, $3, $4, 'Test Account', 'depository', $5)
	`, accID, liID, userID, "plaid-p2-"+suffix, encMask)
	require.NoError(t, err)

	// ── Seed transaction with NULL user_account_id ────────────────────────────
	txID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name)
		VALUES ($1, $2, $3, $4, 'manual', 99.99, '2024-01-01', 'Test Transaction')
	`, txID, userID, accID, liID)
	require.NoError(t, err)

	// ── Precondition: account must have NULL user_account_id before backfill ──
	var before *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM accounts WHERE id = $1", accID).Scan(&before))
	require.Nil(t, before, "precondition: account must have NULL user_account_id before backfill")

	// ── Run BackfillUserAccountIDs ────────────────────────────────────────────
	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))

	// ── Assert account repaired ───────────────────────────────────────────────
	var gotUAID *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM accounts WHERE id = $1", accID).Scan(&gotUAID))
	require.NotNil(t, gotUAID, "BackfillUserAccountIDs must set accounts.user_account_id")
	assert.Equal(t, uaID, *gotUAID)

	// ── Assert transaction cascade ────────────────────────────────────────────
	var gotTxUAID *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM transactions WHERE id = $1", txID).Scan(&gotTxUAID))
	require.NotNil(t, gotTxUAID, "cascade must set transactions.user_account_id")
	assert.Equal(t, uaID, *gotTxUAID)

	// ── Idempotency: re-run must be a no-op ───────────────────────────────────
	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))
	var afterRerun *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM accounts WHERE id = $1", accID).Scan(&afterRerun))
	require.NotNil(t, afterRerun)
	assert.Equal(t, uaID, *afterRerun, "re-run must not change already-repaired value")
}

// TestBackfillUserAccountIDs_NoMatch_Integration verifies that accounts with no matching
// user_account row are left NULL without aborting the backfill.
func TestBackfillUserAccountIDs_NoMatch_Integration(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	suffix := uuid.New().String()[:8]
	plainInstID := "ins-p2-nomatch-" + suffix

	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	// Deliberately do NOT seed a user_account for this institution.

	userID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|p2-nm-"+userID, "p2-nm-"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) })

	liID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, institution_id)
		VALUES ($1, $2, $3, 'enc-test-token', $4)
	`, liID, userID, "item-p2-nm-"+liID, encInstID)
	require.NoError(t, err)

	accID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, $4, 'Orphan Account', 'depository')
	`, accID, liID, userID, "plaid-p2-nm-"+suffix)
	require.NoError(t, err)

	// Backfill must not abort even though this account has no matching user_account.
	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))

	var got *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM accounts WHERE id = $1", accID).Scan(&got))
	assert.Nil(t, got, "account with no matching user_account must remain NULL (self-heals on next re-link)")
}
