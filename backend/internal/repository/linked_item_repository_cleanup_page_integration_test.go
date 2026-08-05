//go:build integration

package repository

// Integration tests for LinkedItemRepository.GetItemsForCleanupPage (Issue #1469).
//
// GetItemsForCleanup (no LIMIT, and ORDER BY li.last_error_at ASC NULLS LAST, li.last_synced_at ASC
// NULLS LAST with NO id tie-breaker) is consumed whole by ItemCleanupCheckWorker.Work
// (internal/queue/jobs/item_cleanup_check.go), which marks every returned row for cleanup and
// enqueues an ItemCleanup job per row. That job calls Plaid /item/remove — irreversible. The job is
// registered with RunOnStart: true (ItemCleanupCheckPeriodicOpts), so the size of that batch on the
// first run after every deploy is whatever the table happens to hold. Same defect class as #1478
// and as the already-fixed GetUsersScheduledForDeletionPage.
//
// The id tie-breaker matters more here than for either user sweep: both existing sort columns are
// NULLABLE, and the "zero active accounts" leg of the predicate matches rows that have never
// errored and never synced, so last_error_at AND last_synced_at can BOTH be NULL for many rows at
// once. Paginating on a non-unique sort key skips and repeats rows; adding li.id makes the order
// total.
//
// The ASC NULLS LAST ordering is preserved exactly, expressed as
// COALESCE(col, 'infinity'::timestamptz) ASC so the same expression can appear inside a row-
// comparison keyset predicate (a tuple comparison has no per-column NULLS clause). For timestamptz
// the two are equivalent: 'infinity' sorts after every finite value and no stored value equals it
// (nothing in this codebase writes an infinite timestamp — every writer of last_error_at /
// last_synced_at passes a Go time.Time or NULL).
//
// pgxmock cannot validate any of this: it matches query text and argument shape only, never whether
// the tuple comparison over two nullable columns plus a uuid actually produces a deterministic,
// non-overlapping page sequence.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetItemsForCleanupPage_Integration -tags integration -v -p 1

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

const (
	cleanupPageErrorDays = 30
	cleanupPageStaleDays = 60
)

// newCleanupPageTestTx begins a transaction, registers a rollback, and returns the CONCRETE
// repository (not the wide interface) so the paged method — which is deliberately not on the wide
// repository.LinkedItemRepository interface — is reachable. Rolling back means these tests leave no
// residue for other suites to trip over, which matters here because every linked_items row with a
// NULL cleanup_scheduled_at and no active accounts is a cleanup candidate.
func newCleanupPageTestTx(t *testing.T, ctx context.Context) (pgx.Tx, *linkedItemRepository, ProfileRepository) {
	t.Helper()
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	return tx, NewLinkedItemRepository(tx, enc), NewProfileRepository(tx, enc)
}

// seedCleanupCandidates creates one user and n 'revoked' linked items (revoked is an unconditional
// cleanup-candidate leg of GetItemsForCleanup's predicate), with strictly increasing last_error_at
// anchored ten years in the past so the seeded rows are the OLDEST candidates in the table. Returns
// the ids in (last_error_at, last_synced_at, id) ascending order.
func seedCleanupCandidates(t *testing.T, ctx context.Context, tx pgx.Tx, repo *linkedItemRepository, profileRepo ProfileRepository, n int) []string {
	t.Helper()

	suffix := fmt.Sprintf("cleanuppage_%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_" + suffix,
		Email:              suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))

	base := time.Now().Add(-10 * 365 * 24 * time.Hour)
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		item := &domain.LinkedItem{
			UserID:          user.ID,
			ItemID:          fmt.Sprintf("plaid-item-%s-%d", suffix, i),
			AccessToken:     fmt.Sprintf("access-token-%s-%d", suffix, i),
			InstitutionID:   "ins_" + suffix,
			InstitutionName: "Test Bank",
			Provider:        "plaid",
		}
		require.NoError(t, repo.Create(ctx, item))
		_, err := tx.Exec(ctx, `
			UPDATE linked_items
			SET status = 'revoked', last_error_at = $1, last_synced_at = NULL, cleanup_scheduled_at = NULL
			WHERE id = $2`, base.Add(time.Duration(i)*time.Minute), item.ID)
		require.NoError(t, err)
		ids[i] = item.ID
	}
	return ids
}

