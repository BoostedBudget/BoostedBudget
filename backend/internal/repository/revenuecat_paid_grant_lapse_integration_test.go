//go:build integration

package repository

// #1652 todo 6 — NAMED BEHAVIOR CHANGE.
//
// Deleting the past-due worker/service dropped two behaviors, both intentionally accepted:
//
//  1. the 21-day auto-cancel of unpaid Paddle subscriptions — Paddle's own dunning owns
//     cancellation, and RevenueCat cannot originate Paddle cancels (the approved constraint); and
//  2. the 14-day past-due Plaid disconnect (past_due_check.go:145-146) — the Plaid disconnect now
//     fires only when RC access resolves inactive via the retention path, so a dunning-but-still-
//     active user keeps their bank link longer (a privacy/retention-relevant change).
//
// This test pins behavior (2) at the repository boundary. It is deliberately NOT framed as
// "past-due": RC emits only active/trial/inactive (domain/revenuecat.go:81-103); the trigger is the
// paid grant's ExpiresAt lapse. While the paid grant is still valid the user stays active and no
// plaid_cleanup_pending_at marker is armed (the link is kept); once the grant lapses and the fresh
// provider read drops it (the todo-3 fix in applyRevenueCatGrants), the SAME Save path projects
// inactive and arms the marker + retention clock.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test -tags integration -p 1 ./internal/repository/... \
//	  -run 'RevenueCatPaidGrantLapse' -v

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestRevenueCatPaidGrantLapse_KeepsLinkWhileActiveThenArmsRetention(t *testing.T) {
	pool, id := revenueCatTestDB(t)
	ctx := context.Background()
	repo := NewRevenueCatRepository(pool, newTestFieldEncryptor(t))

	_, err := pool.Exec(ctx, `UPDATE users SET subscription_status='active' WHERE id=$1`, id)
	require.NoError(t, err)

	var status string
	var marker, start *time.Time
	read := func() {
		require.NoError(t, pool.QueryRow(ctx, `SELECT subscription_status,plaid_cleanup_pending_at,retention_started_at FROM users WHERE id=$1`, id).Scan(&status, &marker, &start))
	}
	save := func(state domain.RevenueCatState) {
		require.NoError(t, repo.WithCustomer(ctx, id, func(s RevenueCatSession) error { return s.Save(ctx, state) }))
	}

	// (1) A live paid grant: the user is active and NO Plaid-cleanup marker is armed. This is the
	// "dunning-but-still-active" state the deleted 14-day past-due disconnect used to act on; under
	// RC authority the link is kept.
	future := time.Now().Add(30 * 24 * time.Hour)
	save(domain.RevenueCatState{UserID: id, AccessUpdatedAt: time.Now(), Grants: []domain.RevenueCatGrant{{Key: "paid_premium", ExpiresAt: &future}}})
	read()
	require.Equal(t, "active", status, "a live paid grant must keep the user active")
	require.Nil(t, marker, "a live paid grant must NOT arm the Plaid-cleanup marker — the link is kept")
	require.Nil(t, start, "a live paid grant must NOT start a retention episode")

	// (2) The grant's ExpiresAt lapses. The fresh provider read drops the expired grant (todo 3's
	// applyRevenueCatGrants fix), so Save receives an empty grant set and projects inactive.
	save(domain.RevenueCatState{UserID: id, AccessUpdatedAt: time.Now(), Grants: nil})
	read()
	require.Equal(t, "inactive", status, "a lapsed paid grant must project inactive")
	require.NotNil(t, marker, "a lapsed paid grant must arm the Plaid-cleanup marker via the retention path")
	require.NotNil(t, start, "a lapsed paid grant must start the retention episode")
}
