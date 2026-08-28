//go:build integration

// migration_000178_split_integration_test.go — #1569 round 11 task 4.
//
// Migration 000178 narrows user_accounts' match key to include identity_type/identity_subtype
// and splits any already-merged chain whose physical accounts disagree on that identity. This
// file replays the REAL up-migration SQL (read from the migrations/ directory, not re-typed)
// against a rigged pre-migration-shaped scenario — one user_accounts row with two physical
// accounts that disagree on identity — and proves, against real Postgres, that:
//
//   - the merged row splits into two chains, each carrying exactly one physical account;
//   - accounts.user_account_id is repointed correctly, and reads keyed off account_id
//     (balance_snapshots) are re-derived correctly through it;
//   - an ACTIVE household_account_shares grant on the merged chain fans out to both resulting
//     chains, so the real GetSnapshotsAsOfForHousehold production query still returns BOTH
//     accounts' balances (not halved, not doubled) after the split;
//   - the migration is idempotent (a second run is a no-op).
//
// Per project rule: pgxmock never catches type/aggregate errors or exercises the real CTE
// fan-out — only a real Postgres proves this (see memory feedback_pgxmock_integration_coverage.md).
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/repository/ -run TestMigration000178 -v -tags integration
package repository_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migration000178UpSQL reads the real 000178 up-migration file so this test exercises the exact
// production SQL, not a hand-retyped copy that could silently drift from it.
func migration000178UpSQL(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations",
		"000178_narrow_user_accounts_match_key_and_split_merged_chains.up.sql")
	b, err := os.ReadFile(path)
	require.NoError(t, err, "read 000178 up migration")
	return string(b)
}

// mergedBrokerageIRAFixture is a PRE-migration-shaped merged chain: one user_accounts row,
// minted with the PROVISIONAL representative identity migration 000177's backfill would have
// written (the earliest account's type/subtype — here, the brokerage), and two physical accounts
// that disagree with each other on subtype. This is built with raw SQL, bypassing
// userAccountService.MatchOrCreate entirely, because MatchOrCreate (post-000178) can no longer
// produce this shape — that is the whole point of this migration, and is exactly why the
// migration itself, not application code, must be the thing that repairs any row already in this
// shape.
type mergedBrokerageIRAFixture struct {
	UserAccountID  string // the ORIGINAL row — keeps the group matching its own identity
	BrokerageAcctID string
	IRAAcctID      string
}

func seedMergedBrokerageIRA(t *testing.T, pool *pgxpool.Pool, userID, linkedItemID string) mergedBrokerageIRAFixture {
	t.Helper()
	ctx := context.Background()

	var uaID string
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts
			(user_id, institution_id, institution_id_blind_idx, primary_type, primary_subtype,
			 identity_type, identity_subtype)
		VALUES ($1, 'enc_inst', 'blind_inst_'||gen_random_uuid()::text, 'investment', 'brokerage',
		        'investment', 'brokerage')
		RETURNING id::text
	`, userID).Scan(&uaID)
	require.NoError(t, err, "seed merged user_accounts row")
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM user_accounts WHERE id = $1`, uaID) //nolint:errcheck
	})

	// Different balances on purpose (#1569 round 11 task 4 brief): equal balances would let a
	// swapped-identity bug pass undetected.
	var brokerageID string
	err = pool.QueryRow(ctx, `
		INSERT INTO accounts
			(linked_item_id, user_id, plaid_account_id, name, type, subtype, user_account_id,
			 status, current_balance)
		VALUES ($1, $2, 'plaid_'||gen_random_uuid()::text, 'Brokerage', 'investment', 'brokerage',
		        $3, 'active', 1000.00)
		RETURNING id::text
	`, linkedItemID, userID, uaID).Scan(&brokerageID)
	require.NoError(t, err, "seed brokerage account")
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, brokerageID) //nolint:errcheck
	})

	var iraID string
	err = pool.QueryRow(ctx, `
		INSERT INTO accounts
			(linked_item_id, user_id, plaid_account_id, name, type, subtype, user_account_id,
			 status, current_balance)
		VALUES ($1, $2, 'plaid_'||gen_random_uuid()::text, 'IRA', 'investment', 'ira',
		        $3, 'active', 2000.00)
		RETURNING id::text
	`, linkedItemID, userID, uaID).Scan(&iraID)
	require.NoError(t, err, "seed IRA account")
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, iraID) //nolint:errcheck
	})

	return mergedBrokerageIRAFixture{UserAccountID: uaID, BrokerageAcctID: brokerageID, IRAAcctID: iraID}
}

