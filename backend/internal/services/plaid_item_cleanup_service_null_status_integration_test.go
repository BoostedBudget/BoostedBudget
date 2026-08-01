package services

// plaid_item_cleanup_service_null_status_integration_test.go — Issue #1413 round-8 Blocker 1 /
// Medium 5: proves the repair pass's ACTUAL disconnect mechanism — not just candidate selection —
// handles a NULL-status linked item end to end.
//
// profile_repository_plaid_disconnect_repair_integration_test.go's
// TestPlaidDisconnectRepair_Integration_NullStatusItemIsLive already proved
// GetUsersNeedingPlaidDisconnectRepair SELECTS a user with a NULL-status item as a repair
// candidate (the round-7 fix). But that test never runs the cleanup service, which is exactly why
// round 8 found the bug survived: PlaidItemCleanupService.DisconnectAllUserItems/
// DisconnectAllUserItemsIfEligible fetch the items to remove via
// LinkedItemRepository.GetActiveItemsByUserID, whose SQL used a BARE `status NOT IN (...)`
// predicate — SQL's three-valued logic evaluates that to UNKNOWN (not TRUE) for a NULL status, so
// the row was silently excluded. The marker write (MarkItemsDisconnected) had the same bare
// predicate. Net effect before the fix: the repair query selected the user, the cleanup service
// then fetched ZERO items, logged "no linked items to clean up", and returned success — the item
// leaked forever and the user was re-selected every single day with no effect.
//
// This test runs the REAL LinkedItemRepository (real GetActiveItemsByUserID fetch, real
// MarkItemsDisconnected write) and the REAL PlaidItemCleanupService — only the Plaid HTTP
// collaborator is faked — and asserts the NULL-status item is actually handed to "Plaid" and
// actually marked 'disconnected' in the database afterward. This is the test that proves Blocker 1
// fixed; the candidate-selection test alone cannot.
//
// No build tag — mirrors plaid_item_cleanup_service_side_effect_claim_integration_test.go's
// convention: skips at runtime if DATABASE_URL is unset.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestPlaidCleanupService_Integration_NullStatus -v

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

// TestPlaidCleanupService_Integration_NullStatusItem_ActuallyDisconnectedAndMarked is the
// end-to-end RED test for Blocker 1.
//
// Falsifiability: reverting GetActiveItemsByUserID/MarkItemsDisconnected to a bare
// `status NOT IN ('disconnected', 'replaced')` predicate makes this test fail — the fake Plaid
// collaborator is never called (0, not 1) and the item's status stays NULL, not 'disconnected'.
//
// Since migration 000124 (Issue #1444), linked_items.status is NOT NULL — a genuinely NULL status
// can no longer be constructed against the real schema at all, in any migrated database. This test
// therefore runs its own transaction that transactionally DROPs the NOT NULL constraint first
// (rolled back, never committed, at the very end — the real schema is never permanently altered),
// with every repository constructed against that same transaction so the service under test sees
// the uncommitted NULL-status row.
func TestPlaidCleanupService_Integration_NullStatusItem_ActuallyDisconnectedAndMarked(t *testing.T) {
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx, "ALTER TABLE linked_items ALTER COLUMN status DROP NOT NULL")
	require.NoError(t, err)

	profileRepo := repository.NewProfileRepository(tx, enc)
	linkedItemRepo := repository.NewLinkedItemRepository(tx, enc)
	claimsRepo := repository.NewWorkClaimsRepository(tx)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_nullstatussvc_" + suffix,
		Email:              "nullstatussvc-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUserTx(ctx, tx, user))

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-nullstatus-" + suffix,
		AccessToken: "access-token-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	// Force NULL status directly — simulates a row that predates migration 000013's 'active'
	// default (linked_items.status was nullable prior to migration 000124/Issue #1444; the NOT
	// NULL constraint was transactionally dropped above so this remains possible within this
	// test's own transaction).
	_, err = tx.Exec(ctx, `UPDATE linked_items SET status = NULL WHERE id = $1`, item.ID)
	require.NoError(t, err)

	fakePlaid := &countingPlaidCollaborator{}
	svc := NewPlaidItemCleanupService(linkedItemRepo, fakePlaid)
	svc.SetWorkClaimsRepo(claimsRepo)

	err = svc.DisconnectAllUserItems(ctx, user.ID)
	require.NoError(t, err)

	assert.Equal(t, 1, fakePlaid.callCount(),
		"the NULL-status item must actually be fetched and handed to Plaid for removal — before the "+
			"fix, GetActiveItemsByUserID's bare `status NOT IN (...)` silently excluded it (SQL "+
			"three-valued logic: NULL NOT IN (...) is UNKNOWN, not TRUE) and the cleanup service saw "+
			"zero items")

	got, err := linkedItemRepo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, string(domain.LinkedItemStatusDisconnected), string(got.Status),
		"the NULL-status item must be marked 'disconnected' after a successful repair — before the "+
			"fix it stayed NULL forever, re-selected by the repair query every day with no cleanup "+
			"effect (a permanent leak, not merely a delay)")
}
