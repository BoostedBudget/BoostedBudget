package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// SubscriptionService handles subscription tier management and limit enforcement
type SubscriptionService struct {
	tierConfig            *config.TierConfig
	overrideRepo          repository.SubscriptionOverrideRepository
	entitlementResolver   HouseholdEntitlementResolver
	overrideTierValidator func(tierName string) bool
}

// NewSubscriptionService creates a new subscription service
func NewSubscriptionService(
	tierConfig *config.TierConfig,
	overrideRepo repository.SubscriptionOverrideRepository,
) *SubscriptionService {
	return &SubscriptionService{
		tierConfig:   tierConfig,
		overrideRepo: overrideRepo,
	}
}

// SetEntitlementResolver injects an optional household entitlement resolver
// (Issue #1366). Nil-safe: GetEffectiveSubscription falls back to
// user.ComputeEffectiveTier() when unset, or on any resolver error — an entitlement
// lookup failure must never break tier/limit resolution for the whole user base.
func (s *SubscriptionService) SetEntitlementResolver(resolver HouseholdEntitlementResolver) {
	s.entitlementResolver = resolver
}

// SetOverrideTierValidator injects a function that reports whether a manual_tier_override value
// names a real, currently-configured tier. GetEffectiveSubscription uses it to stop a malformed
// override from reaching tierConfig.GetTier() as an unrecognized name (Issue #1413 — the tier
// axis of the malformed-override rule): manual_tier_override is an unconstrained VARCHAR(50)
// with no CHECK constraint, so its value can be garbage (a legacy row, a direct SQL write, a
// write made while the tier config was nil). A rejected override falls back to the raw
// SubscriptionTier — this single fix point closes the admin subscription-detail 500 (this
// service backs GET /admin/users/{id}/subscription), the Plaid first-sync misclassification
// (GetHistoricalDepthLimit calls through here), and the household entitlement resolution's own
// downstream tierConfig.GetTier call (a household member's coverage tier flows through eff.Tier
// below).
//
// Same setter-injection convention, same func(string) bool signature, and the same nil-safe
// "skip, don't deny" semantics as repository.profileRepository.SetOverrideTierValidator — see
// that method's doc comment for the full rationale, which applies here unchanged: main.go loads
// the tier config once at startup and os.Exit(1)s if that fails, so in production this validator
// is either wired for the entire process lifetime or the server never boots. If never called,
// validation is skipped and a malformed override reaches tierConfig.GetTier() exactly as before
// this fix — the pre-fix behavior — rather than denying every comp, which would turn a future
// wiring oversight into an instant outage for every currently-comped user.
func (s *SubscriptionService) SetOverrideTierValidator(validator func(tierName string) bool) {
	s.overrideTierValidator = validator
}

// effectiveTierNameAt returns the user's effective tier name as of `now`, validating an active
// manual override against the injected overrideTierValidator (see SetOverrideTierValidator) and
// falling back to the raw SubscriptionTier when it names no configured tier. Reuses
// domain.User.EffectiveTierWithActiveOverrideAt so this and every other tier-axis call site share
// exactly one definition of "is the override active" with the tier/status computations
// elsewhere.
//
// Takes `now` explicitly (Issue #1466) rather than reading time.Now() itself, so
// GetEffectiveSubscriptionAt can derive the tier from the SAME instant its caller already
// captured (e.g. an admin handler's `asOf`) instead of a second, independent clock read. This
// method has exactly one caller (GetEffectiveSubscriptionAt) — there is no longer a no-arg
// wrapper delegating to it, since nothing else needs one; GetEffectiveSubscription's own no-arg
// convenience form threads time.Now() through GetEffectiveSubscriptionAt at the OUTER layer
// instead.
func (s *SubscriptionService) effectiveTierNameAt(user *domain.User, now time.Time) string {
	tier, overrideValue, hasOverride := user.EffectiveTierWithActiveOverrideAt(now)
	if hasOverride && s.overrideTierValidator != nil && !s.overrideTierValidator(overrideValue) {
		slog.Warn("rejecting manual tier override on tier axis: value does not name a configured tier",
			"user_id", user.ID, "manual_tier_override", overrideValue)
		return user.SubscriptionTier
	}
	return tier
}

