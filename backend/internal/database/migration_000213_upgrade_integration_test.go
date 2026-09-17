//go:build integration

package database

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration000213UpgradesPopulated211RowsWithoutStrandingObligations(t *testing.T) {
	pool := connectMigrationTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	schema := "migration_213_" + uuid.NewString()
	_, err = tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	require.NoError(t, err)
	_, err = tx.Exec(ctx, fmt.Sprintf("SET LOCAL search_path TO %s, public", pgx.Identifier{schema}.Sanitize()))
	require.NoError(t, err)
	_, err = tx.Exec(ctx, readMigrationFile(t, "000211_add_posthog_erasure_outbox.up.sql"))
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `INSERT INTO posthog_erasure_outbox
		(idempotency_key, identities_encrypted, project_id, region)
		VALUES ('legacy-outstanding', 'ENC$legacy', '1', 'us')`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO posthog_erasure_outbox
		(idempotency_key, identities_encrypted, project_id, region, state, phase, completed_at,
		 person_verified_at, events_verified_at, recordings_verified_at, surveys_verified_at,
		 support_verified_at)
		VALUES ('legacy-completed', NULL, '1', 'us', 'completed', 'complete', clock_timestamp(),
		 clock_timestamp(), clock_timestamp(), clock_timestamp(), clock_timestamp(), clock_timestamp())`)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, readMigrationFile(t, "000213_fence_posthog_support_during_erasure.up.sql"))
	require.NoError(t, err)

	rows, err := tx.Query(ctx, `SELECT idempotency_key, clerk_fence_required, phase
		FROM posthog_erasure_outbox ORDER BY idempotency_key`)
	require.NoError(t, err)
	defer rows.Close()
	legacy := map[string]struct {
		required bool
		phase    string
	}{}
	for rows.Next() {
		var key, phase string
		var required bool
		require.NoError(t, rows.Scan(&key, &required, &phase))
		legacy[key] = struct {
			required bool
			phase    string
		}{required: required, phase: phase}
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, "person_resolve", legacy["legacy-outstanding"].phase)
	assert.False(t, legacy["legacy-outstanding"].required)
	assert.False(t, legacy["legacy-completed"].required)

	var required bool
	err = tx.QueryRow(ctx, `INSERT INTO posthog_erasure_outbox
		(idempotency_key, identities_encrypted, project_id, region, phase)
		VALUES ('post-213', 'ENC$new', '1', 'us', 'clerk_delete')
		RETURNING clerk_fence_required`).Scan(&required)
	require.NoError(t, err)
	assert.True(t, required)
}
