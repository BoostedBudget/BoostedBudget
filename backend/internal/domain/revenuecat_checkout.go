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
	Package   string    `json:"package"` // RevenueCat package lookup key, e.g. "premium_annual"
}

// RevenueCatPackageKey maps a tier and Paddle-style billing interval ("month"|"year")
// to the RevenueCat package lookup key. Returns ok=false for an unknown tier or interval.
func RevenueCatPackageKey(tier, billingInterval string) (string, bool) {
	var suffix string
	switch billingInterval {
	case "month":
		suffix = "monthly"
	case "year":
		suffix = "annual"
	default:
		return "", false
	}

	switch TierName(tier) {
	case TierPremium, TierHousehold2, TierHousehold3, TierHousehold4, TierHousehold5:
		return tier + "_" + suffix, true
	default:
		return "", false
	}
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