// GetTier returns the tier configuration for a given tier name
func (s *SubscriptionService) GetTier(ctx context.Context, tierName domain.TierName) (domain.SubscriptionTier, error) {
	tier, err := s.tierConfig.GetTier(tierName)
	if err != nil {
		return domain.SubscriptionTier{}, fmt.Errorf("failed to get tier '%s': %w", tierName, err)
	}
	return tier, nil
}

// GetDefaultTier returns the default tier configuration
func (s *SubscriptionService) GetDefaultTier(ctx context.Context) (domain.SubscriptionTier, error) {
	tier, err := s.tierConfig.GetDefaultTier()
	if err != nil {
		return domain.SubscriptionTier{}, fmt.Errorf("failed to get default tier: %w", err)
	}
	return tier, nil
}

// ListTiers returns all available subscription tiers
func (s *SubscriptionService) ListTiers(ctx context.Context) ([]domain.SubscriptionTier, error) {
	tiers := s.tierConfig.ListTiers()
	return tiers, nil
}

// GetEffectiveSubscription returns the effective subscription for a user
// Combines base tier limits with any user-specific overrides and, if an
// entitlement resolver is injected, household coverage (Issue #1366).
//
// Delegates to GetEffectiveSubscriptionAt(ctx, user, time.Now()) — see that method's doc comment
// for why a caller assembling a response around a single already-captured snapshot instant (e.g.
// an admin handler's `asOf` echoed back as `as_of`) must call the -At variant with that instant
// instead of this method, which reads its own fresh clock. Every other call site (GetTier,
// GetDefaultTier, ListTiers aside — this method's own siblings GetHistoricalDepthLimit and
// CanUserAccessFeature, both below) is unaffected by this delegation and needs no change.
func (s *SubscriptionService) GetEffectiveSubscription(
	ctx context.Context,
	user *domain.User,
) (*domain.EffectiveSubscription, error) {
	return s.GetEffectiveSubscriptionAt(ctx, user, time.Now())
}

