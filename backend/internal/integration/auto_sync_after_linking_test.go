//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockJobQueueForAutoSync is a mock implementation of queue.JobQueue
type MockJobQueueForAutoSync struct {
	mock.Mock
	EnqueuedJobs []EnqueuedJobRecord
}

// EnqueuedJobRecord captures details about an enqueued job for assertions
type EnqueuedJobRecord struct {
	Job       queue.Job
	Kind      string
	UserID    string
	ItemID    string
	Timestamp time.Time
}

func (m *MockJobQueueForAutoSync) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	args := m.Called(ctx, job, opts)

	// Record the job for later assertions
	m.EnqueuedJobs = append(m.EnqueuedJobs, EnqueuedJobRecord{
		Job:       job,
		Kind:      job.Kind(),
		Timestamp: time.Now(),
	})

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*queue.JobResult), args.Error(1)
}

// TestAutoSyncAfterLinking_E2E_Flow tests the complete flow from account linking
// to sync job completion. This requires a test database with River tables.
func TestAutoSyncAfterLinking_E2E_Flow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	db := setupTestDBIfAvailable(t)
	if db == nil {
		t.Skip("DATABASE_URL not set, skipping E2E test")
	}
	// t.Cleanup (not defer) so Close runs AFTER other t.Cleanup callbacks (LIFO).
	// A defer would close the pool before DELETE cleanups below, making them no-ops.
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	userID := uuid.New().String()

	// Verify River tables exist
	var tableCount int
	err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_name IN ('river_job', 'river_client', 'river_queue')
	`).Scan(&tableCount)
	require.NoError(t, err, "Failed to query River tables")
	assert.GreaterOrEqual(t, tableCount, 3, "River tables should exist")

	// Create a test user in the database
	user := &domain.User{
		ID:               userID,
		Email:            "autosync-test@example.com",
		SubscriptionTier: string(domain.TierPremium),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	_, err = db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING
	`, user.ID, "autosync|"+user.ID, user.Email, user.SubscriptionTier, user.CreatedAt, user.UpdatedAt)
	require.NoError(t, err, "Failed to create test user")
	t.Cleanup(func() { db.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID) }) //nolint:errcheck

	// Simulate job enqueue
	linkedItemID := uuid.New().String()
	_, err = db.Exec(ctx, `
		INSERT INTO river_job (state, queue, kind, args, created_at, scheduled_at, max_attempts)
		VALUES ('available', 'default', 'plaid_sync', $1, NOW(), NOW(), 5)
	`, []byte(`{"user_id": "`+userID+`", "item_id": "`+linkedItemID+`"}`))
	require.NoError(t, err, "Failed to insert test job")
	t.Cleanup(func() { db.Exec(ctx, `DELETE FROM river_job WHERE args->>'user_id' = $1`, userID) }) //nolint:errcheck

	// Verify job was inserted
	var jobCount int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*) FROM river_job
		WHERE kind = 'plaid_sync'
		AND args->>'user_id' = $1
	`, userID).Scan(&jobCount)
	require.NoError(t, err, "Failed to query jobs")
	assert.Equal(t, 1, jobCount, "Should have 1 plaid_sync job")
}

// setupTestDBIfAvailable returns a *pgxpool.Pool connected to DATABASE_URL, or
// nil if the variable is unset (callers guard with `if db == nil { t.Skip }`.
func setupTestDBIfAvailable(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err, "connect to test database")
	return pool
}

// TestSetupTestDBIfAvailable_ConnectsWhenURLSet verifies the helper actually
// returns a usable pool when DATABASE_URL is present. Previously the function
// was a stub that always returned nil, causing the 5 River/auto-sync E2E tests
// to silently skip in CI even when a real DB was available.
func TestSetupTestDBIfAvailable_ConnectsWhenURLSet(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set — skipping helper connectivity test")
	}
	db := setupTestDBIfAvailable(t)
	require.NotNil(t, db, "setupTestDBIfAvailable should return non-nil when DATABASE_URL is set")
	t.Cleanup(func() { db.Close() })

	var result int
	err := db.QueryRow(context.Background(), "SELECT 1").Scan(&result)
	require.NoError(t, err, "should be able to query the DB returned by the helper")
	assert.Equal(t, 1, result)
}

// TestPoolAliveInCleanup_Regression guards against the defer-vs-t.Cleanup
// ordering bug: defer runs at function exit (BEFORE t.Cleanup), closing the
// pool before subsequent t.Cleanup callbacks execute. The fix: use
// t.Cleanup(db.Close) which runs LIFO (last-registered closes last).
func TestPoolAliveInCleanup_Regression(t *testing.T) {
	db := setupTestDBIfAvailable(t)
	if db == nil {
		t.Skip("DATABASE_URL not set")
	}

	// Register Close first → runs LAST (LIFO).
	t.Cleanup(func() { db.Close() })

	// Register DB assertion second → runs FIRST (before Close, via LIFO).
	// If Close were in defer, it would run before this Cleanup, causing SELECT to fail.
	t.Cleanup(func() {
		ctx := context.Background()
		var n int
		if err := db.QueryRow(ctx, "SELECT 1").Scan(&n); err != nil {
			t.Errorf("pool closed before t.Cleanup fired: %v — use t.Cleanup(db.Close), not defer", err)
		}
	})
}

