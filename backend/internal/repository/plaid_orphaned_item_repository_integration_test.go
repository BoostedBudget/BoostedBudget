//go:build integration

package repository

// Issue #1497 — plaidOrphanedItemRepository against a real PostgreSQL 17.
//
// WHY THESE ARE INTEGRATION TESTS AND NOT pgxmock TESTS. Every property this file asserts is a
// property of the SQL, and pgxmock matches query text and argument shape only — it would pass a purge
// statement that ignored its asOf argument, one that also cleared resolved rows' resolution, one whose
// LIMIT was in the wrong subquery, and one that set token_purged_at without nulling the token. The
// last of those is refused by a CHECK constraint that exists only in the database. So: real Postgres.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestPlaidOrphanedItem -tags integration -v -p 1

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ─── the real fixture helper ─────────────────────────────────────────────────────────────────────

type orphanFixture struct {
	plaidItemID    string
	accessToken    string // "" seeds a NULL access_token_encrypted (the pre-000135 row shape)
	orphanedAt     time.Time
	tokenExpiresAt time.Time
	status         string
	lastErrorCode  string
	resolvedAt     *time.Time
	lastRetryAt    *time.Time
	attemptCount   int
}

func seedOrphanRow(t *testing.T, ctx context.Context, repo *plaidOrphanedItemRepository, f orphanFixture) string {
	t.Helper()

	encItem, err := repo.enc.EncryptString(f.plaidItemID)
	require.NoError(t, err)

	var encToken *string
	if f.accessToken != "" {
		v, encErr := repo.enc.EncryptString(f.accessToken)
		require.NoError(t, encErr)
		encToken = &v
	}

	id := uuid.NewString()
	_, err = repo.db.Exec(ctx, `
		INSERT INTO plaid_orphaned_items (
			id, linked_item_id, plaid_item_id_encrypted, linked_item_status, last_error_code,
			orphaned_at, access_token_encrypted, token_expires_at, resolved_at, last_retry_at,
			retry_attempt_count
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		id, uuid.NewString(), encItem, nullIfEmpty(f.status), nullIfEmpty(f.lastErrorCode),
		f.orphanedAt, encToken, f.tokenExpiresAt, f.resolvedAt, f.lastRetryAt, f.attemptCount)
	require.NoError(t, err)
	return id
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func newOrphanRepo(t *testing.T) (*plaidOrphanedItemRepository, context.Context) {
	t.Helper()
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	return NewPlaidOrphanedItemRepository(pool, enc), context.Background()
}

// tokenState reads the two columns the whole compliance argument turns on, straight from the row.
func tokenState(t *testing.T, ctx context.Context, repo *plaidOrphanedItemRepository, id string) (token *string, purgedAt *time.Time) {
	t.Helper()
	require.NoError(t, repo.db.QueryRow(ctx,
		`SELECT access_token_encrypted, token_purged_at FROM plaid_orphaned_items WHERE id = $1`, id).
		Scan(&token, &purgedAt))
	return token, purgedAt
}

// ─── C: the purge is its own guaranteed action ───────────────────────────────────────────────────

// THE HEADLINE TEST FOR #1497. No retry job is constructed here, let alone run: an expired record's
// token is purged by a statement that knows nothing about retries.
//
// Falsifiability, verified by doing it: adding `AND resolved_at IS NULL` or `AND retry_attempt_count
// > 0` to PurgeExpiredTokens' predicate leaves the token in place and this test fails.
func TestPlaidOrphanedItemRepository_Integration_PurgeExpiredTokens_PurgesWithNoRetryEverHavingRun(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()

	id := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID:    "item-expired-" + uuid.NewString(),
		accessToken:    "access-sandbox-expired",
		orphanedAt:     asOf.Add(-31 * 24 * time.Hour),
		tokenExpiresAt: asOf.Add(-1 * time.Hour),
	})

	before, beforePurged := tokenState(t, ctx, repo, id)
	require.NotNil(t, before, "fixture must actually hold a credential, or this test proves nothing")
	require.Nil(t, beforePurged)

	purged, err := repo.PurgeExpiredTokens(ctx, asOf, 100)
	require.NoError(t, err)
	// GreaterOrEqual, not Equal: the integration suite shares one database, so rows this test did not
	// seed can legitimately also be overdue. The load-bearing assertion is the per-row one below.
	assert.GreaterOrEqual(t, purged, 1)

	after, afterPurged := tokenState(t, ctx, repo, id)
	assert.Nil(t, after, "an expired credential must not survive the sweep")
	assert.NotNil(t, afterPurged, "and the purge must be recorded, not merely performed")
}

