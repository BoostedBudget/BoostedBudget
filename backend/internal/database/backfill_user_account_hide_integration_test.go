//go:build integration

package database

// Issue #1609 Item 3 (part 1) — the STARTUP-REPAIR ordering hazard.
//
// Migration 000185 lifts a legacy accounts.is_hidden up onto its user_accounts chain row, so that
// task 3b's chain-authoritative overlay keeps hiding what the user hid. That migration is
// necessary but NOT sufficient, and this is the part that is easy to miss:
//
//	container migrations run BEFORE the server boots (backend/scripts/docker-entrypoint.sh),
//	and the server THEN repairs accounts with a NULL user_account_id (cmd/server/main.go ->
//	BackfillUserAccountIDs). The repair mints/attaches a user_accounts row.
//
// An account whose user_account_id was still NULL when 000185 ran therefore MISSES the migration
// entirely: the chain row it gets afterwards was born visible (user_accounts.is_hidden is
// NOT NULL DEFAULT FALSE), and 3b's overlay then reads that FALSE and un-hides the account.
// Because the repair runs on EVERY boot, fixing it here is durable in a way that re-ordering the
// migration is not.
//
// These tests pin the repair carrying the hide forward, on BOTH of its outcomes — the synthesised
// chain and the matched-existing chain — and pin the hidden_source it stamps. The classification
// is the SAME rule 000185 uses, applied to the chain the account is joining: a live ('active')
// account could only have been hidden by a human ('user'); anything else is genuinely ambiguous
// ('unknown') and must never be auto-un-hidden by task 3c.
//
// pgxmock cannot see any of this — it returns rows the test author invented — so these are
// integration tests against real PostgreSQL.
//
// Run with:
//
//	DATABASE_URL=postgres://devuser:devpass@localhost:5434/freshcheck?sslmode=disable \
//	  go test ./internal/database/... -run TestBackfillUserAccountIDs_Hide -tags integration -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedHideFixture creates a user + linked_item and returns (userID, linkedItemID, plainInstID).
func seedHideFixture(
	t *testing.T, pool *pgxpool.Pool, enc *appcrypto.FieldEncryptor, tag string,
) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.New().String()[:8]
	plainInstID := "ins-" + tag + "-" + suffix

	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)

	userID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+tag+"-"+userID, tag+"-"+userID+"@example.com")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID) })

	liID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, institution_id)
		VALUES ($1, $2, $3, 'enc-test-token', $4)
	`, liID, userID, "item-"+tag+"-"+liID, encInstID)
	require.NoError(t, err)

	return userID, liID, plainInstID
}

// seedHiddenAccount inserts an account with a NULL user_account_id — i.e. one that MISSED
// migration 000185 — carrying the given legacy hide and status.
func seedHiddenAccount(
	t *testing.T, pool *pgxpool.Pool, enc *appcrypto.FieldEncryptor,
	userID, liID, plainMask, status string, isHidden bool,
) string {
	t.Helper()
	ctx := context.Background()
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)

	var removedAt, expiredAt, replacedAt interface{}
	switch status {
	case "removed":
		removedAt = time.Now().Add(-time.Hour)
	case "expired":
		expiredAt = time.Now().Add(-time.Hour)
	case "replaced":
		replacedAt = time.Now().Add(-time.Hour)
	}

	accID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (
			id, linked_item_id, user_id, plaid_account_id, name, type, subtype, mask,
			status, removed_at, expired_at, replaced_at, is_hidden
		)
		VALUES ($1, $2, $3, $4, 'Hidden Checking', 'depository', 'checking', $5,
		        $6, $7, $8, $9, $10)
	`, accID, liID, userID, "plaid-hide-"+accID, encMask,
		status, removedAt, expiredAt, replacedAt, isHidden)
	require.NoError(t, err)
	return accID
}

func chainStateForAccount(t *testing.T, pool *pgxpool.Pool, accID string) (uaID string, isHidden bool, hiddenSource *string) {
	t.Helper()
	ctx := context.Background()
	var ua *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT user_account_id FROM accounts WHERE id = $1`, accID).Scan(&ua))
	require.NotNil(t, ua, "the repair must have attached the account to a chain")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT is_hidden, hidden_source FROM user_accounts WHERE id = $1`, *ua,
	).Scan(&isHidden, &hiddenSource))
	return *ua, isHidden, hiddenSource
}

