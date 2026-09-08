//go:build integration

package repository

// Integration tests for LinkedItemRepository.RepairStatus (Issue #1609 round 2, BLOCKING 2).
//
// RepairItem/HandleLoginRepaired used to read a linked item, check its status IN GO, then call
// the 17-column full-row Update with no status or user predicate. A concurrent disconnect or
// re-link landing between the read and the write was silently overwritten: 'disconnected' or
// 'replaced' could be resurrected to 'active', and replaced_at/replaced_by_id erased.
// RepairStatus closes that window with a single atomic
// `UPDATE ... WHERE id = $1 AND status = ANY($2) [AND user_id = $3]` and reports whether it
// actually matched via RowsAffected — pgxmock can only match query text, not whether a REAL
// concurrent transaction changed the row before this one's WHERE clause was evaluated, so a real
// database (and a real second connection racing a real open transaction) is required.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestLinkedItemRepository_RepairStatus -tags integration -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// createRepairStatusFixtureUser inserts a bare user row for linked-item fixtures, registering
// cleanup. Mirrors createMarkDisconnectedFixtureUser (linked_item_repository_mark_disconnected_
// integration_test.go).
func createRepairStatusFixtureUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, profileRepo ProfileRepository, suffix string) string {
	t.Helper()

	user := &domain.User{
		ClerkUserID:        "clerk_repairstatus_" + suffix,
		Email:              "repairstatus-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID) })
	return user.ID
}

// createRepairStatusFixtureItem inserts a linked_items row for userID in the given status.
func createRepairStatusFixtureItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo LinkedItemRepository, userID, suffix string, status domain.LinkedItemStatus) *domain.LinkedItem {
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

func TestLinkedItemRepository_RepairStatus_Integration_Success(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createRepairStatusFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createRepairStatusFixtureItem(t, ctx, pool, repo, userID, suffix, domain.LinkedItemStatusLoginRequired)

	errCode := "ITEM_LOGIN_REQUIRED"
	_, err := pool.Exec(ctx, `UPDATE linked_items SET error_code = $2, error_message = 'boom', last_error_at = NOW() WHERE id = $1`, item.ID, errCode)
	require.NoError(t, err)

	repaired, err := repo.RepairStatus(ctx, item.ID, userID, []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired})
	require.NoError(t, err)
	assert.True(t, repaired)

	got, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkedItemStatusActive, got.Status)
	assert.Nil(t, got.ErrorCode)
	assert.Nil(t, got.ErrorMessage)
	assert.Nil(t, got.LastErrorAt)
	require.NotNil(t, got.StatusChangedAt)
}

func TestLinkedItemRepository_RepairStatus_Integration_WrongUserRefused(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createRepairStatusFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createRepairStatusFixtureItem(t, ctx, pool, repo, userID, suffix, domain.LinkedItemStatusLoginRequired)

	repaired, err := repo.RepairStatus(ctx, item.ID, uuid.NewString(), []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired})
	require.NoError(t, err)
	assert.False(t, repaired, "a user_id mismatch must refuse the transition, not silently repair someone else's item")

	got, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkedItemStatusLoginRequired, got.Status)
}

func TestLinkedItemRepository_RepairStatus_Integration_StatusNotAllowedRefused(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createRepairStatusFixtureUser(t, ctx, pool, profileRepo, suffix)
	// Issue #1609 round 2, BLOCKING 1: a 'revoked' item must never be repaired.
	item := createRepairStatusFixtureItem(t, ctx, pool, repo, userID, suffix, domain.LinkedItemStatusRevoked)

	repaired, err := repo.RepairStatus(ctx, item.ID, userID, []domain.LinkedItemStatus{
		domain.LinkedItemStatusLoginRequired,
		domain.LinkedItemStatusError,
		domain.LinkedItemStatusPendingExpiration,
	})
	require.NoError(t, err)
	assert.False(t, repaired)

	got, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkedItemStatusRevoked, got.Status, "a revoked item must stay revoked, never silently become active")
}

