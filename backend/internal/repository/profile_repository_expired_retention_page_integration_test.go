//go:build integration

package repository

// Integration tests for ProfileRepository.GetUsersWithExpiredRetentionPage (Issue #1478).
//
// GetUsersWithExpiredRetention (no LIMIT, no id tie-breaker) feeds the ONLY in-process
// HARD-DELETE sweep in this codebase: RetentionCleanupCheckWorker.deleteExpiredAccounts
// (internal/queue/jobs/retention_cleanup_check.go) consumes the whole slice and calls
// DeletionService.ExecuteDeletion(..., DeletionKindRetention) once per row. That job is
// registered with RunOnStart: true (RetentionCleanupCheckPeriodicOpts), so on the first run
// after every deploy the size of that destructive batch is whatever the table happens to hold.
//
// This is the same defect class already fixed once on this branch for the scheduled-deletion
// sweep (GetUsersScheduledForDeletionPage, profile_repository.go), and the fix is the same:
// ORDER BY (retention_expires_at, id) ASC, a keyset cursor of the
// (retention_expires_at, id) > ($n, $m) form, and a LIMIT — with the caller applying a
// per-sweep cap on the TOTAL processed across pages.
//
// pgxmock cannot validate any of this: it matches query text and argument shape only, never
// whether the row-comparison predicate and the ORDER BY tie-breaker actually produce a
// deterministic, non-overlapping sequence of pages over real rows. Nor can it show a second
// sweep advancing past rows a first sweep already deleted.
//
// ⚠ Which test covers which half, because this file's first version claimed the tie-breaker was
// covered when it was not: the cap/paging tests below seed STRICTLY INCREASING
// retention_expires_at values, so they never create a tie and therefore never exercise the `id`
// component at all — removing `id` from the ORDER BY and from the keyset tuple left every one of
// them green. TestGetUsersWithExpiredRetentionPage_Integration_TiedExpiry_IdTieBreakerMakesOrderTotal
// is the test that actually pins it: it seeds rows sharing ONE retention_expires_at, so the `id`
// component is the only thing that can order them, and it fails if `id` is absent from either
// place. Do not delete it thinking the paging tests cover the same ground.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetUsersWithExpiredRetentionPage_Integration -tags integration -v -p 1

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// expiredRetentionSweepEpoch anchors this test's seeded retention_expires_at values far enough in
// the past that the seeded rows are the OLDEST candidates in the table, which is what makes a
// "processes exactly the cap" assertion meaningful: the cap is spent oldest-first, so an older
// leaked fixture from another suite would silently consume it. The test does not merely HOPE for
// that ordering — assertSeededRowsAreOldestCandidates below proves it, so a leak fails loudly
// instead of quietly weakening the assertion.
var expiredRetentionSweepEpoch = time.Now().Add(-10 * 365 * 24 * time.Hour)

// seedExpiredRetentionFixtureUsers creates n users all matching
// GetUsersWithExpiredRetentionPage's predicate (retention_expires_at in the past,
// subscription_status = 'cancelled', is_deleted = FALSE, deletion_requested_at IS NULL, no
// manual tier override), with strictly increasing retention_expires_at so
// ORDER BY (retention_expires_at, id) ASC yields a known sequence. Returns the ids in that
// ascending order.
func seedExpiredRetentionFixtureUsers(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo ProfileRepository, n int) []string {
	t.Helper()
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		suffix := fmt.Sprintf("page_%d_%d", time.Now().UnixNano(), i)
		expiresAt := expiredRetentionSweepEpoch.Add(time.Duration(i) * time.Minute)
		user := createRetentionFixtureUser(t, ctx, pool, repo, suffix, expiresAt, nil, nil)
		// Issue #1569 T1: the sweep refuses any expired user who was not actually warned at 30 and
		// 7 days, so a seed with NULL markers is a WITHHELD account and every paging/cap assertion
		// below would run against an empty candidate set.
		markRetentionNoticesDelivered(t, ctx, pool, user.ID, expiresAt)
		ids[i] = user.ID
	}
	return ids
}

