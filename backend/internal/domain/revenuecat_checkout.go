package domain

import (
	"errors"
	"time"
)

var ErrRevenueCatExistingSubscription = errors.New("manage the existing paid subscription instead of starting another")
var ErrRevenueCatCheckoutInProgress = errors.New("a checkout is already in progress for this account")

type RevenueCatCheckoutIntent struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expires_at"`
	Offering  string    `json:"offering"`
}

// CanStartCheckout is duplicate-purchase admission, never an access grant. Even
// non-access-granting billing retry/paused/incomplete states require self-service
// on the existing subscription. Unknown future statuses conservatively block.
func (s RevenueCatState) CanStartCheckout(now time.Time) error {
	if s.ErasureStartedAt != nil {
		return ErrRevenueCatErasureStarted
	}
	if _, err := s.ResolveAccess(now); err != nil {
		return err
	}
	if s.BillingError != "" || s.BillingUpdatedAt == nil || s.BillingUpdatedAt.After(now) || !now.Before(s.BillingUpdatedAt.Add(RevenueCatMaxAge)) {
		return ErrRevenueCatUnresolved
	}
	for _, g := range s.Grants {
		_, source, _, _ := RevenueCatKey(g.Key)
		if source == "paid" {
			return ErrRevenueCatExistingSubscription
		}
	}
	for _, b := range s.Billing {
		if b.Store != "promotional" && b.Status != "expired" {
			return ErrRevenueCatExistingSubscription
		}
	}
	return nil
}
