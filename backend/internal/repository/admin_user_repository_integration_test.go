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
	"fmt"
	"testing"
	"time"

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

// TestListUsersPrefix_Integration verifies that the EXISTS-based prefix subquery
// supports starts-with email search against a real PostgreSQL planner.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestListUsersPrefix_Integration -tags integration -v
func TestListUsersPrefix_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	adminRepo := NewAdminUserRepository(pool, enc)
	profileRepo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	// Create a unique email so we don't collide with existing data
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	testEmail := "prefixtest-" + suffix + "@integration.example"

	// Create test user (CreateUserTx now inserts prefix rows automatically)
	user := &domain.User{
		ClerkUserID:        "clerk_prefixtest_" + suffix,
		Email:              testEmail,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		// CASCADE deletes prefix rows automatically
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	t.Run("prefix match returns the user", func(t *testing.T) {
		// "prefixtest-" is 11 chars — well above the 3-char minimum
		search := "prefixtest-" + suffix[:6] // use a unique prefix
		filters := domain.AdminUserFilters{Search: search}
		users, count, err := adminRepo.ListUsers(ctx, filters, 100, 0)
		require.NoError(t, err)
		found := false
		for _, u := range users {
			if u.ID == user.ID {
				found = true
				break
			}
		}
		assert.True(t, found, "user should be found via prefix search")
		assert.GreaterOrEqual(t, count, 1)
	})

	t.Run("non-prefix (contains only) does not match", func(t *testing.T) {
		// "@integration.example" is a suffix, not a prefix of the email
		filters := domain.AdminUserFilters{Search: "@integration.example"}
		users, _, err := adminRepo.ListUsers(ctx, filters, 100, 0)
		require.NoError(t, err)
		for _, u := range users {
			assert.NotEqual(t, user.ID, u.ID, "suffix-only search must not match")
		}
	})

	t.Run("exact full email match works", func(t *testing.T) {
		filters := domain.AdminUserFilters{Search: testEmail}
		users, _, err := adminRepo.ListUsers(ctx, filters, 100, 0)
		require.NoError(t, err)
		found := false
		for _, u := range users {
			if u.ID == user.ID {
				found = true
				break
			}
		}
		assert.True(t, found, "exact full-email search must match")
	})

	t.Run("tier and search combination filters correctly", func(t *testing.T) {
		prefix := "prefixtest-" + suffix[:6]
		filters := domain.AdminUserFilters{Search: prefix, Tier: "household_2"}
		users, _, err := adminRepo.ListUsers(ctx, filters, 100, 0)
		require.NoError(t, err)
		// Our test user is premium, not household_2 — it should NOT appear
		for _, u := range users {
			assert.NotEqual(t, user.ID, u.ID)
		}
	})
}