func TestLinkedItemRepository_RepairStatus_Integration_EmptyUserIDSkipsOwnershipFilter(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createRepairStatusFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createRepairStatusFixtureItem(t, ctx, pool, repo, userID, suffix, domain.LinkedItemStatusLoginRequired)

	// Mirrors HandleLoginRepaired's call: the LOGIN_REPAIRED webhook is system-triggered with no
	// authenticated caller, so it passes userID="" to skip the ownership filter entirely.
	repaired, err := repo.RepairStatus(ctx, item.ID, "", []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired})
	require.NoError(t, err)
	assert.True(t, repaired)

	got, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkedItemStatusActive, got.Status)
}

// TestLinkedItemRepository_RepairStatus_Integration_ConcurrentDisconnectWinsRace is the
// falsifiability-critical test for BLOCKING 2. It proves RepairStatus's predicate is evaluated
// atomically against a real concurrent write, not against a stale in-memory read: a disconnect
// committed WHILE a repair is in flight must win, and the repair must report false rather than
// silently resurrecting the item to 'active'.
//
// Mechanism: a transaction disconnects the item and is held open (uncommitted) — Postgres holds
// the row's write lock until commit or rollback. A concurrent RepairStatus call on a SEPARATE
// connection blocks on that lock; once the disconnect commits, Postgres releases the lock and
// RepairStatus's UPDATE re-evaluates its WHERE clause against the now-committed 'disconnected'
// row, matches zero rows, and returns false. An implementation that reverted to the pre-#1609-
// round-2 pattern (read status in Go, then an unconditional/status-blind Update) would instead
// resurrect the item to 'active' here and this test would fail.
func TestLinkedItemRepository_RepairStatus_Integration_ConcurrentDisconnectWinsRace(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	ctx := context.Background()
	profileRepo := NewProfileRepository(pool, enc)
	repo := NewLinkedItemRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID := createRepairStatusFixtureUser(t, ctx, pool, profileRepo, suffix)
	item := createRepairStatusFixtureItem(t, ctx, pool, repo, userID, suffix, domain.LinkedItemStatusLoginRequired)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE linked_items SET status = 'disconnected' WHERE id = $1`, item.ID)
	require.NoError(t, err)
	// Deliberately NOT committed yet — this holds the row's write lock, so the concurrent
	// RepairStatus call below (on the pool, a different connection) must block on it.

	type repairResult struct {
		repaired bool
		err      error
	}
	repairDone := make(chan repairResult, 1)
	go func() {
		repaired, err := repo.RepairStatus(ctx, item.ID, userID, []domain.LinkedItemStatus{domain.LinkedItemStatusLoginRequired})
		repairDone <- repairResult{repaired, err}
	}()

	// Give the goroutine time to reach the UPDATE and block on tx's row lock before committing.
	select {
	case result := <-repairDone:
		require.NoError(t, tx.Rollback(ctx))
		t.Fatalf("RepairStatus returned (%v, %v) before the concurrent disconnect committed — "+
			"it did not block on the row lock as this test requires to prove atomicity", result.repaired, result.err)
	case <-time.After(200 * time.Millisecond):
		// Expected: still blocked.
	}

	require.NoError(t, tx.Commit(ctx))

	select {
	case result := <-repairDone:
		require.NoError(t, result.err)
		assert.False(t, result.repaired, "RepairStatus must NOT report success once the concurrent "+
			"disconnect committed — reporting true here is the exact defect (Issue #1609 round 2, "+
			"BLOCKING 2) where a status-blind full-row Update silently resurrected a disconnected "+
			"item to active")
	case <-time.After(5 * time.Second):
		t.Fatal("RepairStatus did not return after the blocking transaction committed")
	}

	got, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.LinkedItemStatusDisconnected, got.Status,
		"the concurrent disconnect must win the race — the item must NOT have been resurrected to active")
}
