package operations

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
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
		Stage string `json:"stage,omitempty"`
	}
}

// AdminRemoveTierOverrideInput is the request body for clearing a user's manual tier override.
type AdminRemoveTierOverrideInput struct {
	inputs.AdminAuthenticatedInput
	UserID string `path:"user_id" doc:"ID of the user whose manual tier override should be cleared"`
	Body   struct {
		Reason string `json:"reason" required:"true" minLength:"1" doc:"Reason for removing the manual tier override"`
	}
}

// AdminRemoveTierOverrideOutput is the response body for clearing a user's manual tier override.
type AdminRemoveTierOverrideOutput struct {
	Body struct {
		Stage          string `json:"stage,omitempty"`
		Message        string `json:"message"`
		Removed        bool   `json:"removed"`
		RevertedTier   string `json:"reverted_tier"`
		RevertedStatus string `json:"reverted_status"`
	}
}

// AdminSubscriptionDetailOutput represents the detailed subscription state for an admin to view
type AdminSubscriptionDetailOutput struct {
	Body struct {
		Authority          *services.RevenueCatSummary `json:"authority,omitempty"`
		BaseTier           string                      `json:"base_tier"`
		SubscriptionStatus string                      `json:"subscription_status"`
		EffectiveTier      string                      `json:"effective_tier"`
		// EffectiveStatus is the server-computed effective subscription status (Issue #1413) —
		// admins need both raw (SubscriptionStatus) and effective, since a live manual tier
		// override forces access to "active" regardless of the stale raw status.
		EffectiveStatus    string     `json:"effective_status"`
		ManualTierOverride *string    `json:"manual_tier_override,omitempty"`
		ManualTierEndsAt   *time.Time `json:"manual_tier_ends_at,omitempty"`
		// DeletionRequestedAt/DeletionScheduledAt surface a pending GDPR account deletion (Issue
		// #1413 AC 10) so the admin override modal can warn that a comp will NOT cancel a
		// deletion already in progress — the deletion path is deliberately left untouched by a
		// comp. Absent (omitempty) means no pending deletion. Mirrors
		// domain.AdminUserListItem's identically-named/tagged fields.
		DeletionRequestedAt  *time.Time                        `json:"deletion_requested_at,omitempty"`
		DeletionScheduledAt  *time.Time                        `json:"deletion_scheduled_at,omitempty"`
		Limits               domain.SubscriptionLimits         `json:"limits"`
		Features             domain.SubscriptionFeatures       `json:"features"`
		ActiveOverridesCount int                               `json:"active_overrides_count"`
		ResourceOverrides    []domain.UserSubscriptionOverride `json:"resource_overrides"`
		// HouseholdActiveMemberCount is the number of active members in the household owned by
		// this user (0 if they own no household — see
		// AdminUserService.GetOwnedHouseholdActiveMemberCount's doc comment). Issue #1452: lets
		// the override modal warn BEFORE an admin requests a downgrade that would leave the
		// household over the target tier's seat limit. No omitempty — 0 is
		// a real, meaningful answer ("not an owner" or "an empty household"), not an absence.
		HouseholdActiveMemberCount int `json:"household_active_member_count"`
		// AsOf is the server instant EffectiveTier/EffectiveStatus above were computed from
		// (Issue #1462) — a single snapshot the client can anchor its own override-liveness
		// arithmetic to (isOverrideActive/getOverrideStatus in SubscriptionOverrideModal.tsx)
		// instead of the browser's own clock, avoiding client/server clock skew.
		//
		// Batch S2 Task 2 correction: the ONLY defensible claim here is that all expiry/liveness
		// predicates in THIS response were evaluated against the same asOf parameter — a previous
		// version of this comment overclaimed that the chip and the effective-status badge
		// "cannot disagree with the server (or each other)". That is false on two counts this
		// field does not fix:
		//   1. as_of is NOT a historical/transactional snapshot of the whole response. EffectiveTier,
		//      EffectiveStatus, ResourceOverrides, and ActiveOverridesCount above are each populated
		//      by SEPARATE live queries/computations that merely share this one instant as a filter
		//      parameter — they are not read inside a single transaction. An override created (or a
		//      tier changed) AFTER asOf but before one of those later reads completes will still show
		//      up, because none of these queries filter on created_at/updated_at <= asOf, only on
		//      expiry.
		//   2. asOf is NOT equivalent to a database NOW(): NOW() is Postgres's transaction-start
		//      time; asOf is a Go-process wall-clock read (time.Now()) captured BEFORE any of this
		//      handler's DB I/O runs. The two can and do disagree by however long that I/O takes —
		//      this skew is intentional (asOf anchors the CLIENT's rendering, not the database's
		//      internal transaction bookkeeping), not a bug to fix by trying to make them equal.
		AsOf time.Time `json:"as_of"`
	}
}

// AdminSubscriptionDetailInput represents the request for checking user subscription
type AdminSubscriptionDetailInput struct {
	inputs.AdminAuthenticatedInput
	UserID string `path:"user_id" doc:"ID of the user to fetch"`
}

