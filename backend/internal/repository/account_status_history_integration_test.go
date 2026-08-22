//go:build integration

package repository

// Integration tests for the account_status_history table and its supporting trigger
// (record_account_status_change(), migrations 000151/000152). Issue #1569, Task 3.
//
// A trigger cannot be exercised through pgxmock — pgxmock only replays a canned RowsAffected for
// whatever query text it matched, it does not run real Postgres trigger machinery. Every
// assertion here needs the trigger to actually fire against a live schema, so this is entirely
// an integration suite (see memory feedback_pgxmock_integration_coverage.md).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestAccountStatusHistory -tags integration -v -p 1

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ashInterval mirrors one row of account_status_history for assertions.
type ashInterval struct {
	Status     string
	ValidFrom  time.Time
	ValidTo    *time.Time
	Reason     *string
	Provenance string
}

// ashIntervalsFor returns every account_status_history row for accountID, ordered oldest first.
func ashIntervalsFor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID string) []ashInterval {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT status, valid_from, valid_to, reason, provenance
		FROM account_status_history
		WHERE account_id = $1
		ORDER BY valid_from ASC`, accountID)
	require.NoError(t, err)
	defer rows.Close()

	var out []ashInterval
	for rows.Next() {
		var iv ashInterval
		require.NoError(t, rows.Scan(&iv.Status, &iv.ValidFrom, &iv.ValidTo, &iv.Reason, &iv.Provenance))
		out = append(out, iv)
	}
	require.NoError(t, rows.Err())
	return out
}

// ashLiveAt reports whether accountID has an interval of the given status covering instant `at`
// under the half-open [valid_from, valid_to) rule: live starting AT valid_from, no longer live AT
// valid_to. This is exactly the query shape a future "as of date D" read would use.
func ashLiveAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID, status string, at time.Time) bool {
	t.Helper()
	var live bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM account_status_history
			WHERE account_id = $1 AND status = $2
			  AND valid_from <= $3
			  AND (valid_to IS NULL OR valid_to > $3)
		)`, accountID, status, at).Scan(&live))
	return live
}

