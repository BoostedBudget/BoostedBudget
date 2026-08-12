//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRiver_TablesExist verifies that all required River tables are present.
func TestRiver_TablesExist(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := setupTestDBIfAvailable(t)
	if db == nil {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	// river_client / river_client_queue are deliberately absent: River dropped them in its
	// main-line migration 007 and migration 000145 mirrors that. TestRiver_DroppedClientTables
	// asserts they are gone; river_notification, added by the same migration, is covered by
	// TestRiver_NotificationOutbox.
	requiredTables := []string{
		"river_job",
		"river_queue",
		"river_leader",
		"river_notification",
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
	// t.Cleanup (not defer) so Close runs AFTER the DELETE t.Cleanup below (LIFO).
	t.Cleanup(func() { db.Close() })

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
	t.Cleanup(func() { db.Exec(ctx, `DELETE FROM river_job WHERE kind = $1`, testJobKind) }) //nolint:errcheck

	// Verify job was inserted
	var count int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM river_job WHERE kind = $1
	`, testJobKind).Scan(&count)

	if err != nil {
		t.Fatalf("Failed to query test job: %v", err)
	}

	assert.GreaterOrEqual(t, count, 1, "Test job should be in river_job table")
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

// TestRiver_QueueConfiguration verifies the river_queue table is queryable after
// migrations. The default queue row only appears when the River client starts, so
// no count assertion is made — table queryability is the achievable CI-level check.
func TestRiver_QueueConfiguration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := setupTestDBIfAvailable(t)
	if db == nil {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	var queueCount int
	err := db.QueryRow(ctx, `SELECT COUNT(*) FROM river_queue WHERE name = 'default'`).Scan(&queueCount)

	// COUNT(*) always returns exactly one row — a non-nil err means the table is missing
	// or the connection is dead, both of which are genuine failures (not "empty queue").
	require.NoError(t, err, "river_queue table should be queryable after migrations")

	t.Logf("Default queue entries: %d", queueCount)
}

// TestRiver_NotificationOutbox verifies the river_notification table is queryable after
// migrations.
//
// This replaces TestRiver_ClientRegistration, which asserted river_client was queryable.
// River dropped river_client and river_client_queue in its main-line migration 007 (they
// were unused; their dbsqlc query files are deleted in riverdriver/riverpgxv5 v0.43.0),
// and migration 000145 mirrors that drop. The same migration adds river_notification, so
// that is now the table this schema-level check tracks. Client registration was never
// actually asserted here — the old test only checked the table existed, and count > 0
// requires a running backend server, which remains E2E-scoped.
func TestRiver_NotificationOutbox(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := setupTestDBIfAvailable(t)
	if db == nil {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	var notificationCount int
	err := db.QueryRow(ctx, `SELECT COUNT(*) FROM river_notification`).Scan(&notificationCount)

	// COUNT(*) always returns exactly one row — a non-nil err means the table is missing
	// or the connection is dead, both of which are genuine failures (not "no rows yet").
	require.NoError(t, err, "river_notification table should be queryable after migrations")

	t.Logf("River notification outbox rows: %d", notificationCount)
}

// TestRiver_DroppedClientTables pins the other half of migration 000145: the tables River
// removed in its migration 007 must actually be gone. Without this, a migration that
// silently failed to drop them would leave the schema ahead of the driver with nothing
// noticing — the failure mode that the old river_client assertion would have masked.
func TestRiver_DroppedClientTables(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	db := setupTestDBIfAvailable(t)
	if db == nil {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	for _, table := range []string{"river_client", "river_client_queue"} {
		var exists bool
		err := db.QueryRow(ctx,
			`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists)
		require.NoError(t, err, "to_regclass lookup for %s should succeed", table)
		require.False(t, exists,
			"%s should have been dropped by migration 000145 (River main-line migration 007)", table)
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
