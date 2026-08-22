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

	// Seed encrypted field values and blind indexes using same encryptor as BackfillUserAccountIDs
	suffix := uuid.New().String()[:8]
	plainInstID := "ins-p2-" + suffix
	plainMask := "5678"

	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)
	instBlind := enc.BlindIndex(plainInstID)
	maskBlind := enc.BlindIndex(plainMask)

	// Seed user
	userID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|p2-"+userID, "p2-"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) }) //nolint:errcheck

	// Seed user_account with blind indexes
	uaID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, uaID, userID, encInstID, encMask, instBlind, maskBlind)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM user_accounts WHERE id = $1", uaID) }) //nolint:errcheck

	// Seed linked_item with encrypted institution_id
	liID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, institution_id)
		VALUES ($1, $2, $3, 'enc-test-token', $4)
	`, liID, userID, "item-p2-"+liID, encInstID)
	require.NoError(t, err)

	// Seed account with NULL user_account_id and encrypted mask
	accID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, mask)
		VALUES ($1, $2, $3, $4, 'Test Account', 'depository', $5)
	`, accID, liID, userID, "plaid-p2-"+suffix, encMask)
	require.NoError(t, err)

	// Seed transaction with NULL user_account_id
	txID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id, source, amount, date, name)
		VALUES ($1, $2, $3, $4, 'manual', 99.99, '2024-01-01', 'Test Transaction')
	`, txID, userID, accID, liID)
	require.NoError(t, err)

	// Seed security (required by investment tables; not user-scoped, needs explicit cleanup)
	secID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name)
		VALUES ($1, $2, 'Test Security')
	`, secID, "plaid-sec-p2-"+suffix)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM securities WHERE id = $1", secID) }) //nolint:errcheck

	// Seed investment_transaction with NULL user_account_id
	invTxID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO investment_transactions
		  (id, user_id, account_id, plaid_investment_transaction_id, type, name, amount, date)
		VALUES ($1, $2, $3, $4, 'buy', 'Test Inv Tx', 100.00, '2024-01-01')
	`, invTxID, userID, accID, "plaid-inv-p2-"+suffix)
	require.NoError(t, err)

	// Seed holding with NULL user_account_id
	holdingID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO holdings (id, user_id, account_id, security_id, quantity)
		VALUES ($1, $2, $3, $4, 10.0)
	`, holdingID, userID, accID, secID)
	require.NoError(t, err)

	// Seed balance_snapshot with NULL user_account_id
	bsID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO balance_snapshots (id, account_id, user_id, snapshot_at, source)
		VALUES ($1, $2, $3, '2000-01-01', 'migration')
	`, bsID, accID, userID)
	require.NoError(t, err)

	// Seed savings_goal with NULL user_account_id
	sgID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO savings_goals (id, user_id, account_id, name, target_amount, progress_basis)
		VALUES ($1, $2, $3, $4, 500.00, 'manual')
	`, sgID, userID, accID, "Goal-p2-"+suffix)
	require.NoError(t, err)

	// Seed transfer_suggestion with NULL from/to user_account_ids
	tsID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO transfer_suggestions (id, user_id, from_account_id, to_account_id, amount, source)
		VALUES ($1, $2, $3, $3, 25.00, 'boost')
	`, tsID, userID, accID)
	require.NoError(t, err)

	// Verify account has NULL user_account_id before backfill
	var before *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM accounts WHERE id = $1", accID).Scan(&before))
	require.Nil(t, before, "account must have NULL user_account_id before backfill")

	// Run BackfillUserAccountIDs
	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))

	// Assert account repaired
	var gotUAID *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM accounts WHERE id = $1", accID).Scan(&gotUAID))
	require.NotNil(t, gotUAID, "BackfillUserAccountIDs must set accounts.user_account_id")
	assert.Equal(t, uaID, *gotUAID)

	// Assert transaction cascade
	var gotTxUAID *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM transactions WHERE id = $1", txID).Scan(&gotTxUAID))
	require.NotNil(t, gotTxUAID, "cascade must set transactions.user_account_id")
	assert.Equal(t, uaID, *gotTxUAID)

	// Assert investment_transactions cascade
	var gotInvTxUAID *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM investment_transactions WHERE id = $1", invTxID).Scan(&gotInvTxUAID))
	require.NotNil(t, gotInvTxUAID, "cascade must set investment_transactions.user_account_id")
	assert.Equal(t, uaID, *gotInvTxUAID)

	// Assert holdings cascade
	var gotHoldingUAID *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM holdings WHERE id = $1", holdingID).Scan(&gotHoldingUAID))
	require.NotNil(t, gotHoldingUAID, "cascade must set holdings.user_account_id")
	assert.Equal(t, uaID, *gotHoldingUAID)

	// Assert balance_snapshots cascade
	var gotBsUAID *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM balance_snapshots WHERE id = $1", bsID).Scan(&gotBsUAID))
	require.NotNil(t, gotBsUAID, "cascade must set balance_snapshots.user_account_id")
	assert.Equal(t, uaID, *gotBsUAID)

	// Assert savings_goals cascade
	var gotSgUAID *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM savings_goals WHERE id = $1", sgID).Scan(&gotSgUAID))
	require.NotNil(t, gotSgUAID, "cascade must set savings_goals.user_account_id")
	assert.Equal(t, uaID, *gotSgUAID)

	// Assert transfer_suggestions cascade (both from and to columns)
	var gotFromUAID, gotToUAID *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT from_user_account_id::text, to_user_account_id::text FROM transfer_suggestions WHERE id = $1", tsID).Scan(&gotFromUAID, &gotToUAID))
	require.NotNil(t, gotFromUAID, "cascade must set transfer_suggestions.from_user_account_id")
	assert.Equal(t, uaID, *gotFromUAID)
	require.NotNil(t, gotToUAID, "cascade must set transfer_suggestions.to_user_account_id")
	assert.Equal(t, uaID, *gotToUAID)

	// Idempotency: re-run must be a no-op
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
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) }) //nolint:errcheck

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

	// ⚠ CONTRACT INVERTED BY #1569. This assertion used to be `assert.Nil` — "must remain NULL
	// (self-heals on next re-link)". That was defensible while an identity-less account was
	// eventually cascade-deleted, so the NULL was temporary by construction. Under
	// history-retained-by-default the row is retained forever, so "self-heals on next re-link"
	// became "stays a permanent re-link double-count seed" (dashboard_service.go:35-73) and blocks
	// the accounts.user_account_id NOT NULL constraint the retention model depends on.
	// The backfill now MINTS an identity instead of skipping.
	var got *string
	require.NoError(t, pool.QueryRow(ctx, "SELECT user_account_id::text FROM accounts WHERE id = $1", accID).Scan(&got))
	require.NotNil(t, got, "an account with no matching user_account must have one minted, not be left NULL")
	assert.NotEmpty(t, *got)
}
