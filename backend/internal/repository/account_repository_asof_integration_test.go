// account_repository_asof_integration_test.go — DB-backed behaviour for the as-of account reads
// added by #1569: AccountFilterOptions.AsOf and AccountRepository.GetForHouseholdScopeLiveAsOf.
//
// These run against the real account_status_history table and its triggers (migrations 000151 /
// 000152), because the whole point of the option is that the answer comes from recorded intervals
// rather than from anything Go computes. A pgxmock test here would only assert that the string we
// wrote is the string we wrote.
//
// The fixtures backdate valid_from on the account's own history row. That is not cheating around
// the trigger: an account created by a test is necessarily seconds old, and the behaviour under
// test is what happens at an instant BEFORE it existed versus after. Backdating the recorded
// interval is exactly how an account that has been around for 90 days looks.

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// backdateAccountHistory rewrites the open 'active' interval's valid_from, making the account look
// as though it has been live since `from`.
func backdateAccountHistory(t *testing.T, pool *pgxpool.Pool, accountID string, from time.Time) {
	t.Helper()
	ct, err := pool.Exec(context.Background(), `
		UPDATE account_status_history
		SET valid_from = $2
		WHERE account_id = $1 AND status = 'active' AND valid_to IS NULL`, accountID, from)
	require.NoError(t, err, "backdate account_status_history")
	require.EqualValues(t, 1, ct.RowsAffected(),
		"the INSERT trigger from migration 000152 must have opened exactly one active interval")
}

// removeAccount soft-removes an account the way Task 2's DisconnectInstitution does. It is not
// setAccountStatus: that shared helper NULLs removed_at, which the accounts_removed_at_consistency
// check constraint rejects for status='removed'. The status trigger writes the history transition.
func removeAccount(t *testing.T, pool *pgxpool.Pool, accountID string) {
	t.Helper()
	ct, err := pool.Exec(context.Background(), `
		UPDATE accounts SET status = 'removed', removed_at = NOW(), updated_at = NOW()
		WHERE id = $1`, accountID)
	require.NoError(t, err, "soft-remove account")
	require.EqualValues(t, 1, ct.RowsAffected())
}

func accountIDSet(accounts []domain.Account) map[string]bool {
	out := make(map[string]bool, len(accounts))
	for _, a := range accounts {
		out[a.ID] = true
	}
	return out
}

// TestAccountRepository_AsOf_ResolvesTheUniverseThatReallyExisted is the core of Change A. One
// account was live at the cutoff and has since been removed; one did not exist yet at the cutoff.
// The as-of read must include the first and exclude the second — and the plain active-only read
// must do the exact opposite, which is what makes the two universes genuinely different rather
// than one being a relabelling of the other.
func TestAccountRepository_AsOf_ResolvesTheUniverseThatReallyExisted(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	user := insertTestUser(t, pool)
	longLived := setupHouseholdMemberAccount(t, pool, user)
	openedRecently := setupHouseholdMemberAccount(t, pool, user)

	now := time.Now().UTC()
	cutoff := now.AddDate(0, 0, -30)

	// longLived has been live for 90 days, and was disconnected today.
	backdateAccountHistory(t, pool, longLived.AccountID, now.AddDate(0, 0, -90))
	removeAccount(t, pool, longLived.AccountID)
	// openedRecently was created just now — i.e. AFTER the cutoff — and is still active.

	asOf, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AsOf: &cutoff, IncludeHidden: true})
	require.NoError(t, err)
	got := accountIDSet(asOf)
	assert.True(t, got[longLived.AccountID],
		"it was live 30 days ago; the disconnection since then does not change what was true then. "+
			"This is the $1,000-account-that-reported-0%% bug from the issue.")
	assert.False(t, got[openedRecently.AccountID],
		"it did not exist 30 days ago. AllStatuses could not express this — it returns every "+
			"account that exists TODAY, so it silently admitted accounts opened after the cutoff.")

	current, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{IncludeHidden: true})
	require.NoError(t, err)
	gotCurrent := accountIDSet(current)
	assert.False(t, gotCurrent[longLived.AccountID], "the removed account is not part of TODAY")
	assert.True(t, gotCurrent[openedRecently.AccountID])
}