// insertAshFixtureAccount inserts a plain accounts row (through the live account_status_history
// trigger, since it always fires on INSERT) and returns its id.
func insertAshFixtureAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, linkedItemID, userID, plaidAccountID string) string {
	t.Helper()
	var id string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Test Account', 'depository')
		RETURNING id`,
		linkedItemID, userID, plaidAccountID).Scan(&id))
	return id
}

// TestAccountStatusHistory_Insert_OpensOneActiveInterval is brief assertion 1: inserting an
// account opens exactly one interval, status 'active', valid_to IS NULL.
func TestAccountStatusHistory_Insert_OpensOneActiveInterval(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	accountID := insertAshFixtureAccount(t, ctx, pool, item.ID, userID, "plaid-acct-"+suffix)

	intervals := ashIntervalsFor(t, ctx, pool, accountID)
	require.Len(t, intervals, 1, "inserting an account must open exactly one interval")
	assert.Equal(t, "active", intervals[0].Status)
	assert.Nil(t, intervals[0].ValidTo, "the interval for a brand-new account must be open (current)")
	require.NotNil(t, intervals[0].Reason)
	assert.Equal(t, "created", *intervals[0].Reason)
	assert.Equal(t, "recorded", intervals[0].Provenance)
}

// TestAccountStatusHistory_StatusChange_ClosesOldOpensNewWithNoGapOrOverlap is brief assertion 2:
// a status change closes the old interval and opens a new one with old.valid_to == new.valid_from
// exactly — no gap, no overlap.
func TestAccountStatusHistory_StatusChange_ClosesOldOpensNewWithNoGapOrOverlap(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)
	accountID := insertAshFixtureAccount(t, ctx, pool, item.ID, userID, "plaid-acct-"+suffix)

	require.NoError(t, accountRepo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusRemoved, nil))

	intervals := ashIntervalsFor(t, ctx, pool, accountID)
	require.Len(t, intervals, 2, "a status change must close the old interval and open exactly one new one")

	oldIv, newIv := intervals[0], intervals[1]
	assert.Equal(t, "active", oldIv.Status)
	assert.Equal(t, "removed", newIv.Status)
	require.NotNil(t, oldIv.ValidTo, "the old interval must be closed")
	assert.Nil(t, newIv.ValidTo, "the new interval must be open (current)")
	assert.True(t, oldIv.ValidTo.Equal(newIv.ValidFrom),
		"old.valid_to must equal new.valid_from exactly: no gap, no overlap (got old.valid_to=%v new.valid_from=%v)",
		oldIv.ValidTo, newIv.ValidFrom)
	require.NotNil(t, newIv.Reason)
	assert.Equal(t, "user_disconnect", *newIv.Reason)
}

// TestAccountStatusHistory_SameValueUpdate_WritesNothing is brief assertion 3: an UPDATE that
// sets status to the value it already has must not write a new interval (the no-op guard).
// UpdateStatus in account_repository.go always sets the status column, whether or not the value
// actually changes, and `UPDATE OF status` fires on that regardless — the WHEN clause on the
// accounts trigger is what must suppress it.
func TestAccountStatusHistory_SameValueUpdate_WritesNothing(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)
	accountID := insertAshFixtureAccount(t, ctx, pool, item.ID, userID, "plaid-acct-"+suffix)

	before := ashIntervalsFor(t, ctx, pool, accountID)
	require.Len(t, before, 1)

	// A same-value UPDATE OF status, exactly as UpdateStatus issues (it always assigns the status
	// column, even to its current value).
	ct, err := pool.Exec(ctx, `UPDATE accounts SET status = 'active' WHERE id = $1`, accountID)
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected())

	after := ashIntervalsFor(t, ctx, pool, accountID)
	require.Len(t, after, 1, "a same-value status update must not write a new interval")
	assert.Nil(t, after[0].ValidTo, "the original open interval must remain open")
	assert.Equal(t, before[0].ValidFrom, after[0].ValidFrom, "the original interval must be untouched")
}

// TestAccountStatusHistory_UqAshOpen_RejectsSecondOpenInterval is brief assertion 4: uq_ash_open
// rejects a second open interval for one account. This is the backstop for a bug in the trigger's
// close-before-open ordering, not the primary mechanism — but it must actually work.
func TestAccountStatusHistory_UqAshOpen_RejectsSecondOpenInterval(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)
	accountID := insertAshFixtureAccount(t, ctx, pool, item.ID, userID, "plaid-acct-"+suffix)

	// The INSERT trigger already opened one interval. Attempt to open a second, directly.
	_, err := pool.Exec(ctx, `
		INSERT INTO account_status_history (account_id, user_id, status, valid_from)
		VALUES ($1, $2, 'active', NOW())`, accountID, userID)
	require.Error(t, err, "uq_ash_open must reject a second open interval for the same account")
	assert.Contains(t, err.Error(), "uq_ash_open")
}

// TestAccountStatusHistory_BulkMultiRowUpdate_OneIntervalPerAccount is brief assertion 5: a bulk
// multi-row UPDATE, as MarkRemovedByLinkedItemID issues, must write one interval transition PER
// AFFECTED ACCOUNT (the trigger is FOR EACH ROW, so it must fire once per row touched by the
// single UPDATE statement, not once for the statement as a whole).
func TestAccountStatusHistory_BulkMultiRowUpdate_OneIntervalPerAccount(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	checkingID := insertAshFixtureAccount(t, ctx, pool, item.ID, userID, "plaid-checking-"+suffix)
	savingsID := insertAshFixtureAccount(t, ctx, pool, item.ID, userID, "plaid-savings-"+suffix)

	require.NoError(t, accountRepo.MarkRemovedByLinkedItemID(ctx, item.ID))

	for _, accountID := range []string{checkingID, savingsID} {
		intervals := ashIntervalsFor(t, ctx, pool, accountID)
		require.Lenf(t, intervals, 2, "account %s must have exactly one closed active interval and one open removed interval", accountID)
		assert.Equal(t, "active", intervals[0].Status)
		assert.Equal(t, "removed", intervals[1].Status)
		require.NotNil(t, intervals[0].ValidTo)
		assert.Nil(t, intervals[1].ValidTo)
		assert.True(t, intervals[0].ValidTo.Equal(intervals[1].ValidFrom),
			"account %s: old.valid_to must equal new.valid_from exactly", accountID)
	}
}

// runAccountStatusHistoryBackfillSQL re-runs the exact backfill statements from migration
// 000152_backfill_account_status_history.up.sql. It is a deliberate copy (not a shared constant)
// because the migration is applied once, forever, against the real database at deploy time — by
// the time this suite runs, migrations up to 000152 (including its trigger) are already applied,
// so the ONLY way to exercise the backfill's SQL against fixture data is to run it again here,
// scoped by NOT EXISTS exactly like the migration does. Keep this in sync with 000152's up.sql if
// that file's backfill logic ever changes.
func runAccountStatusHistoryBackfillSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	statements := []string{
		`INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
		 SELECT a.id, a.user_id, a.user_account_id, 'active', a.created_at, NULL, 'created', 'backfilled'
		 FROM accounts a
		 WHERE a.status = 'active'
		   AND NOT EXISTS (SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'active')`,

		`INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
		 SELECT a.id, a.user_id, a.user_account_id, 'active', a.created_at, a.replaced_at, 'created', 'backfilled'
		 FROM accounts a
		 WHERE a.status = 'replaced'
		   AND a.replaced_at IS NOT NULL AND a.replaced_at > a.created_at
		   AND NOT EXISTS (SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'active')`,

		`INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
		 SELECT a.id, a.user_id, a.user_account_id, 'replaced',
		        CASE WHEN a.replaced_at IS NOT NULL AND a.replaced_at > a.created_at THEN a.replaced_at ELSE a.created_at END,
		        NULL, 'relink', 'backfilled'
		 FROM accounts a
		 WHERE a.status = 'replaced'
		   AND NOT EXISTS (SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'replaced')`,

		`INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
		 SELECT a.id, a.user_id, a.user_account_id, 'active', a.created_at, a.updated_at, 'created', 'backfilled'
		 FROM accounts a
		 WHERE a.status = 'expired'
		   AND a.updated_at IS NOT NULL AND a.updated_at > a.created_at
		   AND NOT EXISTS (SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'active')`,

		`INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
		 SELECT a.id, a.user_id, a.user_account_id, 'expired',
		        CASE WHEN a.updated_at IS NOT NULL AND a.updated_at > a.created_at THEN a.updated_at ELSE a.created_at END,
		        NULL, 'backfill_expired_estimated', 'backfilled'
		 FROM accounts a
		 WHERE a.status = 'expired'
		   AND NOT EXISTS (SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'expired')`,

		`INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
		 SELECT a.id, a.user_id, a.user_account_id, 'active', a.created_at, a.removed_at, 'created', 'backfilled'
		 FROM accounts a
		 WHERE a.status = 'removed'
		   AND a.removed_at IS NOT NULL AND a.removed_at > a.created_at
		   AND NOT EXISTS (SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'active')`,

		`INSERT INTO account_status_history (account_id, user_id, user_account_id, status, valid_from, valid_to, reason, provenance)
		 SELECT a.id, a.user_id, a.user_account_id, 'removed',
		        CASE WHEN a.removed_at IS NOT NULL AND a.removed_at > a.created_at THEN a.removed_at ELSE a.created_at END,
		        NULL, 'user_disconnect', 'backfilled'
		 FROM accounts a
		 WHERE a.status = 'removed'
		   AND NOT EXISTS (SELECT 1 FROM account_status_history h WHERE h.account_id = a.id AND h.status = 'removed')`,
	}

	for _, stmt := range statements {
		_, err := pool.Exec(ctx, stmt)
		require.NoError(t, err)
	}
}

// TestAccountStatusHistory_Backfill_Shapes is brief assertion 6: one account per status
// (active/replaced/expired/removed), inserted as if it predated the trigger, produces the
// interval shapes documented in the migration's table, and the expired row carries
// reason='backfill_expired_estimated'.
//
// The accounts trigger already exists in this schema (migrations run once, forever) — an INSERT
// through it always opens an 'active [now, ∞)' interval, which would not exercise the backfill
// logic at all. So this test disables the INSERT trigger, inserts fixture accounts with
// historical timestamps and terminal statuses directly (simulating rows that predate 000152),
// then re-runs the backfill SQL itself (runAccountStatusHistoryBackfillSQL) against them.
func TestAccountStatusHistory_Backfill_Shapes(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	_, err := pool.Exec(ctx, `ALTER TABLE accounts DISABLE TRIGGER account_status_history_insert_trigger`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `ALTER TABLE accounts ENABLE TRIGGER account_status_history_insert_trigger`)
	})

	created := time.Now().Add(-100 * 24 * time.Hour).UTC().Truncate(time.Microsecond)
	replacedAt := created.Add(50 * 24 * time.Hour)
	expiredUpdatedAt := created.Add(60 * 24 * time.Hour)
	removedAt := created.Add(70 * 24 * time.Hour)

	insertHistoricalAccount := func(status string, updatedAt time.Time, replacedAtVal, removedAtVal *time.Time) string {
		var id string
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type, status,
			                       created_at, updated_at, replaced_at, removed_at)
			VALUES ($1, $2, $3, 'Test', 'depository', $4, $5, $6, $7, $8)
			RETURNING id`,
			item.ID, userID, "plaid-bf-"+suffix+"-"+status, status, created, updatedAt, replacedAtVal, removedAtVal,
		).Scan(&id))
		return id
	}

	activeID := insertHistoricalAccount("active", created, nil, nil)
	replacedID := insertHistoricalAccount("replaced", created, &replacedAt, nil)
	expiredID := insertHistoricalAccount("expired", expiredUpdatedAt, nil, nil)
	removedID := insertHistoricalAccount("removed", created, nil, &removedAt)

	// Sanity: disabling the trigger actually worked — no rows exist yet for these accounts.
	require.Empty(t, ashIntervalsFor(t, ctx, pool, activeID))

	runAccountStatusHistoryBackfillSQL(t, ctx, pool)

	// active -> active [created, ∞)
	activeIvs := ashIntervalsFor(t, ctx, pool, activeID)
	require.Len(t, activeIvs, 1)
	assert.Equal(t, "active", activeIvs[0].Status)
	assert.True(t, activeIvs[0].ValidFrom.Equal(created))
	assert.Nil(t, activeIvs[0].ValidTo)

	// replaced -> active[created, replacedAt) + replaced[replacedAt, ∞)
	replacedIvs := ashIntervalsFor(t, ctx, pool, replacedID)
	require.Len(t, replacedIvs, 2)
	assert.Equal(t, "active", replacedIvs[0].Status)
	assert.True(t, replacedIvs[0].ValidFrom.Equal(created))
	require.NotNil(t, replacedIvs[0].ValidTo)
	assert.True(t, replacedIvs[0].ValidTo.Equal(replacedAt))
	assert.Equal(t, "replaced", replacedIvs[1].Status)
	assert.True(t, replacedIvs[1].ValidFrom.Equal(replacedAt))
	assert.Nil(t, replacedIvs[1].ValidTo)

	// expired -> active[created, updated_at) + expired[updated_at, ∞), reason estimated
	expiredIvs := ashIntervalsFor(t, ctx, pool, expiredID)
	require.Len(t, expiredIvs, 2)
	assert.Equal(t, "active", expiredIvs[0].Status)
	assert.True(t, expiredIvs[0].ValidFrom.Equal(created))
	require.NotNil(t, expiredIvs[0].ValidTo)
	assert.True(t, expiredIvs[0].ValidTo.Equal(expiredUpdatedAt))
	assert.Equal(t, "expired", expiredIvs[1].Status)
	assert.True(t, expiredIvs[1].ValidFrom.Equal(expiredUpdatedAt))
	assert.Nil(t, expiredIvs[1].ValidTo)
	require.NotNil(t, expiredIvs[1].Reason)
	assert.Equal(t, "backfill_expired_estimated", *expiredIvs[1].Reason,
		"the expired terminal row's instant is only an estimate (accounts.updated_at could have "+
			"been overwritten since) and must stay greppable as such")

	// removed -> active[created, removedAt) + removed[removedAt, ∞)
	removedIvs := ashIntervalsFor(t, ctx, pool, removedID)
	require.Len(t, removedIvs, 2)
	assert.Equal(t, "active", removedIvs[0].Status)
	assert.True(t, removedIvs[0].ValidFrom.Equal(created))
	require.NotNil(t, removedIvs[0].ValidTo)
	assert.True(t, removedIvs[0].ValidTo.Equal(removedAt))
	assert.Equal(t, "removed", removedIvs[1].Status)
	assert.True(t, removedIvs[1].ValidFrom.Equal(removedAt))
	assert.Nil(t, removedIvs[1].ValidTo)

	for _, iv := range append(append(append(activeIvs, replacedIvs...), expiredIvs...), removedIvs...) {
		assert.Equal(t, "backfilled", iv.Provenance)
	}
}

