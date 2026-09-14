package services

import (
	"context"
	"errors"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// SubscriptionCleanupGuard authorizes only subscription-driven retention work.
// Explicit GDPR erasure must not depend on an active subscription or provider availability.
type SubscriptionCleanupGuard interface {
	CanCleanup(context.Context, string) (bool, error)
}
type cleanupEntitlementReader interface {
	GetActiveEntitlement(context.Context, string, time.Time) (*domain.SubscriptionEntitlement, error)
}
type RevenueCatCleanupGuard struct {
	own          OwnAccessResolver
	states       revenueCatStateReader
	entitlements cleanupEntitlementReader
}

func NewRevenueCatCleanupGuard(own OwnAccessResolver, states revenueCatStateReader, entitlements cleanupEntitlementReader) *RevenueCatCleanupGuard {
	return &RevenueCatCleanupGuard{own: own, states: states, entitlements: entitlements}
}
func (g *RevenueCatCleanupGuard) CanCleanup(ctx context.Context, id string) (bool, error) {
	if g == nil || g.own == nil || g.states == nil || g.entitlements == nil {
		return false, domain.ErrRevenueCatUnresolved
	}
	ids := []string{id}
	ent, err := g.entitlements.GetActiveEntitlement(ctx, id, time.Now())
	if err != nil && !errors.Is(err, repository.ErrEntitlementNotFound) {
		return false, err
	}
	if err == nil && ent == nil {
		return false, domain.ErrRevenueCatUnresolved
	}
	if err == nil && ent != nil && ent.Source == domain.EntitlementSourceHousehold {
		if ent.PayerUserID == nil || ent.HouseholdID == nil {
			return false, domain.ErrRevenueCatUnresolved
		}
		if *ent.PayerUserID != id {
			ids = append(ids, *ent.PayerUserID)
		}
	}
	// Refresh before the final snapshot pass. Every snapshot is checked at the end,
	// so a slow second customer cannot age the first customer's confirmation out.
	for _, customer := range ids {
		if _, err = g.own.ResolveOwnAccessAt(ctx, customer, time.Now()); err != nil {
			return false, err
		}
	}
	states := make([]domain.RevenueCatState, 0, len(ids))
	for _, customer := range ids {
		state, e := g.states.Read(ctx, customer)
		if e != nil {
			return false, e
		}
		states = append(states, state)
	}
	// A membership may change during the provider reads. Do not act on the old
	// coverage graph; the next attempt will resolve the new payer from scratch.
	current, currentErr := g.entitlements.GetActiveEntitlement(ctx, id, time.Now())
	if currentErr != nil && !errors.Is(currentErr, repository.ErrEntitlementNotFound) {
		return false, currentErr
	}
	if currentErr == nil && current == nil {
		return false, domain.ErrRevenueCatUnresolved
	}
	if cleanupCoverageKey(ent) != cleanupCoverageKey(current) {
		return false, nil
	}
	now := time.Now()
	for _, state := range states {
		if !state.SafeForCleanup(now) {
			return false, nil
		}
	}
	return true, nil
}

func cleanupCoverageKey(ent *domain.SubscriptionEntitlement) string {
	if ent == nil || ent.Source != domain.EntitlementSourceHousehold {
		return "own"
	}
	if ent.PayerUserID == nil || ent.HouseholdID == nil {
		return "malformed"
	}
	return *ent.PayerUserID + ":" + *ent.HouseholdID
}
