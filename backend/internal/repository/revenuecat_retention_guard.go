package repository

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// EnableRevenueCatRetentionGuard is wired only after the authority schema exists.
// Explicit user-requested erasure never depends on RevenueCat availability.
func (r *profileRepository) EnableRevenueCatRetentionGuard() { r.revenueCatRetentionGuard = true }

// Lock order is household -> sorted provider customers -> user. Provider refresh
// and grant requests lock those same customer rows, fencing both confirmed access
// changes and unconfirmed intents through the terminal cascade transaction.
func (r *profileRepository) lockRevenueCatRetentionEligibility(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT h.id FROM households h WHERE h.owner_user_id=$1 OR EXISTS (
 SELECT 1 FROM household_members m WHERE m.household_id=h.id AND m.user_id=$1 AND m.status='active'
 ) ORDER BY h.id FOR NO KEY UPDATE`, id)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var household string
		if err = rows.Scan(&household); err != nil {
			rows.Close()
			return false, err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	entitlements := NewEntitlementRepository(tx)
	ent, err := entitlements.GetActiveEntitlement(ctx, id, time.Now())
	if err != nil && !errors.Is(err, ErrEntitlementNotFound) {
		return false, err
	}
	ids := []string{id}
	if ent != nil && ent.Source == domain.EntitlementSourceHousehold {
		if ent.PayerUserID == nil || ent.HouseholdID == nil {
			return false, domain.ErrRevenueCatUnresolved
		}
		if *ent.PayerUserID != id {
			ids = append(ids, *ent.PayerUserID)
		}
	}
	sort.Strings(ids)
	for _, customer := range ids {
		var locked string
		err = tx.QueryRow(ctx, `SELECT user_id FROM revenuecat_customers WHERE user_id=$1 FOR UPDATE`, customer).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&locked); err != nil {
		return false, err
	}
	// The user lock now excludes concurrent invite acceptance. A join committed
	// while we acquired locks is not in the original payer set: defer, never delete.
	current, currentErr := entitlements.GetActiveEntitlement(ctx, id, time.Now())
	if currentErr != nil && !errors.Is(currentErr, ErrEntitlementNotFound) {
		return false, currentErr
	}
	if !sameRevenueCatCoverage(ent, current) {
		return false, nil
	}
	states := make([]domain.RevenueCatState, 0, len(ids))
	for _, customer := range ids {
		state, e := (&revenueCatSession{q: tx, userID: customer, enc: r.enc}).Read(ctx)
		if e != nil {
			return false, e
		}
		states = append(states, state)
	}
	now := time.Now()
	for _, state := range states {
		if !state.SafeForCleanup(now) {
			return false, nil
		}
	}
	return true, nil
}
func sameRevenueCatCoverage(a, b *domain.SubscriptionEntitlement) bool {
	household := func(e *domain.SubscriptionEntitlement) bool {
		return e != nil && e.Source == domain.EntitlementSourceHousehold
	}
	if !household(a) || !household(b) {
		return !household(a) && !household(b)
	}
	return a.PayerUserID != nil && b.PayerUserID != nil && a.HouseholdID != nil && b.HouseholdID != nil && *a.PayerUserID == *b.PayerUserID && *a.HouseholdID == *b.HouseholdID
}
