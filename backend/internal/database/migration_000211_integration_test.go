//go:build integration

package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration000211_PostHogErasureOutboxIsDurableEncryptedAndConstrained(t *testing.T) {
	pool := connectMigrationTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	down := readMigrationFile(t, "000211_add_posthog_erasure_outbox.down.sql")
	_, err = tx.Exec(ctx, down)
	require.NoError(t, err)
	up := readMigrationFile(t, "000211_add_posthog_erasure_outbox.up.sql")
	_, err = tx.Exec(ctx, up)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, up)
	require.NoError(t, err, "up migration must be idempotent")

	id := uuid.NewString()
	_, err = tx.Exec(ctx, "SAVEPOINT plaintext_identity")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO posthog_erasure_outbox
		(id, idempotency_key, identities_encrypted, project_id, region, state, phase)
		VALUES ($1, $2, 'clerk_plaintext', '42', 'us', 'pending', 'person_delete')`, id, "gdpr:"+id)
	require.Error(t, err, "plaintext external identities must be rejected")
	_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT plaintext_identity")
	require.NoError(t, err)

	id = uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO posthog_erasure_outbox
		(id, idempotency_key, identities_encrypted, project_id, region, state, phase)
		VALUES ($1, $2, 'ENC$ciphertext', '42', 'us', 'pending', 'person_delete')`, id, "gdpr:"+id)
	require.NoError(t, err)

	userID := uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')`, userID, "clerk_"+userID, userID+"@example.com")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	require.NoError(t, err)
	var exists bool
	require.NoError(t, tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM posthog_erasure_outbox WHERE id=$1)`, id).Scan(&exists))
	assert.True(t, exists, "outbox must not cascade with users")

	_, err = tx.Exec(ctx, down)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, down)
	require.NoError(t, err, "down migration must be idempotent")
}
