package services

import (
	"context"
	"errors"

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
	entitlementRepo repository.EntitlementRepository
	userRepo        userLoader
	householdRepo   repository.HouseholdRepository
	tierConfig      *config.TierConfig
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

// ResolveEffectiveEntitlement returns the user's effective tier+status. Solo users (no
// active entitlement row, or an own_subscription row) resolve to their own computed
// tier/status — identical to what SubscriptionService computed before this resolver
// existed. Household members resolve to the owner's live tier/status, unless the owner
// has lost active coverage (past_due/cancelled/inactive), in which case the member falls
// back to their own tier. Real repository/lookup errors propagate; the caller
// (SubscriptionService) decides the fail-open policy.
func (s *HouseholdEntitlementService) ResolveEffectiveEntitlement(ctx context.Context, user *domain.User) (domain.EffectiveEntitlement, error) {
	own := domain.EffectiveEntitlement{
		Tier:   user.ComputeEffectiveTier(),
		Status: user.SubscriptionStatus,
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

	if domain.SubscriptionStatus(owner.SubscriptionStatus).IsReadOnlySubscription() {
		// Owner lost active coverage (past_due/cancelled/inactive) — member reverts to own tier.
		return own, nil
	}

	return domain.EffectiveEntitlement{
		Tier:        owner.ComputeEffectiveTier(),
		Status:      owner.SubscriptionStatus,
		Source:      domain.EntitlementSourceHousehold,
		PayerUserID: ent.PayerUserID,
		HouseholdID: ent.HouseholdID,
	}, nil
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
