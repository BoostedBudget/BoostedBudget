package repository_test

// Per project rule: pgxmock never catches constraint/aggregate/type errors — this repo's
// only production query is a straightforward SELECT, but exercised against a real DB per
// feedback_pgxmock_integration_coverage.md convention for new repositories.
//
// Run:
//   DATABASE_URL="postgres://..." go test ./internal/repository/ -run TestEntitlementRepository -v

import (
	"context"
	"errors"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

func TestEntitlementRepository_GetActiveEntitlement_NotFound(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	repo := repository.NewEntitlementRepository(pool)
	userID := insertTestUser(t, pool)

	_, err := repo.GetActiveEntitlement(context.Background(), userID)
	if !errors.Is(err, repository.ErrEntitlementNotFound) {
		t.Fatalf("expected ErrEntitlementNotFound, got %v", err)
	}
}

func TestEntitlementRepository_GetActiveEntitlement_OwnSubscription(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewEntitlementRepository(pool)
	userID := insertTestUser(t, pool)

	_, err := pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status)
		VALUES ($1, 'own_subscription', 'premium', 'active')
	`, userID)
	if err != nil {
		t.Fatalf("insert entitlement: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, userID) //nolint:errcheck
	})

	ent, err := repo.GetActiveEntitlement(ctx, userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ent.Tier != "premium" || string(ent.Source) != "own_subscription" {
		t.Fatalf("unexpected entitlement: %+v", ent)
	}
}

func TestEntitlementRepository_GetActiveEntitlement_HouseholdCoverage(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewEntitlementRepository(pool)
	ownerID := insertTestUser(t, pool)
	memberID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)

	_, err := pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, household_id, payer_user_id)
		VALUES ($1, 'household', 'household_2', 'active', $2, $3)
	`, memberID, householdID, ownerID)
	if err != nil {
		t.Fatalf("insert entitlement: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, memberID) //nolint:errcheck
	})

	ent, err := repo.GetActiveEntitlement(ctx, memberID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(ent.Source) != "household" || ent.PayerUserID == nil || *ent.PayerUserID != ownerID {
		t.Fatalf("unexpected entitlement: %+v", ent)
	}
}
