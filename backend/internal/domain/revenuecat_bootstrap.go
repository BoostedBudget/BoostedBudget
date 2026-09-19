package domain

import (
	"errors"
	"time"
)

// PlanRevenueCatBootstrap carries forward only explicit legacy grant facts.
// It never starts a trial or treats a local active status as proof of payment.
// Any Paddle relationship requires manual inventory before this prelaunch path.
func PlanRevenueCatBootstrap(user *User, now time.Time) ([]RevenueCatOperation, error) {
	if user == nil || user.IsDeleted || user.DeletionRequestedAt != nil {
		return nil, nil
	}
	if (user.PaddleSubscriptionID != nil && *user.PaddleSubscriptionID != "") || (user.PaddleCustomerID != nil && *user.PaddleCustomerID != "") {
		return nil, errors.New("existing Paddle relationship requires manual inventory; prelaunch bootstrap refused")
	}
	if user.SubscriptionStatus == "trial" && user.TrialEndsAt == nil {
		return nil, errors.New("legacy trial has no fixed expiry; bootstrap refused")
	}
	ops := []RevenueCatOperation{}
	add := func(class, key string, end *time.Time) {
		ops = append(ops, RevenueCatOperation{UserID: user.ID, Class: class, Key: key, ExpiresAt: end, Reason: "prelaunch legacy grant bootstrap", CreatedAt: now})
	}
	if user.TrialEndsAt != nil && now.Before(*user.TrialEndsAt) {
		add("trial", "trial_premium", user.TrialEndsAt)
	}
	if user.ManualTierOverride != nil && *user.ManualTierOverride != "" {
		key := "comp_" + *user.ManualTierOverride
		if _, _, _, ok := RevenueCatKey(key); !ok {
			return nil, errors.New("legacy comp tier is invalid; bootstrap refused")
		}
		if user.ManualTierEndsAt == nil || now.Before(*user.ManualTierEndsAt) {
			add("comp", key, user.ManualTierEndsAt)
		}
	}
	return ops, nil
}
