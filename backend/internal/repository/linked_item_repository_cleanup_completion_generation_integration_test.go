//go:build integration

package repository

// TestGetItemsForCleanupPage_Integration_CompletionGenerationIsPopulated pins the 25th scan
// destination on BOTH cleanup SELECTs. Each hardcoded column list once ended at status_changed_at
// while its scan read transaction_sync_completion_generation, so every cleanup read failed with
// "number of field descriptions must equal number of destinations, got 24 and 25". The tempting
// repair — dropping the 25th destination — would have made the queries run while silently zeroing
// the field, so this asserts a NON-zero generation survives both paths.
//
// Lives in its own file rather than beside the sibling GetItemsForCleanupPage tests: those files
// are the failing tests this fix exists to turn green, and their diffs must stay empty.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetItemsForCleanupPage_Integration_CompletionGenerationIsPopulated(t *testing.T) {
	ctx := context.Background()
	tx, repo, profileRepo := newCleanupPageTestTx(t, ctx)

	ids := seedCleanupCandidates(t, ctx, tx, repo, profileRepo, 1)
	assertSeededItemsAreOldestCandidates(t, ctx, tx, ids)
	const generation int64 = 7
	_, err := tx.Exec(ctx,
		`UPDATE linked_items SET transaction_sync_completion_generation = $1 WHERE id = $2`, generation, ids[0])
	require.NoError(t, err)

	page, err := repo.GetItemsForCleanupPage(ctx, cleanupPageErrorDays, cleanupPageStaleDays, nil, 10)
	require.NoError(t, err)
	require.NotEmpty(t, page)
	assert.Equal(t, generation, page[0].TransactionSyncCompletionGeneration,
		"GetItemsForCleanupPage must return the stored generation, not a zeroed field")

	_, err = tx.Exec(ctx,
		`UPDATE linked_items SET cleanup_scheduled_at = NOW() - interval '1 hour' WHERE id = $1`, ids[0])
	require.NoError(t, err)
	pending, err := repo.GetItemsPendingCleanup(ctx)
	require.NoError(t, err)
	pendingIDs := make(map[string]struct{}, len(pending))
	for _, it := range pending {
		pendingIDs[it.ID] = struct{}{}
	}
	require.Contains(t, pendingIDs, ids[0], "the stamped, overdue row must be selected by GetItemsPendingCleanup")
	for _, it := range pending {
		if it.ID == ids[0] {
			assert.Equal(t, generation, it.TransactionSyncCompletionGeneration,
				"GetItemsPendingCleanup must return the stored generation, not a zeroed field")
		}
	}
}