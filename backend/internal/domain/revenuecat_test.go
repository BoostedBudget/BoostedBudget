package domain

import (
	"errors"
	"testing"
	"time"
)

func TestRevenueCatAccessAuthority(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	expired := now
	for _, tc := range []struct {
		name         string
		grants       []RevenueCatGrant
		updated      time.Time
		tier, source string
		unresolved   bool
	}{
		{"paid wins tie", []RevenueCatGrant{{Key: "comp_household_3"}, {Key: "paid_household_3"}, {Key: "trial_premium"}}, now, "household_3", "paid", false},
		{"higher comp additive", []RevenueCatGrant{{Key: "paid_premium"}, {Key: "comp_household_5"}}, now, "household_5", "comp", false},
		{"trial fallback", []RevenueCatGrant{{Key: "trial_premium"}}, now, "premium", "trial", false},
		{"empty confirmed inactive", nil, now, "premium", "none", false},
		{"expired snapshot must refresh", []RevenueCatGrant{{Key: "paid_premium", ExpiresAt: &expired}}, now, "", "", true},
		{"unknown key fails closed", []RevenueCatGrant{{Key: "paid_enterprise"}}, now, "", "", true},
		{"missing snapshot", nil, time.Time{}, "", "", true},
		{"freshness exclusive", []RevenueCatGrant{{Key: "paid_premium"}}, now.Add(-15 * time.Minute), "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (RevenueCatState{Grants: tc.grants, AccessUpdatedAt: tc.updated}).ResolveAccess(now)
			if tc.unresolved {
				if !errors.Is(err, ErrRevenueCatUnresolved) {
					t.Fatalf("expected unresolved, got %v", err)
				}
				return
			}
			if err != nil || got.Tier != tc.tier || got.Source != tc.source {
				t.Fatalf("got %+v %v", got, err)
			}
		})
	}
}

func TestRevenueCatPendingIntentNeverGrantsAccess(t *testing.T) {
	now := time.Now()
	s := RevenueCatState{AccessUpdatedAt: now, PendingOperations: 1}
	got, err := s.ResolveAccess(now)
	if err != nil || got.Status != "inactive" || s.SafeForCleanup(now) {
		t.Fatalf("intent granted access or allowed cleanup: %+v %v", got, err)
	}
}
