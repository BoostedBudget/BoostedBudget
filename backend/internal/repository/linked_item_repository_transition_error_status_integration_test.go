//go:build integration

package repository

// Integration tests for LinkedItemRepository.TransitionErrorStatus (Issue #1609 P3-T2).
//
// This is the atomic, predicated write that HandleLoginRequired/HandleError/
// HandlePendingExpiration/HandlePermissionRevoked now use instead of the full-row 17-column
// Update. Two hazards motivate it:
//
//  1. Duplicate notifications: a webhook and a background sync discovering the SAME failure for
//     the SAME item within seconds of each other must produce only ONE notification. The service
//     layer only notifies when TransitionErrorStatus reports true — so the second producer's call,
//     landing after the first already wrote the target status, must report false.
//  2. Last-write-wins status corruption: the same defect class that already shipped once this
//     phase (commit 5d75eab0, fixed in b668e408 via RepairStatus). Two producers racing on the
//     SAME row must not clobber each other's write with a stale in-memory copy — the predicate
//     must be evaluated atomically against a REAL concurrent write, not a Go-side read.
//
// pgxmock can only match query text, not whether a real concurrent transaction changed the row
// before this one's WHERE clause was evaluated — a real database (and a real second connection
// racing a real open transaction) is required, exactly as for RepairStatus.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestLinkedItemRepository_TransitionErrorStatus -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// terminalStatusesForTest mirrors item_error_service.go's terminalLinkedItemStatuses — duplicated
// here (rather than imported, since this is package repository, not services) to keep the
// integration test package-boundary-clean.
var terminalStatusesForTest = []domain.LinkedItemStatus{
	domain.LinkedItemStatusDisconnected,
	domain.LinkedItemStatusReplaced,
	domain.LinkedItemStatusRevoked,
}

// createTransitionErrorStatusFixtureItem inserts a linked_items row for userID in the given status.
// Mirrors createRepairStatusFixtureItem (linked_item_repository_repair_status_integration_test.go).
func createTransitionErrorStatusFixtureItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo LinkedItemRepository, userID, suffix string, status domain.LinkedItemStatus) *domain.LinkedItem {
	t.Helper()

	item := &domain.LinkedItem{
		UserID:      userID,
		ItemID:      "plaid-item-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, repo.Create(ctx, item))

	if status != "" && status != domain.LinkedItemStatusActive {
		_, err := pool.Exec(ctx, `UPDATE linked_items SET status = $2 WHERE id = $1`, item.ID, string(status))
		require.NoError(t, err)
	}
	return item
}

func TestLinkedItemRepository_TransitionErrorStatus_Integration_Success(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createRepairStatusFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createTransitionErrorStatusFixtureItem(t, ctx, pool, repo, userID, suffix, domain.LinkedItemStatusActive)

	transitioned, err := repo.TransitionErrorStatus(
		ctx, item.ID, domain.LinkedItemStatusLoginRequired, "ITEM_LOGIN_REQUIRED", "boom", terminalStatusesForTest,
	)
	require.NoError(t, err)
	assert.True(t, transitioned)

	got, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkedItemStatusLoginRequired, got.Status)
	require.NotNil(t, got.ErrorCode)
	assert.Equal(t, "ITEM_LOGIN_REQUIRED", *got.ErrorCode)
	require.NotNil(t, got.ErrorMessage)
	assert.Equal(t, "boom", *got.ErrorMessage)
	assert.NotNil(t, got.LastErrorAt)
	assert.NotNil(t, got.StatusChangedAt)
}

func TestLinkedItemRepository_TransitionErrorStatus_Integration_TerminalStatusRefused(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createRepairStatusFixtureUser(t, ctx, pool, profileRepo, suffix)
	// The user deliberately disconnected this item — a background sync error must never
	// overwrite that with an error status.
	item := createTransitionErrorStatusFixtureItem(t, ctx, pool, repo, userID, suffix, domain.LinkedItemStatusDisconnected)

	transitioned, err := repo.TransitionErrorStatus(
		ctx, item.ID, domain.LinkedItemStatusLoginRequired, "ITEM_LOGIN_REQUIRED", "boom", terminalStatusesForTest,
	)
	require.NoError(t, err)
	assert.False(t, transitioned, "a disconnected item must never be overwritten by a background error signal")

	got, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkedItemStatusDisconnected, got.Status)
}

func TestLinkedItemRepository_TransitionErrorStatus_Integration_AlreadyAtTargetRefused(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createRepairStatusFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createTransitionErrorStatusFixtureItem(t, ctx, pool, repo, userID, suffix, domain.LinkedItemStatusLoginRequired)

	// A second producer's signal for the SAME target status is a duplicate — reported false, not
	// an error, so the service layer knows not to notify again.
	transitioned, err := repo.TransitionErrorStatus(
		ctx, item.ID, domain.LinkedItemStatusLoginRequired, "ITEM_LOGIN_REQUIRED", "boom again", terminalStatusesForTest,
	)
	require.NoError(t, err)
	assert.False(t, transitioned)
}