// The purge is keyed on NOTHING BUT the deadline. A resolved row, a row with a high attempt count and
// a row that has never been retried are all purged by the same statement — and an unexpired token is
// not touched by it.
func TestPlaidOrphanedItemRepository_Integration_PurgeExpiredTokens_IgnoresEveryStateButTheDeadline(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()
	resolved := asOf.Add(-2 * time.Hour)

	expiredResolved := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-res-" + uuid.NewString(), accessToken: "tok-a",
		orphanedAt: asOf.Add(-40 * 24 * time.Hour), tokenExpiresAt: asOf.Add(-time.Minute),
		resolvedAt: &resolved,
	})
	expiredBusy := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-busy-" + uuid.NewString(), accessToken: "tok-b",
		orphanedAt: asOf.Add(-40 * 24 * time.Hour), tokenExpiresAt: asOf.Add(-time.Minute),
		attemptCount: 97, lastRetryAt: &resolved,
	})
	stillLive := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-live-" + uuid.NewString(), accessToken: "tok-c",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
	})

	purged, err := repo.PurgeExpiredTokens(ctx, asOf, 100)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, purged, 2)

	for _, id := range []string{expiredResolved, expiredBusy} {
		tok, purgedAt := tokenState(t, ctx, repo, id)
		assert.Nil(t, tok, "row %s: expiry alone decides the purge", id)
		assert.NotNil(t, purgedAt, "row %s", id)
	}
	tok, purgedAt := tokenState(t, ctx, repo, stillLive)
	assert.NotNil(t, tok, "an unexpired credential is still doing its job and must NOT be purged")
	assert.Nil(t, purgedAt)
}

// The purge decides on the asOf it is GIVEN, not on the database's own NOW(). pgxmock cannot tell
// these apart; a real row seeded to expire in the future, swept with a future asOf, can.
func TestPlaidOrphanedItemRepository_Integration_PurgeExpiredTokens_UsesTheGivenInstantNotDatabaseNow(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	now := time.Now().UTC()

	id := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-future-" + uuid.NewString(), accessToken: "tok-future",
		orphanedAt: now, tokenExpiresAt: now.Add(10 * 24 * time.Hour),
	})

	purged, err := repo.PurgeExpiredTokens(ctx, now, 100)
	require.NoError(t, err)
	tok, _ := tokenState(t, ctx, repo, id)
	require.NotNil(t, tok, "not expired at the real now(); purged=%d", purged)

	_, err = repo.PurgeExpiredTokens(ctx, now.Add(11*24*time.Hour), 100)
	require.NoError(t, err)
	tok, purgedAt := tokenState(t, ctx, repo, id)
	assert.Nil(t, tok, "the same row IS expired relative to the instant the caller passed")
	assert.NotNil(t, purgedAt)
}