// TestAccountRepository_AsOf_IsHalfOpenAtTheTransitionInstant pins the interval semantics migration
// 000151 declares: live AT valid_from, NOT live at valid_to. An off-by-one here is a whole day of
// history appearing or vanishing at a boundary, which is invisible in any test that samples the
// middle of an interval.
func TestAccountRepository_AsOf_IsHalfOpenAtTheTransitionInstant(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	user := insertTestUser(t, pool)
	acct := setupHouseholdMemberAccount(t, pool, user)
	from := time.Now().UTC().AddDate(0, 0, -60)
	backdateAccountHistory(t, pool, acct.AccountID, from)
	removeAccount(t, pool, acct.AccountID)

	var validTo time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT valid_to FROM account_status_history
		WHERE account_id = $1 AND status = 'active'`, acct.AccountID).Scan(&validTo))

	atFrom, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AsOf: &from, IncludeHidden: true})
	require.NoError(t, err)
	assert.True(t, accountIDSet(atFrom)[acct.AccountID], "live AT valid_from (inclusive lower bound)")

	justBefore := from.Add(-time.Millisecond)
	before, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AsOf: &justBefore, IncludeHidden: true})
	require.NoError(t, err)
	assert.False(t, accountIDSet(before)[acct.AccountID], "not yet live one millisecond earlier")

	atTo, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AsOf: &validTo, IncludeHidden: true})
	require.NoError(t, err)
	assert.False(t, accountIDSet(atTo)[acct.AccountID], "NOT live at valid_to (exclusive upper bound)")

	justBeforeTo := validTo.Add(-time.Millisecond)
	beforeTo, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AsOf: &justBeforeTo, IncludeHidden: true})
	require.NoError(t, err)
	assert.True(t, accountIDSet(beforeTo)[acct.AccountID], "still live one millisecond before valid_to")
}

// TestAccountRepository_AsOf_RejectsContradictoryCombinations is the precedence table's error half,
// asserted through the public API rather than only against the unexported resolver — a resolver
// that returns an error nobody propagates would still ship the bug.
func TestAccountRepository_AsOf_RejectsContradictoryCombinations(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	user := insertTestUser(t, pool)
	setupHouseholdMemberAccount(t, pool, user)
	asOf := time.Now().UTC()

	_, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AsOf: &asOf, AllStatuses: true})
	require.ErrorIs(t, err, repository.ErrAccountFilterAsOfConflict)

	_, err = repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AsOf: &asOf, ReplacedOnly: true})
	require.ErrorIs(t, err, repository.ErrAccountFilterAsOfConflict)

	// And the combination that IS allowed still works, so the rejection is targeted rather than
	// a blanket "AsOf plus anything".
	_, err = repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AsOf: &asOf, IncludeReplaced: true, IncludeHidden: true})
	require.NoError(t, err)
}

// TestAccountRepository_AsOf_HonoursTheUserAccountIDFilterPlaceholder guards the argument-numbering
// hazard in Change A: the as-of instant is appended after the OPTIONAL user_account_id argument, so
// its placeholder is $2 in one call shape and $3 in the other. A hardcoded number would produce a
// silently wrong comparison (or a bind error) only in whichever shape the tests forgot.
func TestAccountRepository_AsOf_HonoursTheUserAccountIDFilterPlaceholder(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	user := insertTestUser(t, pool)
	wanted := setupHouseholdMemberAccount(t, pool, user)
	other := setupHouseholdMemberAccount(t, pool, user)

	now := time.Now().UTC()
	cutoff := now.AddDate(0, 0, -30)
	backdateAccountHistory(t, pool, wanted.AccountID, now.AddDate(0, 0, -90))
	backdateAccountHistory(t, pool, other.AccountID, now.AddDate(0, 0, -90))

	got, err := repo.GetAccountsWithInstitutionFiltered(ctx, user, repository.AccountFilterOptions{
		AsOf:          &cutoff,
		IncludeHidden: true,
		UserAccountID: wanted.UserAccountID,
	})
	require.NoError(t, err)
	require.Len(t, got, 1, "both filters must apply together, not one silently displacing the other")
	assert.Equal(t, wanted.AccountID, got[0].ID)
	assert.NotEqual(t, other.AccountID, got[0].ID)
}

// TestAccountRepository_GetForHouseholdScopeLiveAsOf_ResolvesTheUniverseThatReallyExisted is the
// household twin of the first test, and additionally pins that the as-of predicate did not become
// a way out of the household scope itself.
func TestAccountRepository_GetForHouseholdScopeLiveAsOf_ResolvesTheUniverseThatReallyExisted(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	owner, member, hh, ownerAcct, memberAcct := setupTwoMemberSharedHousehold(t, pool)
	now := time.Now().UTC()
	cutoff := now.AddDate(0, 0, -30)

	backdateAccountHistory(t, pool, ownerAcct.AccountID, now.AddDate(0, 0, -90))
	backdateAccountHistory(t, pool, memberAcct.AccountID, now.AddDate(0, 0, -90))
	// The member disconnected their bank since the cutoff.
	removeAccount(t, pool, memberAcct.AccountID)

	scope := &domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}}

	// #1569 AC2. This assertion used to DOCUMENT A GAP: the read was called GetForHouseholdScope,
	// its argument was called asOf, and it filtered on current status anyway. The closure was to
	// rename it, not to widen it (task 12's universes ruling forbids the widening — see
	// account_liveness_sql.go), so the same assertion now PINS A CONTRACT the name states out
	// loud: ActiveNow means current status, whatever date you hand it. If this ever starts
	// returning the member's since-removed account, the two household reads have collapsed into
	// one and the LiveAsOf variant below has lost its reason to exist.
	activeOnly, err := repo.GetForHouseholdScopeActiveNow(ctx, owner, scope, cutoff)
	require.NoError(t, err)
	assert.False(t, accountIDSet(activeOnly)[memberAcct.AccountID],
		"GetForHouseholdScopeActiveNow must filter on CURRENT status regardless of its date "+
			"argument — that is what its name promises, and what makes the LiveAsOf variant a "+
			"distinct question rather than a duplicate")

	live, err := repo.GetForHouseholdScopeLiveAsOf(ctx, owner, scope, cutoff)
	require.NoError(t, err)
	gotLive := accountIDSet(live)
	assert.True(t, gotLive[ownerAcct.AccountID])
	assert.True(t, gotLive[memberAcct.AccountID],
		"the member's since-removed shared account WAS live 30 days ago and must be in the baseline")

	// And at an instant before either account existed, the household universe is empty rather
	// than "everything that exists now".
	ancient := now.AddDate(0, 0, -365)
	none, err := repo.GetForHouseholdScopeLiveAsOf(ctx, owner, scope, ancient)
	require.NoError(t, err)
	assert.Empty(t, accountIDSet(none),
		"a cutoff before any account existed must resolve to no accounts, which all-statuses "+
			"could never express")
}

// TestAccountRepository_GetForHouseholdScopeLiveAsOf_StillEnforcesHouseholdScope is the tenancy
// negative. The as-of predicate is an EXISTS on a table keyed only by account_id; if it were ever
// spliced in ahead of the household CTE join it would widen the result set silently.
func TestAccountRepository_GetForHouseholdScopeLiveAsOf_StillEnforcesHouseholdScope(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	owner, member, hh, _, _ := setupTwoMemberSharedHousehold(t, pool)

	outsider := insertTestUser(t, pool)
	outsiderAcct := setupHouseholdMemberAccount(t, pool, outsider)
	backdateAccountHistory(t, pool, outsiderAcct.AccountID, time.Now().UTC().AddDate(0, 0, -90))

	live, err := repo.GetForHouseholdScopeLiveAsOf(ctx, owner,
		&domain.HouseholdScope{HouseholdID: hh, MemberUserIDs: []string{owner, member}},
		time.Now().UTC().AddDate(0, 0, -30))
	require.NoError(t, err)
	assert.False(t, accountIDSet(live)[outsiderAcct.AccountID],
		"an account outside the household must never appear, however live it was at the cutoff")
}

// TestAccountRepository_AsOf_DoesNotFireReplacedCanary keeps the canary's contract intact: it
// counts active-only reads that DROP replaced accounts. An as-of read drops nothing that was live
// at its instant, so — exactly like AllStatuses — it must stay silent, or the counter starts
// reporting the dashboard's own baseline as a filtering event.
func TestAccountRepository_AsOf_DoesNotFireReplacedCanary(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	user := insertTestUser(t, pool)
	acct := setupHouseholdMemberAccount(t, pool, user)
	setAccountStatus(t, pool, acct.AccountID, domain.AccountStatusReplaced)
	asOf := time.Now().UTC()

	counter := observability.AccountsReplacedFiltered.WithLabelValues("accounts_list")
	before := testutil.ToFloat64(counter)
	_, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AsOf: &asOf, IncludeHidden: true})
	require.NoError(t, err)
	assert.Equal(t, before, testutil.ToFloat64(counter),
		"an as-of read is a historical universe, not a filtered current one")
}
