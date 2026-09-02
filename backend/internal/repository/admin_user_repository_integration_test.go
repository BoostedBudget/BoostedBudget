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

// findUserInResults searches for a user by ID in the results list.
// Returns true if found, false otherwise.
func findUserInResults(users []domain.AdminUserListItem, userID string) bool {
	for _, u := range users {
		if u.ID == userID {
			return true
		}
	}
	return false
}

// TestListUsersSort_Integration exercises the dynamic `ORDER BY %s %s%s` in ListUsers — including
// the NULLS LAST that only the nullable `last_login_at` column carries — against a real planner.
//
// ⚠ #1569 r17: this test SEEDS, and it no longer restates its own loop's exit condition. The
// last_login_at assertion used to compute `nullIdx` as the index of the FIRST nil and then assert
// NotNil over `users[:nullIdx]` — a slice that loop had just proved contains no nils. It was a
// tautology: it could not report NULLS LAST being inverted or dropped, and when no row had a NULL
// (an empty result set included) `nullIdx` stayed -1 and the block was skipped entirely. The
// sibling tier assertion had a different hole: it compared `SubscriptionTier` strings that are
// uniform across fixtures, and equal values satisfy LessOrEqual in ANY order — so seeding
// DISTINCT tiers is what makes that assertion real rather than cosmetic.
//
// Isolation: the fixtures share a unique email prefix and every call filters on it, so the
// assertions are about EXACTLY these rows on a shared integration database.
func TestListUsersSort_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	profileRepo := NewProfileRepository(pool, enc)
	// ⚠ NOT t.Context(): the cleanup below must outlive this test's own context, or the fixture rows
	// leak into the shared integration database and corrupt OTHER packages' whole-table reads.
	ctx := context.Background()

	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	search := "sortfix-" + nonce // 8 + 19 = 27 runes: above the 3-rune minimum, under MaxPrefixLen
	base := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Millisecond)

	// Every fixture carries a DISTINCT tier and a DISTINCT (or NULL) last_login_at. Insertion order
	// deliberately matches NEITHER expected output order.
	lastLogin := func(d time.Duration) *time.Time { v := base.Add(d); return &v }
	seeds := []struct {
		label     string
		tier      string
		lastLogin *time.Time
	}{
		{"a", "household_4", lastLogin(2 * time.Hour)},
		{"b", "premium", nil},
		{"c", "household_2", lastLogin(1 * time.Hour)},
		{"d", "household_3", nil},
	}

	byID := make(map[string]string, len(seeds)) // user id -> label
	for _, sd := range seeds {
		user := &domain.User{
			ClerkUserID:        "clerk_" + search + "_" + sd.label,
			Email:              search + "-" + sd.label + "@integration.example",
			SubscriptionTier:   sd.tier,
			SubscriptionStatus: "active",
			PrimaryRegion:      "iad",
			IsActive:           true,
		}
		// CreateUserTx (not a raw INSERT) so the email takes the real encryption + prefix-index
		// path: a hand-written row would carry neither the blind index nor the prefix rows the
		// search filter below matches on, and the filter would be dead.
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))
		require.NoError(t, tx.Commit(ctx))
		userID := user.ID
		t.Cleanup(func() {
			// CASCADE deletes the prefix rows automatically.
			_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID)
		})

		_, err = pool.Exec(ctx, "UPDATE users SET last_login_at = $1 WHERE id = $2", sd.lastLogin, userID)
		require.NoError(t, err)
		byID[userID] = sd.label
	}

	labels := func(users []domain.AdminUserListItem) []string {
		out := make([]string, 0, len(users))
		for _, u := range users {
			out = append(out, byID[u.ID])
		}
		return out
	}
	nullness := func(users []domain.AdminUserListItem) []bool {
		out := make([]bool, 0, len(users))
		for _, u := range users {
			out = append(out, u.LastLoginAt == nil)
		}
		return out
	}
	filters := func(sortBy, sortDir string) domain.AdminUserFilters {
		return domain.AdminUserFilters{Search: search, SortBy: sortBy, SortDir: sortDir}
	}

	t.Run("sorts by last_login_at asc, NULLs last", func(t *testing.T) {
		users, count, err := repo.ListUsers(ctx, filters("last_login_at", "asc"), 100, 0)
		require.NoError(t, err)
		// The floor: without it every assertion below is vacuous again the moment seeding breaks.
		require.Len(t, users, len(seeds), "the fixtures must be visible or the ordering assertion proves nothing")
		require.Equal(t, len(seeds), count)

		assert.Equal(t, []bool{false, false, true, true}, nullness(users),
			"NULL last_login_at rows must sort last under ASC")
		// The two non-null rows must additionally be ascending among themselves. The two NULL rows
		// have no defined order relative to each other, so only their position is pinned.
		assert.Equal(t, []string{"c", "a"}, labels(users)[:2],
			"expected ascending last_login_at among the non-null rows")
	})

	// ⚠ SIBLING, MANDATORY: DESC is the direction where NULLS LAST actually does work. PostgreSQL's
	// default for DESC is NULLS FIRST, so dropping the explicit ` NULLS LAST` changes DESC and
	// leaves ASC untouched — an ASC-only test cannot see that mutation at all.
	t.Run("sorts by last_login_at desc, NULLs still last", func(t *testing.T) {
		users, _, err := repo.ListUsers(ctx, filters("last_login_at", "desc"), 100, 0)
		require.NoError(t, err)
		require.Len(t, users, len(seeds))

		assert.Equal(t, []bool{false, false, true, true}, nullness(users),
			"NULL last_login_at rows must sort last under DESC too — that is what NULLS LAST buys")
		assert.Equal(t, []string{"a", "c"}, labels(users)[:2],
			"expected descending last_login_at among the non-null rows")
	})

	t.Run("sort by subscription_tier asc produces alphabetical order", func(t *testing.T) {
		users, _, err := repo.ListUsers(ctx, filters("subscription_tier", "asc"), 100, 0)
		require.NoError(t, err)
		require.Len(t, users, len(seeds))

		assert.Equal(t, []string{"household_2", "household_3", "household_4", "premium"},
			tiers(users), "expected ascending alphabetical subscription_tier order")
	})

	t.Run("sort by subscription_tier desc produces reverse alphabetical order", func(t *testing.T) {
		users, _, err := repo.ListUsers(ctx, filters("subscription_tier", "desc"), 100, 0)
		require.NoError(t, err)
		require.Len(t, users, len(seeds))

		assert.Equal(t, []string{"premium", "household_4", "household_3", "household_2"},
			tiers(users), "expected descending alphabetical subscription_tier order")
	})
}

