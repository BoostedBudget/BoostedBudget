// backend/internal/repository/household_repository_integration_test.go
package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// insertTestHouseholdMember inserts a household_members row and returns its UUID.
func insertTestHouseholdMember(t *testing.T, pool *pgxpool.Pool, householdID, userID, role string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO household_members (household_id, user_id, role, status)
		VALUES ($1, $2, $3, 'active')
		RETURNING id::text
	`, householdID, userID, role).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestHouseholdMember: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_members WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

// insertTestInvite inserts a household_invites row with the given status/expiry and returns its UUID.
func insertTestInvite(t *testing.T, pool *pgxpool.Pool, householdID, inviterID string, expiresAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO household_invites (household_id, inviter_user_id, invitee_email, invitee_email_blind_idx, token_hash, status, expires_at)
		VALUES ($1, $2, 'enc_test', 'blind_test_'||gen_random_uuid()::text, 'token_'||gen_random_uuid()::text, 'pending', $3)
		RETURNING id::text
	`, householdID, inviterID, expiresAt).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestInvite: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM household_invites WHERE id = $1`, id) //nolint:errcheck
	})
	return id
}

func TestHouseholdRepository_GetHouseholdForUser(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)
	ownerID := insertTestUser(t, pool)
	memberID := insertTestUser(t, pool)
	strangerID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")
	insertTestHouseholdMember(t, pool, householdID, memberID, "member")

	h, isOwner, found, err := repo.GetHouseholdForUser(ctx, ownerID)
	if err != nil || !found || !isOwner || h.ID != householdID {
		t.Fatalf("owner lookup: h=%+v isOwner=%v found=%v err=%v", h, isOwner, found, err)
	}

	h, isOwner, found, err = repo.GetHouseholdForUser(ctx, memberID)
	if err != nil || !found || isOwner || h.ID != householdID {
		t.Fatalf("member lookup: h=%+v isOwner=%v found=%v err=%v", h, isOwner, found, err)
	}

	_, _, found, err = repo.GetHouseholdForUser(ctx, strangerID)
	if err != nil || found {
		t.Fatalf("stranger lookup should be not-found: found=%v err=%v", found, err)
	}
}

func TestHouseholdRepository_CountActiveMembers(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)
	ownerID := insertTestUser(t, pool)
	memberID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")
	insertTestHouseholdMember(t, pool, householdID, memberID, "member")

	count, err := repo.CountActiveMembers(ctx, householdID)
	if err != nil || count != 2 {
		t.Fatalf("expected 2 active members, got %d (err=%v)", count, err)
	}
}

func TestHouseholdRepository_CountPendingInvites_ExcludesExpired(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)
	ownerID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestInvite(t, pool, householdID, ownerID, time.Now().Add(24*time.Hour))  // valid
	insertTestInvite(t, pool, householdID, ownerID, time.Now().Add(-24*time.Hour)) // expired, unswept

	count, err := repo.CountPendingInvites(ctx, householdID)
	if err != nil || count != 1 {
		t.Fatalf("expected 1 non-expired pending invite, got %d (err=%v)", count, err)
	}
}

// TestHouseholdRepository_DeleteRemovedMember_Integration proves the safety-critical
// guard on DeleteRemovedMember (`WHERE id = $1 AND status = 'removed'`) actually holds
// against a real schema: it must delete a genuinely-removed row, but must NEVER delete
// (or otherwise affect) an active-status row for the same household — pgxmock's regex
// match on the query text alone cannot prove the WHERE clause's status predicate is
// doing real work (#1371 Task 0 follow-up, coordinator review).
func TestHouseholdRepository_DeleteRemovedMember_Integration(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewHouseholdRepository(pool)

	ownerID := insertTestUser(t, pool)
	activeMemberID := insertTestUser(t, pool)
	removedMemberID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)
	insertTestHouseholdMember(t, pool, householdID, ownerID, "owner")
	activeRowID := insertTestHouseholdMember(t, pool, householdID, activeMemberID, "member")
	removedRowID := insertTestHouseholdMember(t, pool, householdID, removedMemberID, "member")

	// Transition the second row to 'removed', mirroring RemoveMember's production
	// active -> removed transition.
	if _, err := pool.Exec(ctx, `UPDATE household_members SET status = 'removed', removed_at = now() WHERE id = $1`, removedRowID); err != nil {
		t.Fatalf("transition to removed: %v", err)
	}

	// The guard must refuse to delete an ACTIVE row.
	if _, _, err := repo.DeleteRemovedMember(ctx, activeRowID); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("expected ErrMemberNotFound when targeting an active row, got: %v", err)
	}
	var activeStillPresent int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM household_members WHERE id = $1 AND status = 'active'`, activeRowID).Scan(&activeStillPresent); err != nil {
		t.Fatalf("count active row: %v", err)
	}
	if activeStillPresent != 1 {
		t.Fatalf("guard failed: active row was affected (count=%d)", activeStillPresent)
	}

	// The guard must actually delete a genuinely-removed row, returning its household/user.
	gotHouseholdID, gotUserID, err := repo.DeleteRemovedMember(ctx, removedRowID)
	if err != nil {
		t.Fatalf("DeleteRemovedMember on a removed row: %v", err)
	}
	if gotHouseholdID != householdID || gotUserID != removedMemberID {
		t.Fatalf("unexpected (household_id, user_id): got (%s, %s), want (%s, %s)", gotHouseholdID, gotUserID, householdID, removedMemberID)
	}
	var removedRowCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM household_members WHERE id = $1`, removedRowID).Scan(&removedRowCount); err != nil {
		t.Fatalf("count removed row: %v", err)
	}
	if removedRowCount != 0 {
		t.Fatalf("removed row must be gone, got count=%d", removedRowCount)
	}

	// The unrelated active member's row must remain completely untouched.
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM household_members WHERE id = $1 AND status = 'active'`, activeRowID).Scan(&activeStillPresent); err != nil {
		t.Fatalf("count active row after: %v", err)
	}
	if activeStillPresent != 1 {
		t.Fatalf("unrelated active row was affected, count=%d", activeStillPresent)
	}
}