// assertSeededItemsAreOldestCandidates proves the precondition the cap assertions rely on: the
// first len(want) rows of the real candidate set, in this query's own order, are exactly the seeded
// rows. Written as an independent SQL copy of GetItemsForCleanupPage's predicate rather than a call
// to the method under test, so a leaked candidate from another suite fails loudly instead of
// quietly consuming the cap and weakening the assertion.
func assertSeededItemsAreOldestCandidates(t *testing.T, ctx context.Context, tx pgx.Tx, want []string) {
	t.Helper()
	rows, err := tx.Query(ctx, `
		SELECT li.id FROM linked_items li
		WHERE li.cleanup_scheduled_at IS NULL
		  AND li.status NOT IN ('replaced', 'disconnected')
		  AND (
		      (li.status IN ('error', 'login_required') AND li.last_error_at < NOW() - $1::interval)
		      OR li.status = 'revoked'
		      OR (li.last_synced_at IS NOT NULL AND li.last_synced_at < NOW() - $2::interval)
		      OR NOT EXISTS (SELECT 1 FROM accounts a WHERE a.linked_item_id = li.id AND a.status = 'active')
		  )
		ORDER BY COALESCE(li.last_error_at, 'infinity'::timestamptz) ASC,
		         COALESCE(li.last_synced_at, 'infinity'::timestamptz) ASC,
		         li.id ASC
		LIMIT $3`,
		fmt.Sprintf("%d days", cleanupPageErrorDays), fmt.Sprintf("%d days", cleanupPageStaleDays), len(want))
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
		"test precondition: the seeded items must be the OLDEST candidates, or the per-run cap would "+
			"be spent on another suite's leaked row and the cap assertions below would prove nothing")
}

// runItemCleanupSweep walks pages exactly the way ItemCleanupCheckWorker.Work does: nil cursor,
// advance to the last row of each page, stop on a short page (exhausted) or once the total
// processed reaches sweepCap. Returns the ids processed, in order.
func runItemCleanupSweep(t *testing.T, ctx context.Context, repo *linkedItemRepository, pageSize, sweepCap int) []string {
	t.Helper()
	var cursor *ItemCleanupSweepCursor
	var processed []string
	for {
		page, err := repo.GetItemsForCleanupPage(ctx, cleanupPageErrorDays, cleanupPageStaleDays, cursor, pageSize)
		require.NoError(t, err)
		if len(page) == 0 {
			return processed
		}

		// Boundedness: no single query call may return more than the requested limit. This is the
		// assertion a missing LIMIT trips first.
		require.LessOrEqual(t, len(page), pageSize,
			"a single GetItemsForCleanupPage call must never return more than the requested limit")

		fetched := len(page)
		if remaining := sweepCap - len(processed); fetched >= remaining {
			if remaining <= 0 {
				return processed
			}
			page = page[:remaining]
		}
		for _, it := range page {
			processed = append(processed, it.ID)
		}

		last := page[len(page)-1]
		next := ItemCleanupSweepCursor{LastErrorAt: last.LastErrorAt, LastSyncedAt: last.LastSyncedAt, ItemID: last.ID}
		if cursor != nil {
			require.True(t, itemCleanupCursorAdvanced(*cursor, next),
				"cursor must advance strictly in (last_error_at, last_synced_at, id) order (NULL sorting last): %+v does not come after %+v",
				next, *cursor)
		}
		cursor = &next

		if len(processed) >= sweepCap || fetched < pageSize {
			return processed
		}
	}
}

// itemCleanupCursorAdvanced reports whether `next` is strictly after `prev` in the same total order
// GetItemsForCleanupPage sorts by: (last_error_at, last_synced_at, id) ascending with NULL last.
// NULL-last is modelled the same way the SQL models it — a NULL compares greater than any value.
func itemCleanupCursorAdvanced(prev, next ItemCleanupSweepCursor) bool {
	if c := compareNullLast(prev.LastErrorAt, next.LastErrorAt); c != 0 {
		return c < 0
	}
	if c := compareNullLast(prev.LastSyncedAt, next.LastSyncedAt); c != 0 {
		return c < 0
	}
	return prev.ItemID < next.ItemID
}

