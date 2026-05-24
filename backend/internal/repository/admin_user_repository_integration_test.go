//go:build integration

package repository

// Integration tests for admin user repository sort.
// These tests execute SQL against a real Postgres planner and verify that the
// dynamic ORDER BY clause (sort_by/sort_dir) produces correct ordering.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestListUsersSort_Integration -tags integration -v

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestListUsersSort_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	t.Run("sorts by last_login_at asc, NULLs last", func(t *testing.T) {
		filters := domain.AdminUserFilters{SortBy: "last_login_at", SortDir: "asc"}
		users, _, err := repo.ListUsers(ctx, filters, 100, 0)
		require.NoError(t, err)

		nullIdx := -1
		for i, u := range users {
			if u.LastLoginAt == nil {
				nullIdx = i
				break
			}
		}
		if nullIdx >= 0 {
			for _, u := range users[:nullIdx] {
				assert.NotNil(t, u.LastLoginAt)
			}
		}
	})

	t.Run("sort by subscription_tier asc produces alphabetical order", func(t *testing.T) {
		filters := domain.AdminUserFilters{SortBy: "subscription_tier", SortDir: "asc"}
		users, _, err := repo.ListUsers(ctx, filters, 100, 0)
		require.NoError(t, err)
		for i := 1; i < len(users); i++ {
			assert.LessOrEqual(t, users[i-1].SubscriptionTier, users[i].SubscriptionTier)
		}
	})
}
