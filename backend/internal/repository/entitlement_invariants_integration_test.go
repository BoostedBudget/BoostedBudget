package repository_test

// Verifies the 000112 migration invariants. Reuses setupHouseholdSchemaDB /
// insertTestUser / insertTestHousehold helpers from household_schema_integration_test.go
// (same package). Requires DATABASE_URL with migrations applied through 000112.
//
// Run:
//   DATABASE_URL="postgres://..." go test ./internal/repository/ -run TestEntitlementInvariants -v

import (
	"context"
	"testing"
)

func TestEntitlementInvariants_OneActivePerUser(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	userID := insertTestUser(t, pool)

	_, err := pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status)
		VALUES ($1, 'own_subscription', 'premium', 'active')
	`, userID)
	if err != nil {
		t.Fatalf("first insert should succeed: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, userID) //nolint:errcheck
	})

	_, err = pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status)
		VALUES ($1, 'own_subscription', 'premium', 'active')
	`, userID)
	if err == nil {
		t.Fatal("expected second open-ended entitlement for the same user to be rejected")
	}
}

func TestEntitlementInvariants_PayerMustBeHouseholdOwner(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	ownerID := insertTestUser(t, pool)
	notOwnerID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)

	_, err := pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, household_id, payer_user_id)
		VALUES ($1, 'household', 'premium', 'active', $2, $3)
	`, notOwnerID, householdID, notOwnerID)
	if err == nil {
		t.Fatal("expected entitlement with payer_user_id != household owner to be rejected")
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, household_id, payer_user_id)
		VALUES ($1, 'household', 'premium', 'active', $2, $3)
	`, notOwnerID, householdID, ownerID)
	if err != nil {
		t.Fatalf("expected entitlement with payer_user_id == household owner to succeed: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, notOwnerID) //nolint:errcheck
	})
}