// The per-sweep cap is real. Without it a single run could rewrite an unbounded number of rows in one
// statement; with it the remainder is simply deferred to the next run, which finds it unchanged.
func TestPlaidOrphanedItemRepository_Integration_PurgeExpiredTokens_HonoursItsCap(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()

	// Clear the field first. The integration suite shares one database and earlier runs leave overdue
	// rows behind, so "the two most overdue rows are the two this test seeded" is only a statement about
	// this test's fixtures if nothing else is overdue when it starts.
	_, err := repo.PurgeExpiredTokens(ctx, asOf, 100000)
	require.NoError(t, err)

	ids := make([]string, 3)
	for i := range ids {
		ids[i] = seedOrphanRow(t, ctx, repo, orphanFixture{
			plaidItemID: fmt.Sprintf("item-cap-%d-%s", i, uuid.NewString()), accessToken: "tok",
			orphanedAt: asOf.Add(-40 * 24 * time.Hour), tokenExpiresAt: asOf.Add(-time.Duration(i+1) * time.Hour),
		})
	}

	purged, purgeErr := repo.PurgeExpiredTokens(ctx, asOf, 2)
	require.NoError(t, purgeErr)
	assert.Equal(t, 2, purged, "the cap bounds one statement, it does not silently process everything")

	// Oldest deadline first, so the two purged are the two most overdue.
	tok, _ := tokenState(t, ctx, repo, ids[2])
	assert.Nil(t, tok, "the most overdue row must be taken first")
}

// ─── B: the retry selection ──────────────────────────────────────────────────────────────────────

func TestPlaidOrphanedItemRepository_Integration_GetRetryablePage_SelectsOnlyLiveUnresolvedRows(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()
	resolvedAt := asOf.Add(-time.Hour)

	want := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-want-" + uuid.NewString(), accessToken: "tok-want",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
		status: "replaced",
	})
	expired := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-exp-" + uuid.NewString(), accessToken: "tok-exp",
		orphanedAt: asOf.Add(-40 * 24 * time.Hour), tokenExpiresAt: asOf.Add(-time.Second),
	})
	noToken := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-notok-" + uuid.NewString(),
		orphanedAt:  asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
	})
	alreadyDone := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-done-" + uuid.NewString(), accessToken: "tok-done",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
		resolvedAt: &resolvedAt,
	})

	rows, err := repo.GetRetryablePage(ctx, asOf, nil, 500)
	require.NoError(t, err)

	got := map[string]domain.PlaidOrphanedItem{}
	for _, r := range rows {
		got[r.ID] = r
	}
	require.Contains(t, got, want, "a live, unresolved, credential-bearing row is exactly the retry population")
	assert.NotContains(t, got, expired, "an EXPIRED token must never be presented to Plaid, purge sweep or no purge sweep")
	assert.NotContains(t, got, noToken, "nothing to send")
	assert.NotContains(t, got, alreadyDone, "already removed at Plaid")

	// And the row must arrive DECRYPTED and complete, or the retry cannot call /item/remove with it.
	assert.NotEmpty(t, got[want].AccessToken)
	assert.Equal(t, "replaced", got[want].LinkedItemStatus,
		"the 'replaced' leg is why retrying is worth doing at all — it must survive the read")
	assert.True(t, got[want].PlaidItemID != "" && got[want].PlaidItemID[:5] == "item-",
		"plaid_item_id must be decrypted, got %q", got[want].PlaidItemID)
}

// Keyset pagination, and specifically the tie-breaker. Three rows sharing one orphaned_at is the case
// a bare `ORDER BY orphaned_at` cannot page through deterministically — it can revisit or skip rows
// forever. A tie-breaker no test ever ties on is indistinguishable from an absent one.
func TestPlaidOrphanedItemRepository_Integration_GetRetryablePage_PagesThroughIdenticalTimestamps(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()
	tied := asOf.Add(-3 * time.Hour)

	seeded := map[string]bool{}
	for i := 0; i < 3; i++ {
		id := seedOrphanRow(t, ctx, repo, orphanFixture{
			plaidItemID: fmt.Sprintf("item-tie-%d-%s", i, uuid.NewString()), accessToken: "tok",
			orphanedAt: tied, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
		})
		seeded[id] = true
	}

	seen := map[string]int{}
	var cursor *OrphanRetryCursor
	for pages := 0; pages < 50; pages++ {
		page, err := repo.GetRetryablePage(ctx, asOf, cursor, 1)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		last := page[len(page)-1]
		seen[last.ID]++
		cursor = &OrphanRetryCursor{OrphanedAt: last.OrphanedAt, ID: last.ID}
	}

	for id := range seeded {
		assert.Equal(t, 1, seen[id], "row %s must be visited exactly once across the whole walk", id)
	}
}

