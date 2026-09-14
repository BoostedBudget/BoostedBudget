package services

import (
	"context"
	"encoding/json"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"time"
)

func (s *householdService) revenueCatEnabled() bool {
	return s.entitlementSvc != nil && s.entitlementSvc.ownAccessResolver != nil
}

// The caller holds the user's row lock. Do not refresh over the network here:
// refresh materializes that same row in another transaction. Read the committed
// projection on this transaction and fail closed if repair is needed instead.
func (s *householdService) ownAccessInTransaction(ctx context.Context, q repository.Querier, user *domain.User) (domain.RevenueCatAccess, error) {
	if !s.revenueCatEnabled() {
		return domain.RevenueCatAccess{Tier: s.effectiveTierName(user), Status: user.ComputeEffectiveStatus()}, nil
	}
	var grants []byte
	state := domain.RevenueCatState{UserID: user.ID}
	var updated *time.Time
	if err := q.QueryRow(ctx, `SELECT grants, access_updated_at,
 (SELECT count(*) FROM revenuecat_operations WHERE user_id=$1 AND stage='pending')
 FROM revenuecat_customers WHERE user_id=$1`, user.ID).Scan(&grants, &updated, &state.PendingOperations); err != nil {
		return domain.RevenueCatAccess{}, domain.ErrRevenueCatUnresolved
	}
	if updated != nil {
		state.AccessUpdatedAt = *updated
	}
	if json.Unmarshal(grants, &state.Grants) != nil || state.PendingOperations > 0 {
		return domain.RevenueCatAccess{}, domain.ErrRevenueCatUnresolved
	}
	return state.ResolveAccess(time.Now())
}

// Paid admission uses the independent billing projection, never the effective
// access status (a comp and a paid subscription can coexist).
func (s *householdService) callerHasLiveBillingInTransaction(ctx context.Context, q repository.Querier, user *domain.User) (bool, error) {
	if !s.revenueCatEnabled() {
		return callerHasLivePaddleSubscription(user), nil
	}
	var ciphertext, billingError string
	var updated *time.Time
	if err := q.QueryRow(ctx, `SELECT billing, billing_updated_at, billing_error FROM revenuecat_customers WHERE user_id=$1`, user.ID).Scan(&ciphertext, &updated, &billingError); err != nil {
		return false, domain.ErrRevenueCatUnresolved
	}
	now := time.Now()
	if updated == nil || updated.After(now) || !now.Before(updated.Add(domain.RevenueCatMaxAge)) || billingError != "" || s.enc == nil {
		return false, domain.ErrRevenueCatUnresolved
	}
	plain, err := s.enc.DecryptString(ciphertext)
	if err != nil {
		return false, domain.ErrRevenueCatUnresolved
	}
	var billing []domain.RevenueCatBilling
	if json.Unmarshal([]byte(plain), &billing) != nil {
		return false, domain.ErrRevenueCatUnresolved
	}
	for _, sub := range billing {
		if sub.Store == "promotional" {
			continue
		}
		switch sub.Status {
		case "active", "trialing", "in_grace_period", "in_billing_retry", "paused":
			return true, nil
		case "expired", "canceled", "cancelled":
		default:
			return false, domain.ErrRevenueCatUnresolved
		}
	}
	return false, nil
}
