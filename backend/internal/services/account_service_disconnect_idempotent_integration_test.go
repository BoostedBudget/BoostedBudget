//go:build integration

package services

// account_service_disconnect_idempotent_integration_test.go — Issue #1569, Task 2, fix round 1:
// proves the idempotency split at the SERVICE boundary, against a REAL Postgres instance.
//
// The repository-level guard (internal/repository/account_repository_soft_remove_integration_test.go)
// proves UpdateStatus itself never writes on a repeat call. This test proves the layer above it:
// accountService.DisconnectAccount converts the repository's ErrAccountAlreadyRemoved into a nil
// (idempotent success) return — a mock-only test could get this "right" by accident (return nil for
// any error, forget to check removed_at) without ever proving no write happened. Running the real
// repository against real Postgres is what makes "success AND nothing changed" non-trivial to fake.
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/services/ -run TestDisconnectAccount_Integration -tags integration -p 1 -count=1 -v

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

// TestDisconnectAccount_Integration_RepeatDisconnectIsIdempotentSuccessWithNoWrite is the RED test
// for fix round 1: a second DisconnectAccount call against an already-'removed' account must
// return nil (not an error — the caller's desired end state is already reality), and must NOT move
// removed_at or write a second account_status_history row. "Success" must not become a second
// write in disguise — that's exactly the accidental-clobber shape this whole task exists to close.
func TestDisconnectAccount_Integration_RepeatDisconnectIsIdempotentSuccessWithNoWrite(t *testing.T) {
	pool := setupHouseholdServiceIntegrationDB(t)
	ctx := context.Background()
	enc := newTestFieldEncryptor(t)

	profileRepo := repository.NewProfileRepository(pool, enc)
	linkedItemRepo := repository.NewLinkedItemRepository(pool, enc)
	accountRepo := repository.NewAccountRepository(pool, enc)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := &domain.User{
		ClerkUserID:        "clerk_discidem_" + suffix,
		Email:              "discidem-" + suffix + "@example.com",
		SubscriptionTier:   "premium",
		SubscriptionStatus: "active",
		PrimaryRegion:      "iad",
		IsActive:           true,
	}
	require.NoError(t, profileRepo.CreateUser(ctx, user))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID) })

	item := &domain.LinkedItem{
		UserID:      user.ID,
		ItemID:      "plaid-item-discidem-" + suffix,
		AccessToken: "access-token-discidem-" + suffix,
		Provider:    "plaid",
	}
	require.NoError(t, linkedItemRepo.Create(ctx, item))

	var accountID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO accounts (linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'Idempotent Checking', 'depository')
		RETURNING id`,
		item.ID, user.ID, "plaid-acct-discidem-"+suffix).Scan(&accountID))

	// jobQueue/auditLogger deliberately nil — DisconnectAccount's cleanup and audit steps are both
	// nil-safe no-ops, and this test only cares about the accounts row + history table.
	svc := &accountService{accountRepo: accountRepo}

	require.NoError(t, svc.DisconnectAccount(ctx, accountID, user.ID))

	var firstRemovedAt time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT removed_at FROM accounts WHERE id = $1`, accountID).Scan(&firstRemovedAt))

	var historyCountBefore int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM account_status_history WHERE account_id = $1 AND status = 'removed'`,
		accountID).Scan(&historyCountBefore))
	require.Equal(t, 1, historyCountBefore, "the first disconnect must have written its history interval")

	// Backdate removed_at, matching the bug report's "six months ago" shape, so a silent second
	// write would be unmistakable.
	sixMonthsAgo := time.Now().AddDate(0, -6, 0)
	_, err := pool.Exec(ctx, `UPDATE accounts SET removed_at = $1 WHERE id = $2`, sixMonthsAgo, accountID)
	require.NoError(t, err)

	err = svc.DisconnectAccount(ctx, accountID, user.ID)
	require.NoError(t, err, "a repeat disconnect on an already-removed account must be idempotent success, not an error")

	var status string
	var removedAtAfterRepeat time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, removed_at FROM accounts WHERE id = $1`, accountID).Scan(&status, &removedAtAfterRepeat))
	assert.Equal(t, "removed", status)
	assert.WithinDuration(t, sixMonthsAgo, removedAtAfterRepeat, time.Second,
		"idempotent success must not move removed_at — success must not become a second write in disguise")

	var historyCountAfter int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM account_status_history WHERE account_id = $1 AND status = 'removed'`,
		accountID).Scan(&historyCountAfter))
	assert.Equal(t, historyCountBefore, historyCountAfter,
		"the repeat call must not write a second history row — this is exactly where an accidental write would hide")
}
