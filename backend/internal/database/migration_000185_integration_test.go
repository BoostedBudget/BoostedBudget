//go:build integration

package database

// Integration tests for migration 000185
// (backfill_user_accounts_customization_and_hidden_source) — Issue #1609, Item 3, part 1 of 3.
//
// WHY THE MIGRATION EXISTS
//
// Account customization is WRITTEN to user_accounts (user_account_repository.go) but READ from
// accounts (account_repository.go). Task 3b makes the read CHAIN-AUTHORITATIVE:
//
//	CASE WHEN a.user_account_id IS NOT NULL THEN a_ua.<field> ELSE a.<field> END
//
// deliberately NOT COALESCE — the update API writes NULL to mean "the user cleared this
// override", and COALESCE would resurrect the accounts value the user just deleted. The
// consequence is that once 3b ships, accounts.<field> STOPS BEING READ for a chained row, so any
// legacy value that only ever landed on accounts silently disappears. This migration lifts those
// values up the chain before that happens.
//
// The three things it does, and the property each test pins:
//
//	M1  is_hidden      chain-OR'd up (EXISTS, not a join — one user_account has MANY accounts)
//	M1b nickname/color/icon/emoji/display_order lifted up with a TOTAL tie-break
//	M2  hidden_source  new column, backfilled with a THREE-state classification
//
// ⚠ The M2 classification is the subtle part. The naive rule "has an active account => 'user',
// else 'system'" is WRONG: it labels a historical user-hidden-but-since-disconnected chain
// 'system', and task 3c will un-hide 'system' chains on every re-link — reintroducing the exact
// bug this batch fixes, via our own migration. The required rule is:
//
//	is_hidden AND chain has >=1 'active' account  -> 'user'
//	is_hidden AND no active account               -> 'unknown'   (never auto-un-hidden)
//	NOT is_hidden                                 -> NULL
//
// Each scenario re-applies the REAL 000185 up SQL (read from the migration file, never a
// hand-copied restatement) inside a rolled-back transaction — the established pattern in this
// package (migration_000120/000127_integration_test.go).
//
// pgxmock cannot catch any of this: it asserts a query string and hands back rows the test author
// invented. Every assertion here is about what PostgreSQL actually does, so it lives in an
// integration test against a real database.
//
// Run with:
//
//	DATABASE_URL=postgres://devuser:devpass@localhost:5434/freshcheck?sslmode=disable \
//	  go test ./internal/database/... -run TestMigration000185 -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const migration000185File = "000185_backfill_user_accounts_customization_and_hidden_source.up.sql"

// applyMigration000185 runs the real 000185 up SQL in tx.
func applyMigration000185(t *testing.T, tx pgx.Tx) {
	t.Helper()
	_, err := tx.Exec(context.Background(), readMigrationFile(t, migration000185File))
	require.NoError(t, err, "migration 000185 failed")
}

// ─── seed helpers ────────────────────────────────────────────────────────────────────────────

func seedMig185User(t *testing.T, tx pgx.Tx, suffix string) string {
	t.Helper()
	userID := uuid.New().String()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|mig185-"+suffix+"-"+userID, "mig185-"+suffix+"-"+userID+"@example.com")
	require.NoError(t, err)
	return userID
}

func seedMig185LinkedItem(t *testing.T, tx pgx.Tx, userID, suffix string) string {
	t.Helper()
	liID := uuid.New().String()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, institution_id)
		VALUES ($1, $2, $3, 'enc-test-token', $4)
	`, liID, userID, "item-mig185-"+suffix+"-"+liID, "ins-mig185-"+suffix)
	require.NoError(t, err)
	return liID
}

// seedMig185UserAccount inserts a chain row. isHidden is the PRE-migration state.
func seedMig185UserAccount(t *testing.T, tx pgx.Tx, userID, suffix string, isHidden bool) string {
	t.Helper()
	uaID := uuid.New().String()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO user_accounts (id, user_id, institution_id, mask, is_hidden)
		VALUES ($1, $2, $3, '1111', $4)
	`, uaID, userID, "ins-mig185-"+suffix, isHidden)
	require.NoError(t, err)
	return uaID
}

type mig185Account struct {
	status      string // active | replaced | expired | removed
	isHidden    bool
	nickname    *string
	color       *string
	displayOrd  *int
	updatedAt   time.Time
	uaID        *string // NULL user_account_id when nil
	plaidSuffix string
}