// GetEffectiveSubscriptionAt is the -At twin of GetEffectiveSubscription, evaluated against the
// given instant instead of an internal time.Now() read.
//
// Added for Issue #1466 (a #1462 residual): the admin subscription-detail handler
// (GetUserSubscription, admin_subscriptions.go) captures a single `asOf := time.Now()` and echoes
// it back as `as_of`, alongside an `effective_status` already derived from that same instant
// (domain.User.EffectiveStatusWithActiveOverrideAt, via AdminUserService.GetUserDetail — Issue
// #1465). Before this fix, `effective_tier` was computed via the no-arg GetEffectiveSubscription,
// which read a SECOND, independent time.Now() through effectiveTierName ->
// domain.User.EffectiveTierWithActiveOverride() — so a request whose processing crossed the
// override's expiry boundary between the handler's asOf capture and this call could return an
// as_of/effective_status describing one instant and an effective_tier describing a later one.
//
// Batch S2 Task 1: the injected HouseholdEntitlementResolver (SetEntitlementResolver) IS now
// threaded through this `now` via ResolveEffectiveEntitlementAt. Before this fix, the resolver's
// plain ResolveEffectiveEntitlement(ctx, user) had no instant parameter at all, so it fell back to
// its OWN internal time.Now() — a SECOND, independent clock read taken later than `now`/asOf. That
// silently overwrote tierName (see the unconditional assignment below) for EVERY user the resolver
// is asked about whenever one is wired, not only household members: main.go's production wiring
// (SetEntitlementResolver(householdEntitlementService)) is unconditional for every request, and
// even the resolver's "own" (no household row) branch — the common case — used to read its own
// clock. A previous version of this doc comment scoped the gap to "a household member's tier",
// which understated it. See
// TestGetEffectiveSubscriptionAt_ProductionWiring_RealResolver_TierDerivedFromAsOf_NotResolversOwnClock
// (subscription_service_test.go) for the regression proof, and
// HouseholdEntitlementResolver.ResolveEffectiveEntitlementAt's own doc comment
// (household_entitlement_service.go) for the resolver-side half of this fix.
func (s *SubscriptionService) GetEffectiveSubscriptionAt(
	ctx context.Context,
	user *domain.User,
	now time.Time,
) (*domain.EffectiveSubscription, error) {
	// Determine the effective tier: manual override takes precedence if set, not expired, and
	// (Issue #1413) names a real configured tier — see effectiveTierNameAt's doc comment.
	tierName := domain.TierName(s.effectiveTierNameAt(user, now))

	// A configured authority must succeed. Legacy user fields are not a fallback
	// entitlement during an outage; callers preserve reads but deny protected writes.
	if s.entitlementResolver != nil {
		eff, err := s.entitlementResolver.ResolveEffectiveEntitlementAt(ctx, user, now)
		if err != nil {
			return nil, fmt.Errorf("subscription entitlement resolution failed: %w", err)
		}
		tierName = domain.TierName(eff.Tier)
	}

	// Get tier configuration for the effective tier
	tier, err := s.GetTier(ctx, tierName)
	if err != nil {
		return nil, fmt.Errorf("failed to get tier for user: %w", err)
	}

	// Start with base tier limits
	effectiveLimits := tier.Limits

	// Check for user-specific overrides (if repository is available)
	var hasOverrides bool
	var activeOverridesCount int

	if s.overrideRepo != nil {
		overrides, err := s.overrideRepo.GetActiveOverrides(ctx, user.ID, now)
		if err != nil {
			// Log error but continue with base limits
			// In production, you might want to handle this differently
			// For now, we'll just use base tier limits
		} else if len(overrides) > 0 {
			hasOverrides = true
			activeOverridesCount = len(overrides)

			// Apply overrides to effective limits
			for _, override := range overrides {
				resourceType := domain.ResourceType(override.ResourceType)
				effectiveLimits.SetLimit(resourceType, override.LimitValue)
			}
		}
	}

	return &domain.EffectiveSubscription{
		UserID:               user.ID,
		Tier:                 tierName,
		TierDisplayName:      tier.DisplayName,
		Limits:               effectiveLimits,
		Features:             tier.Features,
		HasOverrides:         hasOverrides,
		ActiveOverridesCount: activeOverridesCount,
		UsersIncluded:        tier.Pricing.UsersIncluded,
	}, nil
}

// ApplyOverride applies a custom limit override for a user
// Used for enterprise deals, add-on purchases, or promotional upgrades
func (s *SubscriptionService) ApplyOverride(
	ctx context.Context,
	userID string,
	resourceType domain.ResourceType,
	newLimit int,
	reason string,
) error {
	if s.overrideRepo == nil {
		return fmt.Errorf("override repository not available")
	}

	override := &domain.UserSubscriptionOverride{
		UserID:       userID,
		ResourceType: string(resourceType),
		LimitValue:   newLimit,
		Reason:       &reason,
	}

	if err := s.overrideRepo.CreateOverride(ctx, override); err != nil {
		return fmt.Errorf("failed to create override: %w", err)
	}

	return nil
}

// RemoveOverride removes a custom limit override for a user
func (s *SubscriptionService) RemoveOverride(
	ctx context.Context,
	userID string,
	resourceType domain.ResourceType,
) error {
	if s.overrideRepo == nil {
		return fmt.Errorf("override repository not available")
	}

	if err := s.overrideRepo.DeleteOverride(ctx, userID, string(resourceType)); err != nil {
		return fmt.Errorf("failed to delete override: %w", err)
	}

	return nil
}

