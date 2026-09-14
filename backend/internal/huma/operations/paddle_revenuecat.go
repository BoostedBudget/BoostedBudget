package operations

import (
	"context"
	"errors"
	"math"
	"time"

	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

type revenueCatBillingReader interface {
	Resolve(context.Context, *domain.User) (*paddle.Subscription, services.RevenueCatSummary, error)
}

func (h *PaddleCheckoutHandler) WithRevenueCatBilling(b revenueCatBillingReader) *PaddleCheckoutHandler {
	h.rcBilling = b
	return h
}
func revenueCatBillingError(err error) error {
	if errors.Is(err, services.ErrSubscriptionNotFound) {
		return huma.Error404NotFound("No paid subscription found.")
	}
	if errors.Is(err, services.ErrRevenueCatMultipleSubscriptions) {
		return huma.Error409Conflict("Multiple paid subscriptions need review. Contact support before changing billing.")
	}
	return huma.Error503ServiceUnavailable("Billing details could not be confirmed. Try again shortly.")
}
func (h *PaddleCheckoutHandler) revenueCatBillingPortal(ctx context.Context, id string) (*GetBillingPortalURLOutput, error) {
	user, err := h.profileRepo.GetUserByID(ctx, id)
	if err != nil || user == nil {
		return nil, huma.Error404NotFound("Account not found.")
	}
	sub, _, err := h.rcBilling.Resolve(ctx, user)
	if err != nil {
		return nil, revenueCatBillingError(err)
	}
	if sub.CustomerID == "" {
		return nil, revenueCatBillingError(domain.ErrRevenueCatUnresolved)
	}
	url, err := h.paddleService.GetBillingPortalURL(ctx, sub.CustomerID)
	if err != nil {
		return nil, revenueCatBillingError(err)
	}
	out := &GetBillingPortalURLOutput{}
	out.Body.PortalURL = url
	return out, nil
}
func (h *PaddleCheckoutHandler) revenueCatCurrent(ctx context.Context, id string) (*GetCurrentSubscriptionOutput, error) {
	user, err := h.profileRepo.GetUserByID(ctx, id)
	if err != nil || user == nil {
		return nil, huma.Error404NotFound("Account not found.")
	}
	sub, summary, err := h.rcBilling.Resolve(ctx, user)
	if err != nil && !errors.Is(err, services.ErrSubscriptionNotFound) {
		return nil, revenueCatBillingError(err)
	}
	out := &GetCurrentSubscriptionOutput{}
	out.Body.Authority = &summary
	out.Body.Status = "unresolved"
	out.Body.Tier = "premium"
	out.Body.Amount = "0.00"
	out.Body.Currency = "USD"
	out.Body.IsComped = summary.Comp != nil
	if summary.Comp != nil && summary.Comp.ExpiresAt != nil {
		out.Body.CompEndsAt = summary.Comp.ExpiresAt.Format(time.RFC3339)
	}
	if summary.Trial != nil && summary.Trial.ExpiresAt != nil {
		out.Body.TrialEndsAt = summary.Trial.ExpiresAt.Format(time.RFC3339)
		days := max(0, int(math.Ceil(summary.Trial.ExpiresAt.Sub(summary.AsOf).Hours()/24)))
		out.Body.TrialDaysRemaining = &days
	}
	if sub == nil {
		if summary.Own != nil {
			out.Body.Status = summary.Own.Status
			out.Body.Tier = summary.Own.Tier
		}
		return out, nil
	}
	if len(sub.Items) != 1 || h.tierConfig == nil {
		return nil, revenueCatBillingError(domain.ErrRevenueCatUnresolved)
	}
	tier, err := h.tierConfig.GetTierByPriceID(sub.Items[0].Price.ID)
	if err != nil {
		return nil, revenueCatBillingError(err)
	}
	out.Body.SubscriptionID = sub.ID
	out.Body.Status = string(sub.Status)
	out.Body.Tier = string(tier)
	out.Body.BillingInterval = string(sub.BillingCycle.Interval)
	out.Body.Amount = sub.Items[0].Price.UnitPrice.Amount
	out.Body.Currency = string(sub.CurrencyCode)
	if sub.CurrentBillingPeriod != nil {
		out.Body.CurrentPeriodEnd = sub.CurrentBillingPeriod.EndsAt
		out.Body.NextBillingDate = sub.CurrentBillingPeriod.EndsAt
	}
	if sub.ScheduledChange != nil && sub.ScheduledChange.Action == paddle.ScheduledChangeActionCancel {
		out.Body.CancelAtPeriodEnd = true
		out.Body.ScheduledChangeAt = sub.ScheduledChange.EffectiveAt
	}
	return out, nil
}
