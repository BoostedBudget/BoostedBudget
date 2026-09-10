//go:build integration

package repository

// Issue #1569 Task 1b — stop dropping new transactions for retained accounts.
//
// GetByLinkedItemID (WHERE status = 'active') is shared by the holdings and liability syncs,
// whose ReplaceForAccount wipe-and-reinsert semantics must stay active-only.
// transaction_sync_service.buildAccountMaps needs the opposite — every account regardless of
// status, so a Plaid-delivered transaction for a disconnected/expired/replaced account is
// still stored rather than Warn'd-and-skipped forever. This file proves, against a real
// database, that the new separate read (GetByLinkedItemIDIncludingRetained) does that without
// disturbing GetByLinkedItemID's existing active-only contract, and that a transaction stored
// against a retained account this way is genuinely visible through the unified feed — not
// merely present in the table.
//
// ⚠ investment-transaction sync is NOT one of GetByLinkedItemID's wipe-and-reinsert callers:
// its writer (investmentTransactionRepository.UpsertBatch) is a pure
// INSERT ... ON CONFLICT DO UPDATE, so it reads GetByLinkedItemIDIncludingRetained too (Issue
// #1569 round 11 task 1). Balance-snapshot sync (createBalanceSnapshots) was never a
// GetByLinkedItemID caller either — it reads GetByLinkedItemIDIncludingRetained for an
// unrelated reason (its account-restore pass needs to see removed rows) and filters to the
// active subset itself before writing any snapshot.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// addSiblingAccount inserts an additional account on the SAME linked_item_id/user as f,
// with the given status, sharing no user_account_id with f (a distinct card/account on the
// same bank connection). removedAt/replacedAt are set when required by the status pairing
// constraints (accounts_removed_at_consistency / accounts_replaced_at_consistency).
func addSiblingAccount(ctx context.Context, t *testing.T, db DB, f retainedFixture, status domain.AccountStatus, plaidAccountID string) uuid.UUID {
	t.Helper()
	accountID := uuid.New()
	userAccountID := uuid.New()

	_, err := db.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'depository')
	`, userAccountID, f.UserID, "sibling-inst-"+userAccountID.String())
	require.NoError(t, err, "seed sibling user_account")

	var removedAt, replacedAt, expiredAt any
	switch status {
	case domain.AccountStatusRemoved:
		removedAt = time.Now().UTC()
	case domain.AccountStatusReplaced:
		replacedAt = time.Now().UTC()
	case domain.AccountStatusExpired:
		// accounts_expired_at_consistency (migration 000155, Issue #1569 Task 6) requires
		// expired_at whenever status = 'expired'.
		expiredAt = time.Now().UTC()
	}

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status,
			user_account_id, removed_at, replaced_at, expired_at)
		VALUES ($1, $2, $3, $4, 'Sibling Account', 'depository', $5, $6, $7, $8, $9)
	`, accountID, f.LinkedItemID, f.UserID, plaidAccountID, string(status), userAccountID, removedAt, replacedAt, expiredAt)
	require.NoError(t, err, "seed sibling account with status %s", status)

	return accountID
}

// TestAccountRepository_GetByLinkedItemIDIncludingRetained_AllStatusesReturned is the
// repository-level regression: one linked item carrying every terminal status Task 1b cares
// about, all in the same result set the new method returns.
func TestAccountRepository_GetByLinkedItemIDIncludingRetained_AllStatusesReturned(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedRetainedFixture(ctx, t, tx, "sync-all-statuses")
	removedID := addSiblingAccount(ctx, t, tx, f, domain.AccountStatusRemoved, "plaid-removed-"+f.LinkedItemID.String())
	expiredID := addSiblingAccount(ctx, t, tx, f, domain.AccountStatusExpired, "plaid-expired-"+f.LinkedItemID.String())
	replacedID := addSiblingAccount(ctx, t, tx, f, domain.AccountStatusReplaced, "plaid-replaced-"+f.LinkedItemID.String())

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	accounts, err := repo.GetByLinkedItemIDIncludingRetained(ctx, f.LinkedItemID.String())
	require.NoError(t, err)

	ids := make(map[string]domain.AccountStatus, len(accounts))
	for _, a := range accounts {
		ids[a.ID] = a.Status
	}
	require.Len(t, ids, 4, "the seeded active account plus all three terminal siblings")
	assert.Equal(t, domain.AccountStatusActive, ids[f.AccountID.String()])
	assert.Equal(t, domain.AccountStatusRemoved, ids[removedID.String()])
	assert.Equal(t, domain.AccountStatusExpired, ids[expiredID.String()])
	assert.Equal(t, domain.AccountStatusReplaced, ids[replacedID.String()])
}

