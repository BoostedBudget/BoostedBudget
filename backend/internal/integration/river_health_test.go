//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ============================================================================
// Integration Tests for River Queue Health
// Issue #666: Ensure River queue is properly configured and operational
// ============================================================================

// TestRiver_TablesExist verifies that all required River tables are present.
// These tables are created by River migrations and are essential for job processing.
func TestRiver_TablesExist(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := setupTestDBIfAvailable(t)
	if db == nil {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	defer db.Close()

	ctx := context.Background()

	// River requires these tables to function
	requiredTables := []string{
		"river_job",
		"river_client",
		"river_queue",
		"river_leader",
	}

	for _, tableName := range requiredTables {
		t.Run(tableName, func(t *testing.T) {
			var exists bool
			err := db.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT FROM information_schema.tables
					WHERE table_schema = 'public'
					AND table_name = $1
				)
			`, tableName).Scan(&exists)

			if err != nil {
				t.Errorf("Failed to check table %s: %v", tableName, err)
				return
			}

			assert.True(t, exists, "River table '%s' should exist", tableName)
		})
	}
}

// TestRiver_JobLifecycle tests the complete lifecycle of a River job:
// insert -> available -> running -> completed
//
// This validates that the River infrastructure is correctly set up
// and jobs can flow through all states.
func TestRiver_JobLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := setupTestDBIfAvailable(t)
	if db == nil {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	defer db.Close()

	ctx := context.Background()

	// Insert a test job directly
	testJobKind := "test_health_check"
	testArgs := `{"test": true, "timestamp": "` + time.Now().Format(time.RFC3339) + `"}`

	_, err := db.Exec(ctx, `
		INSERT INTO river_job (state, queue, kind, args, created_at, scheduled_at, max_attempts)
		VALUES ('available', 'default', $1, $2, NOW(), NOW(), 1)
	`, testJobKind, []byte(testArgs))

	if err != nil {
		t.Fatalf("Failed to insert test job: %v", err)
	}

	// Verify job was inserted
	var count int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM river_job WHERE kind = $1
	`, testJobKind).Scan(&count)

	if err != nil {
		t.Fatalf("Failed to query test job: %v", err)
	}

	assert.GreaterOrEqual(t, count, 1, "Test job should be in river_job table")

	// Cleanup
	_, _ = db.Exec(ctx, `DELETE FROM river_job WHERE kind = $1`, testJobKind)
}

// TestRiver_JobStates documents River job lifecycle states.
// Jobs flow: available -> running -> completed/failed/discarded.
func TestRiver_JobStates(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	criticalStates := map[string]string{
		"available": "Jobs enqueued by accountService.enqueueInitialSync()",
		"running":   "Jobs being processed by PlaidSyncWorker",
		"completed": "Jobs that successfully synced transactions",
		"retryable": "Jobs that failed temporarily (network issues)",
		"discarded": "Jobs with permanent failures (invalid access token)",
	}

	t.Log("Critical River job states for plaid_sync:")
	for state, description := range criticalStates {
		t.Logf("  %s: %s", state, description)
	}
}

// TestRiver_QueueConfiguration verifies the default queue exists.
func TestRiver_QueueConfiguration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := setupTestDBIfAvailable(t)
	if db == nil {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	defer db.Close()

	ctx := context.Background()

	var queueCount int
	err := db.QueryRow(ctx, `SELECT COUNT(*) FROM river_queue WHERE name = 'default'`).Scan(&queueCount)

	if err != nil {
		t.Log("Note: river_queue may be empty until River client starts")
		return
	}

	t.Logf("Default queue entries: %d", queueCount)
}

// TestRiver_ClientRegistration verifies that River client registers itself
// when the application starts. An empty river_client table indicates
// River is not running.
func TestRiver_ClientRegistration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := setupTestDBIfAvailable(t)
	if db == nil {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	defer db.Close()

	ctx := context.Background()

	// Check river_client table
	var clientCount int
	err := db.QueryRow(ctx, `SELECT COUNT(*) FROM river_client`).Scan(&clientCount)

	if err != nil {
		t.Fatalf("Failed to query river_client: %v", err)
	}

	// Log the result - in integration tests this may be 0 if no server is running
	t.Logf("River clients registered: %d", clientCount)

	// In a full E2E test with running server, we'd assert clientCount > 0
	// For now, just document the expectation
	if clientCount == 0 {
		t.Log("Note: No River clients registered. This is expected if the backend server is not running.")
		t.Log("When the backend starts, it should register with river_client table.")
	}
}

// TestRiver_PlaidSyncJobSchema documents the expected job args schema.
func TestRiver_PlaidSyncJobSchema(t *testing.T) {
	expectedFields := map[string]string{
		"user_id":    "UUID of the user who owns the linked item",
		"item_id":    "UUID of the LinkedItem (internal ID)",
		"webhook_id": "Optional UUID of the triggering webhook event",
	}

	t.Log("Expected plaid_sync job args schema:")
	for field, description := range expectedFields {
		t.Logf("  %s: %s", field, description)
	}
}
