//go:build integration

package repository

// Integration tests for AdminUserRepository.UpdateUserTier and
// GetOwnedHouseholdActiveMemberCount (Issue #1413).
//
// pgxmock matches query strings and cannot validate a CTE, FOR UPDATE, or a NULL-actor
// coercion against a real audit_logs FK, nor a real household_members JOIN — a real database
// is required to prove these branches.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestUpdateUserTier_Integration -tags integration -v
//	DATABASE_URL=postgres://... go test ./internal/repository/... -run TestGetOwnedHouseholdActiveMemberCount_Integration -tags integration -v

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// fetchAuditChanges reads and decrypts the `changes` column of the latest audit_logs row
// matching resource_id/action, returning it as a decoded map.
func fetchAuditChanges(t *testing.T, pool *pgxpool.Pool, enc *appcrypto.FieldEncryptor, resourceID, action string) map[string]interface{} {
	t.Helper()
	var encryptedChanges string
	err := pool.QueryRow(context.Background(), `
		SELECT changes FROM audit_logs
		WHERE resource_id = $1 AND action = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, resourceID, action).Scan(&encryptedChanges)
	require.NoError(t, err)

	raw, err := enc.DecryptJSON(encryptedChanges)
	require.NoError(t, err)

	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// TestUpdateUserTier_Integration_SetsOverrideAndReturnsRawState verifies the override columns
// are written and the RETURNING row carries the user's RAW (base) subscription_tier/status —
// unaffected by the override — which the service needs to build the correct EFFECTIVE Clerk
// payload without a second DB read.
func TestUpdateUserTier_Integration_SetsOverrideAndReturnsRawState(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "cancelled")

	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	row, err := repo.UpdateUserTier(ctx, actor.ID, target.ID, "household_4", "comp for testing", &expiresAt)
	require.NoError(t, err)

	assert.Equal(t, "premium", row.SubscriptionTier, "raw base tier must be unaffected by the override")
	assert.Equal(t, "cancelled", row.SubscriptionStatus, "raw base status must be unaffected by the override")
	assert.Nil(t, row.PriorManualTierOverride, "no override existed before this call")

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	require.NotNil(t, snap.ManualTierOverride)
	assert.Equal(t, "household_4", *snap.ManualTierOverride)
	require.NotNil(t, snap.ManualTierEndsAt)
	assert.WithinDuration(t, expiresAt, *snap.ManualTierEndsAt, time.Second)
}

// TestUpdateUserTier_Integration_ReturnsPriorOverride verifies the CTE reads the PRE-UPDATE
// manual_tier_override, not the just-written value.
func TestUpdateUserTier_Integration_ReturnsPriorOverride(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")

	priorOverride := "household_2"
	setManualOverride(t, pool, target.ID, &priorOverride, nil)

	row, err := repo.UpdateUserTier(ctx, actor.ID, target.ID, "household_5", "escalating comp", nil)
	require.NoError(t, err)

	require.NotNil(t, row.PriorManualTierOverride)
	assert.Equal(t, "household_2", *row.PriorManualTierOverride)

	snap := fetchRetentionSnapshot(t, pool, target.ID)
	require.NotNil(t, snap.ManualTierOverride)
	assert.Equal(t, "household_5", *snap.ManualTierOverride, "the NEW override must be what's stored, not the prior one")
}

// TestUpdateUserTier_Integration_AuditRow_ActorAndTarget verifies the audit row lands with
// user_id=actor, resource_id=target, action='update_manual_tier' — the pre-existing bug this
// task fixes recorded the TARGET as the actor (Issue #1413 Defect 2).
func TestUpdateUserTier_Integration_AuditRow_ActorAndTarget(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")
	require.NotEqual(t, actor.ID, target.ID)

	_, err := repo.UpdateUserTier(ctx, actor.ID, target.ID, "premium", "audit wiring check", nil)
	require.NoError(t, err)

	audit := fetchLatestAuditRow(t, pool, target.ID, "update_manual_tier")
	require.NotNil(t, audit.UserID)
	require.NotNil(t, audit.ResourceID)
	assert.Equal(t, actor.ID, *audit.UserID, "audit_logs.user_id must be the ACTING ADMIN")
	assert.Equal(t, target.ID, *audit.ResourceID, "audit_logs.resource_id must be the TARGET user")
	assert.Equal(t, "update_manual_tier", audit.Action)
}

// TestUpdateUserTier_Integration_AuditChanges_ContainsTargetAndPriorOverride verifies the
// encrypted `changes` payload is enriched with the target id and the prior override value.
func TestUpdateUserTier_Integration_AuditChanges_ContainsTargetAndPriorOverride(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")

	priorOverride := "household_3"
	setManualOverride(t, pool, target.ID, &priorOverride, nil)

	_, err := repo.UpdateUserTier(ctx, actor.ID, target.ID, "household_5", "escalating comp", nil)
	require.NoError(t, err)

	changes := fetchAuditChanges(t, pool, enc, target.ID, "update_manual_tier")
	assert.Equal(t, target.ID, changes["target_user_id"])
	assert.Equal(t, "household_3", changes["prior_manual_tier_override"])
	assert.Equal(t, "household_5", changes["tier"])
	assert.Equal(t, "escalating comp", changes["reason"])
}

// TestUpdateUserTier_Integration_EmptyActor_NoFKViolation verifies that an empty actorUserID is
// coerced to SQL NULL (not an invalid-UUID / FK violation) — audit_logs.user_id is nullable
// with ON DELETE SET NULL, and a missing actor must never abort a legitimate admin action.
func TestUpdateUserTier_Integration_EmptyActor_NoFKViolation(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")

	_, err := repo.UpdateUserTier(ctx, "", target.ID, "premium", "system-initiated cleanup", nil)
	require.NoError(t, err, "empty actor must not cause an FK violation or any other error")

	audit := fetchLatestAuditRow(t, pool, target.ID, "update_manual_tier")
	assert.Nil(t, audit.UserID, "audit_logs.user_id must be NULL when actorUserID is empty")
}

// TestUpdateUserTier_Integration_UserNotFound verifies a missing target surfaces as
// ErrUserNotFound (the same sentinel RemoveUserTierOverride uses).
func TestUpdateUserTier_Integration_UserNotFound(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")

	_, err := repo.UpdateUserTier(ctx, actor.ID, "00000000-0000-0000-0000-000000000000", "premium", "no such user", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUserNotFound)
}

// TestUpdateUserTier_Integration_SoftDeletedTarget verifies a soft-deleted (is_deleted=TRUE)
// target also surfaces as ErrUserNotFound.
func TestUpdateUserTier_Integration_SoftDeletedTarget(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	actor := createRemoveTierOverrideUser(t, pool, enc, suffix+"_actor", "premium", "active")
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_target", "premium", "active")

	_, err := pool.Exec(ctx, "UPDATE users SET is_deleted = TRUE WHERE id = $1", target.ID)
	require.NoError(t, err)

	_, err = repo.UpdateUserTier(ctx, actor.ID, target.ID, "premium", "target is soft-deleted", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUserNotFound)
}

// createHouseholdWithMembers creates a household owned by ownerID with the given additional
// member user IDs (all inserted as active members), via the real HouseholdRepository — the
// owner is inserted as an active 'owner' member, matching householdService.CreateHousehold's
// production behavior (owner always counts as an active member).
func createHouseholdWithMembers(t *testing.T, pool *pgxpool.Pool, ownerID string, memberIDs ...string) {
	t.Helper()
	ctx := context.Background()
	hhRepo := NewHouseholdRepository(pool)

	h := &domain.Household{OwnerUserID: ownerID, Name: "Test Household"}
	require.NoError(t, hhRepo.CreateHousehold(ctx, h))
	t.Cleanup(func() { pool.Exec(context.Background(), "DELETE FROM households WHERE id = $1", h.ID) }) //nolint:errcheck

	require.NoError(t, hhRepo.InsertMember(ctx, &domain.HouseholdMember{
		HouseholdID: h.ID, UserID: ownerID, Role: domain.HouseholdRoleOwner, Status: domain.HouseholdMemberStatusActive,
	}))
	for _, m := range memberIDs {
		require.NoError(t, hhRepo.InsertMember(ctx, &domain.HouseholdMember{
			HouseholdID: h.ID, UserID: m, Role: domain.HouseholdRoleMember, Status: domain.HouseholdMemberStatusActive,
		}))
	}
}

// TestGetOwnedHouseholdActiveMemberCount_Integration_OwnerWithMembers verifies the count
// includes the owner plus every active member of a REAL household_members/households join.
func TestGetOwnedHouseholdActiveMemberCount_Integration_OwnerWithMembers(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	owner := createRemoveTierOverrideUser(t, pool, enc, suffix+"_owner", "household_4", "active")
	m1 := createRemoveTierOverrideUser(t, pool, enc, suffix+"_m1", "household_4", "active")
	m2 := createRemoveTierOverrideUser(t, pool, enc, suffix+"_m2", "household_4", "active")
	m3 := createRemoveTierOverrideUser(t, pool, enc, suffix+"_m3", "household_4", "active")

	createHouseholdWithMembers(t, pool, owner.ID, m1.ID, m2.ID, m3.ID)

	count, err := repo.GetOwnedHouseholdActiveMemberCount(ctx, owner.ID)
	require.NoError(t, err)
	assert.Equal(t, 4, count, "owner + 3 members = 4 active members")
}

// TestGetOwnedHouseholdActiveMemberCount_Integration_NotOwner_ReturnsZero verifies a user who
// does not own a household gets 0, unambiguously distinguishing "not an owner" from "owns an
// empty household" (impossible — the owner is always an active member).
func TestGetOwnedHouseholdActiveMemberCount_Integration_NotOwner_ReturnsZero(t *testing.T) {
	pool := connectTestDB(t)
	enc := newTestFieldEncryptor(t)
	repo := NewAdminUserRepository(pool, enc)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	target := createRemoveTierOverrideUser(t, pool, enc, suffix+"_solo", "premium", "active")

	count, err := repo.GetOwnedHouseholdActiveMemberCount(ctx, target.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
