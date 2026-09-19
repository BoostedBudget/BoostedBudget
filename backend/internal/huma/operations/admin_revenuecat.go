package operations

import (
	"context"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/queue/jobs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

type revenueCatAdminGranter interface {
	RequestGrant(context.Context, string, string, string, *time.Time, bool, string, string) (domain.RevenueCatOperation, error)
}
type RevenueCatAdminDeps struct {
	Grants   revenueCatAdminGranter
	Profiles revenueCatSubscriptionProfiles
	Admin    repository.RevenueCatAdminRepository
	States   repository.RevenueCatRepository
	Summary  profileSubscriptionSummary
}

type AdminRevenueCatUserInput struct {
	inputs.AdminAuthenticatedInput
	TargetUserID string `path:"user_id"`
}

type AdminRevenueCatUserOutput struct {
	Body AdminRevenueCatUserOutputBody
}

type AdminRevenueCatUserOutputBody struct {
	Summary    services.RevenueCatSummary   `json:"summary"`
	State      *domain.RevenueCatState      `json:"state"`
	Operations []domain.RevenueCatOperation `json:"operations"`
}

func (AdminRevenueCatUserOutputBody) TransformSchema(_ huma.Registry, schema *huma.Schema) *huma.Schema {
	// A failed state read leaves diagnostics/history available with state: null.
	schema.Properties["state"] = &huma.Schema{AnyOf: []*huma.Schema{schema.Properties["state"], {Type: "null", Enum: []any{nil}}}}
	return schema
}

func (h *AdminHandler) getRevenueCatSubscriptionDetail(ctx context.Context, id string) (*AdminSubscriptionDetailOutput, error) {
	if h.revenueCat.Profiles == nil || h.revenueCat.Summary == nil {
		return nil, huma.Error503ServiceUnavailable("Subscription diagnostics are unavailable.")
	}
	user, err := h.revenueCat.Profiles.GetUserByID(ctx, id)
	if err != nil || user == nil || user.IsDeleted {
		return nil, huma.Error404NotFound("Account not found.")
	}
	summary := h.revenueCat.Summary.Summary(ctx, user)
	out := &AdminSubscriptionDetailOutput{}
	out.Body.Authority = &summary
	out.Body.EffectiveStatus = "unresolved"
	out.Body.SubscriptionStatus = "unresolved"
	out.Body.BaseTier = "premium"
	out.Body.EffectiveTier = "premium"
	out.Body.DeletionRequestedAt = user.DeletionRequestedAt
	out.Body.DeletionScheduledAt = user.DeletionScheduledAt
	out.Body.ResourceOverrides = []domain.UserSubscriptionOverride{}
	if h.subService != nil {
		out.Body.ResourceOverrides, err = h.subService.GetUserOverrides(ctx, id)
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("Resource overrides could not be read.")
		}
		out.Body.ActiveOverridesCount = len(out.Body.ResourceOverrides)
	}
	if summary.Own != nil {
		out.Body.BaseTier = summary.Own.Tier
		out.Body.SubscriptionStatus = summary.Own.Status
	}
	if summary.AccessResolved && summary.Effective != nil {
		out.Body.EffectiveTier = summary.Effective.Tier
		out.Body.EffectiveStatus = summary.Effective.Status
		if h.subService != nil {
			if tier, err := h.subService.GetTier(ctx, domain.TierName(summary.Effective.Tier)); err == nil {
				out.Body.Limits = tier.Limits
				out.Body.Features = tier.Features
				for _, override := range out.Body.ResourceOverrides {
					out.Body.Limits.SetLimit(domain.ResourceType(override.ResourceType), override.LimitValue)
				}
			} else {
				// Unknown tier configuration cannot produce credible limits.
				out.Body.EffectiveStatus = "unresolved"
				out.Body.Authority.AccessResolved = false
				out.Body.Authority.Effective = nil
			}
		}
	}
	if summary.Comp != nil {
		out.Body.ManualTierOverride = &summary.Comp.Tier
		out.Body.ManualTierEndsAt = summary.Comp.ExpiresAt
	}
	if h.adminUserService != nil {
		out.Body.HouseholdActiveMemberCount, err = h.adminUserService.GetOwnedHouseholdActiveMemberCount(ctx, id)
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("Household seat usage could not be read.")
		}
	}
	out.Body.AsOf = summary.AsOf
	return out, nil
}