func seedMig185Account(t *testing.T, tx pgx.Tx, userID, liID string, a mig185Account) string {
	t.Helper()
	accID := uuid.New().String()
	var replacedAt, expiredAt, removedAt interface{}
	switch a.status {
	case "replaced":
		replacedAt = a.updatedAt
	case "expired":
		expiredAt = a.updatedAt
	case "removed":
		removedAt = a.updatedAt
	}
	_, err := tx.Exec(context.Background(), `
		INSERT INTO accounts (
			id, linked_item_id, user_id, plaid_account_id, name, type, subtype, mask,
			status, replaced_at, expired_at, removed_at,
			is_hidden, nickname, custom_color, display_order, user_account_id, updated_at
		)
		VALUES ($1, $2, $3, $4, 'Mig185 Checking', 'depository', 'checking', '1111',
		        $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, accID, liID, userID, "plaid-mig185-"+a.plaidSuffix+"-"+accID,
		a.status, replacedAt, expiredAt, removedAt,
		a.isHidden, a.nickname, a.color, a.displayOrd, a.uaID, a.updatedAt)
	require.NoError(t, err)
	return accID
}

type mig185ChainState struct {
	isHidden     bool
	hiddenSource *string
	nickname     *string
	color        *string
	displayOrder *int
}

func readMig185Chain(t *testing.T, tx pgx.Tx, uaID string) mig185ChainState {
	t.Helper()
	var s mig185ChainState
	require.NoError(t, tx.QueryRow(context.Background(), `
		SELECT is_hidden, hidden_source, nickname, custom_color, display_order
		FROM user_accounts WHERE id = $1
	`, uaID).Scan(&s.isHidden, &s.hiddenSource, &s.nickname, &s.color, &s.displayOrder))
	return s
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

// ─── the tests ───────────────────────────────────────────────────────────────────────────────

func TestMigration000185_BackfillAndClassification(t *testing.T) {
	pool := connectBackfillTestDB(t)
	ctx := context.Background()

	begin := func(t *testing.T) pgx.Tx {
		t.Helper()
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback(ctx) })
		return tx
	}

	// ─── M1: a legacy hide anywhere in the chain must lift, and it must lift via aggregation.
	//
	// The chain here has THREE accounts and only the middle one is hidden. An
	// `UPDATE ... FROM accounts` join would pick an arbitrary joined row and lose the hide
	// roughly two times in three; EXISTS/bool_or cannot.
	t.Run("M1_LegacyHideLiftsUpAChainOfMany", func(t *testing.T) {
		tx := begin(t)
		suffix := fmt.Sprintf("m1-%d", time.Now().UnixNano())
		userID := seedMig185User(t, tx, suffix)
		liID := seedMig185LinkedItem(t, tx, userID, suffix)
		uaID := seedMig185UserAccount(t, tx, userID, suffix, false)

		now := time.Now()
		seedMig185Account(t, tx, userID, liID, mig185Account{
			status: "replaced", isHidden: false, updatedAt: now.Add(-72 * time.Hour),
			uaID: &uaID, plaidSuffix: "a",
		})
		seedMig185Account(t, tx, userID, liID, mig185Account{
			status: "expired", isHidden: true, updatedAt: now.Add(-48 * time.Hour),
			uaID: &uaID, plaidSuffix: "b",
		})
		seedMig185Account(t, tx, userID, liID, mig185Account{
			status: "active", isHidden: false, updatedAt: now,
			uaID: &uaID, plaidSuffix: "c",
		})

		applyMigration000185(t, tx)

		got := readMig185Chain(t, tx, uaID)
		assert.True(t, got.isHidden,
			"a legacy accounts.is_hidden anywhere in the chain must lift to the chain row — "+
				"under 3b's chain-authoritative overlay a FALSE here silently un-hides the account")
		require.NotNil(t, got.hiddenSource)
		assert.Equal(t, "user", *got.hiddenSource,
			"the chain has a live account, so a human is the only thing that could have hidden it")
	})

	// ─── M1: a chain with nothing hidden must stay visible with a NULL source.
	t.Run("M1_UnhiddenChainStaysUnhiddenWithNullSource", func(t *testing.T) {
		tx := begin(t)
		suffix := fmt.Sprintf("m1n-%d", time.Now().UnixNano())
		userID := seedMig185User(t, tx, suffix)
		liID := seedMig185LinkedItem(t, tx, userID, suffix)
		uaID := seedMig185UserAccount(t, tx, userID, suffix, false)
		seedMig185Account(t, tx, userID, liID, mig185Account{
			status: "active", isHidden: false, updatedAt: time.Now(), uaID: &uaID, plaidSuffix: "a",
		})

		applyMigration000185(t, tx)

		got := readMig185Chain(t, tx, uaID)
		assert.False(t, got.isHidden, "nothing hidden anywhere must not manufacture a hide")
		assert.Nil(t, got.hiddenSource, "a visible chain has no hide, so it has no source")
	})

	// ─── M2: THE anti-naive case. A hidden chain with NO active account is 'unknown', never
	// 'system'. Labelling it 'system' would let 3c's re-link un-hide a deliberate user hide.
	t.Run("M2_HiddenChainWithNoActiveAccountIsUnknownNotSystem", func(t *testing.T) {
		tx := begin(t)
		suffix := fmt.Sprintf("m2u-%d", time.Now().UnixNano())
		userID := seedMig185User(t, tx, suffix)
		liID := seedMig185LinkedItem(t, tx, userID, suffix)
		uaID := seedMig185UserAccount(t, tx, userID, suffix, true)
		seedMig185Account(t, tx, userID, liID, mig185Account{
			status: "removed", isHidden: false, updatedAt: time.Now().Add(-time.Hour),
			uaID: &uaID, plaidSuffix: "a",
		})

		applyMigration000185(t, tx)

		got := readMig185Chain(t, tx, uaID)
		assert.True(t, got.isHidden)
		require.NotNil(t, got.hiddenSource)
		assert.Equal(t, "unknown", *got.hiddenSource,
			"a hidden chain with no live account is GENUINELY AMBIGUOUS — the system hides "+
				"orphaned chains and a user may also have hidden this one before it was "+
				"disconnected. 'system' here would let the next re-link silently un-hide a "+
				"deliberate user hide")
	})

	// ─── M2: a hidden chain with a live account is a human's doing.
	t.Run("M2_HiddenChainWithActiveAccountIsUser", func(t *testing.T) {
		tx := begin(t)
		suffix := fmt.Sprintf("m2a-%d", time.Now().UnixNano())
		userID := seedMig185User(t, tx, suffix)
		liID := seedMig185LinkedItem(t, tx, userID, suffix)
		uaID := seedMig185UserAccount(t, tx, userID, suffix, true)
		seedMig185Account(t, tx, userID, liID, mig185Account{
			status: "active", isHidden: false, updatedAt: time.Now(), uaID: &uaID, plaidSuffix: "a",
		})

		applyMigration000185(t, tx)

		got := readMig185Chain(t, tx, uaID)
		require.NotNil(t, got.hiddenSource)
		assert.Equal(t, "user", *got.hiddenSource)
	})

	// ─── M2: a chain with NO accounts at all (an orphan identity) that is hidden is 'unknown'.
	t.Run("M2_HiddenOrphanChainWithNoAccountsAtAllIsUnknown", func(t *testing.T) {
		tx := begin(t)
		suffix := fmt.Sprintf("m2o-%d", time.Now().UnixNano())
		userID := seedMig185User(t, tx, suffix)
		uaID := seedMig185UserAccount(t, tx, userID, suffix, true)

		applyMigration000185(t, tx)

		got := readMig185Chain(t, tx, uaID)
		assert.True(t, got.isHidden)
		require.NotNil(t, got.hiddenSource)
		assert.Equal(t, "unknown", *got.hiddenSource,
			"an identity with no physical accounts cannot prove a human hid it")
	})

	// ─── M1b: the winner among disagreeing chain siblings must be TOTALLY determined —
	// active first, then most recently updated, then id. Here the STALE row is the newest by
	// updated_at, so only the active-first leg can pick the right one.
	t.Run("M1b_CustomizationLiftsWithActiveFirstTieBreak", func(t *testing.T) {
		tx := begin(t)
		suffix := fmt.Sprintf("m1b-%d", time.Now().UnixNano())
		userID := seedMig185User(t, tx, suffix)
		liID := seedMig185LinkedItem(t, tx, userID, suffix)
		uaID := seedMig185UserAccount(t, tx, userID, suffix, false)

		now := time.Now()
		// The NEWEST row is a dead one carrying the OLD nickname.
		seedMig185Account(t, tx, userID, liID, mig185Account{
			status: "removed", isHidden: false, nickname: strPtr("Stale Name"),
			color: strPtr("#111111"), displayOrd: intPtr(9),
			updatedAt: now, uaID: &uaID, plaidSuffix: "dead",
		})
		// The ACTIVE row carries the value the user actually sees today.
		seedMig185Account(t, tx, userID, liID, mig185Account{
			status: "active", isHidden: false, nickname: strPtr("Live Name"),
			color: strPtr("#222222"), displayOrd: intPtr(3),
			updatedAt: now.Add(-24 * time.Hour), uaID: &uaID, plaidSuffix: "live",
		})

		applyMigration000185(t, tx)

		got := readMig185Chain(t, tx, uaID)
		require.NotNil(t, got.nickname)
		assert.Equal(t, "Live Name", *got.nickname,
			"the tie-break must prefer the ACTIVE sibling — otherwise a dead row's stale label "+
				"becomes the name the user sees after 3b")
		require.NotNil(t, got.color)
		assert.Equal(t, "#222222", *got.color)
		require.NotNil(t, got.displayOrder)
		assert.Equal(t, 3, *got.displayOrder)
	})

	// ─── M1b: an existing chain-level value is the source of truth and must NOT be overwritten.
	t.Run("M1b_ExistingChainValueIsNotOverwritten", func(t *testing.T) {
		tx := begin(t)
		suffix := fmt.Sprintf("m1bx-%d", time.Now().UnixNano())
		userID := seedMig185User(t, tx, suffix)
		liID := seedMig185LinkedItem(t, tx, userID, suffix)
		uaID := seedMig185UserAccount(t, tx, userID, suffix, false)
		_, err := tx.Exec(ctx, `UPDATE user_accounts SET nickname = 'Chain Wins' WHERE id = $1`, uaID)
		require.NoError(t, err)

		seedMig185Account(t, tx, userID, liID, mig185Account{
			status: "active", isHidden: false, nickname: strPtr("Legacy Loses"),
			updatedAt: time.Now(), uaID: &uaID, plaidSuffix: "a",
		})

		applyMigration000185(t, tx)

		got := readMig185Chain(t, tx, uaID)
		require.NotNil(t, got.nickname)
		assert.Equal(t, "Chain Wins", *got.nickname,
			"user_accounts is already the source of truth; the backfill only fills HOLES")
	})

	// ─── M1b: an unchained account's values must not leak onto some other chain.
	t.Run("M1b_UnchainedAccountValuesDoNotLeak", func(t *testing.T) {
		tx := begin(t)
		suffix := fmt.Sprintf("m1bl-%d", time.Now().UnixNano())
		userID := seedMig185User(t, tx, suffix)
		liID := seedMig185LinkedItem(t, tx, userID, suffix)
		uaID := seedMig185UserAccount(t, tx, userID, suffix, false)
		seedMig185Account(t, tx, userID, liID, mig185Account{
			status: "active", isHidden: true, nickname: strPtr("Unchained"),
			updatedAt: time.Now(), uaID: nil, plaidSuffix: "orphan",
		})

		applyMigration000185(t, tx)

		got := readMig185Chain(t, tx, uaID)
		assert.Nil(t, got.nickname, "an account with a NULL user_account_id belongs to no chain")
		assert.False(t, got.isHidden, "and its hide must not lift onto an unrelated chain")
	})

	// ─── Idempotency: a second application must change nothing, and in particular must not
	// re-classify a hidden_source a later code path (task 3c) has already written.
	t.Run("IsIdempotentAndDoesNotReclassify", func(t *testing.T) {
		tx := begin(t)
		suffix := fmt.Sprintf("idem-%d", time.Now().UnixNano())
		userID := seedMig185User(t, tx, suffix)
		liID := seedMig185LinkedItem(t, tx, userID, suffix)
		uaID := seedMig185UserAccount(t, tx, userID, suffix, false)
		seedMig185Account(t, tx, userID, liID, mig185Account{
			status: "removed", isHidden: true, nickname: strPtr("Legacy"),
			updatedAt: time.Now(), uaID: &uaID, plaidSuffix: "a",
		})

		applyMigration000185(t, tx)
		first := readMig185Chain(t, tx, uaID)
		require.NotNil(t, first.hiddenSource)
		require.Equal(t, "unknown", *first.hiddenSource)

		// Simulate task 3c having since recorded a system hide on this chain.
		_, err := tx.Exec(ctx, `UPDATE user_accounts SET hidden_source = 'system' WHERE id = $1`, uaID)
		require.NoError(t, err)

		applyMigration000185(t, tx)

		second := readMig185Chain(t, tx, uaID)
		assert.True(t, second.isHidden)
		require.NotNil(t, second.hiddenSource)
		assert.Equal(t, "system", *second.hiddenSource,
			"a re-run must not overwrite a hidden_source the application already owns")
		require.NotNil(t, second.nickname)
		assert.Equal(t, "Legacy", *second.nickname)
	})

	// ─── The CHECK constraint must actually reject a value outside the three-state vocabulary.
	t.Run("HiddenSourceCheckConstraintRejectsUnknownVocabulary", func(t *testing.T) {
		tx := begin(t)
		suffix := fmt.Sprintf("chk-%d", time.Now().UnixNano())
		userID := seedMig185User(t, tx, suffix)
		uaID := seedMig185UserAccount(t, tx, userID, suffix, true)

		applyMigration000185(t, tx)

		_, err := tx.Exec(ctx, `UPDATE user_accounts SET hidden_source = 'plaid' WHERE id = $1`, uaID)
		require.Error(t, err, "hidden_source must be constrained to user/system/unknown/NULL")
	})
}