// ─── B: recording outcomes ───────────────────────────────────────────────────────────────────────

// Success purges the credential IN THE SAME STATEMENT that records the resolution. Two writes would
// mean a window in which the row says "done" and still holds a token — and if the second failed, says
// it forever.
func TestPlaidOrphanedItemRepository_Integration_MarkResolved_PurgesTheTokenAtomically(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()

	id := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-ok-" + uuid.NewString(), accessToken: "tok-ok",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
	})

	updated, err := repo.MarkResolved(ctx, id, asOf)
	require.NoError(t, err)
	assert.True(t, updated)

	tok, purgedAt := tokenState(t, ctx, repo, id)
	assert.Nil(t, tok, "the purpose is spent the instant the Item is removed; the credential goes with it")
	assert.NotNil(t, purgedAt)

	var resolvedAt *time.Time
	var attempts int
	require.NoError(t, repo.db.QueryRow(ctx,
		`SELECT resolved_at, retry_attempt_count FROM plaid_orphaned_items WHERE id = $1`, id).
		Scan(&resolvedAt, &attempts))
	assert.NotNil(t, resolvedAt)
	assert.Equal(t, 1, attempts, "the successful attempt counts as an attempt")
}

// First-wins on resolution, mirroring MarkPlaidRemovalFailed's refusal to overwrite: a second resolve
// must not move the timestamp, so "when was this Item removed" stays answerable.
func TestPlaidOrphanedItemRepository_Integration_MarkResolved_IsFirstWins(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()

	id := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-twice-" + uuid.NewString(), accessToken: "tok",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
	})

	updated, err := repo.MarkResolved(ctx, id, asOf)
	require.NoError(t, err)
	require.True(t, updated)

	var first *time.Time
	require.NoError(t, repo.db.QueryRow(ctx,
		`SELECT resolved_at FROM plaid_orphaned_items WHERE id = $1`, id).Scan(&first))

	updated, err = repo.MarkResolved(ctx, id, asOf.Add(time.Hour))
	require.NoError(t, err)
	assert.False(t, updated, "an already-resolved row is not resolved again")

	var second *time.Time
	require.NoError(t, repo.db.QueryRow(ctx,
		`SELECT resolved_at FROM plaid_orphaned_items WHERE id = $1`, id).Scan(&second))
	assert.Equal(t, first.UTC(), second.UTC())
}

// Failure records the attempt and the code and LEAVES THE CREDENTIAL ALONE — the retention window,
// not the failure, is what ends the retrying.
func TestPlaidOrphanedItemRepository_Integration_RecordRetryFailure_KeepsTokenAndIncrementsAttempt(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()

	id := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-fail-" + uuid.NewString(), accessToken: "tok",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
		lastErrorCode: "INVALID_ACCESS_TOKEN",
	})

	require.NoError(t, repo.RecordRetryFailure(ctx, id, "PRODUCT_NOT_READY", asOf))
	require.NoError(t, repo.RecordRetryFailure(ctx, id, "INTERNAL_SERVER_ERROR", asOf.Add(time.Minute)))

	tok, purgedAt := tokenState(t, ctx, repo, id)
	assert.NotNil(t, tok, "a failed retry must not purge; the deadline does that")
	assert.Nil(t, purgedAt)

	var attempts int
	var lastRetryCode, erasureCode *string
	var lastRetryAt *time.Time
	require.NoError(t, repo.db.QueryRow(ctx, `
		SELECT retry_attempt_count, last_retry_error_code, last_error_code, last_retry_at
		FROM plaid_orphaned_items WHERE id = $1`, id).
		Scan(&attempts, &lastRetryCode, &erasureCode, &lastRetryAt))

	assert.Equal(t, 2, attempts)
	require.NotNil(t, lastRetryCode)
	assert.Equal(t, "INTERNAL_SERVER_ERROR", *lastRetryCode, "the LATEST failure")
	require.NotNil(t, erasureCode)
	assert.Equal(t, "INVALID_ACCESS_TOKEN", *erasureCode,
		"last_error_code records the moment of erasure and must never be overwritten by a retry — "+
			"it is the diagnostic that explains why the row exists at all")
	assert.NotNil(t, lastRetryAt)
}