// TestLinkedItemRepository_TransitionErrorStatus_Integration_ConcurrentProducersDedupe is the
// falsifiability-critical test for hazard 1 (duplicate notifications). It proves that two
// producers (modeling a webhook and a sync worker) discovering the SAME failure for the SAME item
// within moments of each other cause only ONE write to actually change the row — the second's
// UPDATE re-evaluates its WHERE clause against the now-committed row and matches zero rows.
//
// Mechanism: a transaction transitions the item to login_required and is held open (uncommitted)
// — Postgres holds the row's write lock until commit or rollback. A concurrent
// TransitionErrorStatus call for the SAME target status, on a SEPARATE connection, blocks on that
// lock; once the first commits, the second's UPDATE re-evaluates its WHERE clause (status <>
// newStatus) against the now-committed 'login_required' row, matches zero rows, and returns false.
// An implementation that dropped the "status <> newStatus" leg (or read-then-wrote in Go) would
// instead let the second producer "succeed" too, and the service layer would send a duplicate
// notification.
func TestLinkedItemRepository_TransitionErrorStatus_Integration_ConcurrentProducersDedupe(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createRepairStatusFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createTransitionErrorStatusFixtureItem(t, ctx, pool, repo, userID, suffix, domain.LinkedItemStatusActive)

	// Producer 1 (models the webhook): begins the transition and holds it open, uncommitted.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		UPDATE linked_items
		SET status = 'login_required', error_code = 'ITEM_LOGIN_REQUIRED', error_message = 'from webhook',
		    last_error_at = NOW(), status_changed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status <> 'login_required' AND status <> ALL($2)
	`, item.ID, []string{"disconnected", "replaced", "revoked"})
	require.NoError(t, err)
	// Deliberately NOT committed yet — holds the row's write lock, so producer 2's
	// TransitionErrorStatus call below (on the pool, a different connection) must block on it.

	type transitionResult struct {
		transitioned bool
		err          error
	}
	producer2Done := make(chan transitionResult, 1)
	go func() {
		// Producer 2 (models the sync worker): discovers the SAME failure moments later.
		transitioned, err := repo.TransitionErrorStatus(
			ctx, item.ID, domain.LinkedItemStatusLoginRequired, "ITEM_LOGIN_REQUIRED", "from sync", terminalStatusesForTest,
		)
		producer2Done <- transitionResult{transitioned, err}
	}()

	select {
	case result := <-producer2Done:
		require.NoError(t, tx.Rollback(ctx))
		t.Fatalf("producer 2's TransitionErrorStatus returned (%v, %v) before producer 1 committed — "+
			"it did not block on the row lock as this test requires to prove atomicity", result.transitioned, result.err)
	case <-time.After(200 * time.Millisecond):
		// Expected: still blocked.
	}

	require.NoError(t, tx.Commit(ctx))

	select {
	case result := <-producer2Done:
		require.NoError(t, result.err)
		assert.False(t, result.transitioned, "producer 2 must NOT report success once producer 1 committed the "+
			"same target status — reporting true here means the service layer would send a duplicate notification")
	case <-time.After(5 * time.Second):
		t.Fatal("producer 2's TransitionErrorStatus did not return after the blocking transaction committed")
	}

	got, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkedItemStatusLoginRequired, got.Status)
	require.NotNil(t, got.ErrorMessage)
	assert.Equal(t, "from webhook", *got.ErrorMessage, "producer 1's write must survive untouched — producer 2 must not have overwritten it")
}

// TestLinkedItemRepository_TransitionErrorStatus_Integration_ConcurrentDisconnectWinsRace proves
// hazard 2 (last-write-wins status corruption) is closed for the error-transition path exactly as
// RepairStatus closed it for the repair path: a disconnect committed WHILE a background error
// signal is in flight must win, and the error signal must report false rather than silently
// resurrecting the item into an error status.
func TestLinkedItemRepository_TransitionErrorStatus_Integration_ConcurrentDisconnectWinsRace(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createRepairStatusFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createTransitionErrorStatusFixtureItem(t, ctx, pool, repo, userID, suffix, domain.LinkedItemStatusActive)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE linked_items SET status = 'disconnected' WHERE id = $1`, item.ID)
	require.NoError(t, err)
	// Deliberately NOT committed yet — holds the row's write lock.

	type transitionResult struct {
		transitioned bool
		err          error
	}
	transitionDone := make(chan transitionResult, 1)
	go func() {
		transitioned, err := repo.TransitionErrorStatus(
			ctx, item.ID, domain.LinkedItemStatusLoginRequired, "ITEM_LOGIN_REQUIRED", "boom", terminalStatusesForTest,
		)
		transitionDone <- transitionResult{transitioned, err}
	}()

	select {
	case result := <-transitionDone:
		require.NoError(t, tx.Rollback(ctx))
		t.Fatalf("TransitionErrorStatus returned (%v, %v) before the concurrent disconnect committed — "+
			"it did not block on the row lock as this test requires to prove atomicity", result.transitioned, result.err)
	case <-time.After(200 * time.Millisecond):
		// Expected: still blocked.
	}

	require.NoError(t, tx.Commit(ctx))

	select {
	case result := <-transitionDone:
		require.NoError(t, result.err)
		assert.False(t, result.transitioned, "TransitionErrorStatus must NOT report success once the concurrent "+
			"disconnect committed — reporting true here would resurrect a disconnected item into an error status")
	case <-time.After(5 * time.Second):
		t.Fatal("TransitionErrorStatus did not return after the blocking transaction committed")
	}

	got, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkedItemStatusDisconnected, got.Status,
		"the concurrent disconnect must win the race — the item must NOT have been overwritten with an error status")
}