// UpdateUserTier manually overrides a user's subscription tier. Requires admin privileges.
//
// input.UserID is the struct's own path-bound field, which SHADOWS the field promoted from
// AdminAuthenticatedInput -> AuthenticatedInput at the top level. So:
//
//	input.UserID                          -> the TARGET user (path param)
//	input.AdminAuthenticatedInput.UserID  -> the ACTING ADMIN (resolved from JWT/context)
//
// PUT /api/v1/admin/users/{user_id}/subscription/tier
//
// There is no local-write fallback: a change is queued as an audited RevenueCat grant request
// and only takes effect once RevenueCat confirms it. If the RevenueCat admin integration is
// unavailable the handler errors rather than writing local tier columns directly.
func (h *AdminHandler) UpdateUserTier(ctx context.Context, input *AdminSubscriptionTierInput) (*AdminSubscriptionTierOutput, error) {
	grant := &AdminRevenueCatGrantInput{AdminAuthenticatedInput: input.AdminAuthenticatedInput, TargetUserID: input.UserID}
	grant.Body.Class = "comp"
	grant.Body.Tier = input.Body.Tier
	grant.Body.ExpiresAt = input.Body.ExpiresAt
	grant.Body.Reason = input.Body.Reason
	if _, err := h.RequestRevenueCatGrant(ctx, grant); err != nil {
		return nil, err
	}
	out := &AdminSubscriptionTierOutput{}
	out.Body.Stage = "pending"
	out.Body.Message = "Complimentary access change queued. Access changes only after RevenueCat confirms it."
	return out, nil
}

// RemoveUserTierOverride clears a user's manual subscription tier override, restoring their
// raw (non-overridden) tier and status. Requires admin privileges.
//
// Idempotent: calling this on a user with no existing override returns 200 with
// removed=false rather than an error — two admins clicking at once must not give the loser
// an error, and 404 is already taken by "user not found".
//
// POST /api/v1/admin/users/{user_id}/subscription/tier/clear
//
// Like UpdateUserTier, there is no local-write fallback: the revocation is queued as an audited
// RevenueCat comp grant with Revoke=true and only takes effect once RevenueCat confirms it.
func (h *AdminHandler) RemoveUserTierOverride(ctx context.Context, input *AdminRemoveTierOverrideInput) (*AdminRemoveTierOverrideOutput, error) {
	grant := &AdminRevenueCatGrantInput{AdminAuthenticatedInput: input.AdminAuthenticatedInput, TargetUserID: input.UserID}
	grant.Body.Class = "comp"
	grant.Body.Tier = "premium" // revocation is class-scoped, not key-scoped
	grant.Body.Revoke = true
	grant.Body.Reason = input.Body.Reason
	operation, err := h.RequestRevenueCatGrant(ctx, grant)
	if err != nil {
		return nil, err
	}
	out := &AdminRemoveTierOverrideOutput{}
	out.Body.Stage = operation.Body.Stage
	out.Body.Message = "Complimentary access removal queued. Paid access and trials are unchanged."
	return out, nil
}

