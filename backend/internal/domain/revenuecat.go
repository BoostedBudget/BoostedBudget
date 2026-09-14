package domain

import (
	"errors"
	"strings"
	"time"
)

const RevenueCatMaxAge = 15 * time.Minute

var ErrRevenueCatUnresolved = errors.New("subscription authority temporarily unavailable")
var ErrRevenueCatErasureStarted = errors.New("RevenueCat erasure has started; operator reconciliation required")

type RevenueCatGrant struct {
	Key       string     `json:"key"`
	ExpiresAt *time.Time `json:"expires_at"`
}
type RevenueCatBilling struct {
	ID                          string     `json:"id"`
	ProductID                   string     `json:"product_id"`
	StoreSubscriptionIdentifier string     `json:"store_subscription_identifier"`
	Status                      string     `json:"status"`
	Store                       string     `json:"store"`
	Environment                 string     `json:"environment"`
	AutoRenewalStatus           string     `json:"auto_renewal_status"`
	CurrentPeriodEndsAt         *time.Time `json:"current_period_ends_at"`
	EndsAt                      *time.Time `json:"ends_at"`
}
type RevenueCatState struct {
	ErasureStartedAt   *time.Time          `json:"erasure_started_at,omitempty"`
	ErasureConfirmedAt *time.Time          `json:"erasure_confirmed_at,omitempty"`
	UserID             string              `json:"user_id"`
	Grants             []RevenueCatGrant   `json:"grants"`
	AccessUpdatedAt    time.Time           `json:"access_updated_at"`
	Billing            []RevenueCatBilling `json:"billing"`
	BillingUpdatedAt   *time.Time          `json:"billing_updated_at"`
	AccessError        string              `json:"access_error,omitempty"`
	BillingError       string              `json:"billing_error,omitempty"`
	PendingOperations  int                 `json:"pending_operations"`
	// Preserve unreadable ciphertext until an independent billing fetch succeeds.
	PreserveBilling bool `json:"-"`
}
type RevenueCatAccess struct {
	Tier      string     `json:"tier"`
	Status    string     `json:"status"`
	Source    string     `json:"source"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func RevenueCatKey(key string) (tier, source string, rank int, ok bool) {
	source, tier, ok = strings.Cut(key, "_")
	if !ok || (source != "paid" && source != "comp" && source != "trial") {
		return "", "", 0, false
	}
	switch tier {
	case "premium":
		rank = 1
	case "household_2":
		rank = 2
	case "household_3":
		rank = 3
	case "household_4":
		rank = 4
	case "household_5":
		rank = 5
	default:
		return "", "", 0, false
	}
	if source == "trial" && tier != "premium" {
		return "", "", 0, false
	}
	return tier, source, rank, true
}

// ResolveAccess never derives access from an unconfirmed grant intent or billing status.
// An expiry crossed since observation requires refresh, even if another grant survives.
func (s RevenueCatState) ResolveAccess(now time.Time) (RevenueCatAccess, error) {
	if s.AccessUpdatedAt.IsZero() || s.AccessUpdatedAt.After(now) || !now.Before(s.AccessUpdatedAt.Add(RevenueCatMaxAge)) {
		return RevenueCatAccess{}, ErrRevenueCatUnresolved
	}
	result := RevenueCatAccess{Tier: "premium", Status: "inactive", Source: "none"}
	best := 0
	for _, g := range s.Grants {
		tier, source, rank, ok := RevenueCatKey(g.Key)
		if !ok || (g.ExpiresAt != nil && !now.Before(*g.ExpiresAt)) {
			return RevenueCatAccess{}, ErrRevenueCatUnresolved
		}
		score := rank * 10
		switch source {
		case "trial":
			score = 1
		case "paid":
			score++
		}
		if score > best {
			best = score
			result = RevenueCatAccess{Tier: tier, Status: "active", Source: source, ExpiresAt: g.ExpiresAt}
			if source == "trial" {
				result.Status = "trial"
			}
		}
	}
	return result, nil
}
func (s RevenueCatState) SafeForCleanup(now time.Time) bool {
	a, e := s.ResolveAccess(now)
	return e == nil && a.Status == "inactive" && s.PendingOperations == 0
}

type RevenueCatOperation struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Class     string     `json:"class"`
	Key       string     `json:"key"`
	ExpiresAt *time.Time `json:"expires_at"`
	Revoke    bool       `json:"revoke"`
	Version   int64      `json:"version"`
	ActorID   *string    `json:"actor_id"`
	Reason    string     `json:"reason"`
	Stage     string     `json:"stage"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"last_error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}