// compareNullLast returns -1/0/1 for a<b, a==b, a>b treating nil as greater than every value.
func compareNullLast(a, b *time.Time) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	case a.Before(*b):
		return -1
	case a.After(*b):
		return 1
	default:
		return 0
	}
}

// TestGetItemsForCleanupPage_Integration_CapBoundsSweepAndSecondSweepAdvances is the failing-first
// proof for Issue #1469. It seeds MORE cleanup candidates than the per-run cap and asserts that one
// run processes exactly the cap (today's GetItemsForCleanup has no LIMIT, so today's run marks and
// enqueues every candidate it finds), that the cap is spent in the query's own order, and that a
// second run advances past the rows the first run already scheduled.
//
// The second run reaches them for a structural reason, not a timing one: MarkForCleanup sets
// cleanup_scheduled_at, and the predicate requires cleanup_scheduled_at IS NULL, so a successfully
// marked row leaves the candidate set immediately. The test simulates that with the same UPDATE
// MarkForCleanup performs.
func TestGetItemsForCleanupPage_Integration_CapBoundsSweepAndSecondSweepAdvances(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	const pageSize = 2
	const sweepCap = 4
	const seeded = 6 // deliberately MORE rows than the cap

	ids := seedCleanupCandidates(t, ctx, tx, repo, profileRepo, seeded)
	assertSeededItemsAreOldestCandidates(t, ctx, tx, ids)

	first := runItemCleanupSweep(t, ctx, repo, pageSize, sweepCap)
	require.Len(t, first, sweepCap,
		"ONE run must process exactly the per-run cap, not the whole candidate set — that bound is "+
			"what Issue #1469 is about, and the unbounded GetItemsForCleanup cannot provide it")
	assert.Equal(t, ids[:sweepCap], first,
		"the cap must be spent in (last_error_at, last_synced_at, id) order")

	// What MarkForCleanup does to every row the run handled: stamps cleanup_scheduled_at, which
	// removes the row from this predicate entirely.
	for _, id := range first {
		_, err := tx.Exec(ctx, `UPDATE linked_items SET cleanup_scheduled_at = NOW() WHERE id = $1`, id)
		require.NoError(t, err)
	}

	second := runItemCleanupSweep(t, ctx, repo, pageSize, sweepCap)
	assert.Equal(t, ids[sweepCap:], second,
		"a second run must advance PAST the rows the first run already scheduled and reach the rows "+
			"ranked beyond the first run's cap")
}

// TestGetItemsForCleanupPage_Integration_MultiPageWithinOneRun proves the cap is not doing all the
// work: within a SINGLE run whose cap exceeds the seeded count, the keyset cursor must page past the
// first page and reach every seeded row. A bare `LIMIT n` with no cursor would return the same
// oldest page forever.
func TestGetItemsForCleanupPage_Integration_MultiPageWithinOneRun(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	const pageSize = 2
	const seeded = 5

	ids := seedCleanupCandidates(t, ctx, tx, repo, profileRepo, seeded)
	assertSeededItemsAreOldestCandidates(t, ctx, tx, ids)

	processed := runItemCleanupSweep(t, ctx, repo, pageSize, seeded)
	assert.Equal(t, ids, processed,
		"every seeded candidate — including the ones ranked past the first page — must be reached "+
			"within ONE run, which requires the cursor to actually advance across pages")
}

