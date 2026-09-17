package database

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration000212_TransactionSyncCompletionGenerationContract(t *testing.T) {
	up, err := os.ReadFile("../../migrations/000212_add_transaction_sync_completion_generation.up.sql")
	require.NoError(t, err)
	assert.Contains(t, string(up), "transaction_sync_completion_generation BIGINT NOT NULL DEFAULT 0")
	assert.Contains(t, string(up), "transaction_sync_completion_generation >= 0")

	down, err := os.ReadFile("../../migrations/000212_add_transaction_sync_completion_generation.down.sql")
	require.NoError(t, err)
	assert.Contains(t, string(down), "REFUSING TO ROLL BACK 000212")
	assert.Contains(t, string(down), "transaction_sync_completion_generation <> 0")
}