func (h *AdminHandler) GetRevenueCatUser(ctx context.Context, input *AdminRevenueCatUserInput) (*AdminRevenueCatUserOutput, error) {
	if h.revenueCat == nil || h.revenueCat.Profiles == nil || h.revenueCat.Summary == nil || h.revenueCat.States == nil {
		return nil, huma.Error503ServiceUnavailable("Subscription diagnostics are unavailable.")
	}
	user, err := h.revenueCat.Profiles.GetUserByID(ctx, input.TargetUserID)
	if err != nil || user == nil || user.IsDeleted {
		return nil, huma.Error404NotFound("Account not found.")
	}
	out := &AdminRevenueCatUserOutput{}
	out.Body.Summary = h.revenueCat.Summary.Summary(ctx, user)
	if state, err := h.revenueCat.States.Read(ctx, user.ID); err == nil {
		out.Body.State = &state
	}
	out.Body.Operations, err = h.revenueCat.States.ListOperations(ctx, user.ID)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("Grant operations could not be read.")
	}
	return out, nil
}

func (h *AdminHandler) RefreshRevenueCatUser(ctx context.Context, input *AdminRevenueCatUserInput) (*RevenueCatWebhookOutput, error) {
	if input.UserID == "" {
		return nil, huma.Error401Unauthorized("An authenticated admin is required.")
	}
	if h.revenueCat == nil || h.revenueCat.Profiles == nil || h.jobQueue == nil {
		return nil, huma.Error503ServiceUnavailable("Subscription recovery is unavailable.")
	}
	user, err := h.revenueCat.Profiles.GetUserByID(ctx, input.TargetUserID)
	if err != nil || user == nil || user.IsDeleted {
		return nil, huma.Error404NotFound("Account not found.")
	}
	if _, err := h.jobQueue.Enqueue(ctx, jobs.RevenueCatSyncArgs{UserID: user.ID}, jobs.RevenueCatSyncEnqueueOptions(true)...); err != nil {
		return nil, huma.Error503ServiceUnavailable("Recovery could not be queued. Try again shortly.")
	}
	out := &RevenueCatWebhookOutput{}
	out.Body.Status = "queued"
	return out, nil
}

type AdminRevenueCatWebhooksInput struct {
	inputs.AdminAuthenticatedInput
	Limit  int `query:"limit" default:"20" minimum:"1" maximum:"100"`
	Offset int `query:"offset" default:"0" minimum:"0"`
}
type AdminRevenueCatWebhooksOutput struct {
	Body []domain.RevenueCatWebhookReceipt
}

func (h *AdminHandler) ListRevenueCatWebhooks(ctx context.Context, input *AdminRevenueCatWebhooksInput) (*AdminRevenueCatWebhooksOutput, error) {
	if input.Limit < 1 || input.Limit > 100 || input.Offset < 0 {
		return nil, huma.Error400BadRequest("Choose a page size between 1 and 100.")
	}
	if h.revenueCat == nil || h.revenueCat.Admin == nil {
		return nil, huma.Error503ServiceUnavailable("Webhook receipts are unavailable.")
	}
	items, err := h.revenueCat.Admin.ListWebhooks(ctx, input.Limit, input.Offset)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("Webhook receipts could not be read.")
	}
	return &AdminRevenueCatWebhooksOutput{Body: items}, nil
}

type AdminRevenueCatOperationInput struct {
	AdminRevenueCatUserInput
	OperationID string `path:"operation_id"`
}

func (h *AdminHandler) RetryRevenueCatOperation(ctx context.Context, input *AdminRevenueCatOperationInput) (*RevenueCatWebhookOutput, error) {
	if input.UserID == "" {
		return nil, huma.Error401Unauthorized("An authenticated admin is required.")
	}
	if h.revenueCat == nil || h.revenueCat.States == nil || h.jobQueue == nil {
		return nil, huma.Error503ServiceUnavailable("Subscription recovery is unavailable.")
	}
	ops, err := h.revenueCat.States.ListOperations(ctx, input.TargetUserID)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("Grant operations could not be read.")
	}
	for _, op := range ops {
		if op.ID != input.OperationID {
			continue
		}
		if op.Stage != "pending" {
			return nil, huma.Error409Conflict("Only pending operations can be retried. Refresh the customer to reconcile completed work.")
		}
		if _, err := h.jobQueue.Enqueue(ctx, jobs.RevenueCatSyncArgs{UserID: input.TargetUserID, OperationID: op.ID}, jobs.RevenueCatSyncEnqueueOptions(true)...); err != nil {
			return nil, huma.Error503ServiceUnavailable("Recovery could not be queued. Try again shortly.")
		}
		out := &RevenueCatWebhookOutput{}
		out.Body.Status = "queued"
		return out, nil
	}
	return nil, huma.Error404NotFound("Grant operation not found for this account.")
}

type AdminRevenueCatHealthInput struct{ inputs.AdminAuthenticatedInput }
type AdminRevenueCatHealthOutput struct {
	Body AdminRevenueCatHealthOutputBody
}

