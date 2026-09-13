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
	"fmt"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/require"
)

// Match PostgreSQL's microsecond precision, independently of either machine's clock.
var entitlementFixtureTime = time.Date(2024, 1, 15, 12, 0, 0, 123456000, time.UTC)

func readEntitlementFixture(t *testing.T, db repository.DB, userID string, open bool) domain.SubscriptionEntitlement {
	t.Helper()
	var ent domain.SubscriptionEntitlement
	err := db.QueryRow(context.Background(), `
		SELECT id, user_id, source, tier, status, payer_user_id, household_id,
		       effective_from, effective_until, created_at, updated_at
		FROM subscription_entitlements
		WHERE user_id = $1 AND (effective_until IS NULL) = $2
	`, userID, open).Scan(&ent.ID, &ent.UserID, &ent.Source, &ent.Tier, &ent.Status,
		&ent.PayerUserID, &ent.HouseholdID, &ent.EffectiveFrom, &ent.EffectiveUntil, &ent.CreatedAt, &ent.UpdatedAt)
	require.NoError(t, err)
	require.NotEmpty(t, ent.ID)
	return ent
}

func assertEntitlementFixtureTime(t *testing.T, ent domain.SubscriptionEntitlement) {
	t.Helper()
	require.True(t, ent.EffectiveFrom.Equal(entitlementFixtureTime), entitlementDiagnostic(ent, entitlementFixtureTime))
}

func entitlementDiagnostic(ent domain.SubscriptionEntitlement, asOf time.Time) string {
	return fmt.Sprintf("expected T=%s from=%s asOf=%s until=%v ID=%s", entitlementFixtureTime, ent.EffectiveFrom, asOf, ent.EffectiveUntil, ent.ID)
}

func assertActiveEntitlementFixture(t *testing.T, repo repository.EntitlementRepository, userID string, fixture domain.SubscriptionEntitlement, asOf time.Time) *domain.SubscriptionEntitlement {
	t.Helper()
	diagnostic := entitlementDiagnostic(fixture, asOf)
	ent, err := repo.GetActiveEntitlement(context.Background(), userID, asOf)
	require.NoError(t, err, diagnostic)
	require.NotNil(t, ent, diagnostic)
	require.Equal(t, fixture.ID, ent.ID, diagnostic)
	require.Equal(t, fixture.Source, ent.Source, diagnostic)
	require.Equal(t, fixture.Tier, ent.Tier, diagnostic)
	require.True(t, fixture.EffectiveFrom.Equal(ent.EffectiveFrom), diagnostic)
	require.Nil(t, ent.EffectiveUntil, diagnostic)
	return ent
}

func assertEntitlementStartBoundary(t *testing.T, repo repository.EntitlementRepository, userID string, fixture domain.SubscriptionEntitlement) {
	t.Helper()
	for _, tc := range []struct {
		name   string
		offset time.Duration
	}{
		{"before", -time.Microsecond},
		{"equal", 0},
		{"after", time.Microsecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asOf := entitlementFixtureTime.Add(tc.offset)
			if tc.offset < 0 {
				_, err := repo.GetActiveEntitlement(context.Background(), userID, asOf)
				require.ErrorIs(t, err, repository.ErrEntitlementNotFound, entitlementDiagnostic(fixture, asOf))
				return
			}
			assertActiveEntitlementFixture(t, repo, userID, fixture, asOf)
		})
	}
}

