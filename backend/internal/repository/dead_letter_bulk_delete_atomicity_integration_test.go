//go:build integration

package repository

// Integration coverage for the bulk-delete confirmation bound (issue #1432, batch EE
// re-review). pgxmock matches statements as text and evaluates neither a WHERE clause nor a
// transaction boundary, so the unit test in dead_letter_job_repository_test.go can only prove
// that the rollback PATH is taken. Whether the rows actually survive the abort — and whether
// a narrow, correctly-confirmed delete still removes exactly its own rows and nothing else —
// is a property of a real planner, and this is where it is checked.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -tags integration -p 1 \
//	  -run TestDeadLetterJobRepository_BulkDeleteAtomicity_Integration -v

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestDeadLetterJobRepository_BulkDeleteAtomicity_Integration(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewDeadLetterJobRepository(pool)
	ctx := context.Background()

	// A kind unique to this test keeps the filter from selecting another package's fixtures
	// on the shared integration database, and keeps this test from deleting them.
	const kind = "bulk_delete_atomicity_test"

	insertRows := func(t *testing.T, n int) []int64 {
		t.Helper()
		ids := make([]int64, 0, n)
		for i := 0; i < n; i++ {
			var id int64
			err := pool.QueryRow(ctx, `
				INSERT INTO dead_letter_jobs (original_job_id, kind, queue, args, errors, max_attempts, created_at, failed_at)
				VALUES (0, $1, 'default', '{}'::jsonb, '[]'::jsonb, 5, NOW(), NOW())
				RETURNING id`, kind).Scan(&id)
			require.NoError(t, err)
			ids = append(ids, id)
		}
		// Explicit context, not t.Context(): a cancelled context in cleanup leaves fixture
		// rows behind that break OTHER packages on the shared database.
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM dead_letter_jobs WHERE kind = $1`, kind)
		})
		return ids
	}

	countRows := func(t *testing.T) int64 {
		t.Helper()
		var n int64
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM dead_letter_jobs WHERE kind = $1`, kind).Scan(&n))
		return n
	}

	k := kind
	filters := domain.DeadLetterJobFilters{Kind: &k}

	t.Run("a delete whose match count drifted removes nothing at all", func(t *testing.T) {
		insertRows(t, 3)
		require.Equal(t, int64(3), countRows(t))

		// The caller confirmed 2; three match. This is the COUNT/DELETE snapshot drift the
		// finding was about, reproduced deterministically.
		deleted, err := repo.DeleteByFilters(ctx, filters, 2)

		require.Error(t, err)
		require.ErrorIs(t, err, ErrDLQBulkDeleteCountDrifted)
		assert.Equal(t, int64(0), deleted)
		// The load-bearing assertion: the transaction really did abort. A DELETE that ran and
		// was merely reported as an error would leave 0 rows here.
		assert.Equal(t, int64(3), countRows(t), "an unconfirmed delete must leave every row in place")
	})

	t.Run("a correctly-confirmed delete still removes exactly its rows", func(t *testing.T) {
		insertRows(t, 3)
		require.Equal(t, int64(3), countRows(t))

		deleted, err := repo.DeleteByFilters(ctx, filters, 3)

		require.NoError(t, err)
		assert.Equal(t, int64(3), deleted)
		assert.Equal(t, int64(0), countRows(t))
	})
}