// GetUserOverrides returns all active overrides for a user.
//
// Delegates to GetUserOverridesAt(ctx, userID, time.Now()) — see that method's doc comment for
// why a caller assembling a response around a single already-captured snapshot instant (the admin
// subscription-detail handler's `asOf`) must call the -At variant instead of this method, which
// reads its own fresh clock.
func (s *SubscriptionService) GetUserOverrides(
	ctx context.Context,
	userID string,
) ([]domain.UserSubscriptionOverride, error) {
	return s.GetUserOverridesAt(ctx, userID, time.Now())
}

// GetUserOverridesAt is the -At twin of GetUserOverrides, evaluated against the given instant
// instead of an internal time.Now() read.
//
// Added for Batch S2 Task 2: GetUserSubscription (admin_subscriptions.go) calls this a SECOND
// time in the same handler invocation, after GetEffectiveSubscriptionAt already queried
// GetActiveOverrides once internally (for ActiveOverridesCount/limits) — both must filter
// against the SAME `asOf` the handler already captured and echoes back as `as_of`, or the
// returned resource_overrides list could show an override as active/expired inconsistently with
// active_overrides_count in the very same response, purely because a moment of NOW() elapsed
// between the two independent live queries.
func (s *SubscriptionService) GetUserOverridesAt(
	ctx context.Context,
	userID string,
	asOf time.Time,
) ([]domain.UserSubscriptionOverride, error) {
	if s.overrideRepo == nil {
		return []domain.UserSubscriptionOverride{}, nil
	}

	overrides, err := s.overrideRepo.GetActiveOverrides(ctx, userID, asOf)
	if err != nil {
		return nil, fmt.Errorf("failed to get user overrides: %w", err)
	}

	return overrides, nil
}

// ValidateUpgrade checks if a user can upgrade to a target tier
// Returns error if downgrade attempted or invalid tier
func (s *SubscriptionService) ValidateUpgrade(
	ctx context.Context,
	currentTier, targetTier domain.TierName,
) error {
	// Get both tier configurations
	current, err := s.GetTier(ctx, currentTier)
	if err != nil {
		return fmt.Errorf("invalid current tier: %w", err)
	}

	target, err := s.GetTier(ctx, targetTier)
	if err != nil {
		return fmt.Errorf("invalid target tier: %w", err)
	}

	// Check if it's actually an upgrade (higher price)
	if target.Pricing.PriceMonthly < current.Pricing.PriceMonthly {
		return fmt.Errorf("cannot downgrade from %s to %s", currentTier, targetTier)
	}

	return nil
}

// GetHistoricalDepthLimit returns the number of days of historical transactions
// the user is allowed to sync based on their subscription tier.
// Replaces SyncLimitService.GetHistoricalDepthLimit (Issue #839: sync limits removal).
func (s *SubscriptionService) GetHistoricalDepthLimit(ctx context.Context, user *domain.User) (int, error) {
	subscription, err := s.GetEffectiveSubscription(ctx, user)
	if err != nil {
		return 0, fmt.Errorf("failed to get subscription: %w", err)
	}
	return subscription.Limits.TransactionHistoryDays, nil
}

// CanUserAccessFeature checks if a user's tier includes a specific feature
func (s *SubscriptionService) CanUserAccessFeature(
	ctx context.Context,
	user *domain.User,
	featureName string,
) (bool, error) {
	effectiveSub, err := s.GetEffectiveSubscription(ctx, user)
	if err != nil {
		return false, fmt.Errorf("failed to get effective subscription: %w", err)
	}

	// Check feature availability
	switch featureName {
	case "api_access":
		return effectiveSub.Features.APIAccess, nil
	case "priority_support":
		return effectiveSub.Features.PrioritySupport, nil
	case "custom_categories":
		return effectiveSub.Features.CustomCategories, nil
	case "advanced_reports":
		return effectiveSub.Features.AdvancedReports, nil
	default:
		return false, fmt.Errorf("unknown feature: %s", featureName)
	}
}