// A hidden, LIVE account that missed the migration must end up on a HIDDEN chain marked 'user'.
func TestBackfillUserAccountIDs_Hide_SynthesisCarriesLegacyHideForward(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)

	userID, liID, _ := seedHideFixture(t, pool, enc, "hidesynth")
	accID := seedHiddenAccount(t, pool, enc, userID, liID, "4242", "active", true)

	require.NoError(t, BackfillUserAccountIDs(context.Background(), pool, enc))

	_, isHidden, source := chainStateForAccount(t, pool, accID)
	assert.True(t, isHidden,
		"the chain minted for a legacy-hidden account must be born HIDDEN: migrations run "+
			"before the server boots, so this account never saw 000185, and a visible chain "+
			"row here is silently un-hidden by 3b's overlay")
	require.NotNil(t, source, "a hidden chain must record WHY it is hidden")
	assert.Equal(t, "user", *source,
		"only a human could have hidden a LIVE account, and 'user' is what stops task 3c "+
			"un-hiding it on the next re-link")
}

// The same, but the account is no longer live: the cause is ambiguous, so 'unknown'.
func TestBackfillUserAccountIDs_Hide_SynthesisOfDeadAccountIsUnknownNotSystem(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)

	userID, liID, _ := seedHideFixture(t, pool, enc, "hidedead")
	accID := seedHiddenAccount(t, pool, enc, userID, liID, "5151", "removed", true)

	require.NoError(t, BackfillUserAccountIDs(context.Background(), pool, enc))

	_, isHidden, source := chainStateForAccount(t, pool, accID)
	assert.True(t, isHidden)
	require.NotNil(t, source)
	assert.Equal(t, "unknown", *source,
		"a hidden account that is no longer live is genuinely ambiguous — 'system' here would "+
			"let the next re-link un-hide what may well have been a deliberate user hide")
}

// An account that was never hidden must not manufacture a hide.
func TestBackfillUserAccountIDs_Hide_VisibleAccountMintsVisibleChain(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)

	userID, liID, _ := seedHideFixture(t, pool, enc, "hidevis")
	accID := seedHiddenAccount(t, pool, enc, userID, liID, "6262", "active", false)

	require.NoError(t, BackfillUserAccountIDs(context.Background(), pool, enc))

	_, isHidden, source := chainStateForAccount(t, pool, accID)
	assert.False(t, isHidden, "the repair must not hide an account nobody hid")
	assert.Nil(t, source, "a visible chain has no hide, so it has no source")
}

// The MATCHED branch: the account joins a chain that already exists and is visible. The same
// chain-OR rule migration 000185 applies has to hold here, or the ordering hazard simply moves
// from the synthesis branch to the match branch.
func TestBackfillUserAccountIDs_Hide_MatchedExistingChainAdoptsTheHide(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	userID, liID, plainInstID := seedHideFixture(t, pool, enc, "hidematch")
	plainMask := "7373"

	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)

	// A pre-existing, VISIBLE chain the repair will match on (identity_type NULL matches any
	// incoming identity — see lookupUserAccountID).
	uaID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts
			(id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx, is_hidden)
		VALUES ($1, $2, $3, $4, $5, $6, FALSE)
	`, uaID, userID, encInstID, encMask, enc.BlindIndex(plainInstID), enc.BlindIndex(plainMask))
	require.NoError(t, err)

	accID := seedHiddenAccount(t, pool, enc, userID, liID, plainMask, "active", true)

	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))

	gotUA, isHidden, source := chainStateForAccount(t, pool, accID)
	assert.Equal(t, uaID, gotUA, "the repair should have matched the existing identity")
	assert.True(t, isHidden,
		"a legacy hide joining an existing chain must lift onto it — this is exactly the "+
			"chain-OR rule migration 000185 applies, and skipping it here reopens the same hole")
	require.NotNil(t, source)
	assert.Equal(t, "user", *source)
}
