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
	assert.Contains(t, sql, "'clerk_delete'")
	assert.Contains(t, sql, "clerk_deleted_at IS NOT NULL")
}
