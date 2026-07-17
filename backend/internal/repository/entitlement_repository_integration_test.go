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

// TestEntitlementRepository_ExpireActiveHouseholdEntitlement_ExpiresMatchingRow proves
// the positive case: a genuinely-matching open household entitlement row (same user_id,
// household_id, source='household', effective_until IS NULL) IS actually closed out by
// the scoped method — this is the case
// TestEntitlementRepository_ExpireActiveHouseholdEntitlement_DoesNotTouchOwnSubscription
// alone doesn't cover, since a no-op implementation would also leave an unrelated row
// untouched (Codex review finding).
func TestEntitlementRepository_ExpireActiveHouseholdEntitlement_ExpiresMatchingRow(t *testing.T) {
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
		t.Fatalf("insert household entitlement: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, memberID) //nolint:errcheck
	})

	if err := repo.ExpireActiveHouseholdEntitlement(ctx, memberID, householdID); err != nil {
		t.Fatalf("ExpireActiveHouseholdEntitlement: %v", err)
	}

	_, err = repo.GetActiveEntitlement(ctx, memberID)
	if !errors.Is(err, repository.ErrEntitlementNotFound) {
		t.Fatalf("expected the matching household entitlement to be expired (ErrEntitlementNotFound), got: %v", err)
	}
}

// TestEntitlementRepository_ExpireActiveHouseholdEntitlement_DoesNotTouchOwnSubscription
// proves the #1371 scoped expiry helper's reason for existing: a removed household
// member who has since started their OWN subscription must keep that new coverage.
// The existing broad ExpireActiveEntitlement(ctx, userID) matches by user_id ONLY and
// would incorrectly expire the unrelated own_subscription row; the scoped method
// (user_id + household_id + source='household') must leave it untouched.
func TestEntitlementRepository_ExpireActiveHouseholdEntitlement_DoesNotTouchOwnSubscription(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewEntitlementRepository(pool)
	ownerID := insertTestUser(t, pool)
	memberID := insertTestUser(t, pool)
	householdID := insertTestHousehold(t, pool, ownerID)

	// The member's household entitlement was already expired at removal time (RemoveMember
	// calls the broad ExpireActiveEntitlement defensively) — simulate that closed epoch here.
	_, err := pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, household_id, payer_user_id, effective_from, effective_until)
		VALUES ($1, 'household', 'household_2', 'active', $2, $3, now() - interval '31 days', now() - interval '30 days')
	`, memberID, householdID, ownerID)
	if err != nil {
		t.Fatalf("insert closed household entitlement: %v", err)
	}

	// The member has since started their own, still-open subscription.
	_, err = pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status)
		VALUES ($1, 'own_subscription', 'premium', 'active')
	`, memberID)
	if err != nil {
		t.Fatalf("insert own subscription entitlement: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, memberID) //nolint:errcheck
	})

	if err := repo.ExpireActiveHouseholdEntitlement(ctx, memberID, householdID); err != nil {
		t.Fatalf("ExpireActiveHouseholdEntitlement: %v", err)
	}

	ent, err := repo.GetActiveEntitlement(ctx, memberID)
	if err != nil {
		t.Fatalf("own_subscription entitlement should still be active, got err: %v", err)
	}
	if string(ent.Source) != "own_subscription" || ent.Tier != "premium" {
		t.Fatalf("own_subscription entitlement was disturbed: %+v", ent)
	}
}
