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
	"time"

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

// TestEntitlementRepository_GetActiveEntitlement_ExpiredRowExcluded proves the
// query's `effective_until IS NULL` filter genuinely excludes a closed epoch,
// not just the "zero rows for this user" case already covered by NotFound above.
// Issue #1366 TDD checklist item: "Entitlement row effective_until expired ->
// falls back to own tier."
func TestEntitlementRepository_GetActiveEntitlement_ExpiredRowExcluded(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewEntitlementRepository(pool)
	userID := insertTestUser(t, pool)

	from := time.Now().Add(-48 * time.Hour)
	until := time.Now().Add(-24 * time.Hour) // closed epoch: until > from, both in the past

	_, err := pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, effective_from, effective_until)
		VALUES ($1, 'own_subscription', 'premium', 'cancelled', $2, $3)
	`, userID, from, until)
	if err != nil {
		t.Fatalf("insert expired entitlement: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, userID) //nolint:errcheck
	})

	_, err = repo.GetActiveEntitlement(ctx, userID)
	if !errors.Is(err, repository.ErrEntitlementNotFound) {
		t.Fatalf("expected ErrEntitlementNotFound for a closed-epoch row, got %v", err)
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