// GetUserSubscription gets user subscription details including overrides.
//
// GET /api/v1/admin/users/{user_id}/subscription
func (h *AdminHandler) GetUserSubscription(ctx context.Context, input *AdminSubscriptionDetailInput) (*AdminSubscriptionDetailOutput, error) {
	if h.revenueCat != nil {
		return h.getRevenueCatSubscriptionDetail(ctx, input.UserID)
	}
	// Issue #1462: captured before the rest of this handler's work so `as_of` reflects (at the
	// latest) the instant EffectiveTier/EffectiveStatus below started being computed from.
	asOf := time.Now()

	// Pass asOf through so EffectiveStatus (enrichAdminUser, inside GetUserDetail) is derived from
	// the identical instant reported below as `as_of` — not a second, independent time.Now() read
	// (Codex review finding on Issue #1462/#1413).
	detail, err := h.adminUserService.GetUserDetail(ctx, input.UserID, asOf)
	if err != nil {
		if errors.Is(err, services.ErrAdminUserNotFound) {
			return nil, huma.Error404NotFound("user not found")
		}
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to fetch user: %w", err))
	}

	userObj := &domain.User{
		ID:                 detail.ID,
		SubscriptionTier:   detail.SubscriptionTier,
		SubscriptionStatus: detail.SubscriptionStatus,
		ManualTierOverride: detail.ManualTierOverride,
		ManualTierEndsAt:   detail.ManualTierEndsAt,
	}

	var effectiveSub *domain.EffectiveSubscription
	if h.subService != nil {
		// Issue #1466: derive EffectiveTier from the SAME asOf instant already captured above for
		// `as_of` and (via GetUserDetail) for EffectiveStatus — not a second, independent
		// time.Now() read. Before this fix, GetEffectiveSubscription's no-arg form read its own
		// fresh clock through domain.User.EffectiveTierWithActiveOverride(), so a request crossing
		// an override's expiry boundary between asOf's capture and this call could return an
		// effective_tier describing a later instant than as_of/effective_status.
		//
		// Batch S2 Task 1 closed a residual of this same bug class one layer down: when a
		// household entitlement resolver is wired (main.go's production wiring, unconditional for
		// every request, not only household members), GetEffectiveSubscriptionAt used to have its
		// asOf-derived tier UNCONDITIONALLY OVERWRITTEN by
		// HouseholdEntitlementResolver.ResolveEffectiveEntitlement(ctx, user), which had no instant
		// parameter of its own and fell back to ITS OWN internal time.Now(). That is now threaded
		// through via ResolveEffectiveEntitlementAt(ctx, user, asOf) — see that method's doc
		// comment (household_entitlement_service.go) — so this call's `asOf` genuinely reaches
		// every tier computation this response depends on, not just the tier-axis's own top-level
		// override check.
		effectiveSub, err = h.subService.GetEffectiveSubscriptionAt(ctx, userObj, asOf)
		if err != nil {
			return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to calculate effective limits: %w", err))
		}
	} else {
		effectiveSub = &domain.EffectiveSubscription{
			Tier:     domain.TierName(userObj.SubscriptionTier),
			Limits:   domain.SubscriptionLimits{},
			Features: domain.SubscriptionFeatures{},
		}
	}

	// Batch S2 Task 2: GetUserOverridesAt(ctx, userObj.ID, asOf) — not the no-arg GetUserOverrides
	// — so this list filters against the exact SAME instant already captured above as `asOf` and
	// used by GetEffectiveSubscriptionAt's own internal GetActiveOverrides call (which backs
	// active_overrides_count/limits below). Before this fix, this was a second, independent
	// NOW()-filtered query — theoretically able to disagree with active_overrides_count in the
	// very same response if an override's expiry fell between the two live reads.
	overrides := []domain.UserSubscriptionOverride{}
	if h.subService != nil {
		if o, _ := h.subService.GetUserOverridesAt(ctx, userObj.ID, asOf); o != nil {
			overrides = o
		}
	}

	resp := &AdminSubscriptionDetailOutput{}
	resp.Body.BaseTier = detail.SubscriptionTier
	resp.Body.SubscriptionStatus = detail.SubscriptionStatus
	resp.Body.EffectiveTier = string(effectiveSub.Tier)
	// EffectiveStatus is already computed by AdminUserService.GetUserDetail (enrichAdminUser) —
	// the single source of truth for override-liveness — so it is surfaced directly rather than
	// recomputed here.
	resp.Body.EffectiveStatus = detail.EffectiveStatus
	resp.Body.ManualTierOverride = detail.ManualTierOverride
	resp.Body.ManualTierEndsAt = detail.ManualTierEndsAt
	// Sourced from the same detail already loaded above — no second query (Issue #1413 AC 10).
	resp.Body.DeletionRequestedAt = detail.DeletionRequestedAt
	resp.Body.DeletionScheduledAt = detail.DeletionScheduledAt
	resp.Body.Limits = effectiveSub.Limits
	resp.Body.Features = effectiveSub.Features
	resp.Body.ActiveOverridesCount = effectiveSub.ActiveOverridesCount
	resp.Body.ResourceOverrides = overrides

	// Issue #1452: the pre-apply seat-reduction warning needs this count available in the
	// SAME response the modal already loads — a real repository failure here (not "0, no
	// household") should fail the request rather than silently rendering as "not an owner".
	householdActiveMemberCount, err := h.adminUserService.GetOwnedHouseholdActiveMemberCount(ctx, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to fetch household active member count: %w", err))
	}
	resp.Body.HouseholdActiveMemberCount = householdActiveMemberCount
	resp.Body.AsOf = asOf

	return resp, nil
}

// AdminCreateOverrideInput is the request body for creating a per-user resource limit override
type AdminCreateOverrideInput struct {
	inputs.AdminAuthenticatedInput
	UserID string `path:"user_id" doc:"ID of the user to override"`
	Body   struct {
		ResourceType string `json:"resource_type" enum:"transaction_history_days" doc:"The resource type to override"`
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
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to create override: %w", err))
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

	// POST /api/v1/admin/users/{user_id}/subscription/tier/clear
	huma.Register(api, huma.Operation{
		OperationID: "remove-admin-user-tier-override",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/users/{user_id}/subscription/tier/clear",
		Summary:     "Remove manual subscription tier override",
		Description: "Clear a user's manual tier override, restoring their raw subscription tier and status. Idempotent — returns removed=false if no override exists. Requires admin privileges.",
		Tags:        []string{"Admin Subscriptions"},
		Security: []map[string][]string{
			{"clerk": {}},
		},
		Middlewares: huma.Middlewares{
			humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
		},
	}, handler.RemoveUserTierOverride)

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
		Description: "Create a per-user resource limit override (e.g. transaction_history_days). Requires admin privileges.",
		Tags:        []string{"Admin Subscriptions"},
		Security: []map[string][]string{
			{"clerk": {}},
		},
		Middlewares: huma.Middlewares{
			humamiddleware.NewAdminMiddleware(handler.config, handler.adminService),
		},
	}, handler.CreateSubscriptionOverride)
}
