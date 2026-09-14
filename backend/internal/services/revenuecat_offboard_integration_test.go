//go:build integration

package services

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/disposabledb"
	"github.com/stretchr/testify/require"
)

func TestRevenueCatAutomaticOffboardFencesGrantUntilCommit(t *testing.T) {
	if raw := os.Getenv("DATABASE_URL"); raw != "" {
		_, err := disposabledb.ParseURL(raw)
		require.NoError(t, err)
	}
	f := setupHouseholdServiceIntegration(t)
	ctx := context.Background()
	owner := insertIntegrationTestUser(t, f.profileRepo, "rc-owner-"+t.Name()+"@example.com", "household_2", "active")
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, owner.ID) })
	hh, err := f.svc.CreateHousehold(ctx, owner.ID, "RevenueCat fencing fixture")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM households WHERE id=$1`, hh.ID) })
	_, err = f.pool.Exec(ctx, `INSERT INTO revenuecat_customers(user_id,grants,access_updated_at) VALUES($1,'[{"key":"paid_household_2"}]',clock_timestamp())`, owner.ID)
	require.NoError(t, err)
	f.svc.entitlementSvc.SetOwnAccessResolver(&cleanupAccess{})
	anchor := time.Now().Add(-15 * 24 * time.Hour).Truncate(time.Microsecond)
	_, err = repository.NewHouseholdRepository(f.pool).SetOverCapacity(ctx, hh.ID, anchor)
	require.NoError(t, err)
	rc := repository.NewRevenueCatRepository(f.pool, newTestFieldEncryptor(t))
	var grantErr error
	f.svc.offboardExcessMembersAfterOwnerLockHook = func() {
		c, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		grantErr = rc.RequestOperation(c, &domain.RevenueCatOperation{UserID: owner.ID, Class: "comp", Key: "comp_household_5", Reason: "race fixture"})
	}
	_, err = f.svc.OffboardExcessMembers(ctx, hh.ID, anchor)
	require.ErrorIs(t, grantErr, context.DeadlineExceeded, "grant cannot commit after the final automatic-removal decision")
	require.NoError(t, err)
	require.NoError(t, rc.RequestOperation(ctx, &domain.RevenueCatOperation{UserID: owner.ID, Class: "comp", Key: "comp_household_5", Reason: "after commit"}))
}
