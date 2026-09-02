//go:build integration

package repository

// Integration tests for dead letter job repository sort.
// These tests execute SQL against a real Postgres planner and verify that the
// dynamic ORDER BY clause (sort_by/sort_dir) produces correct ordering.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestListDLQSort_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestDeadLetterJobRepository_RetryClaimInTx_Integration exercises the claim against a real planner.
// pgxmock matches statements as text and never evaluates a WHERE clause or a transaction boundary,
// so it can see neither the `retried_at IS NULL` compare-and-set nor the fact that an uncommitted
// claim leaves the row untouched — and those two are the whole mechanism (#1492 part B, #1508).
func TestDeadLetterJobRepository_RetryClaimInTx_Integration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewDeadLetterJobRepository(pool)
	ctx := context.Background()

	insertRow := func(t *testing.T) int64 {
		t.Helper()
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO dead_letter_jobs (original_job_id, kind, queue, args, errors, max_attempts, created_at, failed_at)
			VALUES (0, 'release_retry_claim_test', 'default', '{}'::jsonb, '[]'::jsonb, 5, NOW(), NOW())
			RETURNING id`).Scan(&id)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `DELETE FROM dead_letter_jobs WHERE id = $1`, id)
		})
		return id
	}

	readClaim := func(t *testing.T, id int64) (retriedAt *time.Time, retryJobID *int64) {
		t.Helper()
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT retried_at, retry_job_id FROM dead_letter_jobs WHERE id = $1`, id).
			Scan(&retriedAt, &retryJobID))
		return retriedAt, retryJobID
	}

	t.Run("a rolled-back claim leaves the row retryable", func(t *testing.T) {
		id := insertRow(t)

		tx, err := repo.BeginRetryTx(ctx)
		require.NoError(t, err)
		claimed, err := repo.AtomicMarkRetriedInTx(ctx, tx, id, 0)
		require.NoError(t, err)
		require.True(t, claimed)
		require.NoError(t, tx.Rollback(ctx))

		retriedAt, retryJobID := readClaim(t, id)
		assert.Nil(t, retriedAt, "retried_at must be NULL or the row's one retry stays consumed")
		assert.Nil(t, retryJobID)

		// The proof that "retryable" is not just a column value: the CAS accepts a new claim.
		tx2, err := repo.BeginRetryTx(ctx)
		require.NoError(t, err)
		defer func() { _ = tx2.Rollback(ctx) }()
		reclaimed, err := repo.AtomicMarkRetriedInTx(ctx, tx2, id, 0)
		require.NoError(t, err)
		assert.True(t, reclaimed, "the compare-and-set must be able to claim the rolled-back row again")
	})

	t.Run("a committed claim carries the real job id and refuses a second claim", func(t *testing.T) {
		id := insertRow(t)

		tx, err := repo.BeginRetryTx(ctx)
		require.NoError(t, err)
		claimed, err := repo.AtomicMarkRetriedInTx(ctx, tx, id, 0)
		require.NoError(t, err)
		require.True(t, claimed)
		// The success path overwrites the 0 placeholder with the real River job ID, in the same
		// transaction — so no committed row is ever observable at 0.
		require.NoError(t, repo.MarkRetriedInTx(ctx, tx, id, 4242))
		require.NoError(t, tx.Commit(ctx))

		retriedAt, retryJobID := readClaim(t, id)
		require.NotNil(t, retriedAt, "a row with a real retry job must stay retried")
		require.NotNil(t, retryJobID)
		assert.Equal(t, int64(4242), *retryJobID)

		tx2, err := repo.BeginRetryTx(ctx)
		require.NoError(t, err)
		defer func() { _ = tx2.Rollback(ctx) }()
		reclaimed, err := repo.AtomicMarkRetriedInTx(ctx, tx2, id, 0)
		require.NoError(t, err)
		assert.False(t, reclaimed, "the single-shot property must survive the move into a transaction")
	})
}