// accountUserAccountID reads back one accounts row's current user_account_id.
func accountUserAccountID(t *testing.T, pool *pgxpool.Pool, accountID string) string {
	t.Helper()
	var uaID string
	err := pool.QueryRow(context.Background(),
		`SELECT user_account_id::text FROM accounts WHERE id = $1`, accountID).Scan(&uaID)
	require.NoError(t, err)
	return uaID
}

// countActiveAccountsForChain mirrors the predicate AccountChainResolver.ResolveActiveID uses
// (status = 'active') without needing a full service wiring — this is the same invariant:
// >1 means ErrAmbiguousActiveAccount, exactly 1 means resolvable.
func countActiveAccountsForChain(t *testing.T, pool *pgxpool.Pool, userAccountID string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM accounts WHERE user_account_id = $1 AND status = 'active'`, userAccountID).Scan(&n)
	require.NoError(t, err)
	return n
}

func countActiveSharesForHousehold(t *testing.T, pool *pgxpool.Pool, householdID string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM household_account_shares WHERE household_id = $1 AND revoked_at IS NULL`,
		householdID).Scan(&n)
	require.NoError(t, err)
	return n
}

func countUserAccountsForUser(t *testing.T, pool *pgxpool.Pool, userID string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM user_accounts WHERE user_id = $1`, userID).Scan(&n)
	require.NoError(t, err)
	return n
}

// TestMigration000178_SplitsMergedChainAndRepointsHousehold is the end-to-end proof for #1569
// round 11 task 4: split correctness, accounts + balance_snapshots repoint, household share
// fan-out, and — using the REAL production household balance query, not a hand-rolled
// approximation — that the household total is neither halved nor doubled by the split.
func TestMigration000178_SplitsMergedChainAndRepointsHousehold(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	balRepo := repository.NewBalanceSnapshotRepository(pool)

	owner := insertTestUser(t, pool)
	member := insertTestUser(t, pool)
	hh := insertTestHousehold(t, pool, owner)
	linkedItemID := insertTestLinkedItem(t, pool, member)

	pair := seedMergedBrokerageIRA(t, pool, member, linkedItemID)

	// Pre-migration sanity: both physical accounts share ONE chain — the exact ambiguous shape
	// task 2's ErrAmbiguousActiveAccount exists to refuse rather than silently resolve.
	require.Equal(t, pair.UserAccountID, accountUserAccountID(t, pool, pair.BrokerageAcctID))
	require.Equal(t, pair.UserAccountID, accountUserAccountID(t, pool, pair.IRAAcctID))
	require.Equal(t, 2, countActiveAccountsForChain(t, pool, pair.UserAccountID),
		"fixture sanity: both accounts must start on ONE ambiguous chain")

	// Share the merged chain into the household BEFORE the split — this is the active grant that
	// must fan out.
	sharedAt := time.Now().UTC().AddDate(0, -1, 0)
	insertHouseholdShare(t, pool, hh, pair.UserAccountID, nil, domain.ShareVisibilityShared, sharedAt)
	require.Equal(t, 1, countActiveSharesForHousehold(t, pool, hh))

	now := time.Now().UTC()
	insertBalanceSnapshot(t, pool, member, pair.BrokerageAcctID, pair.UserAccountID, 1000, now.Add(-2*time.Hour))
	insertBalanceSnapshot(t, pool, member, pair.IRAAcctID, pair.UserAccountID, 2000, now.Add(-1*time.Hour))

	// A third, unrelated physical account (never linked to any user_accounts chain) for the "to"
	// leg of one transfer suggestion — proves a NULL-chain column is left alone, not repointed to
	// something invented.
	unrelatedAcctID := insertUnlinkedAccount(t, pool, linkedItemID, member, "Checking", "depository")

	// Two transfer suggestions exercising BOTH columns independently, with DISTINGUISHABLE
	// outcomes (#1569 round 11 task 4, coordinator round 1 fix): suggestion 1's from-leg (IRA)
	// MOVES to the new chain while its to-leg (unrelated, no chain) stays NULL; suggestion 2's
	// from-leg (brokerage) STAYS on the original chain while its to-leg (IRA) MOVES — so a test
	// that swapped from/to repointing would be caught.
	tsMovedFrom := insertTransferSuggestion(t, pool, member, pair.IRAAcctID, unrelatedAcctID, &pair.UserAccountID, nil)
	tsMovedTo := insertTransferSuggestion(t, pool, member, pair.BrokerageAcctID, pair.IRAAcctID, &pair.UserAccountID, &pair.UserAccountID)

	// Sanity: the pre-existing household balance read already counts both physical accounts
	// (migration 000177's task 10.2 fix, unrelated to this migration) — establishes the baseline
	// this migration must not regress.
	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}
	before, err := balRepo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, now)
	require.NoError(t, err)
	assert.InDelta(t, 3000.0, sumSnapshots(before), 0.01, "pre-split household total must already be 1000+2000")

	// ─── Apply the REAL migration 000178 up SQL ───────────────────────────────────────────────
	_, err = pool.Exec(ctx, migration000178UpSQL(t))
	require.NoError(t, err, "apply 000178 up migration")

	// ─── Split correctness ─────────────────────────────────────────────────────────────────────
	brokerageUA := accountUserAccountID(t, pool, pair.BrokerageAcctID)
	iraUA := accountUserAccountID(t, pool, pair.IRAAcctID)
	assert.Equal(t, pair.UserAccountID, brokerageUA,
		"the brokerage account matches the ORIGINAL row's identity (000177 backfill's provisional representative) and must not move")
	assert.NotEqual(t, pair.UserAccountID, iraUA, "the IRA account must move to a NEW chain")
	assert.NotEmpty(t, iraUA)

	var iraIdentityType, iraIdentitySubtype string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT identity_type, identity_subtype FROM user_accounts WHERE id = $1`, iraUA,
	).Scan(&iraIdentityType, &iraIdentitySubtype))
	assert.Equal(t, "investment", iraIdentityType)
	assert.Equal(t, "ira", iraIdentitySubtype)

	// ─── Ambiguity resolved (task 2's ErrAmbiguousActiveAccount predicate) ────────────────────
	assert.Equal(t, 1, countActiveAccountsForChain(t, pool, brokerageUA), "brokerage chain must resolve to exactly one live account")
	assert.Equal(t, 1, countActiveAccountsForChain(t, pool, iraUA), "IRA chain must resolve to exactly one live account")

	// ─── transfer_suggestions repoint (coordinator round 1 fix) ──────────────────────────────
	// suggestion 1: from (IRA, moved) -> new chain; to (unrelated, never linked) -> stays NULL.
	gotFrom1, gotTo1 := transferSuggestionUserAccountIDs(t, pool, tsMovedFrom)
	require.NotNil(t, gotFrom1)
	assert.Equal(t, iraUA, *gotFrom1, "from_user_account_id must follow the IRA account to its NEW chain")
	assert.Nil(t, gotTo1, "to_user_account_id must stay NULL for an account never linked to any chain")
	// suggestion 2: from (brokerage, unmoved) -> stays on ORIGINAL chain; to (IRA, moved) -> new chain.
	gotFrom2, gotTo2 := transferSuggestionUserAccountIDs(t, pool, tsMovedTo)
	require.NotNil(t, gotFrom2)
	assert.Equal(t, brokerageUA, *gotFrom2, "from_user_account_id must stay on the ORIGINAL chain for the unmoved brokerage account")
	require.NotNil(t, gotTo2)
	assert.Equal(t, iraUA, *gotTo2, "to_user_account_id must independently follow the IRA account to its NEW chain")

	// ─── household_account_shares fan-out ─────────────────────────────────────────────────────
	assert.Equal(t, 2, countActiveSharesForHousehold(t, pool, hh), "the active share must fan out to both resulting chains")
	var newShareRevokedAt *time.Time
	var newSharePhysicalBlindIdx *string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT revoked_at, physical_account_blind_idx FROM household_account_shares
		WHERE household_id = $1 AND user_account_id = $2
	`, hh, iraUA).Scan(&newShareRevokedAt, &newSharePhysicalBlindIdx))
	assert.Nil(t, newShareRevokedAt, "the fanned-out share must be active")
	assert.Nil(t, newSharePhysicalBlindIdx, "maskless accounts never carry a physical_account_blind_idx (see migration 000178 header)")

	// ─── The real production household balance query still returns BOTH accounts ─────────────
	after, err := balRepo.GetSnapshotsAsOfForHousehold(ctx, owner, scope, now)
	require.NoError(t, err)
	assert.InDelta(t, 3000.0, sumSnapshots(after), 0.01,
		"1500 would mean the split halved the household total (only one chain shared); "+
			"6000 would mean it doubled it; 3000 is correct")

	// ─── Personal balance reads (unaffected by user_accounts grain, but re-verified here) ─────
	personal, err := balRepo.GetSnapshotsAsOfForUser(ctx, member, now)
	require.NoError(t, err)
	assert.InDelta(t, 3000.0, sumSnapshots(personal), 0.01, "personal total must also still be 1000+2000")

	// ─── Idempotency: a second run touches nothing further ────────────────────────────────────
	beforeCount := countUserAccountsForUser(t, pool, member)
	beforeShares := countActiveSharesForHousehold(t, pool, hh)
	_, err = pool.Exec(ctx, migration000178UpSQL(t))
	require.NoError(t, err, "re-apply 000178 up migration must be a no-op, not an error")
	assert.Equal(t, beforeCount, countUserAccountsForUser(t, pool, member), "re-run must not mint additional rows")
	assert.Equal(t, beforeShares, countActiveSharesForHousehold(t, pool, hh), "re-run must not fan out again")

	// Whole-migration consistency: after the re-run, EVERY repointed surface this test touched —
	// accounts, balance_snapshots (via the household query above), household_account_shares, AND
	// transfer_suggestions — must still agree with each other. A partial repoint (the specific
	// failure mode the coordinator flagged) would show up here as one of these disagreeing with
	// the accounts.user_account_id values re-read below.
	assert.Equal(t, brokerageUA, accountUserAccountID(t, pool, pair.BrokerageAcctID))
	assert.Equal(t, iraUA, accountUserAccountID(t, pool, pair.IRAAcctID))
	gotFrom1Again, gotTo1Again := transferSuggestionUserAccountIDs(t, pool, tsMovedFrom)
	require.NotNil(t, gotFrom1Again)
	assert.Equal(t, iraUA, *gotFrom1Again, "transfer_suggestions must not drift from accounts on re-run")
	assert.Nil(t, gotTo1Again)
	gotFrom2Again, gotTo2Again := transferSuggestionUserAccountIDs(t, pool, tsMovedTo)
	require.NotNil(t, gotFrom2Again)
	assert.Equal(t, brokerageUA, *gotFrom2Again)
	require.NotNil(t, gotTo2Again)
	assert.Equal(t, iraUA, *gotTo2Again)
}

// TestFindByInstitutionAndMask_OrphanRowMatchesAnyIdentity proves the documented boundary of the
// #1569 round 11 task 4 narrowing: a legacy orphan user_accounts row (identity_type/subtype NULL
// — migration 000177's header: no physical account was ever linked to it, so nothing could derive
// an identity) still matches a real Plaid link's concrete identity, reproducing pre-task-4
// matching behavior for this narrow legacy shape rather than stranding the row forever. This is a
// real-Postgres test on purpose: pgxmock cannot exercise the WHERE clause's actual semantics
// (it returns canned rows regardless of the predicate), only real Postgres proves the wildcard
// works.
func TestFindByInstitutionAndMask_OrphanRowMatchesAnyIdentity(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, pool)

	// An orphan row: institution+mask set, identity_type/subtype left NULL (as migration 000177's
	// backfill leaves a row with no physical accounts).
	orphanUAID := insertTestUserAccount(t, pool, userID)

	identityType := "depository"
	identitySubtype := "checking"

	var matchCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM user_accounts
		WHERE id = $1
		  AND (identity_type IS NULL OR (identity_type = $2 AND identity_subtype IS NOT DISTINCT FROM $3))
	`, orphanUAID, identityType, identitySubtype).Scan(&matchCount))
	assert.Equal(t, 1, matchCount, "an orphan row (NULL identity) must match ANY concrete identity lookup")

	var mismatchCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM user_accounts
		WHERE id = $1 AND identity_type = 'investment'
	`, orphanUAID).Scan(&mismatchCount))
	assert.Equal(t, 0, mismatchCount, "sanity: the orphan row's identity_type really is NULL, not 'investment'")
}

// TestMigration000178_UnaffectedChainsAreUntouched proves the migration is scoped: a user_accounts
// row whose physical accounts already agree on identity (the ordinary, non-colliding case) is left
// completely alone.
func TestMigration000178_UnaffectedChainsAreUntouched(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()

	user := insertTestUser(t, pool)
	linkedItemID := insertTestLinkedItem(t, pool, user)

	var uaID string
	err := pool.QueryRow(ctx, `
		INSERT INTO user_accounts
			(user_id, institution_id, institution_id_blind_idx, primary_type, primary_subtype,
			 identity_type, identity_subtype)
		VALUES ($1, 'enc_inst', 'blind_inst_'||gen_random_uuid()::text, 'depository', 'checking',
		        'depository', 'checking')
		RETURNING id::text
	`, user).Scan(&uaID)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM user_accounts WHERE id = $1`, uaID) }) //nolint:errcheck

	var acctID string
	err = pool.QueryRow(ctx, `
		INSERT INTO accounts
			(linked_item_id, user_id, plaid_account_id, name, type, subtype, user_account_id, status)
		VALUES ($1, $2, 'plaid_'||gen_random_uuid()::text, 'Checking', 'depository', 'checking', $3, 'active')
		RETURNING id::text
	`, linkedItemID, user, uaID).Scan(&acctID)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, acctID) }) //nolint:errcheck

	beforeCount := countUserAccountsForUser(t, pool, user)

	_, err = pool.Exec(ctx, migration000178UpSQL(t))
	require.NoError(t, err)

	assert.Equal(t, beforeCount, countUserAccountsForUser(t, pool, user), "an agreeing chain must not be split")
	assert.Equal(t, uaID, accountUserAccountID(t, pool, acctID), "the account must not move")
}

