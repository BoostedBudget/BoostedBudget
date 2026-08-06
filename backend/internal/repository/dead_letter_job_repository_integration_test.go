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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestDeadLetterJobRepository_ReleaseRetryClaim_Integration exercises the claim/release round trip
// against a real planner (#1492 part B). pgxmock matches statements as text and never evaluates a
// WHERE clause, so it cannot tell whether the retry_job_id = 0 guard works — and that guard is the
// only thing standing between "release a claim whose enqueue failed" and "un-retry a row whose retry
// is running".
func TestDeadLetterJobRepository_ReleaseRetryClaim_Integration(t *testing.T) {
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

	t.Run("releases a placeholder claim so the row is retryable again", func(t *testing.T) {
		id := insertRow(t)

		claimed, err := repo.AtomicMarkRetried(ctx, id, 0)
		require.NoError(t, err)
		require.True(t, claimed)

		require.NoError(t, repo.ReleaseRetryClaim(ctx, id))

		retriedAt, retryJobID := readClaim(t, id)
		assert.Nil(t, retriedAt, "retried_at must be NULL again or the row's one retry stays consumed")
		assert.Nil(t, retryJobID)

		// The proof that "retryable again" is not just a column value: the CAS accepts a new claim.
		reclaimed, err := repo.AtomicMarkRetried(ctx, id, 0)
		require.NoError(t, err)
		assert.True(t, reclaimed, "AtomicMarkRetried must be able to claim the released row again")
	})

	t.Run("leaves a completed retry alone", func(t *testing.T) {
		id := insertRow(t)

		claimed, err := repo.AtomicMarkRetried(ctx, id, 0)
		require.NoError(t, err)
		require.True(t, claimed)
		// The success path overwrites the 0 placeholder with the real River job ID.
		require.NoError(t, repo.MarkRetried(ctx, id, 4242))

		require.NoError(t, repo.ReleaseRetryClaim(ctx, id))

		retriedAt, retryJobID := readClaim(t, id)
		require.NotNil(t, retriedAt, "a row with a real retry job must stay retried")
		require.NotNil(t, retryJobID)
		assert.Equal(t, int64(4242), *retryJobID)
	})
}

func TestListDLQSort_Integration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewDeadLetterJobRepository(pool)
	ctx := context.Background()

	t.Run("retried_at asc NULLS LAST puts non-null values before nulls", func(t *testing.T) {
		filters := domain.DeadLetterJobFilters{SortBy: "retried_at", SortDir: "asc"}
		jobs, _, err := repo.ListAll(ctx, filters, 100, 0)
		require.NoError(t, err)

		// Once we see a NULL retried_at, all subsequent rows must also be NULL (NULLS LAST).
		seenNull := false
		for _, j := range jobs {
			if j.RetriedAt == nil {
				seenNull = true
			}
			if seenNull {
				assert.Nil(t, j.RetriedAt, "expected all NULL retried_at rows after first NULL (NULLS LAST)")
			}
		}
	})

	t.Run("kind asc produces alphabetical order", func(t *testing.T) {
		filters := domain.DeadLetterJobFilters{SortBy: "kind", SortDir: "asc"}
		jobs, _, err := repo.ListAll(ctx, filters, 100, 0)
		require.NoError(t, err)
		for i := 1; i < len(jobs); i++ {
			assert.LessOrEqual(t, jobs[i-1].Kind, jobs[i].Kind,
				"expected ascending alphabetical kind order")
		}
	})
}
