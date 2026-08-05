//go:build integration

package repository

// Integration tests for ProfileRepository.GetUsersScheduledForDeletionPage (Batch S2 Task 4).
//
// GetUsersScheduledForDeletion (no LIMIT, no id tie-breaker) fed a real hard delete (#1440) from a
// job now registered with RunOnStart: true (#1448) — see that method's own doc comment. A bare
// `LIMIT n` on its own would starve later users if an oldest row is permanently or long stuck
// (e.g. a continuously-renewed admin comp): the same oldest N rows would occupy every batch
// forever. GetUsersScheduledForDeletionPage's keyset cursor — ORDER BY (deletion_scheduled_at, id)
// plus a WHERE (deletion_scheduled_at, id) > (cursor) predicate — is what lets a caller loop past
// a stuck row within a single run.
//
// pgxmock cannot validate any of this: it only matches query text/arg shape, never whether the
// tuple comparison and ORDER BY tie-breaker actually produce a deterministic, non-overlapping
// sequence of pages across real rows.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetUsersScheduledForDeletionPage_Integration -tags integration -v -p 1

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedScheduledDeletionFixtureUsers creates n users, all eligible for deletion (deletion_scheduled_at
// in the past, is_deleted = FALSE), with strictly increasing deletion_scheduled_at so ORDER BY
// (deletion_scheduled_at, id) ASC gives a deterministic, known sequence. Returns the users in that
// same ascending order. Registers cleanup for both the users and any deletion_scheduled_at side
// effects.
func seedScheduledDeletionFixtureUsers(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo ProfileRepository, n int) []string {
	t.Helper()
	base := time.Now().Add(-24 * time.Hour) // safely in the past for every seeded row
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		suffix := fmt.Sprintf("%d-%d", time.Now().UnixNano(), i)
		user := createClaimForDeletionFixtureUser(t, ctx, pool, repo, suffix, false, nil, nil, nil)
		scheduledAt := base.Add(time.Duration(i) * time.Minute)
		require.NoError(t, repo.MarkForDeletion(ctx, user.ID, scheduledAt))
		ids[i] = user.ID
	}
	return ids
}