// TestAccountRepository_GetByLinkedItemIDIncludingRetained_DoesNotLeakAcrossUsers is the
// cross-user negative Task 1b's brief calls for: the widened, all-statuses read must not
// admit another user's accounts just because both linked items carry retained rows.
func TestAccountRepository_GetByLinkedItemIDIncludingRetained_DoesNotLeakAcrossUsers(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	mine := seedRetainedFixture(ctx, t, tx, "sync-leak-mine")
	theirs := seedRetainedFixture(ctx, t, tx, "sync-leak-theirs")

	disconnect(ctx, t, tx, mine)
	disconnect(ctx, t, tx, theirs)
	addSiblingAccount(ctx, t, tx, theirs, domain.AccountStatusExpired, "plaid-theirs-expired")

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	accounts, err := repo.GetByLinkedItemIDIncludingRetained(ctx, mine.LinkedItemID.String())
	require.NoError(t, err)

	require.Len(t, accounts, 1, "only my own account, regardless of the other user's retained rows")
	assert.Equal(t, mine.AccountID.String(), accounts[0].ID)
	assert.Equal(t, mine.UserID.String(), accounts[0].UserID)
}

// TestAccountRepository_GetByLinkedItemID_StaysActiveOnlySideBySideWithIncludingRetained pins
// the trap this task's brief warns about most directly: the shared, active-only
// GetByLinkedItemID must NOT have been widened. Holdings and liability syncs wipe-and-reinsert
// on every run against whatever GetByLinkedItemID returns; if it silently started returning a
// retained account, a later sync could clear or replace that account's
// holdings/liabilities — the destruction Issue #1569 exists to stop.
func TestAccountRepository_GetByLinkedItemID_StaysActiveOnlySideBySideWithIncludingRetained(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedRetainedFixture(ctx, t, tx, "sync-side-by-side")
	removedID := addSiblingAccount(ctx, t, tx, f, domain.AccountStatusRemoved, "plaid-side-removed")

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))

	activeOnly, err := repo.GetByLinkedItemID(ctx, f.LinkedItemID.String())
	require.NoError(t, err)
	require.Len(t, activeOnly, 1, "GetByLinkedItemID must stay active-only — holdings/liability "+
		"syncs depend on this to avoid destroying a retained account's data")
	assert.Equal(t, f.AccountID.String(), activeOnly[0].ID)
	for _, a := range activeOnly {
		assert.NotEqual(t, removedID.String(), a.ID, "removed sibling must not leak into the active-only read")
	}

	allStatuses, err := repo.GetByLinkedItemIDIncludingRetained(ctx, f.LinkedItemID.String())
	require.NoError(t, err)
	assert.Len(t, allStatuses, 2, "the sync-only read sees both the active account and its removed sibling")
}

// TestBuildAccountMapsStorageRoundTrip_RetainedAccountTransactionVisibleInUnifiedFeed is Task
// 1b's end-to-end proof: an account this task's fix now includes in accountMap has a
// transaction stored against it, and that transaction is visible through the REAL unified
// feed query — not merely present in the transactions table. It exercises the same three
// pieces buildAccountMaps' fix threads together: the all-statuses account read, CreateBatch
// storage, and GetByUserIDKeysetUnified's retainedAccountVisibilityLeg.
func TestBuildAccountMapsStorageRoundTrip_RetainedAccountTransactionVisibleInUnifiedFeed(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedRetainedFixture(ctx, t, tx, "sync-roundtrip")
	disconnect(ctx, t, tx, f) // the account is now 'removed', the user disconnected this card

	accountRepo := NewAccountRepository(tx, newTestFieldEncryptor(t))

	// Step 1 (the fix): buildAccountMaps' read must still find this retained account.
	accounts, err := accountRepo.GetByLinkedItemIDIncludingRetained(ctx, f.LinkedItemID.String())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, domain.AccountStatusRemoved, accounts[0].Status)

	// Step 2: a Plaid transaction for this account is mapped and stored exactly as
	// processAddedTransactions/CreateBatch would, now that the account is in accountMap.
	txnRepo := NewTransactionRepository(tx, newTestFieldEncryptor(t))
	txnID := uuid.New().String()
	pfcPrimary := "FOOD_AND_DRINK"
	_, err = txnRepo.CreateBatch(ctx, []domain.Transaction{{
		ID:                    txnID,
		UserID:                f.UserID.String(),
		AccountID:             accounts[0].ID,
		LinkedItemID:          f.LinkedItemID.String(),
		UserAccountID:         strPtrIfSet(f.UserAccountID.String()),
		Source:                domain.SourcePlaid,
		ExternalTransactionID: strPtrIfSet(txnID),
		Amount:                12.34,
		Date:                  time.Now().UTC().AddDate(0, 0, -1),
		Name:                  "Post-Disconnect Delivery",
		PFCPrimary:            &pfcPrimary,
	}})
	require.NoError(t, err, "the retained account must accept a stored transaction, not be skipped")

	// Step 3: the row is visible through the real unified feed, not merely present in the table.
	items, _, _, err := txnRepo.GetByUserIDKeysetUnified(ctx, f.UserID.String(), domain.TransactionFilters{}, nil, 50)
	require.NoError(t, err)
	assert.Equal(t, []string{"Post-Disconnect Delivery"}, feedNames(items),
		"a transaction stored against a retained account after this fix must appear in the unified feed")
}

func strPtrIfSet(s string) *string { return &s }
