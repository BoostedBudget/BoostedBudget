package middleware_test

// Issue #1413 — closes the household-owner mutation bypass: manual_tier_override is an
// unconstrained VARCHAR(50) with no CHECK constraint, so it can hold a value naming no
// configured tier (a legacy row, a direct SQL write, a write made while tierConfig was nil).
// Earlier work on this branch stopped such a value from granting write access on the
// AFFECTED USER'S OWN request path (profile_repository.go / internal/huma/operations/profile.go).
// household_entitlement_service.go's owner-lapse gate was the one access-granting path that
// was missed: any non-empty live override on a household OWNER — garbage included — made the
// owner look "active", so the resolver returned household coverage and this package's
// read-only middleware permitted a MEMBER's mutation.
//
// This test drives the REAL read-only middleware together with a REAL
// *services.HouseholdEntitlementService (backed by minimal fakes for its repository
// dependencies) — NOT this package's mockHouseholdCoverageChecker test double (see
// read_only_test.go), which hard-codes its answer and therefore cannot exercise the resolver
// logic this bug lives in. Proving the HTTP outcome (403 vs 200), not just the service's
// return value, is the point: a bug in ResolveEffectiveEntitlement's owner-status gate is
// exactly the kind of defect a hard-coded test double would hide.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
)

// fakeEntitlementRepoForOwnerOverrideTest is a minimal repository.EntitlementRepository
// double. Only GetActiveEntitlement is exercised by ResolveEffectiveEntitlement's
// household-source branch, so the other methods are unused stubs.
type fakeEntitlementRepoForOwnerOverrideTest struct {
	ent *domain.SubscriptionEntitlement
}

func (f *fakeEntitlementRepoForOwnerOverrideTest) GetActiveEntitlement(ctx context.Context, userID string, asOf time.Time) (*domain.SubscriptionEntitlement, error) {
	return f.ent, nil
}

func (f *fakeEntitlementRepoForOwnerOverrideTest) CreateHouseholdEntitlement(ctx context.Context, userID, tier, status, payerUserID, householdID string) error {
	return nil
}

func (f *fakeEntitlementRepoForOwnerOverrideTest) ExpireActiveEntitlement(ctx context.Context, userID string) error {
	return nil
}

func (f *fakeEntitlementRepoForOwnerOverrideTest) ExpireActiveHouseholdEntitlement(ctx context.Context, userID, householdID string) error {
	return nil
}

// fakeUserLoaderForOwnerOverrideTest is a minimal user-by-ID lookup double.
type fakeUserLoaderForOwnerOverrideTest struct {
	users map[string]*domain.User
}

func (f *fakeUserLoaderForOwnerOverrideTest) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	return f.users[id], nil
}

// TestReadOnlyMiddleware_HouseholdOwnerMalformedOverride_MemberMutation_Blocked is the RED
// test for the bypass this task closes: a household member whose own raw status is read-only,
// covered by an owner whose live manual_tier_override names no configured tier, must NOT be
// permitted a mutation.
func TestReadOnlyMiddleware_HouseholdOwnerMalformedOverride_MemberMutation_Blocked(t *testing.T) {
	const ownerID = "owner-with-bad-override"
	const memberID = "member-under-bad-owner"
	payerID := ownerID
	householdID := "household-1"
	badOverride := "not-a-real-tier"

	entRepo := &fakeEntitlementRepoForOwnerOverrideTest{
		ent: &domain.SubscriptionEntitlement{
			Source:      domain.EntitlementSourceHousehold,
			PayerUserID: &payerID,
			HouseholdID: &householdID,
		},
	}
	userRepo := &fakeUserLoaderForOwnerOverrideTest{
		users: map[string]*domain.User{
			memberID: {ID: memberID, SubscriptionTier: "premium", SubscriptionStatus: "inactive"},
			ownerID: {
				ID: ownerID, SubscriptionTier: "household_2", SubscriptionStatus: "past_due", // owner's real billing status is read-only
				ManualTierOverride: &badOverride, // garbage — no CHECK constraint stops this
			},
		},
	}

	svc := services.NewHouseholdEntitlementService(entRepo, userRepo, nil, nil)
	// The fix under test: the SAME injected-validator seam profile_repository.go /
	// internal/huma/operations/profile.go already use for this exact malformed-override
	// problem — only "household_2"/"premium" are configured; "not-a-real-tier" is rejected.
	svc.SetOverrideTierValidator(func(tier string) bool {
		return tier == "household_2" || tier == "premium"
	})

	server, _ := setupReadOnlyTestAPIWithHouseholdChecker(t, svc)

	// The member's own raw status is read-only too — #1367's covered-member fallback never
	// mutates a covered member's own subscription_status, so a real covered member can look
	// exactly like this if they joined the household while already lapsed.
	resp := doRequestWithUser(t, server, http.MethodPost, "/api/v1/transactions", "inactive", memberID)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode,
		"a malformed live override on the household OWNER must not grant every MEMBER write access")
}

// TestReadOnlyMiddleware_HouseholdOwnerValidOverride_MemberMutation_Allowed is the POSITIVE
// CONTROL: the exact same validator, with a VALID live owner override, must still let the
// member mutate. This is requirement 5 of this work — an over-tight fix that also blocks
// legitimate comps would lock out paying households, which is worse than the bypass it closes.
func TestReadOnlyMiddleware_HouseholdOwnerValidOverride_MemberMutation_Allowed(t *testing.T) {
	const ownerID = "owner-with-valid-override"
	const memberID = "member-under-good-owner"
	payerID := ownerID
	householdID := "household-2"
	validOverride := "household_2"
	futureEnd := time.Now().Add(30 * 24 * time.Hour)

	entRepo := &fakeEntitlementRepoForOwnerOverrideTest{
		ent: &domain.SubscriptionEntitlement{
			Source:      domain.EntitlementSourceHousehold,
			PayerUserID: &payerID,
			HouseholdID: &householdID,
		},
	}
	userRepo := &fakeUserLoaderForOwnerOverrideTest{
		users: map[string]*domain.User{
			memberID: {ID: memberID, SubscriptionTier: "premium", SubscriptionStatus: "inactive"},
			ownerID: {
				ID: ownerID, SubscriptionTier: "household_2", SubscriptionStatus: "inactive", // real subscription lapsed
				ManualTierOverride: &validOverride, ManualTierEndsAt: &futureEnd, // valid, live comp
			},
		},
	}

	svc := services.NewHouseholdEntitlementService(entRepo, userRepo, nil, nil)
	svc.SetOverrideTierValidator(func(tier string) bool {
		return tier == "household_2" || tier == "premium"
	})

	server, _ := setupReadOnlyTestAPIWithHouseholdChecker(t, svc)

	resp := doRequestWithUser(t, server, http.MethodPost, "/api/v1/transactions", "inactive", memberID)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"a VALID live owner override must still cover the household's members")
}