// tiers projects the subscription tier of each row, in result order.
func tiers(users []domain.AdminUserListItem) []string {
	out := make([]string, 0, len(users))
	for _, u := range users {
		out = append(out, u.SubscriptionTier)
	}
	return out
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
		assert.True(t, findUserInResults(users, user.ID), "user should be found via prefix search")
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
		assert.True(t, findUserInResults(users, user.ID), "exact full-email search must match")
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

	t.Run("positive compound filter: search + matching tier returns user", func(t *testing.T) {
		// The test user is "premium" tier. Searching with the matching tier must return it.
		prefix := "prefixtest-" + suffix[:6]
		filters := domain.AdminUserFilters{Search: prefix, Tier: "premium"}
		users, _, err := adminRepo.ListUsers(ctx, filters, 100, 0)
		require.NoError(t, err)
		assert.True(t, findUserInResults(users, user.ID), "user must be found when both search prefix and tier match")
	})

	t.Run("email update removes old prefix and adds new prefix for search", func(t *testing.T) {
		newEmail := "updated-" + suffix + "@integration.example"
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, profileRepo.UpdateUserEmailTx(ctx, tx, user.ID, newEmail))
		require.NoError(t, tx.Commit(ctx))

		// Old prefix no longer matches via admin search
		oldSearch := "prefixtest-" + suffix[:6]
		users, _, err := adminRepo.ListUsers(ctx, domain.AdminUserFilters{Search: oldSearch}, 100, 0)
		require.NoError(t, err)
		for _, u := range users {
			assert.NotEqual(t, user.ID, u.ID, "old email prefix must not match after update")
		}

		// New prefix matches via admin search
		newSearch := "updated-" + suffix[:6]
		users, _, err = adminRepo.ListUsers(ctx, domain.AdminUserFilters{Search: newSearch}, 100, 0)
		require.NoError(t, err)
		assert.True(t, findUserInResults(users, user.ID), "new email prefix must match after update")
	})
}
