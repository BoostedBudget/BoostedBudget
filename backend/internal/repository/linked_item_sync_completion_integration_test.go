//go:build integration

package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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
		disposition TransactionSyncCompletionDisposition
		err         error
	}, 2)
	var wg sync.WaitGroup
	for _, cursor := range []string{"cursor-a", "cursor-b"} {
		wg.Add(1)
		go func(next string) {
			defer wg.Done()
			<-start
			disposition, err := repo.RecordTransactionSyncCompletion(ctx, itemID, "", &next)
			results <- struct {
				disposition TransactionSyncCompletionDisposition
				err         error
			}{disposition: disposition, err: err}
		}(cursor)
	}
	close(start)
	wg.Wait()
	close(results)

	firstWins, staleLosers := 0, 0
	for result := range results {
		if result.err == nil && result.disposition == TransactionSyncCompletionInitial {
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
	disposition, err := repo.RecordTransactionSyncCompletion(ctx, partialID, "cursor-partial", &partialFinal)
	require.NoError(t, err)
	assert.Equal(t, TransactionSyncCompletionInitial, disposition, "the first eventual full catch-up remains initial after partial progress")

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
	disposition, err = repo.RecordTransactionSyncCompletion(ctx, itemID, "", &recoveryCursor)
	require.NoError(t, err)
	assert.Equal(t, TransactionSyncCompletionRefresh, disposition)

	relinkedID := insertItem()
	relinkedCursor := "cursor-relinked"
	disposition, err = repo.RecordTransactionSyncCompletion(ctx, relinkedID, "", &relinkedCursor)
	require.NoError(t, err)
	assert.Equal(t, TransactionSyncCompletionInitial, disposition)

	// After the migration, legacy last_synced_at activity cannot prove whether an old binary fully
	// completed or persisted partial progress. The first new-code completion is conservatively
	// suppressed while latching the item; only the following completion is a refresh.
	legacyCases := []struct {
		name       string
		cursor     string
		lastSynced time.Time
	}{
		{name: "old_partial", cursor: "legacy-partial-cursor", lastSynced: time.Now().Add(-time.Minute)},
		{name: "old_full", cursor: "legacy-full-cursor", lastSynced: time.Now()},
	}
	for _, legacyActivity := range legacyCases {
		t.Run(legacyActivity.name+"_after_migration_is_suppressed_once", func(t *testing.T) {
			legacyID := insertItem()
			_, updateErr := pool.Exec(ctx, `
				UPDATE linked_items SET cursor = $2, last_synced_at = $3 WHERE id = $1`,
				legacyID, legacyActivity.cursor, legacyActivity.lastSynced)
			require.NoError(t, updateErr)
			require.NoError(t, pool.QueryRow(ctx, `
				SELECT first_transaction_sync_completed_at IS NOT NULL FROM linked_items WHERE id = $1`, legacyID).Scan(&completed))
			assert.False(t, completed)
			legacyCompletedCursor := "legacy-completed"
			got, recordErr := repo.RecordTransactionSyncCompletion(ctx, legacyID, legacyActivity.cursor, &legacyCompletedCursor)
			require.NoError(t, recordErr)
			assert.Equal(t, TransactionSyncCompletionSuppressedRolloutAmbiguous, got)
			nextRefreshCursor := "legacy-next-refresh"
			got, recordErr = repo.RecordTransactionSyncCompletion(ctx, legacyID, legacyCompletedCursor, &nextRefreshCursor)
			require.NoError(t, recordErr)
			assert.Equal(t, TransactionSyncCompletionRefresh, got)
		})
	}

	// Hold an old-style last_synced_at write uncommitted while the new completion starts. The
	// completion must re-evaluate the row after the lock is released and suppress, not use the
	// pre-wait NULL as proof of a new initial sync.
	racingLegacyID := insertItem()
	legacyTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = legacyTx.Exec(ctx, `UPDATE linked_items SET last_synced_at = NOW() WHERE id = $1`, racingLegacyID)
	require.NoError(t, err)
	raceResult := make(chan struct {
		disposition TransactionSyncCompletionDisposition
		err         error
	}, 1)
	go func() {
		got, recordErr := repo.RecordTransactionSyncCompletion(ctx, racingLegacyID, "", nil)
		raceResult <- struct {
			disposition TransactionSyncCompletionDisposition
			err         error
		}{disposition: got, err: recordErr}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waitingOnRowLock bool
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE datname = current_database()
				  AND pid <> pg_backend_pid()
				  AND state = 'active'
				  AND wait_event_type = 'Lock'
				  AND query LIKE '%completion_disposition%'
			)`).Scan(&waitingOnRowLock))
		if waitingOnRowLock {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completion query never reached the legacy writer's row lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NoError(t, legacyTx.Commit(ctx))
	raced := <-raceResult
	require.NoError(t, raced.err)
	assert.Equal(t, TransactionSyncCompletionSuppressedRolloutAmbiguous, raced.disposition)
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
		disposition TransactionSyncCompletionDisposition
		err         error
	}, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			disposition, recordErr := repo.RecordTransactionSyncCompletion(ctx, itemID, "", nil)
			results <- struct {
				disposition TransactionSyncCompletionDisposition
				err         error
			}{disposition: disposition, err: recordErr}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	firstCount, refreshCount := 0, 0
	for result := range results {
		require.NoError(t, result.err)
		if result.disposition == TransactionSyncCompletionInitial {
			firstCount++
		} else if result.disposition == TransactionSyncCompletionRefresh {
			refreshCount++
		}
	}
	assert.Equal(t, 1, firstCount)
	assert.Equal(t, 1, refreshCount)
}
