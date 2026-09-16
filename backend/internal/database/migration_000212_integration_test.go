//go:build integration

package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration000212_CompletionGenerationIsConstrainedAndRollbackGuarded(t *testing.T) {
	pool := connectMigrationTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	_, err = tx.Exec(ctx, `ALTER TABLE linked_items
		DROP CONSTRAINT IF EXISTS chk_linked_items_transaction_sync_completion_generation,
		DROP COLUMN IF EXISTS transaction_sync_completion_generation`)
	require.NoError(t, err)

	up := readMigrationFile(t, "000212_add_transaction_sync_completion_generation.up.sql")
	_, err = tx.Exec(ctx, up)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, up)
	require.NoError(t, err, "up migration must be idempotent")

	userID := uuid.NewString()
	itemID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')`,
		userID, "clerk_"+userID, userID+"@example.com")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'ciphertext')`, itemID, userID, "plaid_"+itemID)
	require.NoError(t, err)

	var generation int64
	require.NoError(t, tx.QueryRow(ctx, `SELECT transaction_sync_completion_generation
		FROM linked_items WHERE id=$1`, itemID).Scan(&generation))
	assert.Zero(t, generation)

	_, err = tx.Exec(ctx, "SAVEPOINT invalid_generation")
	require.NoError(t, err)
	_, invalidErr := tx.Exec(ctx, `UPDATE linked_items
		SET transaction_sync_completion_generation=-1 WHERE id=$1`, itemID)
	require.Error(t, invalidErr)
	_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT invalid_generation")
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `UPDATE linked_items
		SET transaction_sync_completion_generation=1 WHERE id=$1`, itemID)
	require.NoError(t, err)
	down := readMigrationFile(t, "000212_add_transaction_sync_completion_generation.down.sql")
	_, err = tx.Exec(ctx, "SAVEPOINT guarded_down")
	require.NoError(t, err)
	_, guardedDownErr := tx.Exec(ctx, down)
	require.ErrorContains(t, guardedDownErr, "REFUSING TO ROLL BACK 000212")
	_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT guarded_down")
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `UPDATE linked_items
		SET transaction_sync_completion_generation=0 WHERE id=$1`, itemID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, down)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, down)
	require.NoError(t, err, "down migration must be idempotent")

	var remaining int
	require.NoError(t, tx.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='linked_items'
		  AND column_name='transaction_sync_completion_generation'`).Scan(&remaining))
	assert.Zero(t, remaining)
}
