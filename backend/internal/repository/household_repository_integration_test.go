// backend/internal/repository/household_repository_integration_test.go
package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