// TestAccountStatusHistory_Backfill_ZeroLengthIntervalGuard covers the edge case documented in
// 000152: a terminal timestamp that is NULL or <= created_at must not produce a zero-length (or
// negative-length) 'active' prefix interval, which chk_ash_interval would reject outright. The
// deterministic handling: skip the 'active' prefix and start the terminal interval at created_at.
//
// NULL is not actually reachable for removed_at when status='removed' — migration 000149's
// accounts_removed_at_consistency CHECK already forbids that combination at the accounts level —
// so the reachable edge case is a non-NULL terminal timestamp that is <= created_at (e.g. bad
// historical data, or created_at and removed_at colliding at the same instant).
func TestAccountStatusHistory_Backfill_ZeroLengthIntervalGuard(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)

	_, err := pool.Exec(ctx, `ALTER TABLE accounts DISABLE TRIGGER account_status_history_insert_trigger`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `ALTER TABLE accounts ENABLE TRIGGER account_status_history_insert_trigger`)
	})

	created := time.Now().Add(-10 * 24 * time.Hour).UTC().Truncate(time.Microsecond)

	// removed_at == created_at: a nonsensical but non-NULL (and therefore constraint-legal)
	// zero-length transition instant.
	var removedID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type, status, created_at, updated_at, removed_at)
		VALUES ($1, $2, $3, 'Test', 'depository', 'removed', $4, $4, $4)
		RETURNING id`,
		item.ID, userID, "plaid-bf-nullts-"+suffix, created).Scan(&removedID))

	runAccountStatusHistoryBackfillSQL(t, ctx, pool)

	ivs := ashIntervalsFor(t, ctx, pool, removedID)
	require.Len(t, ivs, 1, "with no valid removed_at, only the terminal interval is written — no zero-length 'active' prefix")
	assert.Equal(t, "removed", ivs[0].Status)
	assert.True(t, ivs[0].ValidFrom.Equal(created), "the terminal interval must start at created_at when the transition instant is unknown")
	assert.Nil(t, ivs[0].ValidTo)
}

// TestAccountStatusHistory_HalfOpenBoundary_LiveAtStartNotAtEnd is brief assertion 7, the
// highest-risk one: interval [t1, t2) must be live AT t1 and NOT live AT t2. Pinned at the exact
// instant returned by the trigger (clock_timestamp()), not an approximation, so an off-by-one in
// either direction (>= vs >, < vs <=) fails this test.
func TestAccountStatusHistory_HalfOpenBoundary_LiveAtStartNotAtEnd(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	linkedItemRepo := NewLinkedItemRepository(pool, enc)
	accountRepo := NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createMarkDisconnectedFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createMarkDisconnectedFixtureItem(t, ctx, linkedItemRepo, userID, suffix)
	accountID := insertAshFixtureAccount(t, ctx, pool, item.ID, userID, "plaid-acct-"+suffix)

	require.NoError(t, accountRepo.UpdateStatus(ctx, accountID, userID, domain.AccountStatusRemoved, nil))

	intervals := ashIntervalsFor(t, ctx, pool, accountID)
	require.Len(t, intervals, 2)
	activeIv, removedIv := intervals[0], intervals[1]
	require.NotNil(t, activeIv.ValidTo)
	t1 := activeIv.ValidFrom // the account-creation instant: start of the active interval
	t2 := *activeIv.ValidTo  // the transition instant: end of active, start of removed

	require.True(t, t2.After(t1), "sanity: the transition must be strictly after creation")

	// The active interval [t1, t2): live AT t1, NOT live AT t2.
	assert.True(t, ashLiveAt(t, ctx, pool, accountID, "active", t1),
		"active interval must be live AT its own valid_from (t1)")
	assert.False(t, ashLiveAt(t, ctx, pool, accountID, "active", t2),
		"active interval must NOT be live AT its valid_to (t2) — half-open, exclusive upper bound")

	// The removed interval [t2, ∞): live AT t2 (the exact instant the old interval stopped).
	assert.True(t, ashLiveAt(t, ctx, pool, accountID, "removed", t2),
		"removed interval must be live AT its own valid_from (t2) — the same instant the active "+
			"interval stops being live, so exactly one status is live at any given instant")
	assert.Nil(t, removedIv.ValidTo)

	// A microsecond before t2, only "active" is live; a microsecond at/after t2, only "removed" is.
	justBeforeT2 := t2.Add(-time.Microsecond)
	if justBeforeT2.After(t1) {
		assert.True(t, ashLiveAt(t, ctx, pool, accountID, "active", justBeforeT2))
		assert.False(t, ashLiveAt(t, ctx, pool, accountID, "removed", justBeforeT2))
	}
}
