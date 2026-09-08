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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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

// ─── Fix round 1, I1: the two writes are not atomic, so their ORDER is the safety property ───
//
// BackfillUserAccountIDs runs against a *pgxpool.Pool, so every statement AUTOCOMMITS — there is
// no surrounding transaction, and there cannot cheaply be one (the scan is paged across a run cap).
// That makes the ordering of the repair's two writes load-bearing:
//
//	UPDATE accounts SET user_account_id = ...   <- makes the row invisible to the next boot's
//	                                              page query (WHERE a.user_account_id IS NULL)
//	UPDATE user_accounts SET is_hidden = TRUE   <- carries the hide
//
// Written in that order, a crash, a cancelled context or a pool error in the window between them
// leaves the account attached and the hide LOST PERMANENTLY: nothing will ever select that row
// again, so there is no repair path. Written the other way round the same failure leaves
// user_account_id NULL, so the next boot retries the whole account — and hiding a chain the
// account has not joined yet converges to the same end state, because the hide write is
// idempotent.
//
// A test that only asserts "the hide was carried" passes under BOTH orders. This one asserts the
// order itself, by failing the hide write and observing what survives.

// execFailOnFragment wraps a backfillQuerier and fails any Exec whose SQL contains `fragment`,
// delegating everything else. It is the only way to observe the ordering: the property is about
// what is DURABLE after a mid-repair failure, which needs a real failure at a chosen statement.
type execFailOnFragment struct {
	backfillQuerier
	fragment string
	fired    bool
}

func (e *execFailOnFragment) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	if strings.Contains(sql, e.fragment) {
		e.fired = true
		return pgconn.CommandTag{}, errors.New("injected failure at the hide write")
	}
	return e.backfillQuerier.Exec(ctx, sql, args...)
}

func TestBackfillUserAccountIDs_Hide_HideIsWrittenBeforeTheAccountIsAttached(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	userID, liID, plainInstID := seedHideFixture(t, pool, enc, "hideorder")
	plainMask := "8484"

	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)

	// An existing chain to match, so the repair takes the lookup branch and the only Execs in play
	// are the two whose order is under test.
	uaID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts
			(id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx, is_hidden)
		VALUES ($1, $2, $3, $4, $5, $6, FALSE)
	`, uaID, userID, encInstID, encMask, enc.BlindIndex(plainInstID), enc.BlindIndex(plainMask))
	require.NoError(t, err)

	accID := seedHiddenAccount(t, pool, enc, userID, liID, plainMask, "active", true)

	db := &execFailOnFragment{backfillQuerier: pool, fragment: "SET is_hidden"}
	row := backfillAccountRow{
		id: accID, userID: userID, mask: &encMask, instID: &encInstID,
		acctType: strp("depository"), acctSubtype: strp("checking"),
		isHidden: true, status: "active",
	}
	var repaired, synthesised, skippedNoMatch, skippedDecrypt int
	err = repairBackfillAccountPage(ctx, db, enc, []backfillAccountRow{row},
		&repaired, &synthesised, &skippedNoMatch, &skippedDecrypt)

	require.Error(t, err, "the injected hide-write failure must propagate, not be swallowed")
	require.True(t, db.fired, "the hide write must actually have been attempted")

	var got *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT user_account_id FROM accounts WHERE id = $1`, accID).Scan(&got))
	assert.Nil(t, got,
		"a failure at the hide write must leave user_account_id NULL. The repair autocommits "+
			"each statement, so attaching the account FIRST and then failing would make the row "+
			"invisible to every future run (the page query is WHERE user_account_id IS NULL) and "+
			"lose the hide permanently, with no repair path. The hide must be written first: it "+
			"is idempotent, and hiding a chain the account has not joined yet converges.")
}

// ─── Fix round 1, I2: a 'system' cause must be upgradable to 'user' ──────────────────────────
//
// Task 3c will start writing hidden_source = 'system' for the automatic "the chain lost its last
// active account" hide, and will un-hide ONLY 'system' chains on re-link. So a chain sitting at
// 'system' that a legacy-hidden LIVE account then joins is a real user hide wearing a cause that
// says "safe to un-hide". The repair has to upgrade it.
//
// Only ever an UPGRADE: 'user' is never downgraded, and 'unknown' is left alone (it is already
// never auto-un-hidden, so rewriting it would buy nothing and lose the record that the migration
// genuinely could not tell).

func TestBackfillUserAccountIDs_Hide_UpgradesSystemCauseToUserForALiveAccount(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	userID, liID, plainInstID := seedHideFixture(t, pool, enc, "hideupg")
	plainMask := "9595"

	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)

	// The chain 3c would have written: already hidden, and marked as the SYSTEM's doing.
	uaID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts
			(id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx,
			 is_hidden, hidden_source)
		VALUES ($1, $2, $3, $4, $5, $6, TRUE, 'system')
	`, uaID, userID, encInstID, encMask, enc.BlindIndex(plainInstID), enc.BlindIndex(plainMask))
	require.NoError(t, err)

	accID := seedHiddenAccount(t, pool, enc, userID, liID, plainMask, "active", true)

	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))

	gotUA, isHidden, source := chainStateForAccount(t, pool, accID)
	assert.Equal(t, uaID, gotUA)
	assert.True(t, isHidden)
	require.NotNil(t, source)
	assert.Equal(t, "user", *source,
		"a legacy hide on a LIVE account joining a chain marked 'system' must UPGRADE the cause. "+
			"Leaving it 'system' hands task 3c a real user hide labelled safe-to-un-hide, and the "+
			"next re-link destroys it")
}

func TestBackfillUserAccountIDs_Hide_NeverDowngradesAUserCause(t *testing.T) {
	pool := connectBackfillTestDB(t)
	enc := newBackfillTestEnc(t)
	ctx := context.Background()

	userID, liID, plainInstID := seedHideFixture(t, pool, enc, "hidenodg")
	plainMask := "9696"

	encInstID, err := enc.EncryptString(plainInstID)
	require.NoError(t, err)
	encMask, err := enc.EncryptString(plainMask)
	require.NoError(t, err)

	uaID := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO user_accounts
			(id, user_id, institution_id, mask, institution_id_blind_idx, mask_blind_idx,
			 is_hidden, hidden_source)
		VALUES ($1, $2, $3, $4, $5, $6, TRUE, 'user')
	`, uaID, userID, encInstID, encMask, enc.BlindIndex(plainInstID), enc.BlindIndex(plainMask))
	require.NoError(t, err)

	// A DEAD hidden account joining the chain would classify as 'unknown' on its own.
	accID := seedHiddenAccount(t, pool, enc, userID, liID, plainMask, "removed", true)

	require.NoError(t, BackfillUserAccountIDs(ctx, pool, enc))

	_, isHidden, source := chainStateForAccount(t, pool, accID)
	assert.True(t, isHidden)
	require.NotNil(t, source)
	assert.Equal(t, "user", *source,
		"a weaker cause arriving later must never overwrite a stronger one already recorded")
}

func strp(s string) *string { return &s }
