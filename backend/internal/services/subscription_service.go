package services

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// SubscriptionService handles subscription tier management and limit enforcement
type SubscriptionService struct {
	tierConfig   *config.TierConfig
	overrideRepo repository.SubscriptionOverrideRepository
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

// CheckLimit checks if a user is within the limit for a specific resource type
// Returns a LimitCheckResult with details about current usage vs limit
func (s *SubscriptionService) CheckLimit(
	ctx context.Context,
	user *domain.User,
	resourceType domain.ResourceType,
	currentUsage int,
) (*domain.LimitCheckResult, error) {
	// Get user's effective subscription (base tier + any overrides)
	effectiveSub, err := s.GetEffectiveSubscription(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("failed to get effective subscription: %w", err)
	}

	// Get the limit for this resource type
	limit := effectiveSub.Limits.GetLimit(resourceType)

	// Create and return the limit check result
	result := domain.NewLimitCheckResult(resourceType, currentUsage, limit)
	return result, nil
}

// GetEffectiveSubscription returns the effective subscription for a user
// Combines base tier limits with any user-specific overrides
func (s *SubscriptionService) GetEffectiveSubscription(
	ctx context.Context,
	user *domain.User,
) (*domain.EffectiveSubscription, error) {
	// Determine the effective tier: manual override takes precedence if set and not expired
	tierName := domain.TierName(user.SubscriptionTier)
	if user.ManualTierOverride != nil && *user.ManualTierOverride != "" {
		expired := user.ManualTierEndsAt != nil && user.ManualTierEndsAt.Before(time.Now())
		if !expired {
			tierName = domain.TierName(*user.ManualTierOverride)
		}
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
		overrides, err := s.overrideRepo.GetActiveOverrides(ctx, user.ID)
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

// GetUserOverrides returns all active overrides for a user
func (s *SubscriptionService) GetUserOverrides(
	ctx context.Context,
	userID string,
) ([]domain.UserSubscriptionOverride, error) {
	if s.overrideRepo == nil {
		return []domain.UserSubscriptionOverride{}, nil
	}

	overrides, err := s.overrideRepo.GetActiveOverrides(ctx, userID)
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
	case "ai_insights":
		return effectiveSub.Features.AIInsights, nil
	case "api_access":
		return effectiveSub.Features.APIAccess, nil
	case "priority_support":
		return effectiveSub.Features.PrioritySupport, nil
	case "custom_categories":
		return effectiveSub.Features.CustomCategories, nil
	case "advanced_reports":
		return effectiveSub.Features.AdvancedReports, nil
	case "ads_disabled":
		return !effectiveSub.Features.AdsEnabled, nil
	default:
		return false, fmt.Errorf("unknown feature: %s", featureName)
	}
}