type AdminRevenueCatHealthOutputBody struct {
	Available  bool                     `json:"available"`
	Configured bool                     `json:"configured"`
	Metrics    *domain.RevenueCatHealth `json:"metrics"`
}

func (AdminRevenueCatHealthOutputBody) TransformSchema(_ huma.Registry, schema *huma.Schema) *huma.Schema {
	schema.Properties["metrics"] = &huma.Schema{AnyOf: []*huma.Schema{schema.Properties["metrics"], {Type: "null", Enum: []any{nil}}}}
	return schema
}

func (h *AdminHandler) GetRevenueCatHealth(ctx context.Context, _ *AdminRevenueCatHealthInput) (*AdminRevenueCatHealthOutput, error) {
	out := &AdminRevenueCatHealthOutput{}
	out.Body.Configured = h.config != nil && h.config.RevenueCatProjectID != ""
	if h.revenueCat == nil || h.revenueCat.Admin == nil {
		return out, nil
	}
	metrics, err := h.revenueCat.Admin.Health(ctx, time.Now())
	if err != nil {
		return out, nil
	}
	out.Body.Available = true
	out.Body.Metrics = &metrics
	return out, nil
}

type AdminRevenueCatReplayInput struct {
	inputs.AdminAuthenticatedInput
	EventID string `path:"event_id"`
}

func (h *AdminHandler) ReplayRevenueCatWebhook(ctx context.Context, input *AdminRevenueCatReplayInput) (*RevenueCatWebhookOutput, error) {
	if input.UserID == "" {
		return nil, huma.Error401Unauthorized("An authenticated admin is required.")
	}
	if h.revenueCat == nil || h.revenueCat.Admin == nil || h.jobQueue == nil {
		return nil, huma.Error503ServiceUnavailable("Subscription recovery is unavailable.")
	}
	receipt, err := h.revenueCat.Admin.GetWebhook(ctx, input.EventID)
	if err != nil {
		return nil, huma.Error404NotFound("Webhook receipt not found.")
	}
	args := jobs.RevenueCatSyncArgs{UserID: receipt.UserID}
	if receipt.ProcessedAt == nil {
		args.EventID = receipt.EventID
	}
	if _, err := h.jobQueue.Enqueue(ctx, args, jobs.RevenueCatSyncEnqueueOptions(true)...); err != nil {
		return nil, huma.Error503ServiceUnavailable("Recovery could not be queued. Try again shortly.")
	}
	out := &RevenueCatWebhookOutput{}
	out.Body.Status = "queued"
	return out, nil
}

func (h *AdminHandler) WithRevenueCat(deps RevenueCatAdminDeps) *AdminHandler {
	h.revenueCat = &deps
	return h
}

type AdminRevenueCatGrantInput struct {
	inputs.AdminAuthenticatedInput
	TargetUserID string `path:"user_id"`
	Body         struct {
		Class     string     `json:"class" enum:"comp,trial"`
		Tier      string     `json:"tier" enum:"premium,household_2,household_3,household_4,household_5"`
		ExpiresAt *time.Time `json:"expires_at"`
		Revoke    bool       `json:"revoke"`
		Reason    string     `json:"reason" minLength:"1" maxLength:"1000"`
	}
}
type AdminRevenueCatGrantOutput struct{ Body domain.RevenueCatOperation }

func (h *AdminHandler) RequestRevenueCatGrant(ctx context.Context, input *AdminRevenueCatGrantInput) (*AdminRevenueCatGrantOutput, error) {
	actor := input.UserID
	if actor == "" {
		return nil, huma.Error401Unauthorized("An authenticated admin is required.")
	}
	class, tier, reason := input.Body.Class, input.Body.Tier, strings.TrimSpace(input.Body.Reason)
	_, source, _, valid := domain.RevenueCatKey(class + "_" + tier)
	if !valid || source == "paid" || reason == "" || len(reason) > 1000 || input.TargetUserID == "" {
		return nil, huma.Error400BadRequest("Choose a trial or complimentary tier and provide a reason.")
	}
	if !input.Body.Revoke && ((class == "trial" && input.Body.ExpiresAt == nil) || (input.Body.ExpiresAt != nil && !input.Body.ExpiresAt.After(time.Now()))) {
		return nil, huma.Error400BadRequest("Choose a future expiry. Trials must have an end date.")
	}
	if h.revenueCat == nil || h.revenueCat.Grants == nil || h.revenueCat.Profiles == nil {
		return nil, huma.Error503ServiceUnavailable("Subscription administration is unavailable.")
	}
	user, err := h.revenueCat.Profiles.GetUserByID(ctx, input.TargetUserID)
	if err != nil || user == nil || user.IsDeleted {
		return nil, huma.Error404NotFound("Account not found.")
	}
	if class == "trial" && !user.HadTrial {
		return nil, huma.Error409Conflict("This account has no registration trial to manage.")
	}
	op, err := h.revenueCat.Grants.RequestGrant(ctx, input.TargetUserID, class, tier, input.Body.ExpiresAt, input.Body.Revoke, actor, reason)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("The grant request could not be saved. Try again shortly.")
	}
	// The operation is already durable. A queue outage cannot erase it; the repair
	// sweep will enqueue the same operation without renewing its fixed expiry.
	if h.jobQueue != nil {
		_, _ = h.jobQueue.Enqueue(ctx, jobs.RevenueCatSyncArgs{UserID: op.UserID, OperationID: op.ID}, jobs.RevenueCatSyncEnqueueOptions(true)...)
	}
	return &AdminRevenueCatGrantOutput{Body: op}, nil
}