// A resolved row is terminal: a late-arriving failure from an in-flight duplicate must not reopen it.
func TestPlaidOrphanedItemRepository_Integration_RecordRetryFailure_CannotReopenAResolvedRow(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()
	resolved := asOf.Add(-time.Hour)

	id := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-late-" + uuid.NewString(),
		orphanedAt:  asOf.Add(-2 * time.Hour), tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
		resolvedAt: &resolved,
	})

	require.NoError(t, repo.RecordRetryFailure(ctx, id, "INTERNAL_SERVER_ERROR", asOf))

	var attempts int
	var code *string
	require.NoError(t, repo.db.QueryRow(ctx,
		`SELECT retry_attempt_count, last_retry_error_code FROM plaid_orphaned_items WHERE id = $1`, id).
		Scan(&attempts, &code))
	assert.Equal(t, 0, attempts, "a resolved row takes no further attempts")
	assert.Nil(t, code)
}

// ─── D: the admin read ───────────────────────────────────────────────────────────────────────────

func TestPlaidOrphanedItemRepository_Integration_List_ReturnsBothAdminStates(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()

	retrying := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-retry-" + uuid.NewString(), accessToken: "tok",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
	})
	manual := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-manual-" + uuid.NewString(),
		orphanedAt:  asOf.Add(-40 * 24 * time.Hour), tokenExpiresAt: asOf.Add(-24 * time.Hour),
		lastErrorCode: "INVALID_ACCESS_TOKEN",
	})

	rows, total, err := repo.List(ctx, 100, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 2)

	states := map[string]domain.OrphanedItemAdminState{}
	for _, r := range rows {
		states[r.ID] = r.AdminState(asOf)
	}
	assert.Equal(t, domain.OrphanedItemAutoRetrying, states[retrying])
	assert.Equal(t, domain.OrphanedItemManualInterventionRequired, states[manual],
		"the state an operator must act on, and the reason this table is surfaced at all")
}

// ─── the schema's own guarantee ──────────────────────────────────────────────────────────────────

// TestPlaidOrphanedItems_Integration_PurgedTokenIsNullIsEnforcedByTheDatabase proves the claim that
// "purge believed to have happened but has not" is not merely unlikely but IMPOSSIBLE to record. Every
// such claim in this system reads token_purged_at; migration 000135's CHECK constraint is what makes
// that read trustworthy without re-inspecting the ciphertext.
func TestPlaidOrphanedItems_Integration_PurgedTokenIsNullIsEnforcedByTheDatabase(t *testing.T) {
	repo, ctx := newOrphanRepo(t)
	asOf := time.Now().UTC()

	id := seedOrphanRow(t, ctx, repo, orphanFixture{
		plaidItemID: "item-check-" + uuid.NewString(), accessToken: "tok",
		orphanedAt: asOf, tokenExpiresAt: asOf.Add(domain.OrphanedItemTokenRetention),
	})

	_, err := repo.db.Exec(ctx,
		`UPDATE plaid_orphaned_items SET token_purged_at = NOW() WHERE id = $1`, id)
	require.Error(t, err, "the database must refuse a purge stamp that leaves the credential in place")
	assert.Contains(t, err.Error(), "plaid_orphaned_items_purged_token_is_null")
}
