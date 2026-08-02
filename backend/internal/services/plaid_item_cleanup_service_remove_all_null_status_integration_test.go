package services

// plaid_item_cleanup_service_remove_all_null_status_integration_test.go — Issue #1413 round-10
// (closing most of #1444): RemoveAllUserItems (the hard-deletion path — DeletionService.
// ExecuteDeletion calls this before DeleteUserCascade) fetches items via
// LinkedItemRepository.GetByUserID, which — unlike GetActiveItemsByUserID/GetByID (fixed in
// round-8) — had NO status predicate at all and scanned status directly into a bare string,
// panicking outright ("cannot scan NULL into *string") on a genuinely NULL-status row, instead of
// merely misclassifying it. This is the ONE NULL-status scan site round-10's brief calls
// "reachable": a user whose hard deletion is in flight, with a linked_items row predating
// migration 000013's 'active' default, would have crashed the deletion job instead of cleaning it
// up.
//
// This test runs the REAL LinkedItemRepository (real GetByUserID fetch, real
// MarkItemsDisconnected write) and the REAL PlaidItemCleanupService — only the Plaid HTTP
// collaborator is faked — and asserts a NULL-status item survives RemoveAllUserItems end to end:
// no panic, the fake Plaid collaborator is actually invoked, and the item is actually marked
// 'disconnected' afterward.
//
// No build tag — mirrors plaid_item_cleanup_service_null_status_integration_test.go's convention:
// skips at runtime if DATABASE_URL is unset.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestPlaidCleanupService_Integration_NullStatus_RemoveAllUserItems -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlaidCleanupService_Integration_NullStatus_RemoveAllUserItems_ActuallyDisconnectedAndMarked
// is the round-10 RED test for the reachable NULL-status scan site (LinkedItemRepository.
// GetByUserID).
//
// Falsifiability: reverting GetByUserID to scan `status` directly into `&item.Status` (a bare
// string) makes this test panic instead of merely failing — "cannot scan NULL into *string" —
// which is precisely why this is the highest-severity of the seven sites the brief calls out.
func TestPlaidCleanupService_Integration_NullStatus_RemoveAllUserItems_ActuallyDisconnectedAndMarked(t *testing.T) {
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)
	profileRepo := repository.NewProfileRepository(pool, enc)
	linkedItemRepo := repository.NewLinkedItemRepository(pool, enc)
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_nullstatusrmall_" + suffix,
		Email:              "nullstatusrmall-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))
	require.NoError(t, tx.Commit(ctx))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE user_id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM work_claims WHERE resource_type = $1 AND resource_id = $2", repository.ResourceTypeUser, user.ID)
	})

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-nullstatus-rmall-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	// Force NULL status directly — simulates a row that predates migration 000013's 'active'
	// default (linked_items.status is nullable; no NOT NULL constraint exists on it).
	_, err = pool.Exec(ctx, `UPDATE linked_items SET status = NULL WHERE id = $1`, item.ID)
	require.NoError(t, err)

	fakePlaid := &countingPlaidCollaborator{}
	svc := NewPlaidItemCleanupService(linkedItemRepo, fakePlaid)
	svc.SetWorkClaimsRepo(claimsRepo)

	// This is the hard-deletion path's call (DeletionService.ExecuteDeletion, before
	// DeleteUserCascade) — must not panic and must actually disconnect the NULL-status item.
	err = svc.RemoveAllUserItems(ctx, user.ID)
	require.NoError(t, err)

	assert.Equal(t, 1, fakePlaid.callCount(),
		"the NULL-status item must actually be fetched (via GetByUserID) and handed to Plaid for "+
			"removal — before the fix, GetByUserID scanning status directly into a bare string "+
			"panicked outright on this row")

	got, err := linkedItemRepo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, string(domain.LinkedItemStatusDisconnected), string(got.Status),
		"the NULL-status item must be marked 'disconnected' after a successful hard-deletion cleanup")
}
