package services

// plaid_item_cleanup_service_terminal_failure_integration_test.go — Issue #1494.
//
// WHY THIS EXISTS ON TOP OF THE UNIT AND REPOSITORY TESTS. Those cover the chain in halves: the unit
// tests drive classification with a mocked repository, and the repository tests drive the cascade with
// verdicts written by hand. Neither runs the whole path, so a defect in the JOIN between them — a
// verdict written for the wrong id, a code that never reaches the column, a mark the guard cannot read —
// would leave both halves green. An adversarial review named exactly this gap.
//
// So this test runs the REAL LinkedItemRepository, the REAL PlaidItemCleanupService and the REAL
// DeleteUserCascade against real Postgres, with only Plaid's HTTP call faked, and asserts the complete
// transition: an INVALID_ACCESS_TOKEN response → verdict recorded on the row → cooling-off window
// respected → C2 unblocks → the Item preserved in plaid_orphaned_items → the user erased.
//
// It also covers the recovery direction in the same shape, which is the one that matters most if the
// classification is ever too wide: a deployment whose Plaid environment is misconfigured produces the
// same INVALID_ACCESS_TOKEN, and once the configuration is fixed the retry must succeed, stamp
// plaid_removed_at, and complete the erasure with NO orphan record at all.
//
// No build tag — mirrors the sibling cleanup-service integration tests' convention: skips at runtime if
// DATABASE_URL is unset.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestPlaidCleanupService_Integration_TerminalFailure -v

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// switchablePlaidCollaborator fails every RemoveItem with the configured error until failWith is set to
// nil — the shape of "the environment was misconfigured, then an operator fixed it".
type switchablePlaidCollaborator struct {
	failWith error
	calls    int
}

func (s *switchablePlaidCollaborator) RemoveItem(ctx context.Context, accessToken string) error {
	s.calls++
	return s.failWith
}