// seedTiedExpiryFixtureUsers creates n users that all share the SAME retention_expires_at, so
// ORDER BY retention_expires_at alone cannot order them and the `id` component of the sort key is
// the only thing that makes the order total. Returns the ids sorted the way the query must return
// them: ascending by id, since the timestamp component ties for every row.
//
// This is the retention-side counterpart of the item sweep's
// TestGetItemsForCleanupPage_Integration_AllNullSortKeys_IdTieBreakerMakesOrderTotal. Ties are not
// hypothetical here: every writer of retention_expires_at computes it in Postgres as
// clock_timestamp() + make_interval(months => n) (see RepairRetentionClock), so a batch of users
// whose retention clocks were armed by the same statement — or by the same
// StampMissingPlaidCleanupMarkers-style bulk UPDATE — shares a timestamp exactly.
func seedTiedExpiryFixtureUsers(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo ProfileRepository, n int) []string {
	t.Helper()
	// One shared instant, distinct from the increasing-epoch seeds above so the two suites cannot
	// interfere with each other's ordering assumptions.
	expiresAt := expiredRetentionSweepEpoch.Add(-48 * time.Hour)
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		suffix := fmt.Sprintf("tied_%d_%d", time.Now().UnixNano(), i)
		user := createRetentionFixtureUser(t, ctx, pool, repo, suffix, expiresAt, nil, nil)
		markRetentionNoticesDelivered(t, ctx, pool, user.ID, expiresAt) // Issue #1569 T1, as above
		ids[i] = user.ID
	}
	sort.Strings(ids)
	return ids
}