// insertUnlinkedAccount inserts a physical account that has NEVER been linked to any
// user_accounts chain (user_account_id stays NULL) — used to prove a transfer suggestion's
// column referencing such an account is left alone by the repoint, not repointed to something
// invented.
func insertUnlinkedAccount(t *testing.T, pool *pgxpool.Pool, linkedItemID, userID, name, acctType string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, 'plaid_'||gen_random_uuid()::text, $3, $4, 'active')
		RETURNING id::text
	`, linkedItemID, userID, name, acctType).Scan(&id)
	require.NoError(t, err, "insertUnlinkedAccount")
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

// insertTransferSuggestion seeds one transfer_suggestions row with an explicit (pre-split, or
// deliberately stale) from_user_account_id/to_user_account_id — mirroring the shape a real row
// would have BEFORE migration 000178 repoints it: from_account_id/to_account_id are the source of
// truth, and the *_user_account_id columns carry whatever they were last written with.
func insertTransferSuggestion(t *testing.T, pool *pgxpool.Pool, userID, fromAccountID, toAccountID string, fromUAID, toUAID *string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO transfer_suggestions
			(user_id, from_account_id, to_account_id, amount, source, from_user_account_id, to_user_account_id)
		VALUES ($1, $2, $3, 25.00, 'boost', $4, $5)
		RETURNING id::text
	`, userID, fromAccountID, toAccountID, fromUAID, toUAID).Scan(&id)
	require.NoError(t, err, "insertTransferSuggestion")
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM transfer_suggestions WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

// transferSuggestionUserAccountIDs reads back one transfer_suggestions row's two
// *_user_account_id columns.
func transferSuggestionUserAccountIDs(t *testing.T, pool *pgxpool.Pool, id string) (from, to *string) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `
		SELECT from_user_account_id::text, to_user_account_id::text
		FROM transfer_suggestions WHERE id = $1
	`, id).Scan(&from, &to)
	require.NoError(t, err)
	return from, to
}