// TestListDLQSort_Integration exercises the dynamic `ORDER BY %s %s%s` in ListAll — including the
// NULLS LAST that only the nullable `retried_at` column carries — against a real planner.
//
// ⚠ #1569 r17: this test SEEDS. It used to assert over whatever `dead_letter_jobs` happened to
// hold, and every INSERT in this file belongs to a *different* top-level test that pairs it with a
// `t.Cleanup` DELETE — so by the time this test ran the table was empty, both loops iterated zero
// times, and the test could not fail for an inverted direction, a dropped ORDER BY, or a dropped
// NULLS LAST. The old `seenNull` form had a second, independent hole: it asserted `Nil` on the very
// row that SET `seenNull`, so it was trivially satisfied by any single-row (or zero-row) result.
//
// Isolation: the fixtures are stamped with a unique far-future `failed_at` window and every call
// filters on that window, so the assertions are about EXACTLY these rows on a shared database.
// The window is in the FUTURE deliberately — a past one would make these rows the oldest
// unresolved jobs in the table and perturb GetOldestJobAge for the age-alarm test below.
func TestListDLQSort_Integration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewDeadLetterJobRepository(pool)
	// ⚠ NOT t.Context(): the cleanup below must outlive this test's own context, or the fixture rows
	// leak into the shared integration database and corrupt OTHER packages' whole-table aggregates.
	ctx := context.Background()

	nonce := time.Now().UnixNano()
	kindPrefix := fmt.Sprintf("dlq_sort_%d_", nonce)
	base := time.Now().UTC().
		Add(1000 * 24 * time.Hour).
		Add(time.Duration(nonce%1_000_000) * time.Second).
		Truncate(time.Millisecond)
	windowStart := base.Add(-time.Minute)
	windowEnd := base.Add(24 * time.Hour)

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM dead_letter_jobs WHERE kind LIKE $1`, kindPrefix+"%")
	})

	// Every fixture carries a DISTINCT kind and a DISTINCT (or NULL) retried_at: equal sort keys
	// satisfy an ordering assertion in any order, so uniform values would make the whole test
	// cosmetic. Insertion order deliberately matches NEITHER expected output order.
	seeds := []struct {
		kind      string
		retriedAt *time.Time
		retryJob  *int64
	}{
		{kindPrefix + "alpha", ptrTime(base.Add(2 * time.Hour)), ptrInt64(101)},
		{kindPrefix + "bravo", nil, nil},
		{kindPrefix + "charlie", ptrTime(base.Add(1 * time.Hour)), ptrInt64(102)},
		{kindPrefix + "delta", nil, nil},
	}
	for _, sd := range seeds {
		_, err := pool.Exec(ctx, `
			INSERT INTO dead_letter_jobs (original_job_id, kind, queue, args, errors, max_attempts,
			                              created_at, failed_at, retried_at, retry_job_id)
			VALUES (0, $1, 'default', '{}'::jsonb, '[]'::jsonb, 5, $2, $2, $3, $4)`,
			sd.kind, base, sd.retriedAt, sd.retryJob)
		require.NoError(t, err)
	}

	window := func(sortBy, sortDir string) domain.DeadLetterJobFilters {
		return domain.DeadLetterJobFilters{
			StartDate: &windowStart,
			EndDate:   &windowEnd,
			SortBy:    sortBy,
			SortDir:   sortDir,
		}
	}
	kinds := func(jobs []*domain.DeadLetterJob) []string {
		out := make([]string, 0, len(jobs))
		for _, j := range jobs {
			out = append(out, j.Kind)
		}
		return out
	}
	nullness := func(jobs []*domain.DeadLetterJob) []bool {
		out := make([]bool, 0, len(jobs))
		for _, j := range jobs {
			out = append(out, j.RetriedAt == nil)
		}
		return out
	}

	t.Run("retried_at asc NULLS LAST puts non-null values before nulls", func(t *testing.T) {
		jobs, total, err := repo.ListAll(ctx, window("retried_at", "asc"), 100, 0)
		require.NoError(t, err)
		// The floor: without it every assertion below is vacuous again the moment seeding breaks.
		require.Len(t, jobs, len(seeds), "the fixtures must be visible or the ordering assertion proves nothing")
		require.Equal(t, len(seeds), total)

		assert.Equal(t, []bool{false, false, true, true}, nullness(jobs),
			"NULL retried_at rows must sort last under ASC")
		// The two non-null rows must additionally be in ascending order among themselves. The two
		// NULL rows have no defined order relative to each other, so only their position is pinned.
		assert.Equal(t, []string{kindPrefix + "charlie", kindPrefix + "alpha"}, kinds(jobs)[:2],
			"expected ascending retried_at among the non-null rows")
	})

	// ⚠ SIBLING, MANDATORY: DESC is the direction where NULLS LAST actually does work. PostgreSQL's
	// default for DESC is NULLS FIRST, so dropping the explicit ` NULLS LAST` changes DESC and
	// leaves ASC untouched — an ASC-only test cannot see that mutation at all.
	t.Run("retried_at desc still puts nulls last", func(t *testing.T) {
		jobs, _, err := repo.ListAll(ctx, window("retried_at", "desc"), 100, 0)
		require.NoError(t, err)
		require.Len(t, jobs, len(seeds))

		assert.Equal(t, []bool{false, false, true, true}, nullness(jobs),
			"NULL retried_at rows must sort last under DESC too — that is what NULLS LAST buys")
		assert.Equal(t, []string{kindPrefix + "alpha", kindPrefix + "charlie"}, kinds(jobs)[:2],
			"expected descending retried_at among the non-null rows")
	})

	t.Run("kind asc produces alphabetical order", func(t *testing.T) {
		jobs, _, err := repo.ListAll(ctx, window("kind", "asc"), 100, 0)
		require.NoError(t, err)
		require.Len(t, jobs, len(seeds))

		assert.Equal(t, []string{
			kindPrefix + "alpha", kindPrefix + "bravo", kindPrefix + "charlie", kindPrefix + "delta",
		}, kinds(jobs), "expected ascending alphabetical kind order")
	})

	t.Run("kind desc produces reverse alphabetical order", func(t *testing.T) {
		jobs, _, err := repo.ListAll(ctx, window("kind", "desc"), 100, 0)
		require.NoError(t, err)
		require.Len(t, jobs, len(seeds))

		assert.Equal(t, []string{
			kindPrefix + "delta", kindPrefix + "charlie", kindPrefix + "bravo", kindPrefix + "alpha",
		}, kinds(jobs), "expected descending alphabetical kind order")
	})
}

func ptrTime(t time.Time) *time.Time { return &t }

func ptrInt64(v int64) *int64 { return &v }

// ─── Round 15, Tier E item 6: the stranded retry_job_id = 0 rows ─────────────────────────────────
//
// A row left by the pre-#1508 two-commit path has retried_at set and retry_job_id = 0 — a state
// BeginRetryTx's own comment calls undecidable. It was being read in the two most flattering ways at
// once: counted as a SUCCESS by GetStats' `retried` bucket, and filtered OUT of the age alarm by
// GetOldestJobAge's `WHERE retried_at IS NULL`. So a row whose replay demonstrably did not complete
// both inflated the recovery count and was invisible to the one signal that says "this has been
// sitting here too long".
//
// ⚠ INTEGRATION, NOT pgxmock, AND THAT IS THE POINT. pgxmock matches query TEXT and never evaluates
// a FILTER clause or an aggregate, so it cannot tell these two predicates apart at all — it would
// pass against either spelling. Only a real planner can answer this.
//
// ⚠ ASSERTED AS DELTAS, never as absolute totals: these are unfiltered whole-table aggregates and
// the integration database is shared, so an absolute count would be a false failure the moment
// another test's fixture existed alongside this one.
func TestDeadLetterJobRepository_StrandedRetryRows_Integration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewDeadLetterJobRepository(pool)
	// ⚠ NOT t.Context(): the cleanup below must outlive this test's own context, or the fixture rows
	// leak into the shared integration database and corrupt OTHER packages' whole-table aggregates.
	ctx := context.Background()

	// failed_at is set far in the past so this row is unambiguously the oldest unresolved one, which
	// is what makes the age assertion below a statement about THIS row rather than about whatever
	// else happens to be in the table.
	insertStranded := func(t *testing.T) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO dead_letter_jobs (original_job_id, kind, queue, args, errors, max_attempts, created_at, failed_at, retried_at, retry_job_id)
			VALUES (0, 'stranded_retry_test', 'default', '{}'::jsonb, '[]'::jsonb, 5,
			        NOW() - INTERVAL '400 days', NOW() - INTERVAL '400 days', NOW(), 0)
			RETURNING id`).Scan(&id))
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `DELETE FROM dead_letter_jobs WHERE id = $1`, id)
		})
		return id
	}

	insertGenuinelyRetried := func(t *testing.T) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO dead_letter_jobs (original_job_id, kind, queue, args, errors, max_attempts, created_at, failed_at, retried_at, retry_job_id)
			VALUES (0, 'genuine_retry_test', 'default', '{}'::jsonb, '[]'::jsonb, 5,
			        NOW() - INTERVAL '401 days', NOW() - INTERVAL '401 days', NOW(), 9977)
			RETURNING id`).Scan(&id))
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `DELETE FROM dead_letter_jobs WHERE id = $1`, id)
		})
		return id
	}

	t.Run("a stranded row is not counted as a successful retry", func(t *testing.T) {
		before, err := repo.GetStats(ctx)
		require.NoError(t, err)

		insertStranded(t)

		after, err := repo.GetStats(ctx)
		require.NoError(t, err)

		assert.Equal(t, before.Retried, after.Retried,
			"retry_job_id = 0 means the replay's id write never landed — counting it under `retried` reports a demonstrably incomplete replay as a recovery")
		assert.Equal(t, before.Total+1, after.Total,
			"it is still a dead letter job and must still be in the total — the fix must not make it disappear instead")
	})

	// ⚠ SIBLING, MANDATORY. Without it the assertion above would also pass against a predicate that
	// counted NOTHING as retried. This pins that a real replay still reads as a success.
	t.Run("a genuinely retried row is still counted as a successful retry", func(t *testing.T) {
		before, err := repo.GetStats(ctx)
		require.NoError(t, err)

		insertGenuinelyRetried(t)

		after, err := repo.GetStats(ctx)
		require.NoError(t, err)

		assert.Equal(t, before.Retried+1, after.Retried,
			"a row carrying a real River job id is a completed replay and must keep counting as one")
	})

	t.Run("a stranded row is visible to the age alarm", func(t *testing.T) {
		insertStranded(t)

		age, err := repo.GetOldestJobAge(ctx)
		require.NoError(t, err)

		// 400 days ≈ 9600 hours. The old `WHERE retried_at IS NULL` excluded this row entirely, so
		// the alarm reported whatever else was in the table — for a table holding only stranded rows,
		// zero.
		assert.GreaterOrEqual(t, age, int64(9000),
			"the one signal that says a dead letter job has been sitting too long must not be blind to the rows that are stuck with nobody looking at them")
	})

	// ⚠ SIBLING, MANDATORY, and it is the direction that would break the alarm outright: a genuinely
	// retried row is RESOLVED and must still be excluded, or the age alarm would fire forever on
	// every job the system successfully recovered.
	t.Run("a genuinely retried row stays excluded from the age alarm", func(t *testing.T) {
		insertGenuinelyRetried(t)

		age, err := repo.GetOldestJobAge(ctx)
		require.NoError(t, err)

		assert.Less(t, age, int64(9000),
			"a completed replay is resolved — including it would make the age alarm permanent on every recovered job")
	})
}