// assertSeededRowsAreOldestCandidates proves the precondition the cap assertions rely on: the
// first len(want) rows of the real candidate set, in this query's own (retention_expires_at, id)
// order, are exactly the seeded rows. Written as a direct SQL copy of
// GetUsersWithExpiredRetentionPage's predicate rather than a call to it, so it stays a genuinely
// independent check of the seed rather than an assertion about the method under test.
func assertSeededRowsAreOldestCandidates(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want []string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT id FROM users
		WHERE retention_expires_at IS NOT NULL
		  AND retention_expires_at <= NOW()
		  AND subscription_status IN ('inactive', 'cancelled')
		  AND is_deleted = FALSE
		  AND deletion_requested_at IS NULL
		  AND (manual_tier_override IS NULL OR manual_tier_override = '')
		  -- Issue #1569 T1's notice gate, copied here for the same reason as every other clause in
		  -- this hand-written twin: it must stay an independent restatement of the real predicate,
		  -- so that if the two disagree this precondition check is what says so.
		  AND retention_notice_30d_sent_at IS NOT NULL
		  AND retention_notice_7d_sent_at IS NOT NULL
		ORDER BY retention_expires_at ASC, id ASC
		LIMIT $1`, len(want))
	require.NoError(t, err)
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		got = append(got, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, want, got,
		"test precondition: the seeded rows must be the OLDEST candidates, or the cap would be "+
			"spent on someone else's leaked fixture and the cap assertions below would prove nothing")
}

// runExpiredRetentionSweep walks pages exactly the way
// RetentionCleanupCheckWorker.deleteExpiredAccounts does: nil cursor, advance to the last row of
// each page, stop on a short page (exhausted) or once the total processed reaches sweepCap.
// Returns the ids processed, in order.
func runExpiredRetentionSweep(t *testing.T, ctx context.Context, repo *profileRepository, pageSize, sweepCap int) []string {
	t.Helper()
	var cursor *RetentionSweepCursor
	var processed []string
	for {
		page, err := repo.GetUsersWithExpiredRetentionPage(ctx, cursor, pageSize)
		require.NoError(t, err)
		if len(page) == 0 {
			return processed
		}

		// Boundedness: no single query call may return more than the requested limit. This is the
		// assertion a missing LIMIT trips first.
		require.LessOrEqual(t, len(page), pageSize,
			"a single GetUsersWithExpiredRetentionPage call must never return more than the requested limit")

		fetched := len(page)
		if remaining := sweepCap - len(processed); fetched >= remaining {
			if remaining <= 0 {
				return processed
			}
			page = page[:remaining]
		}
		for _, u := range page {
			processed = append(processed, u.ID)
		}

		last := page[len(page)-1]
		next := RetentionSweepCursor{ExpiresAt: *last.RetentionExpiresAt, UserID: last.ID}
		if cursor != nil {
			advanced := next.ExpiresAt.After(cursor.ExpiresAt) ||
				(next.ExpiresAt.Equal(cursor.ExpiresAt) && next.UserID > cursor.UserID)
			require.True(t, advanced,
				"cursor must advance strictly in (retention_expires_at, id) order: (%s, %s) does not come after (%s, %s)",
				next.ExpiresAt, next.UserID, cursor.ExpiresAt, cursor.UserID)
		}
		cursor = &next

		if len(processed) >= sweepCap || fetched < pageSize {
			return processed
		}
	}
}

// TestGetUsersWithExpiredRetentionPage_Integration_CapBoundsSweepAndSecondSweepAdvances is the
// failing-first proof for Issue #1478. It seeds MORE expired-retention rows than the per-sweep
// cap and asserts:
//
//	(a) one sweep processes EXACTLY the cap, never the whole candidate set (today's
//	    GetUsersWithExpiredRetention has no LIMIT at all, so today's sweep hard-deletes every
//	    row it finds);
//	(b) the rows it processes are the oldest `cap` rows, in (retention_expires_at, id) order;
//	(c) a SECOND sweep, after the first sweep's rows are actually gone (which is what
//	    ExecuteDeletion does — DeleteUserCascade hard-deletes the users row), advances past them
//	    and reaches the row ranked beyond the first sweep's cap.
//
// (c) is the bound THIS test can honestly claim, and it is a claim about the QUERY only: the second
// sweep here advances because the first sweep's rows are genuinely gone, not because anything was
// persisted. That was once the whole story — every sweep restarted from a nil cursor, so a set of
// eligible-but-never-completing rows larger than the cap starved everything past it forever.
// Issue #1477 changed the CALLER, not this query: RetentionCleanupCheckWorker.deleteExpiredAccounts
// now persists where a capped sweep stopped (sweep_cursors) and passes it back as `after`. The
// cross-run behaviour is therefore proved in queue/jobs, not here — see
// retention_cleanup_check_sweep_cursor_integration_test.go — and the guarantee, with its bound, is
// stated once in repository/sweep_cursor_repository.go. Do not restate it as "no starvation".
func TestGetUsersWithExpiredRetentionPage_Integration_CapBoundsSweepAndSecondSweepAdvances(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	const pageSize = 2
	const sweepCap = 4
	const seeded = 6 // deliberately MORE rows than the cap

	ids := seedExpiredRetentionFixtureUsers(t, ctx, pool, repo, seeded)
	assertSeededRowsAreOldestCandidates(t, ctx, pool, ids)

	first := runExpiredRetentionSweep(t, ctx, repo, pageSize, sweepCap)

	require.Len(t, first, sweepCap,
		"ONE sweep must process exactly the per-sweep cap, not the whole candidate set — this is the "+
			"blast-radius bound Issue #1478 is about, and the unbounded GetUsersWithExpiredRetention "+
			"cannot satisfy it")
	assert.Equal(t, ids[:sweepCap], first,
		"the cap must be spent oldest-first, in (retention_expires_at, id) order")

	// Simulate what ExecuteDeletion(..., DeletionKindRetention) does to each processed row:
	// DeleteUserCascade hard-deletes the users row, so it stops matching this predicate entirely.
	for _, id := range first {
		_, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
		require.NoError(t, err)
	}

	second := runExpiredRetentionSweep(t, ctx, repo, pageSize, sweepCap)
	assert.Equal(t, ids[sweepCap:], second,
		"a second sweep must advance PAST the rows the first sweep already handled and reach the "+
			"rows ranked beyond the first sweep's cap — deferred continuation, not permanent truncation")
}

// TestGetUsersWithExpiredRetentionPage_Integration_MultiPageWithinOneSweep proves the cap is not
// doing all the work: within a SINGLE sweep whose cap exceeds the seeded count, the keyset cursor
// must page past the first page and reach every seeded row. A bare `LIMIT n` with no cursor would
// return the same oldest page forever.
func TestGetUsersWithExpiredRetentionPage_Integration_MultiPageWithinOneSweep(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	const pageSize = 2
	const seeded = 5

	ids := seedExpiredRetentionFixtureUsers(t, ctx, pool, repo, seeded)
	assertSeededRowsAreOldestCandidates(t, ctx, pool, ids)

	// A cap comfortably above the seeded count, so the walk stops only on exhaustion.
	processed := runExpiredRetentionSweep(t, ctx, repo, pageSize, seeded)
	assert.Equal(t, ids, processed,
		"every seeded row — including the ones ranked past the first page — must be reached within "+
			"ONE sweep, which requires the cursor to actually advance across pages")
}

// TestGetUsersWithExpiredRetentionPage_Integration_NoLimit_DefaultsToPositive proves the
// documented non-positive-limit fallback: limit <= 0 must not silently return zero rows or error.
func TestGetUsersWithExpiredRetentionPage_Integration_NoLimit_DefaultsToPositive(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	ids := seedExpiredRetentionFixtureUsers(t, ctx, pool, repo, 1)

	page, err := repo.GetUsersWithExpiredRetentionPage(ctx, nil, 0)
	require.NoError(t, err)

	found := false
	for _, u := range page {
		if u.ID == ids[0] {
			found = true
		}
	}
	assert.True(t, found, "limit<=0 must fall back to a sane positive default, not silently return nothing")
}

// walkAllExpiredRetentionUsers pages GetUsersWithExpiredRetentionPage to exhaustion and returns
// every user it selects. It reproduces the result SET that the unbounded GetUsersWithExpiredRetention
// returned before #1488 deleted it, and exists for tests that assert "this fixture user IS / IS NOT
// selected": these run against a SHARED database, so committed rows from other suites can fill any
// single page ahead of the fixture (the order is retention_expires_at ASC), and a bare one-page call
// would make such an assertion depend on unrelated fixture volume rather than on the predicate.
func walkAllExpiredRetentionUsers(t *testing.T, ctx context.Context, repo *profileRepository) []*domain.User {
	t.Helper()
	const pageSize = 100
	var all []*domain.User
	var cursor *RetentionSweepCursor
	for {
		page, err := repo.GetUsersWithExpiredRetentionPage(ctx, cursor, pageSize)
		require.NoError(t, err)
		all = append(all, page...)
		if len(page) < pageSize {
			return all
		}
		last := page[len(page)-1]
		cursor = &RetentionSweepCursor{ExpiresAt: *last.RetentionExpiresAt, UserID: last.ID}
	}
}

// TestGetUsersWithExpiredRetentionPage_Integration_KeepsManualOverrideExclusion pins that the
// paged form carries every part of the retention predicate — in
// particular noManualOverrideAtAllPredicate, whose absence would let a comped user (live OR
// lapsed-but-uncleared) be hard-deleted. That was this branch's only CRITICAL finding.
func TestGetUsersWithExpiredRetentionPage_Integration_KeepsManualOverrideExclusion(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	expiresAt := expiredRetentionSweepEpoch.Add(time.Minute)
	fixtures := setupRetentionFixtures(t, ctx, pool, repo, "expired_page", expiresAt)
	// Issue #1569 T1: fully warn all three, so the only thing that can exclude the ordinary user is
	// the manual-override rule this test exists to check.
	for _, u := range []*domain.User{fixtures.activeComp, fixtures.expiredComp, fixtures.ordinaryUser} {
		markRetentionNoticesDelivered(t, ctx, pool, u.ID, expiresAt)
	}

	assertRetentionSelection(t, walkAllExpiredRetentionUsers(t, ctx, repo), fixtures)
}

// TestGetUsersWithExpiredRetentionPage_Integration_TiedExpiry_IdTieBreakerMakesOrderTotal is the
// test that actually pins the `id` component of the sort key on the HARD-DELETE side. Every other
// test in this file seeds strictly increasing retention_expires_at values, so none of them creates a
// tie and none of them can detect `id` going missing from the ORDER BY or the keyset tuple —
// verified by mutation: removing it left all of them, and both full packages, green.
//
// With ties, `id` is load-bearing in both places at once:
//   - ORDER BY retention_expires_at alone leaves tied rows in an arbitrary, run-to-run-unstable
//     order, so consecutive pages are not guaranteed to be disjoint.
//   - A keyset predicate of `retention_expires_at > $1` alone SKIPS every remaining row that shares
//     the cursor's timestamp — in this sweep, that means eligible users are silently never deleted,
//     which is a GDPR erasure failure rather than a performance nit.
//
// The assertion is "exactly once", which catches both directions (a skipped row appears zero times,
// a repeated row more than once).
func TestGetUsersWithExpiredRetentionPage_Integration_TiedExpiry_IdTieBreakerMakesOrderTotal(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	const pageSize = 2
	const seeded = 5 // > pageSize, so the walk must cross pages WITHIN the tied group

	ids := seedTiedExpiryFixtureUsers(t, ctx, pool, repo, seeded)

	// Sanity-check the fixture itself: if these rows did not actually tie, this test would silently
	// degrade into a duplicate of the increasing-timestamp tests and prove nothing.
	var distinctExpiries int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT retention_expires_at) FROM users WHERE id = ANY($1)`, ids).Scan(&distinctExpiries))
	require.Equal(t, 1, distinctExpiries,
		"fixture precondition: all %d seeded rows must share ONE retention_expires_at, otherwise the "+
			"id tie-breaker is not being exercised at all", seeded)

	seen := map[string]int{}
	var order []string
	var cursor *RetentionSweepCursor
	pages := 0
	for {
		page, err := repo.GetUsersWithExpiredRetentionPage(ctx, cursor, pageSize)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		pages++
		require.LessOrEqual(t, pages, 5000, "runaway pagination — the cursor is not advancing")

		for _, u := range page {
			seen[u.ID]++
			for _, id := range ids {
				if u.ID == id {
					order = append(order, u.ID)
				}
			}
		}

		last := page[len(page)-1]
		next := RetentionSweepCursor{ExpiresAt: *last.RetentionExpiresAt, UserID: last.ID}
		if cursor != nil {
			advanced := next.ExpiresAt.After(cursor.ExpiresAt) ||
				(next.ExpiresAt.Equal(cursor.ExpiresAt) && next.UserID > cursor.UserID)
			require.True(t, advanced,
				"cursor must advance strictly even when the timestamp component TIES — that is exactly "+
					"what the id tie-breaker is for: (%s, %s) does not come after (%s, %s)",
				next.ExpiresAt, next.UserID, cursor.ExpiresAt, cursor.UserID)
		}
		cursor = &next

		if len(page) < pageSize {
			break
		}
	}

	for _, id := range ids {
		assert.Equal(t, 1, seen[id],
			"user %s (seeded, eligible, tied retention_expires_at) must be returned EXACTLY once "+
				"across the whole walk — zero means the keyset predicate skipped past a tied row and that "+
				"user is never deleted; more than once means pages overlap", id)
	}
	assert.Equal(t, ids, order,
		"within a tied group the rows must come back in ascending id order — the only total order "+
			"available when retention_expires_at is identical for every row")
}
