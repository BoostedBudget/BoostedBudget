//go:build integration

package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration000208_ClassifiesPreexistingAndLegacyInsertedLinkedItems(t *testing.T) {
	pool := connectMigrationTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	_, err = tx.Exec(ctx, `
		ALTER TABLE linked_items
		DROP COLUMN IF EXISTS transaction_sync_state,
		DROP COLUMN IF EXISTS first_transaction_sync_completed_at`)
	require.NoError(t, err)

	userID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')`,
		userID, "clerk_"+userID, userID+"@example.com")
	require.NoError(t, err)

	blankLegacyID := uuid.NewString()
	syncedLegacyID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, cursor, last_synced_at)
		VALUES ($1, $3, $4, 'ciphertext', '', NULL),
		       ($2, $3, $5, 'ciphertext', 'cursor-old', NOW())`,
		blankLegacyID, syncedLegacyID, userID, "plaid_"+blankLegacyID, "plaid_"+syncedLegacyID)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, readMigrationFile(t, "000208_add_linked_items_first_transaction_sync_completed_at.up.sql"))
	require.NoError(t, err)
	betweenMigrationsLegacyID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, cursor, last_synced_at)
		VALUES ($1, $2, $3, 'ciphertext', '', NOW())`,
		betweenMigrationsLegacyID, userID, "plaid_"+betweenMigrationsLegacyID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, readMigrationFile(t, "000209_classify_linked_item_transaction_sync_state.up.sql"))
	require.NoError(t, err)
	_, err = tx.Exec(ctx, readMigrationFile(t, "000210_validate_linked_item_transaction_sync_state.up.sql"))
	require.NoError(t, err)

	var state string
	var latched bool
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT transaction_sync_state, first_transaction_sync_completed_at IS NOT NULL
		FROM linked_items WHERE id = $1`, blankLegacyID).Scan(&state, &latched))
	assert.Equal(t, "legacy_ambiguous", state)
	assert.False(t, latched)

	require.NoError(t, tx.QueryRow(ctx, `
		SELECT transaction_sync_state, first_transaction_sync_completed_at IS NOT NULL
		FROM linked_items WHERE id = $1`, syncedLegacyID).Scan(&state, &latched))
	assert.Equal(t, "completed", state)
	assert.True(t, latched)
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT transaction_sync_state FROM linked_items WHERE id = $1`, betweenMigrationsLegacyID).Scan(&state))
	assert.Equal(t, "legacy_ambiguous", state,
		"an old writer inserting after the schema migration must receive the fail-closed default")

	postMigrationLegacyID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, cursor)
		VALUES ($1, $2, $3, 'ciphertext', '')`, postMigrationLegacyID, userID, "plaid_"+postMigrationLegacyID)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT transaction_sync_state FROM linked_items WHERE id = $1`, postMigrationLegacyID).Scan(&state))
	assert.Equal(t, "legacy_ambiguous", state, "an old writer omitting the new column must fail closed")

	_, err = tx.Exec(ctx, `UPDATE linked_items SET last_synced_at = NOW() WHERE id = $1`, postMigrationLegacyID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, readMigrationFile(t, "000208_add_linked_items_first_transaction_sync_completed_at.up.sql"))
	require.NoError(t, err)
	_, err = tx.Exec(ctx, readMigrationFile(t, "000209_classify_linked_item_transaction_sync_state.up.sql"))
	require.NoError(t, err)
	_, err = tx.Exec(ctx, readMigrationFile(t, "000210_validate_linked_item_transaction_sync_state.up.sql"))
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT transaction_sync_state FROM linked_items WHERE id = $1`, postMigrationLegacyID).Scan(&state))
	assert.Equal(t, "legacy_ambiguous", state, "migration replay must not reclassify post-cutover old-writer activity")

	_, err = tx.Exec(ctx, "SAVEPOINT invalid_sync_state")
	require.NoError(t, err)
	_, invalidStateErr := tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, transaction_sync_state)
		VALUES ($1, $2, $3, 'ciphertext', 'unknown')`, uuid.NewString(), userID, "plaid_"+uuid.NewString())
	require.Error(t, invalidStateErr)
	_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT invalid_sync_state")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SAVEPOINT null_sync_state")
	require.NoError(t, err)
	_, nullStateErr := tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, transaction_sync_state)
		VALUES ($1, $2, $3, 'ciphertext', NULL)`, uuid.NewString(), userID, "plaid_"+uuid.NewString())
	require.Error(t, nullStateErr)
	_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT null_sync_state")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SAVEPOINT completed_without_timestamp")
	require.NoError(t, err)
	_, missingTimestampErr := tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted, transaction_sync_state)
		VALUES ($1, $2, $3, 'ciphertext', 'completed')`, uuid.NewString(), userID, "plaid_"+uuid.NewString())
	require.Error(t, missingTimestampErr)
	_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT completed_without_timestamp")
	require.NoError(t, err)

	validationDown := readMigrationFile(t, "000210_validate_linked_item_transaction_sync_state.down.sql")
	_, err = tx.Exec(ctx, validationDown)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, validationDown)
	require.NoError(t, err)
	classificationDown := readMigrationFile(t, "000209_classify_linked_item_transaction_sync_state.down.sql")
	_, err = tx.Exec(ctx, classificationDown)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, classificationDown)
	require.NoError(t, err)
	schemaDown := readMigrationFile(t, "000208_add_linked_items_first_transaction_sync_completed_at.down.sql")
	_, err = tx.Exec(ctx, schemaDown)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, schemaDown)
	require.NoError(t, err)
	var remainingColumns int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'linked_items'
		  AND column_name IN ('transaction_sync_state', 'first_transaction_sync_completed_at')`).Scan(&remainingColumns))
	assert.Zero(t, remainingColumns)
}
