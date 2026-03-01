package operations

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// AdminSubscriptionTierInput is the request body for updating a user's tier
type AdminSubscriptionTierInput struct {
	inputs.AdminAuthenticatedInput
	UserID string `path:"user_id" doc:"ID of the user to update"`
	Body   struct {
		Tier      string     `json:"tier" enum:"premium,household_2,household_3,household_4,household_5" doc:"The subscription tier to set"`
		Reason    string     `json:"reason" required:"true" doc:"Reason for the manual tier override"`
		ExpiresAt *time.Time `json:"expires_at" doc:"Optional expiration date for the manual override"`
	}
}

// AdminSubscriptionTierOutput is the response body for updating a user's tier
type AdminSubscriptionTierOutput struct {
	Body struct {
		Message string `json:"message"`
	}
}

// AdminSubscriptionDetailOutput represents the detailed subscription state for an admin to view
type AdminSubscriptionDetailOutput struct {
	Body struct {
		BaseTier             string                            `json:"base_tier"`
		SubscriptionStatus   string                            `json:"subscription_status"`
		EffectiveTier        string                            `json:"effective_tier"`
		ManualTierOverride   *string                           `json:"manual_tier_override,omitempty"`
		ManualTierEndsAt     *time.Time                        `json:"manual_tier_ends_at,omitempty"`
		Limits               domain.SubscriptionLimits         `json:"limits"`
		Features             domain.SubscriptionFeatures       `json:"features"`
		ActiveOverridesCount int                               `json:"active_overrides_count"`
		ResourceOverrides    []domain.UserSubscriptionOverride `json:"resource_overrides"`
	}
}

// AdminSubscriptionDetailInput represents the request for checking user subscription
type AdminSubscriptionDetailInput struct {
	inputs.AdminAuthenticatedInput
	UserID string `path:"user_id" doc:"ID of the user to fetch"`
}

// UpdateUserTier manually overrides a user's subscription tier. Requires admin privileges.
//
// PUT /api/v1/admin/users/{user_id}/subscription/tier
func (h *AdminHandler) UpdateUserTier(ctx context.Context, input *AdminSubscriptionTierInput) (*AdminSubscriptionTierOutput, error) {
	clerkUserID, ok := ctx.Value(inputs.ClerkUserIDKey).(string)
	if !ok || clerkUserID == "" {
		return nil, huma.Error401Unauthorized("unauthorized: missing clerk user id")
	}

	err := h.adminUserService.UpdateUserTier(
		ctx,
		clerkUserID,
		input.UserID,
		input.Body.Tier,
		input.Body.Reason,
		input.Body.ExpiresAt,
	)

	if err != nil {
		if errors.Is(err, services.ErrAdminUserNotFound) {
			return nil, huma.Error404NotFound("user not found")
		}
		return nil, huma.Error500InternalServerError("failed to update user tier", err)
	}

	resp := &AdminSubscriptionTierOutput{}
	resp.Body.Message = "User subscription tier updated successfully"
	return resp, nil
}

// GetUserSubscription gets user subscription details including overrides.
//
// GET /api/v1/admin/users/{user_id}/subscription
func (h *AdminHandler) GetUserSubscription(ctx context.Context, input *AdminSubscriptionDetailInput) (*AdminSubscriptionDetailOutput, error) {
	// 1. Get the user details to see the base tier and manual overrides
	detail, err := h.adminUserService.GetUserDetail(ctx, input.UserID)
	if err != nil {
		if errors.Is(err, services.ErrAdminUserNotFound) {
			return nil, huma.Error404NotFound("user not found")
		}
		return nil, huma.Error500InternalServerError("failed to fetch user", err)
	}

	// 2. We need a domain.User to get the effective subscription limits and features
	userObj := &domain.User{
		ID:                 detail.ID,
		SubscriptionTier:   detail.SubscriptionTier,
		SubscriptionStatus: detail.SubscriptionStatus,
		ManualTierOverride: detail.ManualTierOverride,
		ManualTierEndsAt:   detail.ManualTierEndsAt,
	}

	// 3. Get effective subscription limits
	var effectiveSub *domain.EffectiveSubscription
	if h.subService != nil {
		effectiveSub, err = h.subService.GetEffectiveSubscription(ctx, userObj)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to calculate effective limits", err)
		}
	} else {
		effectiveSub = &domain.EffectiveSubscription{
			Tier:     domain.TierName(userObj.SubscriptionTier),
			Limits:   domain.SubscriptionLimits{},
			Features: domain.SubscriptionFeatures{},
		}
	}

	// 4. Get specific resource overrides
	var overrides []domain.UserSubscriptionOverride
	if h.subService != nil {
		overrides, _ = h.subService.GetUserOverrides(ctx, userObj.ID)
	}
	if overrides == nil {
		overrides = []domain.UserSubscriptionOverride{}
	}

	// Populate response
	resp := &AdminSubscriptionDetailOutput{}
	resp.Body.BaseTier = detail.SubscriptionTier
	resp.Body.SubscriptionStatus = detail.SubscriptionStatus
	resp.Body.EffectiveTier = string(effectiveSub.Tier)
	resp.Body.ManualTierOverride = detail.ManualTierOverride
	resp.Body.ManualTierEndsAt = detail.ManualTierEndsAt
	resp.Body.Limits = effectiveSub.Limits
	resp.Body.Features = effectiveSub.Features
	resp.Body.ActiveOverridesCount = effectiveSub.ActiveOverridesCount
	resp.Body.ResourceOverrides = overrides

	return resp, nil
}

