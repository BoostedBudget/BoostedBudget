package services

import (
	"context"
	"errors"
	"log/slog"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// userLoader is the minimal user-lookup capability HouseholdEntitlementService needs.
// repository.ProfileRepository satisfies this structurally — passed directly in main.go.
type userLoader interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// HouseholdEntitlementResolver resolves a user's effective tier+status, accounting for
// household coverage. Injected into SubscriptionService as an optional, nil-safe
// dependency (see SubscriptionService.SetEntitlementResolver).
type HouseholdEntitlementResolver interface {
	ResolveEffectiveEntitlement(ctx context.Context, user *domain.User) (domain.EffectiveEntitlement, error)
}

// HouseholdEntitlementService implements HouseholdEntitlementResolver by resolving
// household coverage LIVE from the owner's current user row on every call, rather than
// storing a tier snapshot on the entitlement row and keeping it fresh with a recompute
// job. This deliberately deviates from issue #1366's literal "store + recompute" design:
// an adversarial review found that model goes stale across owner manual overrides, Paddle
// reconciliation, trial expiry, and the dedicated past-due webhook handler — none of which
// a webhook-triggered recompute would reliably catch. Live resolution reflects all of those
// automatically, at the cost of one extra indexed lookup (owner by PK) when a household
// entitlement row exists. Never mutates domain.User.SubscriptionTier for members — that
// invariant is what this whole layer exists to preserve.
type HouseholdEntitlementService struct {
	entitlementRepo       repository.EntitlementRepository
	userRepo              userLoader
	householdRepo         repository.HouseholdRepository
	tierConfig            *config.TierConfig
	overrideTierValidator func(tierName string) bool
}

// NewHouseholdEntitlementService creates a new HouseholdEntitlementService.
func NewHouseholdEntitlementService(
	entitlementRepo repository.EntitlementRepository,
	userRepo userLoader,
	householdRepo repository.HouseholdRepository,
	tierConfig *config.TierConfig,
) *HouseholdEntitlementService {
	return &HouseholdEntitlementService{
		entitlementRepo: entitlementRepo,
		userRepo:        userRepo,
		householdRepo:   householdRepo,
		tierConfig:      tierConfig,
	}
}

// SetOverrideTierValidator injects a function that reports whether a manual_tier_override
// value names a real, currently-configured tier. ResolveEffectiveEntitlement uses it to stop
// a malformed household-OWNER override from covering every member of the household with write
// access — the mutation bypass this task closes (Issue #1413): manual_tier_override is an
// unconstrained VARCHAR(50) with no CHECK constraint, so its value can be garbage (a legacy
// row, a direct SQL write, a write made while the tier config was nil). Earlier work on this
// branch (G2/round-5) stopped such a value from granting write access on the AFFECTED USER'S
// OWN request path — profile_repository.go's GetEffectiveSubscriptionStatusByUserID and
// internal/huma/operations/profile.go's enrichUserProfile — but this resolver's owner-lapse
// gate kept reading the bare, unvalidated ComputeEffectiveStatus(): a garbage OWNER override
// was correctly denied on the owner's own access, yet still made the owner look "active" here,
// so IsCoveredByHousehold(member) returned true and the read-only middleware permitted the
// MEMBER's mutation — access granted to someone other than the user whose row is corrupt.
//
// Same setter-injection convention, same func(string) bool signature, and the same nil-safe
// "skip, don't deny" semantics as the other two call sites above — see
// profile_repository.go's SetOverrideTierValidator doc comment for the full rationale, which
// applies here unchanged: main.go loads the tier config once at startup and os.Exit(1)s if
// that fails, so in production this validator is either wired for the entire process lifetime
// or the server never boots. If never called, validation is skipped and an active override is
// trusted — the pre-fix behavior — rather than denying every comp, which would turn a future
// wiring oversight into an instant outage for every currently-comped household.
//
// Deliberately reuses this existing seam rather than reading h.tierConfig directly the way
// PaddleCheckoutHandler.isConfiguredOverrideTier does (this service already holds a tierConfig
// field, used by GetEntitlementSummary) — a third distinct mechanism for the identical check
// would only make the existing two definitions of "configured tier" likelier to drift apart.
func (s *HouseholdEntitlementService) SetOverrideTierValidator(validator func(tierName string) bool) {
	s.overrideTierValidator = validator
}

// ResolveEffectiveEntitlement returns the user's effective tier+status. Solo users (no
// active entitlement row, or an own_subscription row) resolve to their own computed
// tier/status — identical to what SubscriptionService computed before this resolver
// existed. Household members resolve to the owner's live tier/status, unless the owner
// has lost active coverage (past_due/cancelled/inactive) AND isn't currently comped via an
// active manual_tier_override (#1413 — an admin comp must keep covering the owner's
// members, not just the owner), in which case the member falls back to their own tier.
// Real repository/lookup errors propagate; the caller (SubscriptionService) decides the
// fail-open policy.
//
// Precedence (ratified decision — Gap Analysis Round 2, GT-2, #1367): while the owner is
// active, household coverage ALWAYS wins over a covered member's own
// users.manual_tier_override — an admin comp does not stack with, nor take priority over,
// free household coverage. This was previously just the mechanical result of Phase 2's
// implementation order (never an explicit product decision — see the deferred-from-Phase-2
// comment on #1367); it is now locked in as intended behavior and covered by
// TestResolveEffectiveEntitlement_MemberManualOverride_LosesToActiveHouseholdCoverage. The
// member's own override still applies as soon as the owner lapses (the existing
// owner-read-only fallback below composes with it for free, since ComputeEffectiveTier()
// already resolves the override) — see the sibling
// TestResolveEffectiveEntitlement_MemberManualOverride_AppliesWhenOwnerLapses.
func (s *HouseholdEntitlementService) ResolveEffectiveEntitlement(ctx context.Context, user *domain.User) (domain.EffectiveEntitlement, error) {
	own := domain.EffectiveEntitlement{
		Tier:   s.effectiveTierName(ctx, user),
		Status: user.ComputeEffectiveStatus(),
		Source: domain.EntitlementSourceOwn,
	}

	ent, err := s.entitlementRepo.GetActiveEntitlement(ctx, user.ID)
	if errors.Is(err, repository.ErrEntitlementNotFound) {
		return own, nil
	}
	if err != nil {
		return domain.EffectiveEntitlement{}, err
	}

	if ent.Source != domain.EntitlementSourceHousehold {
		return own, nil
	}

	owner, err := s.userRepo.GetUserByID(ctx, *ent.PayerUserID)
	if err != nil {
		return domain.EffectiveEntitlement{}, err
	}

	// Issue #1413 — household-owner mutation bypass: manual_tier_override is an unconstrained
	// VARCHAR(50) with no CHECK constraint, so it can hold a value naming no configured tier.
	// Validate it here via the injected overrideTierValidator (see SetOverrideTierValidator)
	// before trusting the "active" status it would otherwise force — the same rule
	// GetEffectiveSubscriptionStatusByUserID enforces on the owner's OWN access path
	// (profile_repository.go). A rejected override falls back to the owner's RAW status,
	// exactly like that sibling call site, and is logged at warn for the same reason: a silent
	// denial here would be indistinguishable from the original bypass from the member's point
	// of view. EffectiveStatusWithActiveOverride is a single shared clock read, so this check
	// and the Status field returned below can never disagree about whether the override was
	// live.
	ownerStatus, ownerOverrideValue, ownerHasOverride := owner.EffectiveStatusWithActiveOverride()
	if ownerHasOverride && s.overrideTierValidator != nil && !s.overrideTierValidator(ownerOverrideValue) {
		slog.WarnContext(ctx, "rejecting household owner's manual tier override: value does not name a configured tier",
			"owner_id", owner.ID, "manual_tier_override", ownerOverrideValue)
		ownerStatus = owner.SubscriptionStatus
		// #1413 (this task): also clear ownerHasOverride so the Tier line below — which reuses
		// this same rejection decision rather than re-deriving it — falls back to the owner's
		// raw SubscriptionTier instead of the just-rejected garbage value.
		ownerHasOverride = false
	}

	if domain.SubscriptionStatus(ownerStatus).IsReadOnlySubscription() {
		// Owner lost active coverage (past_due/cancelled/inactive), and isn't currently
		// comped via an active, VALID manual_tier_override — member reverts to own tier (#1413).
		return own, nil
	}

	// #1413: Status must be the owner's EFFECTIVE (validated) status, matching the Tier line
	// below. Reporting an effective tier next to a raw status is precisely the asymmetry that
	// caused this issue in the first place — a comped owner would surface here as "household
	// coverage at tier household_4, status inactive", which is self-contradictory. No consumer
	// reads this field today, which is exactly why it would rot unnoticed until one did.
	//
	// Tier is now ALSO validated (Issue #1413, this task): a malformed owner override used to
	// reach owner.ComputeEffectiveTier() unvalidated here — matching enrichUserProfile's
	// identical asymmetry before its own fix (profile.go) — and flow straight into the
	// caller's tierConfig.GetTier() as an unrecognized name (GetEntitlementSummary,
	// SubscriptionService.GetEffectiveSubscription). effectiveTierName reuses the same
	// ownerOverrideValue/ownerHasOverride computed above rather than re-deriving them (and
	// re-reading time.Now()) via a second EffectiveTierWithActiveOverride call.
	ownerTier := owner.SubscriptionTier
	if ownerHasOverride {
		ownerTier = ownerOverrideValue
	}
	return domain.EffectiveEntitlement{
		Tier:        ownerTier,
		Status:      ownerStatus,
		Source:      domain.EntitlementSourceHousehold,
		PayerUserID: ent.PayerUserID,
		HouseholdID: ent.HouseholdID,
	}, nil
}

// effectiveTierName returns user's effective tier name, validating an active manual override
// against the injected overrideTierValidator (see SetOverrideTierValidator) and falling back to
// the raw SubscriptionTier when it names no configured tier (Issue #1413 — the tier axis of the
// malformed-override rule, this task). Used for the "own" (solo/no-household-row) branch of
// ResolveEffectiveEntitlement; the household-owner branch inlines the identical check against
// its own already-computed ownerOverrideValue/ownerHasOverride (see the status-axis check just
// above it) rather than calling this a second time, to avoid a redundant time.Now() read.
func (s *HouseholdEntitlementService) effectiveTierName(ctx context.Context, user *domain.User) string {
	tier, overrideValue, hasOverride := user.EffectiveTierWithActiveOverride()
	if hasOverride && s.overrideTierValidator != nil && !s.overrideTierValidator(overrideValue) {
		slog.WarnContext(ctx, "rejecting manual tier override on tier axis: value does not name a configured tier",
			"user_id", user.ID, "manual_tier_override", overrideValue)
		return user.SubscriptionTier
	}
	return tier
}

// IsCoveredByHousehold reports whether userID is currently covered by an active
// household plan — i.e. ResolveEffectiveEntitlement would resolve them to
// source=household. Used by the read-only middleware (#1367 — Phase 3) so a covered
// member whose own subscription_status is itself read-only (never mutated while
// covered) isn't locked out of the app. Reuses ResolveEffectiveEntitlement rather than
// re-deriving the owner-read-only fallback logic.
func (s *HouseholdEntitlementService) IsCoveredByHousehold(ctx context.Context, userID string) (bool, error) {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	eff, err := s.ResolveEffectiveEntitlement(ctx, user)
	if err != nil {
		return false, err
	}
	return eff.Source == domain.EntitlementSourceHousehold, nil
}

// GetEntitlementSummary composes the resolved entitlement with household seat counts for
// GET /api/v1/households/me/entitlement. Solo users (no active household membership)
// get a summary with SeatsActive/SeatsPending 0 and IsOwner false — never an error.
func (s *HouseholdEntitlementService) GetEntitlementSummary(ctx context.Context, user *domain.User) (domain.HouseholdEntitlementSummary, error) {
	eff, err := s.ResolveEffectiveEntitlement(ctx, user)
	if err != nil {
		return domain.HouseholdEntitlementSummary{}, err
	}

	tier, err := s.tierConfig.GetTier(domain.TierName(eff.Tier))
	if err != nil {
		return domain.HouseholdEntitlementSummary{}, err
	}

	summary := domain.HouseholdEntitlementSummary{
		Tier:          eff.Tier,
		UsersIncluded: tier.Pricing.UsersIncluded,
		Source:        eff.Source,
	}

	household, isOwner, found, err := s.householdRepo.GetHouseholdForUser(ctx, user.ID)
	if err != nil {
		return domain.HouseholdEntitlementSummary{}, err
	}
	if !found {
		return summary, nil
	}
	summary.IsOwner = isOwner

	activeCount, err := s.householdRepo.CountActiveMembers(ctx, household.ID)
	if err != nil {
		return domain.HouseholdEntitlementSummary{}, err
	}
	summary.SeatsActive = activeCount

	pendingCount, err := s.householdRepo.CountPendingInvites(ctx, household.ID)
	if err != nil {
		return domain.HouseholdEntitlementSummary{}, err
	}
	summary.SeatsPending = pendingCount

	return summary, nil
}
