//go:build integration

package repository

// Issue #1569 task 21, item D — the product ruling, in executable form.
//
//	"If account is closed, treat as empty balance, we don't want orphaned balances."
//
// A terminal-status account (removed / replaced / expired) must present NO current balance
// anywhere. Not a frozen last-known figure, not a stale number with an "as of" label beside it.
//
// THE SEAM, which is the entire content of the rule. This is about the CURRENT universe only. A
// closed account's HISTORY stays exactly as queryable as it was: as-of balances come from
// balance_snapshots and holding_snapshots, which carry their own as_of instant and are untouched
// here. Both halves are asserted below, in the same test, over the same account — separating them
// would let either half drift without the other noticing.
//
// NULL, NOT ZERO. domain.Account.CurrentBalance is *float64 with `json:"current_balance,omitempty"`,
// so nil disappears from the JSON entirely and a consumer that sums sees nothing to add. Zero is a
// NUMBER: a client renders it as "$0.00" — an assertion that the account is empty, which is a
// different and false claim — and a summing consumer adds it as a real datapoint. nil is also
// already the established "no balance known" value on this field (Plaid omits available_balance for
// several account types), so every existing nil-check already does the right thing with it;
// savings_progress_service.go's two recompute paths and its LinkedBalance projection all skip on
// nil, which is why this ruling REINFORCES task 13's freeze rather than fighting it.
//
// WHY THE ENUMERATION IS TABLE-DRIVEN AND NAMED. A Go-only reader audit cannot see the JSON
// contract, and this plan made that exact mistake once already. Every read below is one that CAN
// return a non-active account; each is exercised by name so a future reader can check the list
// against the repository rather than trusting a claim about it. The mechanical half is
// TestNoAccountProjectionCarriesATerminalBalance, which cannot be satisfied by a neighbouring
// query.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestAccountRepository_TerminalAccount_CarriesNoCurrentBalance_Integration is item D's behavioural
// statement.
func TestAccountRepository_TerminalAccount_CarriesNoCurrentBalance_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	repo := NewAccountRepository(tx, newTestFieldEncryptor(t))

	userID := uuid.New()
	linkedItemID := uuid.New()
	userAccountID := uuid.New()
	accountID := uuid.New()
	prefix := "terminal-bal-" + accountID.String()

	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+prefix, prefix+"@example.com")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`, linkedItemID, userID, prefix+"-item")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'depository')`, userAccountID, userID, prefix+"-inst")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status,
		                      current_balance, available_balance, user_account_id)
		VALUES ($1, $2, $3, $4, 'Closed Checking', 'depository', 'active', 1234.56, 1200.00, $5)
	`, accountID, linkedItemID, userID, prefix+"-acct", userAccountID)
	require.NoError(t, err)

	now := time.Now().UTC()

	// A snapshot on record BEFORE the closure — the historical half of the seam.
	_, err = tx.Exec(ctx, `
		INSERT INTO balance_snapshots (id, account_id, user_account_id, user_id, current_balance,
		                               available_balance, currency_code, snapshot_at, source)
		VALUES ($1, $2, $3, $4, 1234.56, 1200.00, 'USD', $5, 'sync')
	`, uuid.New(), accountID, userAccountID, userID, now.AddDate(0, 0, -5))
	require.NoError(t, err)

	// Precondition: while ACTIVE, the balance is present. Without this the assertions below could
	// all pass on a fixture that never had a balance at all — the vacuity this plan found six of.
	whileActive, err := repo.GetByIDAndUser(ctx, accountID.String(), userID.String())
	require.NoError(t, err)
	require.NotNil(t, whileActive.CurrentBalance, "fixture precondition: an ACTIVE account has its balance")
	require.InDelta(t, 1234.56, *whileActive.CurrentBalance, 0.001)
	require.NotNil(t, whileActive.AvailableBalance, "fixture precondition: and its available balance")

	// Close it, ten days ago, and close the recorded interval with it.
	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = $2, updated_at = $2 WHERE id = $1`,
		accountID, now.AddDate(0, 0, -10))
	require.NoError(t, err)
	// The UPDATE above already fired the status trigger, which CLOSED the 'active' interval at
	// clock_timestamp() and opened a 'removed' one — so there is no open 'active' row left to match
	// on `valid_to IS NULL`. Backdate the closed interval instead, both ends: the trigger opened it
	// at insert time moments ago, and an interval of [now, now] would make every as-of subtest below
	// return an empty result and pass on nothing at all. RowsAffected is asserted for exactly that
	// reason — a silent zero here is the vacuity, not the fix.
	ct, err := tx.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2, valid_to = $3
		WHERE account_id = $1 AND status = 'active'`,
		accountID, now.AddDate(0, 0, -90), now.AddDate(0, 0, -10))
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "the trigger must have recorded exactly one active interval")

	assertNoBalance := func(t *testing.T, read string, got *domain.Account) {
		t.Helper()
		require.NotNil(t, got, "%s returned nothing — the read is not exercising a terminal account, "+
			"so its assertion below would be vacuous", read)
		require.NotEqual(t, domain.AccountStatusActive, got.Status,
			"%s returned an ACTIVE account — wrong fixture, the assertion proves nothing", read)
		assert.Nil(t, got.CurrentBalance,
			"%s: a closed account must carry NO current balance (got %v)", read, got.CurrentBalance)
		assert.Nil(t, got.AvailableBalance,
			"%s: available_balance is the sibling field — leaving it populated is half a fix", read)
	}

	// ── Every read that can return a terminal account, by name. ───────────────────────────
	t.Run("GetByIDAndUser", func(t *testing.T) {
		got, err := repo.GetByIDAndUser(ctx, accountID.String(), userID.String())
		require.NoError(t, err)
		assertNoBalance(t, "GetByIDAndUser", got)
	})

	t.Run("GetByUserAccountIDAndUser", func(t *testing.T) {
		got, err := repo.GetByUserAccountIDAndUser(ctx, userAccountID.String(), userID.String())
		require.NoError(t, err)
		assertNoBalance(t, "GetByUserAccountIDAndUser", got)
	})

	t.Run("GetByLinkedItemIDIncludingRetained", func(t *testing.T) {
		got, err := repo.GetByLinkedItemIDIncludingRetained(ctx, linkedItemID.String())
		require.NoError(t, err)
		require.Len(t, got, 1)
		assertNoBalance(t, "GetByLinkedItemIDIncludingRetained", &got[0])
	})

	t.Run("GetByUserAccountID", func(t *testing.T) {
		got, err := repo.GetByUserAccountID(ctx, userAccountID.String())
		require.NoError(t, err)
		require.Len(t, got, 1)
		assertNoBalance(t, "GetByUserAccountID", &got[0])
	})

	t.Run("GetByUserAccountAndStatus", func(t *testing.T) {
		got, err := repo.GetByUserAccountAndStatus(ctx, userAccountID.String(), string(domain.AccountStatusRemoved), userID.String())
		require.NoError(t, err)
		require.Len(t, got, 1)
		assertNoBalance(t, "GetByUserAccountAndStatus", &got[0])
	})

	// The "Closed accounts" view: AccountsPage requests status=all_statuses, and this is the read
	// behind it. The brief called it the clearest instance of the ruling being violated.
	t.Run("GetAccountsWithInstitutionFiltered_AllStatuses", func(t *testing.T) {
		got, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID.String(),
			AccountFilterOptions{AllStatuses: true, IncludeHidden: true})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assertNoBalance(t, "GetAccountsWithInstitutionFiltered(AllStatuses)", &got[0])
	})

	// An AS-OF read returns the account because it WAS live then. It still must not carry the
	// live `accounts` row's balance column — that column has no as-of meaning at all. The balance
	// for that instant lives in balance_snapshots, asserted below.
	t.Run("GetAccountsWithInstitutionFiltered_AsOf", func(t *testing.T) {
		asOf := now.AddDate(0, 0, -20)
		got, err := repo.GetAccountsWithInstitutionFiltered(ctx, userID.String(),
			AccountFilterOptions{AsOf: &asOf, IncludeHidden: true})
		require.NoError(t, err)
		require.Len(t, got, 1, "the account was live 20 days ago, so the as-of universe contains it")
		assertNoBalance(t, "GetAccountsWithInstitutionFiltered(AsOf)", &got[0])
	})

	t.Run("GetForHouseholdScopeLiveAsOf", func(t *testing.T) {
		var householdID string
		require.NoError(t, tx.QueryRow(ctx,
			`INSERT INTO households (owner_user_id, name) VALUES ($1, 'Terminal Balance HH') RETURNING id::text`,
			userID).Scan(&householdID))
		_, err := tx.Exec(ctx, `
			INSERT INTO household_members (household_id, user_id, role, status)
			VALUES ($1, $2, 'owner', 'active')`, householdID, userID)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
			INSERT INTO household_account_shares (household_id, user_account_id, visibility, shared_at)
			VALUES ($1, $2, 'shared', $3)`, householdID, userAccountID, now.AddDate(0, 0, -90))
		require.NoError(t, err)

		scope := &domain.HouseholdScope{HouseholdID: householdID, MemberUserIDs: []string{userID.String()}}
		got, err := repo.GetForHouseholdScopeLiveAsOf(ctx, userID.String(), scope, now.AddDate(0, 0, -20))
		require.NoError(t, err)
		require.Len(t, got, 1, "the account was live and shared 20 days ago")
		assertNoBalance(t, "GetForHouseholdScopeLiveAsOf", &got[0])
	})

	// ── The HISTORICAL half of the seam: untouched. ───────────────────────────────────────
	t.Run("history remains fully queryable", func(t *testing.T) {
		snapRepo := NewBalanceSnapshotRepository(tx)
		snaps, err := snapRepo.GetSnapshotsAsOfForUser(ctx, userID.String(), now)
		require.NoError(t, err)
		require.Len(t, snaps, 1,
			"the closed account's snapshot must still be readable — this fix is about the CURRENT "+
				"universe, and a historical read that lost its balance would be the #1569 bug itself")
		require.NotNil(t, snaps[0].CurrentBalance)
		assert.InDelta(t, 1234.56, *snaps[0].CurrentBalance, 0.001,
			"the historical balance is a fact about a past instant; closing the account cannot change it")
		require.NotNil(t, snaps[0].AvailableBalance)
		assert.InDelta(t, 1200.00, *snaps[0].AvailableBalance, 0.001)
	})
}