// AdminCreateOverrideInput is the request body for creating a per-user resource limit override
type AdminCreateOverrideInput struct {
	inputs.AdminAuthenticatedInput
	UserID string `path:"user_id" doc:"ID of the user to override"`
	Body   struct {
		ResourceType string `json:"resource_type" enum:"accounts,automation_rules,syncs_per_day,transaction_history_days" doc:"The resource type to override"`
		LimitValue   int    `json:"limit_value" doc:"The new limit value (-1 for unlimited)"`
		Reason       string `json:"reason" required:"true" doc:"Reason for the override"`
	}
}

// AdminCreateOverrideOutput is the response body for creating a resource limit override
type AdminCreateOverrideOutput struct {
	Body struct {
		Message string `json:"message"`
	}
}

// CreateSubscriptionOverride creates a per-user resource limit override.
//
// POST /api/v1/admin/users/{user_id}/subscription/override
func (h *AdminHandler) CreateSubscriptionOverride(ctx context.Context, input *AdminCreateOverrideInput) (*AdminCreateOverrideOutput, error) {
	if h.subService == nil {
		return nil, huma.Error503ServiceUnavailable("subscription service not configured")
	}

	err := h.subService.ApplyOverride(
		ctx,
		input.UserID,
		domain.ResourceType(input.Body.ResourceType),
		input.Body.LimitValue,
		input.Body.Reason,
	)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to create override", err)
	}

	resp := &AdminCreateOverrideOutput{}
	resp.Body.Message = "Subscription override created successfully"
	return resp, nil
}

// RegisterAdminSubscriptionsOperations registers the admin user operations
func RegisterAdminSubscriptionsOperations(api huma.API, handler *AdminHandler) {
	// PUT /api/v1/admin/users/{user_id}/subscription/tier
	huma.Register(api, huma.Operation{
		OperationID: "update-admin-user-tier",
		Method:      http.MethodPut,
		Path:        "/api/v1/admin/users/{user_id}/subscription/tier",
		Summary:     "Update user subscription tier",
		Description: "Manually override a user's subscription tier. Requires admin privileges.",
		Tags:        []string{"Admin Subscriptions"},
		Security: []map[string][]string{
			{"clerk": {}},
		},
		Middlewares: huma.Middlewares{
			humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
		},
	}, handler.UpdateUserTier)

	// GET /api/v1/admin/users/{user_id}/subscription
	huma.Register(api, huma.Operation{
		OperationID: "get-admin-user-subscription",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/users/{user_id}/subscription",
		Summary:     "Get user subscription details",
		Description: "View a user's subscription tier, manual overrides, and effective limits.",
		Tags:        []string{"Admin Subscriptions"},
		Security: []map[string][]string{
			{"clerk": {}},
		},
		Middlewares: huma.Middlewares{
			humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
		},
	}, handler.GetUserSubscription)

	// POST /api/v1/admin/users/{user_id}/subscription/override
	huma.Register(api, huma.Operation{
		OperationID: "create-admin-subscription-override",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/users/{user_id}/subscription/override",
		Summary:     "Create subscription resource override",
		Description: "Create a per-user resource limit override (e.g. linked_accounts, automation_rules). Requires admin privileges.",
		Tags:        []string{"Admin Subscriptions"},
		Security: []map[string][]string{
			{"clerk": {}},
		},
		Middlewares: huma.Middlewares{
			humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
		},
	}, handler.CreateSubscriptionOverride)
}
