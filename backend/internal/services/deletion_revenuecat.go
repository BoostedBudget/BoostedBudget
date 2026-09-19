package services

import (
	"context"
	paddle "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"sort"
)

// Resolve every real Paddle record before spending any irreversible effect.
// Reuse the existing per-call authorization, operation claim and residue writes;
// users.subscription_status is now access and cannot decide cancellation.
func (s *deletionService) cleanupRevenueCatPaddleResources(ctx context.Context, id string, kind repository.DeletionClaimKind, user *domain.User, gate func(context.Context) (bool, error)) (issued paddleEffectsIssued, err error) {
	subs, err := s.revenueCatCleanupSvc.Subscriptions(ctx, id)
	if err != nil {
		return issued, err
	}
	customers := map[string]bool{}
	if user.PaddleCustomerID != nil {
		customers[*user.PaddleCustomerID] = true
	}
	for _, sub := range subs {
		if sub == nil {
			return issued, domain.ErrRevenueCatUnresolved
		}
		switch sub.Status {
		case paddle.SubscriptionStatusActive, paddle.SubscriptionStatusTrialing, paddle.SubscriptionStatusPastDue, paddle.SubscriptionStatusPaused, paddle.SubscriptionStatusCanceled:
		default:
			return issued, domain.ErrRevenueCatUnresolved
		}
		customers[sub.CustomerID] = true
	}
	if (len(subs) > 0 || len(customers) > 0) && s.paddleCleanupSvc == nil {
		return issued, domain.ErrRevenueCatUnresolved
	}
	for _, sub := range subs {
		clone := *user
		clone.PaddleSubscriptionID = &sub.ID
		clone.PaddleCustomerID = nil
		clone.SubscriptionStatus = "active"
		if sub.Status == paddle.SubscriptionStatusCanceled {
			clone.SubscriptionStatus = "cancelled"
		}
		part, e := s.cleanupPaddleResources(ctx, id, kind, &clone, gate)
		issued.subscriptionCancel = issued.subscriptionCancel || part.subscriptionCancel
		if e != nil {
			return issued, e
		}
	}
	ids := make([]string, 0, len(customers))
	for customer := range customers {
		ids = append(ids, customer)
	}
	sort.Strings(ids)
	for _, customer := range ids {
		clone := *user
		clone.PaddleSubscriptionID = nil
		clone.PaddleCustomerID = &customer
		part, e := s.cleanupPaddleResources(ctx, id, kind, &clone, gate)
		issued.customerArchive = issued.customerArchive || part.customerArchive
		if e != nil {
			return issued, e
		}
	}
	return issued, nil
}
