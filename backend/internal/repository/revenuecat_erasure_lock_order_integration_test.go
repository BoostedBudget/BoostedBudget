//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/disposabledb"
	"github.com/stretchr/testify/require"
)

func TestRevenueCatExplicitErasureOrdersCustomerBeforeUserDuringRefresh(t *testing.T) {
	if raw := os.Getenv("DATABASE_URL"); raw != "" {
		_, err := disposabledb.ParseURL(raw)
		require.NoError(t, err)
	}
	pool := connectTestDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)
	profile := NewProfileRepository(pool, enc)
	profile.EnableRevenueCatRetentionGuard()
	requested := time.Now().Add(-31 * 24 * time.Hour).Truncate(time.Microsecond)
	user := createDeleteCascadeFixtureUser(t, ctx, pool, profile, fmt.Sprintf("rc-lock-%d", time.Now().UnixNano()), &requested, nil, nil)
	rc := NewRevenueCatRepository(pool, enc)
	state := domain.RevenueCatState{AccessUpdatedAt: time.Now(), Grants: []domain.RevenueCatGrant{{Key: "comp_premium"}}}
	require.NoError(t, rc.WithCustomer(ctx, user.ID, func(s RevenueCatSession) error { return s.Save(ctx, state) }))
	_, err := pool.Exec(ctx, `UPDATE revenuecat_customers SET erasure_started_at=clock_timestamp(),erasure_confirmed_at=clock_timestamp() WHERE user_id=$1`, user.ID)
	require.NoError(t, err)

	// Hold exactly the first lock used by a fenced readback refresh. The barrier
	// below proves the real cascade is waiting for this customer, without sleeps.
	refresh, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = refresh.Rollback(ctx) }()
	var locked string
	require.NoError(t, refresh.QueryRow(ctx, `SELECT user_id FROM revenuecat_customers WHERE user_id=$1 FOR UPDATE`, user.ID).Scan(&locked))
	var pid int32
	require.NoError(t, refresh.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	type result struct {
		deleted bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		deleted, _, e := profile.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
		done <- result{deleted, e}
	}()
	waitUntilBlockedBy(t, pool, pid, 5*time.Second)

	// Execute the real refresh projection write. Before the fix, the cascade
	// already owns users and this Save waits for it while owning its RC customer.
	bounded, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	saveErr := (&revenueCatSession{q: refresh, userID: user.ID, enc: enc}).Save(bounded, state)
	if saveErr == nil {
		require.NoError(t, refresh.Commit(ctx))
	} else {
		_ = refresh.Rollback(ctx)
	}
	select {
	case got := <-done:
		require.NoError(t, saveErr, "refresh must obtain users while the cascade waits for the customer")
		require.NoError(t, got.err)
		require.True(t, got.deleted, "explicit GDPR remains authorized despite active comp access")
	case <-time.After(5 * time.Second):
		t.Fatal("cascade did not finish after releasing the refresh transaction")
	}
}
