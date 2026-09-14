//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteUserCascade_Integration_PostHogOutboxSurvivesWithEncryptedHistoricalIdentities(t *testing.T) {
	pool := connectTestDB(t)
	enc, err := appcrypto.NewFieldEncryptor(reversibleTestEncryption{}, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "posthog-erasure-integration-" + suffix
	repo.SetPostHogErasureTarget(project, "us")
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)

	historical := "clerk_historical_" + suffix
	historicalEncrypted, err := enc.EncryptString(historical)
	require.NoError(t, err)
	providerEncrypted, err := enc.EncryptString("provider_" + suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO user_identities
		(user_id, clerk_user_id, provider, provider_user_id, is_primary, clerk_user_id_blind_idx)
		VALUES ($1, $2, 'github', $3, FALSE, $4)`,
		user.ID, historicalEncrypted, providerEncrypted, enc.BlindIndex(historical))
	require.NoError(t, err)

	deleted, _, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted)
	assertUserRowGone(t, ctx, pool, user.ID)

	var outboxID, ciphertext string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT id, identities_encrypted FROM posthog_erasure_outbox WHERE project_id=$1`, project).
		Scan(&outboxID, &ciphertext))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox WHERE id=$1", outboxID) })
	assert.NotContains(t, ciphertext, historical)
	assert.NotContains(t, ciphertext, user.ClerkUserID)
	raw, err := enc.DecryptJSON(ciphertext)
	require.NoError(t, err)
	var identities []string
	require.NoError(t, json.Unmarshal(raw, &identities))
	assert.ElementsMatch(t, []string{user.ID, user.ClerkUserID, historical}, identities)
}

func TestDeleteUserCascade_Integration_OutboxInsertFailureRollsBackLocalErasure(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewProfileRepository(pool, enc)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	project := "posthog-erasure-rollback-" + suffix
	repo.SetPostHogErasureTarget(project, "us")
	user := createDeleteCascadeFixtureUser(t, ctx, pool, repo, suffix, nil, nil, nil)
	constraint := "ck_posthog_erasure_rollback_" + suffix
	_, err := pool.Exec(ctx, fmt.Sprintf(
		"ALTER TABLE posthog_erasure_outbox ADD CONSTRAINT %s CHECK (project_id <> '%s')", constraint, project))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, fmt.Sprintf("ALTER TABLE posthog_erasure_outbox DROP CONSTRAINT IF EXISTS %s", constraint))
	})

	deleted, _, err := repo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.Error(t, err)
	assert.False(t, deleted)
	assertUserRowExists(t, ctx, pool, user.ID)
	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM posthog_erasure_outbox WHERE project_id=$1", project).Scan(&count))
	assert.Zero(t, count)
}
