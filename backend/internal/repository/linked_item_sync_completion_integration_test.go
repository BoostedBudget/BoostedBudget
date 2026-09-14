//go:build integration

package repository

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinkedItemRepository_RecordTransactionSyncCompletionIsDurableAndConcurrent(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewLinkedItemRepository(pool, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad') RETURNING id`,
		userID, "clerk_"+userID, userID+"@example.com").Scan(&userID))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID) })

	insertItem := func() string {
		id := uuid.NewString()
		_, err := pool.Exec(ctx, `
			INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, cursor)
			VALUES ($1, $2, $3, 'ciphertext', '')`, id, userID, "plaid_"+id)
		require.NoError(t, err)
		return id
	}

	itemID := insertItem()
	start := make(chan struct{})
	results := make(chan struct {
		first bool
		err   error
	}, 2)
	var wg sync.WaitGroup
	for _, cursor := range []string{"cursor-a", "cursor-b"} {
		wg.Add(1)
		go func(next string) {
			defer wg.Done()
			<-start
			first, err := repo.RecordTransactionSyncCompletion(ctx, itemID, "", &next)
			results <- struct {
				first bool
				err   error
			}{first: first, err: err}
		}(cursor)
	}
	close(start)
	wg.Wait()
	close(results)

	firstWins, staleLosers := 0, 0
	for result := range results {
		if result.err == nil && result.first {
			firstWins++
		} else if errors.Is(result.err, ErrLinkedItemSyncStateChanged) {
			staleLosers++
		}
	}
	assert.Equal(t, 1, firstWins)
	assert.Equal(t, 1, staleLosers)

	var durableCursor string
	var completed bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT cursor, first_transaction_sync_completed_at IS NOT NULL
		FROM linked_items WHERE id = $1`, itemID).Scan(&durableCursor, &completed))
	assert.Contains(t, []string{"cursor-a", "cursor-b"}, durableCursor)
	assert.True(t, completed)

	partialID := insertItem()
	require.NoError(t, repo.RecordTransactionSyncProgress(ctx, partialID, "", "cursor-partial"))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT first_transaction_sync_completed_at IS NOT NULL FROM linked_items WHERE id = $1`, partialID).Scan(&completed))
	assert.False(t, completed, "resumable partial progress is not a completed sync")
	partialFinal := "cursor-final"
	first, err := repo.RecordTransactionSyncCompletion(ctx, partialID, "cursor-partial", &partialFinal)
	require.NoError(t, err)
	assert.True(t, first, "the first eventual full catch-up remains initial after partial progress")

	// A stale INVALID_CURSOR response must not erase a cursor committed by a newer sync.
	err = repo.ResetCursorIfUnchanged(ctx, itemID, "")
	require.ErrorIs(t, err, ErrLinkedItemSyncStateChanged)
	require.NoError(t, pool.QueryRow(ctx, `SELECT cursor FROM linked_items WHERE id = $1`, itemID).Scan(&durableCursor))
	assert.Contains(t, []string{"cursor-a", "cursor-b"}, durableCursor)

	// INVALID_CURSOR deliberately clears only the cursor. The durable latch keeps the recovery a
	// refresh, and a newly relinked row gets its own first completion.
	_, err = pool.Exec(ctx, `UPDATE linked_items SET cursor = '' WHERE id = $1`, itemID)
	require.NoError(t, err)
	recoveryCursor := "cursor-recovered"
	first, err = repo.RecordTransactionSyncCompletion(ctx, itemID, "", &recoveryCursor)
	require.NoError(t, err)
	assert.False(t, first)

	relinkedID := insertItem()
	relinkedCursor := "cursor-relinked"
	first, err = repo.RecordTransactionSyncCompletion(ctx, relinkedID, "", &relinkedCursor)
	require.NoError(t, err)
	assert.True(t, first)

	// Legacy binaries finalize through UpdateCursor. The migration trigger dual-writes the latch so
	// a rolling deployment cannot later classify the same linked item as initial again.
	legacyID := insertItem()
	_, err = pool.Exec(ctx, `
		UPDATE linked_items SET cursor = 'legacy-cursor', last_synced_at = NOW() WHERE id = $1`, legacyID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT first_transaction_sync_completed_at IS NOT NULL FROM linked_items WHERE id = $1`, legacyID).Scan(&completed))
	assert.True(t, completed)
}

func TestLinkedItemRepository_BlankFinalCompletionsLatchInitialOnce(t *testing.T) {
	pool := connectTestDB(t)
	repo := NewLinkedItemRepository(pool, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad') RETURNING id`,
		userID, "clerk_"+userID, userID+"@example.com").Scan(&userID))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID) })

	itemID := uuid.NewString()
	_, err := pool.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, cursor)
		VALUES ($1, $2, $3, 'ciphertext', '')`, itemID, userID, "plaid_"+itemID)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan struct {
		first bool
		err   error
	}, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			first, recordErr := repo.RecordTransactionSyncCompletion(ctx, itemID, "", nil)
			results <- struct {
				first bool
				err   error
			}{first: first, err: recordErr}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	firstCount, refreshCount := 0, 0
	for result := range results {
		require.NoError(t, result.err)
		if result.first {
			firstCount++
		} else {
			refreshCount++
		}
	}
	assert.Equal(t, 1, firstCount)
	assert.Equal(t, 1, refreshCount)
}
