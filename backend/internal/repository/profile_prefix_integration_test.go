//go:build integration

package repository

// Integration tests for prefix blind index maintenance in ProfileRepository.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestProfilePrefixIndices_Integration -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestProfilePrefixIndices_Integration(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	email1 := "profileprefix-" + suffix + "@example.com"
	email2 := "updated-" + suffix + "@example.com"

	user := &domain.User{
		ClerkUserID:        "clerk_pfx_" + suffix,
		Email:              email1,
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}

	// Create user via transaction
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) }) //nolint:errcheck

	t.Run("CreateUserTx inserts prefix rows", func(t *testing.T) {
		var count int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", user.ID,
		).Scan(&count))
		assert.Greater(t, count, 0, "prefix rows should exist after CreateUserTx")
	})

	t.Run("insertPrefixIndicesTx is idempotent (ON CONFLICT DO NOTHING)", func(t *testing.T) {
		var beforeCount int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", user.ID,
		).Scan(&beforeCount))

		// UpdateUserEmailTx with the same email should not change row count
		tx3, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, repo.UpdateUserEmailTx(ctx, tx3, user.ID, email1)) // same email — re-inserts
		require.NoError(t, tx3.Commit(ctx))

		var afterCount int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", user.ID,
		).Scan(&afterCount))
		assert.Equal(t, beforeCount, afterCount, "idempotent re-insert should not change row count")
	})

	t.Run("UpdateUserEmailTx replaces all prefix rows atomically", func(t *testing.T) {
		tx4, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, repo.UpdateUserEmailTx(ctx, tx4, user.ID, email2))
		require.NoError(t, tx4.Commit(ctx))

		enc2 := newTestFieldEncryptor(t)
		// Verify a 3-rune prefix of the OLD email no longer matches
		oldPrefixIdx := enc2.BlindIndexWithLabel("email_prefix",
			string([]rune(email1)[:3]))
		var oldCount int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1 AND prefix_blind_idx = $2",
			user.ID, oldPrefixIdx,
		).Scan(&oldCount))
		assert.Equal(t, 0, oldCount, "old email prefix rows should be removed")

		// Verify a 3-rune prefix of the NEW email is present
		newPrefixIdx := enc2.BlindIndexWithLabel("email_prefix",
			string([]rune(email2)[:3]))
		var newCount int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1 AND prefix_blind_idx = $2",
			user.ID, newPrefixIdx,
		).Scan(&newCount))
		assert.Equal(t, 1, newCount, "new email prefix rows should exist after update")
	})

	t.Run("ON DELETE CASCADE removes prefix rows on user delete", func(t *testing.T) {
		_, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)

		var count int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM user_email_prefix_indices WHERE user_id = $1", user.ID,
		).Scan(&count))
		assert.Equal(t, 0, count, "CASCADE should delete all prefix rows when user is deleted")

		// Prevent the t.Cleanup from failing on the already-deleted user
		user.ID = uuid.New().String() // point cleanup at a non-existent ID
	})
}