// TestGetItemsForCleanupPage_Integration_AllNullSortKeys_IdTieBreakerMakesOrderTotal is the test
// that specifically justifies the id tie-breaker. It seeds candidates whose last_error_at AND
// last_synced_at are both NULL — reachable in production via the "zero active accounts" leg of the
// predicate, for an item that has never errored and never synced — so the two pre-existing sort
// columns provide NO ordering at all between them. Without li.id in both the ORDER BY and the
// keyset predicate, paging over such a group skips or repeats rows.
func TestGetItemsForCleanupPage_Integration_AllNullSortKeys_IdTieBreakerMakesOrderTotal(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	const pageSize = 2
	const seeded = 5

	ids := seedCleanupCandidates(t, ctx, tx, repo, profileRepo, seeded)
	// Null out BOTH sort keys for every seeded row: now only li.id can order them.
	_, err := tx.Exec(ctx, `UPDATE linked_items SET last_error_at = NULL, last_synced_at = NULL WHERE id = ANY($1)`, ids)
	require.NoError(t, err)

	// With both sort keys NULL these rows now sort LAST, not first, so the "seeded rows are the
	// oldest candidates" precondition no longer applies. Assert on set membership within a
	// generous, cap-free walk instead, and require every seeded row to appear exactly once.
	seen := map[string]int{}
	var cursor *ItemCleanupSweepCursor
	pages := 0
	for {
		page, pageErr := repo.GetItemsForCleanupPage(ctx, cleanupPageErrorDays, cleanupPageStaleDays, cursor, pageSize)
		require.NoError(t, pageErr)
		if len(page) == 0 {
			break
		}
		pages++
		require.LessOrEqual(t, pages, 5000, "runaway pagination — the cursor is not advancing")
		for _, it := range page {
			seen[it.ID]++
		}
		last := page[len(page)-1]
		next := ItemCleanupSweepCursor{LastErrorAt: last.LastErrorAt, LastSyncedAt: last.LastSyncedAt, ItemID: last.ID}
		if cursor != nil {
			require.True(t, itemCleanupCursorAdvanced(*cursor, next),
				"cursor must advance strictly even when BOTH timestamp sort keys are NULL — that is exactly what the li.id tie-breaker is for")
		}
		cursor = &next
		if len(page) < pageSize {
			break
		}
	}

	for _, id := range ids {
		assert.Equal(t, 1, seen[id],
			"item %s must be returned EXACTLY once across the whole walk — a non-total sort order "+
				"makes keyset pagination skip or repeat rows", id)
	}
}

// TestGetItemsForCleanupPage_Integration_NoLimit_DefaultsToPositive proves the documented
// non-positive-limit fallback: limit <= 0 must not silently return zero rows or error.
func TestGetItemsForCleanupPage_Integration_NoLimit_DefaultsToPositive(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	ids := seedCleanupCandidates(t, ctx, tx, repo, profileRepo, 1)

	page, err := repo.GetItemsForCleanupPage(ctx, cleanupPageErrorDays, cleanupPageStaleDays, nil, 0)
	require.NoError(t, err)

	found := false
	for _, it := range page {
		if it.ID == ids[0] {
			found = true
		}
	}
	assert.True(t, found, "limit<=0 must fall back to a sane positive default, not silently return nothing")
}

// TestGetItemsForCleanupPage_Integration_MatchesUnpagedPredicate pins that the paged form selects
// the same SET of rows the unbounded GetItemsForCleanup does — no predicate leg was dropped or
// tightened while adding the LIMIT and the cursor. Compared as sets over the seeded ids only, since
// the two calls are made at slightly different NOW() values.
func TestGetItemsForCleanupPage_Integration_MatchesUnpagedPredicate(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	const seeded = 5
	ids := seedCleanupCandidates(t, ctx, tx, repo, profileRepo, seeded)

	unpaged, err := repo.GetItemsForCleanup(ctx, cleanupPageErrorDays, cleanupPageStaleDays)
	require.NoError(t, err)
	unpagedSeeded := map[string]bool{}
	for _, it := range unpaged {
		unpagedSeeded[it.ID] = true
	}

	paged := runItemCleanupSweep(t, ctx, repo, 2, seeded)
	pagedSeeded := map[string]bool{}
	for _, id := range paged {
		pagedSeeded[id] = true
	}

	for _, id := range ids {
		assert.True(t, unpagedSeeded[id], "sanity: the unbounded query must select seeded item %s", id)
		assert.True(t, pagedSeeded[id], "the paged query must select the same seeded item %s the unbounded one does", id)
	}
}
