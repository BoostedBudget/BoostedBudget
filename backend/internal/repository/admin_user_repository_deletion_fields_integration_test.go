//go:build integration

package repository

// Integration tests for AdminUserRepository.ListUsers and GetUserDetail surfacing
// deletion_requested_at/deletion_scheduled_at (Issue #1413 Task 8b).
//
// pgxmock matches query strings and cannot validate a real SELECT column order against the
// Scan() argument order — a mismatch between these two *time.Time columns compiles fine and
// silently swaps the values at runtime. A real database is required: the SQL driver returns
// columns in the query's literal order regardless of what the test author "intended", so this
// is an independent check on the Scan wiring, not just a restatement of it.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestListUsers_Integration_DeletionFields -tags integration -v
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetUserDetail_Integration_DeletionFields -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// setDeletionState sets deletion_requested_at/deletion_scheduled_at directly via SQL (bypassing
// the repository under test, since we're constructing the "prior" state, not exercising it).
func setDeletionState(t *testing.T, pool *pgxpool.Pool, userID string, requestedAt, scheduledAt *time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		"UPDATE users SET deletion_requested_at = $1, deletion_scheduled_at = $2 WHERE id = $3",
		requestedAt, scheduledAt, userID,
	)
	require.NoError(t, err)
}

// TestListUsers_Integration_DeletionFields_NilWhenNotSet proves ListUsers returns nil
// DeletionRequestedAt/DeletionScheduledAt for a user with no pending deletion.
func TestListUsers_Integration_DeletionFields_NilWhenNotSet(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createRemoveTierOverrideUser(t, pool, enc, suffix+"_nodel", "premium", "active")

	filters := domain.AdminUserFilters{Search: "rto-" + suffix + "_nodel"}
	users, _, err := repo.ListUsers(ctx, filters, 100, 0)
	require.NoError(t, err)

	found := findUserInResults(users, user.ID)
	require.True(t, found, "test user must be found via search")
	for _, u := range users {
		if u.ID == user.ID {
			assert.Nil(t, u.DeletionRequestedAt)
			assert.Nil(t, u.DeletionScheduledAt)
		}
	}
}

// TestListUsers_Integration_DeletionFields_SetWhenPending proves ListUsers scans
// deletion_requested_at/deletion_scheduled_at into the correct fields respectively (not
// swapped) when both are set. The two timestamps are deliberately far apart (a day) and
// asserted with tight tolerance so a Scan-argument swap between these two columns is
// detectable.
func TestListUsers_Integration_DeletionFields_SetWhenPending(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createRemoveTierOverrideUser(t, pool, enc, suffix+"_pending", "premium", "cancelled")

	requestedAt := time.Now().Add(-2 * 24 * time.Hour).Truncate(time.Second)
	scheduledAt := time.Now().Add(28 * 24 * time.Hour).Truncate(time.Second)
	setDeletionState(t, pool, user.ID, &requestedAt, &scheduledAt)

	filters := domain.AdminUserFilters{Search: "rto-" + suffix + "_pending"}
	users, _, err := repo.ListUsers(ctx, filters, 100, 0)
	require.NoError(t, err)

	var got *domain.AdminUserListItem
	for i := range users {
		if users[i].ID == user.ID {
			got = &users[i]
		}
	}
	require.NotNil(t, got, "test user must be found via search")
	require.NotNil(t, got.DeletionRequestedAt)
	require.NotNil(t, got.DeletionScheduledAt)
	assert.WithinDuration(t, requestedAt, *got.DeletionRequestedAt, time.Second,
		"deletion_requested_at must scan into DeletionRequestedAt, not swapped with DeletionScheduledAt")
	assert.WithinDuration(t, scheduledAt, *got.DeletionScheduledAt, time.Second,
		"deletion_scheduled_at must scan into DeletionScheduledAt, not swapped with DeletionRequestedAt")
}

// TestGetUserDetail_Integration_DeletionFields_NilWhenNotSet proves GetUserDetail returns nil
// DeletionRequestedAt/DeletionScheduledAt for a user with no pending deletion.
func TestGetUserDetail_Integration_DeletionFields_NilWhenNotSet(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createRemoveTierOverrideUser(t, pool, enc, suffix+"_detail_nodel", "premium", "active")

	detail, err := repo.GetUserDetail(ctx, user.ID)
	require.NoError(t, err)
	assert.Nil(t, detail.DeletionRequestedAt)
	assert.Nil(t, detail.DeletionScheduledAt)
}

// TestGetUserDetail_Integration_DeletionFields_SetWhenPending proves GetUserDetail scans
// deletion_requested_at/deletion_scheduled_at into the correct fields respectively (not
// swapped) when both are set — the swap guard for GetUserDetail, mirroring the ListUsers case
// above with independently distinct timestamps.
func TestGetUserDetail_Integration_DeletionFields_SetWhenPending(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := createRemoveTierOverrideUser(t, pool, enc, suffix+"_detail_pending", "premium", "cancelled")

	requestedAt := time.Now().Add(-5 * 24 * time.Hour).Truncate(time.Second)
	scheduledAt := time.Now().Add(25 * 24 * time.Hour).Truncate(time.Second)
	setDeletionState(t, pool, user.ID, &requestedAt, &scheduledAt)

	detail, err := repo.GetUserDetail(ctx, user.ID)
	require.NoError(t, err)
	require.NotNil(t, detail.DeletionRequestedAt)
	require.NotNil(t, detail.DeletionScheduledAt)
	assert.WithinDuration(t, requestedAt, *detail.DeletionRequestedAt, time.Second,
		"deletion_requested_at must scan into DeletionRequestedAt, not swapped with DeletionScheduledAt")
	assert.WithinDuration(t, scheduledAt, *detail.DeletionScheduledAt, time.Second,
		"deletion_scheduled_at must scan into DeletionScheduledAt, not swapped with DeletionRequestedAt")
}
