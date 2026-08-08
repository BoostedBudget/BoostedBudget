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
