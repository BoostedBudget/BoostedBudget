//go:build integration

package database

// Issue #1569 Phase 0 — synthesising a stable identity for accounts that have none.
//
// BackfillUserAccountIDs previously MATCHED an existing user_accounts row or gave up
// (skippedNoMatch). Giving up was survivable while identity-less accounts were eventually
// cascade-deleted. Under history-retained-by-default they are retained forever, so a NULL
// user_account_id becomes a permanent re-link double-count seed (dashboard_service.go:35-73)
// and blocks the accounts.user_account_id NOT NULL constraint the retention model needs.
//
// These tests pin the new behaviour: no match => mint one. Synthesis has to happen here rather
// than in a SQL migration because user_accounts.institution_id/mask are encrypted with
// blind-index uniqueness (000053, 000100) and only the application holds the HMAC key.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/database/... \
//	  -run TestBackfillUserAccountIDs_Synthesis -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillUserAccountIDs_Synthesis_MintsIdentityWhenNoMatchExists(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	suffix := uuid.New().String()[:8]
	plainInstID := "ins-synth-" + suffix
	plainMask := "4242"

	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)

	userID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|synth-"+userID, "synth-"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) }) //nolint:errcheck

	liID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, institution_id)
		VALUES ($1, $2, $3, 'enc-test-token', $4)
	`, liID, userID, "item-synth-"+liID, encInstID)
	require.NoError(t, err)

	accID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, subtype, mask)
		VALUES ($1, $2, $3, $4, 'Orphan Checking', 'depository', 'checking', $5)
	`, accID, liID, userID, "plaid-synth-"+suffix, encMask)
	require.NoError(t, err)

	// Precondition: genuinely no matching user_accounts row for this blind-index triple.
	var pre int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_accounts WHERE user_id = $1`, userID).Scan(&pre))
	require.Zero(t, pre, "the orphan must start with no candidate identity to match against")

	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))

	var uaID *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT user_account_id FROM accounts WHERE id = $1`, accID).Scan(&uaID))
	require.NotNil(t, uaID,
		"an account with no matching identity must have one MINTED, not be skipped: a retained "+
			"account with a NULL user_account_id is a permanent double-count seed")

	// The minted row must be a real, matchable identity — same blind indexes the matcher
	// computes — or the next re-link mints a second one and the chain splits anyway.
	var gotUser, gotInstBlind string
	var gotMaskBlind *string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT user_id, institution_id_blind_idx, mask_blind_idx
		FROM user_accounts WHERE id = $1`, *uaID).Scan(&gotUser, &gotInstBlind, &gotMaskBlind))
	assert.Equal(t, userID, gotUser)
	assert.Equal(t, enc.BlindIndex(plainInstID), gotInstBlind,
		"the minted identity must carry the institution blind index the matcher looks up")
	require.NotNil(t, gotMaskBlind)
	assert.Equal(t, enc.BlindIndex(plainMask), *gotMaskBlind,
		"the minted identity must carry the mask blind index the matcher looks up")
}

// TestBackfillUserAccountIDs_Synthesis_IsIdempotent proves a second run neither mints a duplicate
// nor trips 000100's blind-index unique index.
func TestBackfillUserAccountIDs_Synthesis_IsIdempotent(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	suffix := uuid.New().String()[:8]
	plainInstID := "ins-synth-idem-" + suffix
	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString("7777")
	require.NoError(t, err)

	userID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|synthidem-"+userID, "synthidem-"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) }) //nolint:errcheck

	liID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, institution_id)
		VALUES ($1, $2, $3, 'enc-test-token', $4)
	`, liID, userID, "item-synthidem-"+liID, encInstID)
	require.NoError(t, err)

	accID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, subtype, mask)
		VALUES ($1, $2, $3, $4, 'Idem Checking', 'depository', 'checking', $5)
	`, accID, liID, userID, "plaid-synthidem-"+suffix, encMask)
	require.NoError(t, err)

	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))
	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc), "second run must not error")

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_accounts WHERE user_id = $1`, userID).Scan(&count))
	assert.Equal(t, 1, count, "a second backfill run must not mint a duplicate identity")
}

// TestBackfillUserAccountIDs_Synthesis_TwoAccountsSameRealAccountShareOneIdentity pins that
// synthesis reuses the identity it just minted rather than minting per-account. Two accounts in
// the same re-link chain (same institution + mask) must converge on ONE user_account, otherwise
// synthesis manufactures exactly the split-chain double-count it exists to prevent.
func TestBackfillUserAccountIDs_Synthesis_TwoAccountsSameRealAccountShareOneIdentity(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	suffix := uuid.New().String()[:8]
	plainInstID := "ins-synth-chain-" + suffix
	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString("3131")
	require.NoError(t, err)

	userID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|synthchain-"+userID, "synthchain-"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) }) //nolint:errcheck

	// Two linked items for the same institution — an old (replaced) and a new one.
	var accIDs []string
	for i, itemSuffix := range []string{"old", "new"} {
		liID := uuid.New().String()
		_, err = pool.Exec(ctx, `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, institution_id)
			VALUES ($1, $2, $3, 'enc-test-token', $4)
		`, liID, userID, "item-synthchain-"+itemSuffix+"-"+liID, encInstID)
		require.NoError(t, err)

		accID := uuid.New().String()
		_, err = pool.Exec(ctx, `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, subtype, mask,
			                      status, replaced_at)
			VALUES ($1, $2, $3, $4, 'Chain Checking', 'depository', 'checking', $5, $6, $7)
		`, accID, liID, userID, "plaid-synthchain-"+itemSuffix+"-"+suffix, encMask,
			map[int]string{0: "replaced", 1: "active"}[i],
			// accounts_replaced_at_consistency (000033) requires replaced_at whenever status='replaced'.
			map[int]interface{}{0: time.Now().Add(-24 * time.Hour), 1: nil}[i])
		require.NoError(t, err)
		accIDs = append(accIDs, accID)
	}

	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))

	var first, second *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT user_account_id FROM accounts WHERE id = $1`, accIDs[0]).Scan(&first))
	require.NoError(t, pool.QueryRow(ctx, `SELECT user_account_id FROM accounts WHERE id = $1`, accIDs[1]).Scan(&second))
	require.NotNil(t, first)
	require.NotNil(t, second)
	assert.Equal(t, *first, *second,
		"both members of a re-link chain must converge on ONE synthesised identity — minting per "+
			"account would manufacture the split chain this work exists to eliminate")
}
