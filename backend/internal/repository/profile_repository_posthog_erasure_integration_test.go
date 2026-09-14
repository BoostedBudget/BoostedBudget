//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
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

func TestPostHogErasureOperatorCompletion_IntegrationAtomicallyDestroysIdentity(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewPostHogErasureOutboxRepository(pool, enc)
	ctx := context.Background()
	id := uuid.NewString()
	identities, err := enc.EncryptJSON([]byte(`["clerk_secret"]`))
	require.NoError(t, err)
	personUUIDs, err := enc.EncryptJSON([]byte(`["11111111-1111-4111-8111-111111111111"]`))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO posthog_erasure_outbox
		(id,idempotency_key,identities_encrypted,provider_person_uuids_encrypted,project_id,region,state,phase,
		 accepted_at,person_verified_at,events_verified_at,recordings_first_verified_at,surveys_first_verified_at,support_first_verified_at)
		VALUES($1,$2,$3,$4,'42','us','manual_pending','support_manual',clock_timestamp(),clock_timestamp(),clock_timestamp(),
		 clock_timestamp()-interval '6 minutes',clock_timestamp()-interval '6 minutes',clock_timestamp()-interval '6 minutes')`,
		id, "completion:"+id, identities, personUUIDs)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_operator_audit WHERE erasure_id=$1", id)
		_, _ = pool.Exec(ctx, "DELETE FROM posthog_erasure_outbox WHERE id=$1", id)
	})
	for _, surface := range []string{"recordings", "surveys", "support"} {
		_, err = repo.RecordOperatorEvidence(ctx, id, uuid.NewString(), surface, "provider_zero")
		require.NoError(t, err)
	}
	var state string
	var remainingIdentities, remainingUUIDs *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT state,identities_encrypted,provider_person_uuids_encrypted FROM posthog_erasure_outbox WHERE id=$1`, id).Scan(&state, &remainingIdentities, &remainingUUIDs))
	assert.Equal(t, "completed", state)
	assert.Nil(t, remainingIdentities)
	assert.Nil(t, remainingUUIDs)
}