func RegisterAdminRevenueCatRoutes(api huma.API, handler *AdminHandler) {
	huma.Register(api, huma.Operation{OperationID: "getAdminRevenueCatUser", Method: "GET", Path: "/api/v1/admin/users/{user_id}/revenuecat", Summary: "Inspect confirmed access, billing and grant operations", Tags: []string{"Admin Subscriptions"}, Security: []map[string][]string{{"clerk": {}}}, Middlewares: huma.Middlewares{humamiddleware.NewAdminMiddleware(handler.config, handler.adminService)}}, handler.GetRevenueCatUser)
	huma.Register(api, huma.Operation{OperationID: "refreshAdminRevenueCatUser", Method: "POST", Path: "/api/v1/admin/users/{user_id}/revenuecat/refresh", Summary: "Queue a customer subscription refresh", Tags: []string{"Admin Subscriptions"}, Security: []map[string][]string{{"clerk": {}}}, Middlewares: huma.Middlewares{humamiddleware.NewAdminMiddleware(handler.config, handler.adminService)}}, handler.RefreshRevenueCatUser)
	huma.Register(api, huma.Operation{OperationID: "listAdminRevenueCatWebhooks", Method: "GET", Path: "/api/v1/admin/revenuecat/webhooks", Summary: "List authenticated webhook receipts and processing results", Tags: []string{"Admin Subscriptions"}, Security: []map[string][]string{{"clerk": {}}}, Middlewares: huma.Middlewares{humamiddleware.NewAdminMiddleware(handler.config, handler.adminService)}}, handler.ListRevenueCatWebhooks)
	huma.Register(api, huma.Operation{OperationID: "retryAdminRevenueCatOperation", Method: "POST", Path: "/api/v1/admin/users/{user_id}/revenuecat/operations/{operation_id}/retry", Summary: "Retry a pending grant operation", Tags: []string{"Admin Subscriptions"}, Security: []map[string][]string{{"clerk": {}}}, Middlewares: huma.Middlewares{humamiddleware.NewAdminMiddleware(handler.config, handler.adminService)}}, handler.RetryRevenueCatOperation)
	huma.Register(api, huma.Operation{OperationID: "getAdminRevenueCatHealth", Method: "GET", Path: "/api/v1/admin/revenuecat/health", Summary: "Get RevenueCat synchronization metrics", Tags: []string{"Admin Subscriptions"}, Security: []map[string][]string{{"clerk": {}}}, Middlewares: huma.Middlewares{humamiddleware.NewAdminMiddleware(handler.config, handler.adminService)}}, handler.GetRevenueCatHealth)
	huma.Register(api, huma.Operation{OperationID: "replayAdminRevenueCatWebhook", Method: "POST", Path: "/api/v1/admin/revenuecat/webhooks/{event_id}/replay", Summary: "Retry webhook processing or refresh a completed receipt's customer", Tags: []string{"Admin Subscriptions"}, Security: []map[string][]string{{"clerk": {}}}, Middlewares: huma.Middlewares{humamiddleware.NewAdminMiddleware(handler.config, handler.adminService)}}, handler.ReplayRevenueCatWebhook)
	huma.Register(api, huma.Operation{OperationID: "requestAdminRevenueCatGrant", Method: "POST", Path: "/api/v1/admin/users/{user_id}/revenuecat/grants", Summary: "Request a trial or complimentary entitlement change", Description: "Saves an audited, retryable operation. Pending does not mean access has been applied.", Tags: []string{"Admin Subscriptions"}, Security: []map[string][]string{{"clerk": {}}}, Middlewares: huma.Middlewares{humamiddleware.NewAdminMiddleware(handler.config, handler.adminService)}}, handler.RequestRevenueCatGrant)
}
