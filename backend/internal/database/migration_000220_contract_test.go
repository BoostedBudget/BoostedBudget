package database

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration000220_RevenueCatOperationAbandonedContract(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/000220_revenuecat_operation_abandoned.up.sql")
	require.NoError(t, err)
	sql := string(raw)
	for _, fragment := range []string{
		"DROP CONSTRAINT IF EXISTS revenuecat_operations_stage_check",
		"CHECK (stage IN ('pending', 'applied', 'superseded', 'abandoned'))",
		"NOT VALID",
		"VALIDATE CONSTRAINT revenuecat_operations_stage_check",
	} {
		assert.Contains(t, sql, fragment)
	}

	down, err := os.ReadFile("../../migrations/000220_revenuecat_operation_abandoned.down.sql")
	require.NoError(t, err)
	assert.Contains(t, string(down), "LOCK TABLE revenuecat_operations IN ACCESS EXCLUSIVE MODE")
	assert.Contains(t, string(down), "RevenueCat operation rollback refused while abandoned operations exist")
	assert.Contains(t, string(down), "CHECK (stage IN ('pending', 'applied', 'superseded'))")
}
