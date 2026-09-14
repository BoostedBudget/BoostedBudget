package database

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration000213AddsDurableClerkAndSupportFence(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/000213_fence_posthog_support_during_erasure.up.sql")
	require.NoError(t, err)
	sql := string(raw)
	assert.Contains(t, sql, "clerk_user_id_encrypted")
	assert.Contains(t, sql, "clerk_user_id_blind_idx")
	assert.Contains(t, sql, "clerk_deleted_at")
	assert.Contains(t, sql, "clerk_fence_required")
	assert.Contains(t, sql, "UPDATE posthog_erasure_outbox SET clerk_fence_required = FALSE")
	assert.Contains(t, sql, "ALTER COLUMN clerk_fence_required SET DEFAULT TRUE")
	assert.Contains(t, sql, "'clerk_delete'")
	assert.Contains(t, sql, "(NOT clerk_fence_required OR clerk_deleted_at IS NOT NULL)")
	assert.Contains(t, sql, "clerk_user_id_blind_idx IS NULL")
}