func TestEntitlementRepository_GetActiveEntitlement_NotFound(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	repo := repository.NewEntitlementRepository(pool)
	userID := insertTestUser(t, pool)

	_, err := repo.GetActiveEntitlement(context.Background(), userID, entitlementFixtureTime)
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
		INSERT INTO subscription_entitlements (user_id, source, tier, status, effective_from)
		VALUES ($1, 'own_subscription', 'premium', 'active', $2)
	`, userID, entitlementFixtureTime)
	if err != nil {
		t.Fatalf("insert entitlement: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, userID) //nolint:errcheck
	})

	fixture := readEntitlementFixture(t, pool, userID, true)
	assertEntitlementFixtureTime(t, fixture)
	ent := assertActiveEntitlementFixture(t, repo, userID, fixture, entitlementFixtureTime)
	if ent.Tier != "premium" || string(ent.Source) != "own_subscription" {
		t.Fatalf("unexpected entitlement: %+v", ent)
	}
	assertEntitlementStartBoundary(t, repo, userID, fixture)
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

	from := entitlementFixtureTime.Add(-48 * time.Hour)
	until := entitlementFixtureTime.Add(-24 * time.Hour) // closed epoch: until > from

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

	_, err = repo.GetActiveEntitlement(ctx, userID, entitlementFixtureTime)
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
		INSERT INTO subscription_entitlements (user_id, source, tier, status, household_id, payer_user_id, effective_from)
		VALUES ($1, 'household', 'household_2', 'active', $2, $3, $4)
	`, memberID, householdID, ownerID, entitlementFixtureTime)
	if err != nil {
		t.Fatalf("insert entitlement: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, memberID) //nolint:errcheck
	})

	fixture := readEntitlementFixture(t, pool, memberID, true)
	assertEntitlementFixtureTime(t, fixture)
	ent := assertActiveEntitlementFixture(t, repo, memberID, fixture, entitlementFixtureTime)
	if string(ent.Source) != "household" || ent.PayerUserID == nil || *ent.PayerUserID != ownerID {
		t.Fatalf("unexpected entitlement: %+v", ent)
	}
	require.Equal(t, "household_2", ent.Tier)
	require.NotNil(t, ent.HouseholdID)
	require.Equal(t, householdID, *ent.HouseholdID)
	assertEntitlementStartBoundary(t, repo, memberID, fixture)
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
		INSERT INTO subscription_entitlements (user_id, source, tier, status, household_id, payer_user_id, effective_from)
		VALUES ($1, 'household', 'household_2', 'active', $2, $3, $4)
	`, memberID, householdID, ownerID, entitlementFixtureTime)
	if err != nil {
		t.Fatalf("insert household entitlement: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, memberID) //nolint:errcheck
	})

	fixture := readEntitlementFixture(t, pool, memberID, true)
	assertEntitlementFixtureTime(t, fixture)
	assertActiveEntitlementFixture(t, repo, memberID, fixture, entitlementFixtureTime)
	if err := repo.ExpireActiveHouseholdEntitlement(ctx, memberID, householdID); err != nil {
		t.Fatalf("ExpireActiveHouseholdEntitlement: %v", err)
	}

	closed := readEntitlementFixture(t, pool, memberID, false)
	diagnostic := entitlementDiagnostic(closed, entitlementFixtureTime)
	require.Equal(t, fixture.ID, closed.ID, diagnostic)
	require.Equal(t, fixture.Source, closed.Source, diagnostic)
	require.Equal(t, fixture.Tier, closed.Tier, diagnostic)
	require.True(t, fixture.EffectiveFrom.Equal(closed.EffectiveFrom), diagnostic)
	require.NotNil(t, closed.EffectiveUntil, diagnostic)
	require.True(t, closed.EffectiveUntil.After(closed.EffectiveFrom), diagnostic)

	// As in the existing as-of threading tests, closing an epoch excludes it even
	// when asOf was inside that epoch: effective_until IS NULL is current state.
	_, err = repo.GetActiveEntitlement(ctx, memberID, entitlementFixtureTime)
	require.ErrorIs(t, err, repository.ErrEntitlementNotFound, diagnostic)
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
	closedFrom := entitlementFixtureTime.Add(-31 * 24 * time.Hour)
	closedUntil := entitlementFixtureTime.Add(-30 * 24 * time.Hour)
	_, err := pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, household_id, payer_user_id, effective_from, effective_until)
		VALUES ($1, 'household', 'household_2', 'active', $2, $3, $4, $5)
	`, memberID, householdID, ownerID, closedFrom, closedUntil)
	if err != nil {
		t.Fatalf("insert closed household entitlement: %v", err)
	}

	// The member has since started their own, still-open subscription.
	_, err = pool.Exec(ctx, `
		INSERT INTO subscription_entitlements (user_id, source, tier, status, effective_from)
		VALUES ($1, 'own_subscription', 'premium', 'active', $2)
	`, memberID, entitlementFixtureTime)
	if err != nil {
		t.Fatalf("insert own subscription entitlement: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM subscription_entitlements WHERE user_id = $1`, memberID) //nolint:errcheck
	})

	fixture := readEntitlementFixture(t, pool, memberID, true)
	assertEntitlementFixtureTime(t, fixture)
	closed := readEntitlementFixture(t, pool, memberID, false)
	require.True(t, closed.EffectiveFrom.Equal(closedFrom), entitlementDiagnostic(closed, entitlementFixtureTime))
	require.NotNil(t, closed.EffectiveUntil)
	require.True(t, closed.EffectiveUntil.Equal(closedUntil), entitlementDiagnostic(closed, entitlementFixtureTime))
	assertActiveEntitlementFixture(t, repo, memberID, fixture, entitlementFixtureTime)
	if err := repo.ExpireActiveHouseholdEntitlement(ctx, memberID, householdID); err != nil {
		t.Fatalf("ExpireActiveHouseholdEntitlement: %v", err)
	}

	ent := assertActiveEntitlementFixture(t, repo, memberID, fixture, entitlementFixtureTime)
	if string(ent.Source) != "own_subscription" || ent.Tier != "premium" {
		t.Fatalf("own_subscription entitlement was disturbed: %+v", ent)
	}
	ownAfter := readEntitlementFixture(t, pool, memberID, true)
	require.Equal(t, fixture, ownAfter, entitlementDiagnostic(ownAfter, entitlementFixtureTime))
	closedAfter := readEntitlementFixture(t, pool, memberID, false)
	require.Equal(t, closed, closedAfter, entitlementDiagnostic(closedAfter, entitlementFixtureTime))
}