// TestPlaidCleanupService_Integration_TerminalFailure_UnblocksErasureAndPreservesTheItem walks the whole
// #1494 chain with nothing mocked but Plaid itself.
func TestPlaidCleanupService_Integration_TerminalFailure_UnblocksErasureAndPreservesTheItem(t *testing.T) {
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	profileRepo := repository.NewProfileRepository(pool, enc)
	linkedItemRepo := repository.NewLinkedItemRepository(pool, enc)
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_x2e2e_" + suffix,
		Email:              "x2e2e-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUser(ctx, user))
	// This user's erasure is authorised by the RETENTION rule (DeleteUserCascade is called below with
	// a nil expectedDeletionRequestedAt, which forces deletionAuthorisationUnionPredicate's retention
	// branch). Since #1440 T0-retention that branch re-verifies the retention clock and the
	// subscription status, so the fixture must be a genuine expired-retention candidate — the
	// property this test always implied and never established.
	armElapsedRetentionClock(t, ctx, pool, profileRepo, user.ID)
	item := &domain.LinkedItem{
		UserID:          user.ID,
		ItemID:          "plaid-item-e2e-" + suffix,
		AccessToken:     "wrong-environment-token-" + suffix,
		InstitutionName: "Second Unremovable Bank",
		Provider:        "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE id = $1", item.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM plaid_orphaned_items WHERE linked_item_id = $1", item.ID)
	})

	fakePlaid := &switchablePlaidCollaborator{failWith: plaidInvalidAccessTokenErr()}
	svc := NewPlaidItemCleanupService(linkedItemRepo, fakePlaid)
	svc.SetWorkClaimsRepo(claimsRepo)

	// 1. The deletion flow's own call. It must report the sentinel — not a generic failure, which is
	//    what used to make this user's erasure impossible forever.
	err := svc.RemoveAllUserItems(ctx, user.ID)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPlaidRemovalUnrecoverable)
	assert.Equal(t, 1, fakePlaid.calls, "Plaid must actually have been called — no verdict without an attempt")

	// 2. The verdict is on the row, with the code, written by the real repository.
	var failedAt *time.Time
	var code *string
	var removedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT plaid_removal_failed_at, plaid_removal_error_code, plaid_removed_at FROM linked_items WHERE id = $1`,
		item.ID).Scan(&failedAt, &code, &removedAt))
	require.NotNil(t, failedAt)
	require.NotNil(t, code)
	assert.Equal(t, "INVALID_ACCESS_TOKEN", *code)
	assert.Nil(t, removedAt,
		"nothing may claim the Plaid side is done: Plaid rejected our token and said nothing about the Item")

	// 3. The erasure still waits out the cooling-off window. This is the step that makes the retry in
	//    part 5 possible at all.
	deleted, err := profileRepo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	require.False(t, deleted, "a verdict reached seconds ago must not unblock an irreversible erasure")

	// 4. Daily retries keep happening throughout the window — the row is still selectable, and the
	//    service still attempts it. Nothing about the verdict removed this work from the queue.
	require.Error(t, svc.RemoveAllUserItems(ctx, user.ID))
	assert.Equal(t, 2, fakePlaid.calls, "a marked row must keep being attempted, not skipped")
	var failedAtAfterRetry *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT plaid_removal_failed_at FROM linked_items WHERE id = $1`, item.ID).Scan(&failedAtAfterRetry))
	require.NotNil(t, failedAtAfterRetry)
	assert.Equal(t, failedAt.UTC(), failedAtAfterRetry.UTC(),
		"the retry must NOT refresh the verdict's timestamp — that would push the window's deadline forward "+
			"on every attempt and the erasure could never complete")

	// 5. Once the verdict has held longer than the window, the erasure completes and the Item survives as
	//    a record an operator can act on.
	_, err = pool.Exec(ctx,
		`UPDATE linked_items SET plaid_removal_failed_at = plaid_removal_failed_at - INTERVAL '8 days' WHERE id = $1`,
		item.ID)
	require.NoError(t, err)

	deleted, err = profileRepo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted, "#1494: the erasure must eventually complete")

	var plaidItemCipher, instCipher *string
	var orphanCode *string
	var terminalSince *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT plaid_item_id_encrypted, institution_name_encrypted, last_error_code, terminal_since
		FROM plaid_orphaned_items WHERE linked_item_id = $1`, item.ID).Scan(&plaidItemCipher, &instCipher, &orphanCode, &terminalSince))
	require.NotNil(t, plaidItemCipher)
	plainItemID, err := enc.DecryptString(*plaidItemCipher)
	require.NoError(t, err)
	assert.Equal(t, "plaid-item-e2e-"+suffix, plainItemID,
		"the whole chain must end with the Plaid Item id an operator needs, recoverable from the surviving record")
	require.NotNil(t, instCipher)
	plainInst, err := enc.DecryptString(*instCipher)
	require.NoError(t, err)
	assert.Equal(t, "Second Unremovable Bank", plainInst)
	require.NotNil(t, orphanCode)
	assert.Equal(t, "INVALID_ACCESS_TOKEN", *orphanCode)
	assert.NotNil(t, terminalSince)
}

// TestPlaidCleanupService_Integration_TerminalFailure_ConfigFixInsideTheWindowLeavesNoOrphan is the
// recovery direction, and it is the reason the cooling-off window exists rather than an immediate
// unblock.
//
// Plaid returns INVALID_ACCESS_TOKEN both for a dead token and when a token is used against the wrong
// environment or client_id. The second is a deployment misconfiguration: every Item answers this way,
// and an ops fix cures all of them. Inside the window that fix must fully heal the row — real removal,
// plaid_removed_at stamped, erasure completed with NO orphan record — because an orphan here would be a
// false report of an outstanding vendor obligation, and (before the window existed) the erasure would
// already have destroyed the token that was about to start working again.
func TestPlaidCleanupService_Integration_TerminalFailure_ConfigFixInsideTheWindowLeavesNoOrphan(t *testing.T) {
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	profileRepo := repository.NewProfileRepository(pool, enc)
	linkedItemRepo := repository.NewLinkedItemRepository(pool, enc)
	claimsRepo := repository.NewWorkClaimsRepository(pool)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_x2heal_" + suffix,
		Email:              "x2heal-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "cancelled",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUser(ctx, user))
	// This user's erasure is authorised by the RETENTION rule (DeleteUserCascade is called below with
	// a nil expectedDeletionRequestedAt, which forces deletionAuthorisationUnionPredicate's retention
	// branch). Since #1440 T0-retention that branch re-verifies the retention clock and the
	// subscription status, so the fixture must be a genuine expired-retention candidate — the
	// property this test always implied and never established.
	armElapsedRetentionClock(t, ctx, pool, profileRepo, user.ID)
	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-heal-" + suffix,
		AccessToken: "token-that-starts-working-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM linked_items WHERE id = $1", item.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM plaid_orphaned_items WHERE linked_item_id = $1", item.ID)
	})

	fakePlaid := &switchablePlaidCollaborator{failWith: plaidInvalidAccessTokenErr()}
	svc := NewPlaidItemCleanupService(linkedItemRepo, fakePlaid)
	svc.SetWorkClaimsRepo(claimsRepo)

	// Day 1: misconfigured. The verdict is recorded and the erasure is refused — nothing irreversible
	// has happened.
	require.ErrorIs(t, svc.RemoveAllUserItems(ctx, user.ID), ErrPlaidRemovalUnrecoverable)
	deleted, err := profileRepo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	require.False(t, deleted)

	// Day 3: an operator fixes the Plaid environment. The next daily attempt succeeds for real.
	fakePlaid.failWith = nil
	require.NoError(t, svc.RemoveAllUserItems(ctx, user.ID),
		"with a working configuration the removal must simply succeed — the earlier verdict blocks nothing")

	var removedAt, failedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT plaid_removed_at, plaid_removal_failed_at FROM linked_items WHERE id = $1`,
		item.ID).Scan(&removedAt, &failedAt))
	assert.NotNil(t, removedAt, "the successful removal must be recorded")
	assert.NotNil(t, failedAt,
		"the earlier verdict is deliberately NOT cleared: a code we called unrecoverable that later succeeded "+
			"is the only signal that the classification was too wide")

	// The erasure now completes on the strength of the REMOVAL, and leaves nothing for an operator to
	// chase.
	deleted, err = profileRepo.DeleteUserCascade(ctx, user.ID, user.DeletionRequestedAt)
	require.NoError(t, err)
	require.True(t, deleted)

	var orphanCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM plaid_orphaned_items WHERE linked_item_id = $1`, item.ID).Scan(&orphanCount))
	assert.Equal(t, 0, orphanCount,
		"a row that healed inside the window owes nothing: recording it as an orphan would send an operator "+
			"after an Item that is already removed")
}