func TestGetUsersScheduledForDeletionPage_Integration_BoundedAndReachesLaterUsers(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	const limit = 2
	const seeded = 5 // deliberately MORE rows than the limit

	ids := seedScheduledDeletionFixtureUsers(t, ctx, pool, repo, seeded)
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	// Derive the page ceiling from the candidate population this query can ACTUALLY see, instead of
	// hand-picking a number. The predicate below is a copy of GetUsersScheduledForDeletionPage's
	// (profile_repository.go), so `candidates` counts exactly the rows the walk can return —
	// including rows this test did not create.
	//
	// Why this is not just "a bigger number": the previous ceiling was raised from 3-ish to 100 (5x)
	// citing leaked asof_threading_integration_test.go fixtures becoming deletion candidates. That
	// specific mechanism is impossible — those tests pass deletionRequestedAt=nil,
	// createClaimForDeletionFixtureUser writes that nil into deletion_scheduled_at, and this query
	// requires deletion_scheduled_at IS NOT NULL, so such a row can never be a candidate. The
	// coupling itself is real, though: several other integration suites in this package and in
	// internal/services DO create non-nil deletion_scheduled_at rows (deletion_cancel_marker,
	// delete_user_cascade, plaid_disconnect_repair, retention_clock_repair, claim_for_deletion's
	// &requestedAt cases, admin_user_repository_deletion_fields, deletion_service_episode), and any
	// of those leaking from a failed cleanup inflates the page count here. Counting the population
	// removes the guesswork entirely: the ceiling stays tight (4 pages when the table holds only
	// this test's 5 rows at limit 2) and cannot be inflated by an unrelated package's fixtures.
	//
	// Integration runs use -p 1 (see CLAUDE.md), so no other package inserts concurrently; the +1
	// page of slack covers a row whose deletion_scheduled_at falls between this COUNT's NOW() and a
	// later page's time.Now(), which would make it newly eligible mid-walk.
	var candidates int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users
		WHERE deletion_scheduled_at IS NOT NULL
		  AND deletion_scheduled_at <= NOW()
		  AND is_deleted = FALSE`).Scan(&candidates))
	require.GreaterOrEqual(t, candidates, seeded,
		"the candidate count must include at least this test's %d seeded rows — otherwise the seed "+
			"or the predicate copy above is wrong and the ceiling below is meaningless", seeded)
	maxPages := (candidates+limit-1)/limit + 1

	// Walk every page exactly the way ScheduledDeletionCheckWorker.Work does: start with a nil
	// cursor, advance to the last row of each page, stop once a page comes back short.
	var cursor *DeletionSweepCursor
	seen := map[string]bool{}
	var pageCount, pageSizeSum int
	for {
		page, err := repo.GetUsersScheduledForDeletionPage(ctx, cursor, limit)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		pageCount++
		pageSizeSum += len(page)

		// (a) Boundedness: no single query call may return more than `limit` rows.
		require.LessOrEqual(t, len(page), limit,
			"a single GetUsersScheduledForDeletionPage call must never return more than the requested limit")

		for _, u := range page {
			if idSet[u.ID] {
				seen[u.ID] = true
			}
		}

		last := page[len(page)-1]
		next := DeletionSweepCursor{ScheduledAt: *last.DeletionScheduledAt, UserID: last.ID}

		// (c) The direct runaway oracle: the cursor must move STRICTLY forward in
		// (deletion_scheduled_at, id) order on every iteration. A page count is only a proxy for
		// this; asserting it directly catches a non-advancing or backwards cursor on the very first
		// bad iteration instead of after N wasted pages, and it does not care how many rows the
		// shared database happens to hold.
		if cursor != nil {
			advanced := next.ScheduledAt.After(cursor.ScheduledAt) ||
				(next.ScheduledAt.Equal(cursor.ScheduledAt) && next.UserID > cursor.UserID)
			require.True(t, advanced,
				"cursor must advance strictly in (deletion_scheduled_at, id) order: (%s, %s) does not come after (%s, %s)",
				next.ScheduledAt, next.UserID, cursor.ScheduledAt, cursor.UserID)
		}
		cursor = &next

		if len(page) < limit {
			break // exhausted
		}
		// Ceiling derived above from the real candidate population (see the comment at maxPages),
		// so it stays tight without a fixture leak from an unrelated suite reading as a false
		// "cursor bug".
		require.LessOrEqual(t, pageCount, maxPages,
			"runaway pagination loop — something is wrong with the cursor advancing (%d candidate rows at limit %d allows at most %d pages)",
			candidates, limit, maxPages)
	}

	require.Greater(t, pageCount, 1,
		"seeding 5 rows against a page size of 2 must require MORE THAN ONE page call — otherwise "+
			"this test isn't actually exercising pagination")

	// (b) No starvation: EVERY seeded user — including the ones ranked well past the first
	// page/batch — must be reached within this single walk, not just the oldest `limit` rows.
	for _, id := range ids {
		assert.True(t, seen[id], "user %s (seeded, eligible for deletion) was never returned by any page — later users must not be starved by the page size", id)
	}
	assert.Equal(t, seeded, len(seen), "every seeded eligible user must be reached exactly once across all pages")
}

// TestGetUsersScheduledForDeletionPage_Integration_NoLimit_DefaultsTo500 proves the documented
// non-positive-limit fallback (profile_repository.go) without needing to seed 500+ rows: a
// limit of 0 must not silently return zero rows or error.
func TestGetUsersScheduledForDeletionPage_Integration_NoLimit_DefaultsTo500(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	ids := seedScheduledDeletionFixtureUsers(t, ctx, pool, repo, 1)

	page, err := repo.GetUsersScheduledForDeletionPage(ctx, nil, 0)
	require.NoError(t, err)

	found := false
	for _, u := range page {
		if u.ID == ids[0] {
			found = true
		}
	}
	assert.True(t, found, "limit<=0 must fall back to a sane positive default, not silently return nothing")
}
