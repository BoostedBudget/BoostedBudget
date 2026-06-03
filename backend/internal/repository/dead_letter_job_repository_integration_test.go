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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

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
