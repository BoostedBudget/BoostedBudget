// account_repository_all_statuses_integration_test.go — DB-backed behavior for the two
// all-statuses account reads added by #1537 round 10:
// AccountFilterOptions.AllStatuses.
//
// These exist because the whole point of the change is a SQL status predicate, and the two
// instruments that LOOK like they already do this job do not:
//
//   - IncludeReplaced produces `status IN ('active','replaced')` — it EXCLUDES 'expired',
//     which is exactly what ExpireAccountsByFingerprint writes for a fully-disconnected
//     chain (the case the dashboard comparison baseline is about).
//   - GetForHouseholdScopeActiveNow filters `a.status = 'active'` REGARDLESS of its asOf argument,
//     so passing a past cutoff to it does not widen the universe at all.
//
// Per project rule, pgxmock cannot see any of that — only real Postgres can (see memory
// feedback_pgxmock_integration_coverage.md).
//
// Run:
//
//	DATABASE_URL="postgres://..." go test ./internal/repository/ -run TestAccountRepository_.*AllStatuses -v
package repository_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// setAccountStatus forces an accounts row into a non-active status, standing in for the
// re-link handler (status='replaced') and ExpireAccountsByFingerprint (status='expired').
func setAccountStatus(t *testing.T, pool *pgxpool.Pool, accountID string, status domain.AccountStatus) {
	t.Helper()
	// accounts_replaced_at_consistency requires replaced_at to be set exactly when
	// status='replaced' (NULL otherwise), and accounts_expired_at_consistency (migration 000155,
	// Issue #1569 Task 6) requires the same pairing for expired_at/'expired' — so both move with
	// status together.
	ct, err := pool.Exec(context.Background(), `
		UPDATE accounts
		SET status = $2::text,
		    replaced_at = CASE WHEN $2::text = 'replaced' THEN NOW() ELSE NULL END,
		    expired_at = CASE WHEN $2::text = 'expired' THEN NOW() ELSE NULL END
		WHERE id = $1`, accountID, status)
	require.NoError(t, err, "set account status")
	require.EqualValues(t, 1, ct.RowsAffected(), "status update must actually hit the row")
}

func TestAccountRepository_GetAccountsWithInstitutionFiltered_AllStatuses_IncludesExpired(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	user := insertTestUser(t, pool)
	activeAcct := setupHouseholdMemberAccount(t, pool, user)
	replacedAcct := setupHouseholdMemberAccount(t, pool, user)
	expiredAcct := setupHouseholdMemberAccount(t, pool, user)
	setAccountStatus(t, pool, replacedAcct.AccountID, domain.AccountStatusReplaced)
	setAccountStatus(t, pool, expiredAcct.AccountID, domain.AccountStatusExpired)

	ids := func(accounts []domain.Account) map[string]bool {
		out := make(map[string]bool, len(accounts))
		for _, a := range accounts {
			out[a.ID] = true
		}
		return out
	}

	// The trap: IncludeReplaced is NOT an all-statuses switch.
	includeReplaced, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{IncludeReplaced: true, IncludeHidden: true})
	require.NoError(t, err)
	gotReplaced := ids(includeReplaced)
	assert.True(t, gotReplaced[activeAcct.AccountID])
	assert.True(t, gotReplaced[replacedAcct.AccountID])
	assert.False(t, gotReplaced[expiredAcct.AccountID],
		"IncludeReplaced emits status IN ('active','replaced'), so it must NOT reach 'expired' — "+
			"this is why a separate AllStatuses option is needed")

	allStatuses, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AllStatuses: true, IncludeHidden: true})
	require.NoError(t, err)
	gotAll := ids(allStatuses)
	assert.True(t, gotAll[activeAcct.AccountID], "AllStatuses must include active")
	assert.True(t, gotAll[replacedAcct.AccountID], "AllStatuses must include replaced")
	assert.True(t, gotAll[expiredAcct.AccountID], "AllStatuses must include expired")
}

// TestAccountRepository_GetAccountsWithInstitutionFiltered_AllStatuses_HonoursIncludeHidden pins
// that AllStatuses widens the STATUS predicate only. The dashboard baseline passes
// IncludeHidden:true deliberately, because GetByUserID — the source of the current-total account
// map it is compared against — applies no is_hidden predicate at all; if AllStatuses had silently
// swallowed the hidden filter too, the caller's explicit choice would be untestable.
func TestAccountRepository_GetAccountsWithInstitutionFiltered_AllStatuses_HonoursIncludeHidden(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	user := insertTestUser(t, pool)
	hiddenExpired := setupHouseholdMemberAccount(t, pool, user)
	setAccountStatus(t, pool, hiddenExpired.AccountID, domain.AccountStatusExpired)
	// #1609: the hide is written to BOTH tables. user_accounts is where the product writes it
	// (PATCH /api/v1/user-accounts/{id}) and, for a CHAINED account like this fixture's, it is now
	// the authoritative side of the customization overlay — an accounts-only hide would be
	// overridden by the chain's FALSE and this fixture would silently stop being hidden at all.
	// accounts keeps its TRUE so the assertion below still holds for the unchained fallback leg
	// too. The assertion itself is unchanged: AllStatuses widens the STATUS predicate only.
	_, err := pool.Exec(ctx, `UPDATE accounts SET is_hidden = TRUE WHERE id = $1`, hiddenExpired.AccountID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE user_accounts SET is_hidden = TRUE WHERE id = $1`, hiddenExpired.UserAccountID)
	require.NoError(t, err)

	withHidden, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AllStatuses: true, IncludeHidden: true})
	require.NoError(t, err)
	require.Len(t, withHidden, 1, "IncludeHidden:true must surface the hidden expired account")
	assert.Equal(t, hiddenExpired.AccountID, withHidden[0].ID)

	withoutHidden, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AllStatuses: true})
	require.NoError(t, err)
	assert.Empty(t, withoutHidden,
		"AllStatuses must widen the status predicate only — the is_hidden clause still applies")
}

// TestAccountRepository_GetAccountsWithInstitutionFiltered_AllStatuses_DoesNotFireReplacedCanary
// guards the canary decision. AccountsReplacedFiltered{family="accounts_list"} counts executions
// that EXCLUDE replaced accounts; an AllStatuses call excludes nothing, so counting it would make
// the canary report the opposite of what it measures.
//
// TestAccountsReplacedFilteredFamiliesMatchCallSites cannot catch this: it cross-checks label
// literals against the doc comment, and a spuriously-firing emit uses the same literal. Verified by
// mutation — restoring the unguarded `if !opts.IncludeReplaced` leaves that guard green and only
// this test red.
func TestAccountRepository_GetAccountsWithInstitutionFiltered_AllStatuses_DoesNotFireReplacedCanary(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	user := insertTestUser(t, pool)
	acct := setupHouseholdMemberAccount(t, pool, user)
	setAccountStatus(t, pool, acct.AccountID, domain.AccountStatusExpired)

	counter := observability.AccountsReplacedFiltered.WithLabelValues("accounts_list")

	before := testutil.ToFloat64(counter)
	_, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{AllStatuses: true, IncludeHidden: true})
	require.NoError(t, err)
	assert.Equal(t, before, testutil.ToFloat64(counter),
		"an AllStatuses call filters nothing out and must not increment the replaced-accounts canary")

	// Positive control: the active-only path must still increment, or this test would pass
	// vacuously the moment the call site disappeared entirely.
	_, err = repo.GetAccountsWithInstitutionFiltered(ctx, user, repository.AccountFilterOptions{})
	require.NoError(t, err)
	assert.Equal(t, before+1, testutil.ToFloat64(counter),
		"the active-only path is the canary's actual subject and must still fire")
}

// TestAccountRepository_GetAccountsWithInstitutionFiltered_ReplacedOnlyBeatsAllStatuses pins the
// documented precedence of the two contradictory status flags, and the canary's silence for both.
//
// The combination is unreachable today (accountService only ever sets ReplacedOnly for
// StatusFilter=="replaced", never together with AllStatuses), so nothing observes it in production
// — which is exactly why it needs a test: the switch matches ReplacedOnly first, so AllStatuses is
// IGNORED and active rows are dropped. Before resolveAccountStatusScope, the canary condition
// `!IncludeReplaced && !AllStatuses` read AllStatuses independently of that switch and stayed
// SILENT for this combination, so the counter whose job is catching an unexpected active-only
// filter reported nothing at all while a filter was applied. The canary is now derived from the
// resolved scope, so it cannot disagree with the query.
func TestAccountRepository_GetAccountsWithInstitutionFiltered_ReplacedOnlyBeatsAllStatuses(t *testing.T) {
	pool := setupHouseholdSchemaDB(t)
	ctx := context.Background()
	repo := repository.NewAccountRepository(pool, newTestFieldEncryptor(t))

	user := insertTestUser(t, pool)
	activeAcct := setupHouseholdMemberAccount(t, pool, user)
	replacedAcct := setupHouseholdMemberAccount(t, pool, user)
	expiredAcct := setupHouseholdMemberAccount(t, pool, user)
	setAccountStatus(t, pool, replacedAcct.AccountID, domain.AccountStatusReplaced)
	setAccountStatus(t, pool, expiredAcct.AccountID, domain.AccountStatusExpired)

	counter := observability.AccountsReplacedFiltered.WithLabelValues("accounts_list")
	before := testutil.ToFloat64(counter)

	got, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{ReplacedOnly: true, AllStatuses: true, IncludeHidden: true})
	require.NoError(t, err)
	require.Len(t, got, 1,
		"most specific wins: ReplacedOnly is matched before AllStatuses, so the result is the "+
			"replaced row only — AllStatuses does not widen it back out")
	assert.Equal(t, replacedAcct.AccountID, got[0].ID)
	assert.Equal(t, before, testutil.ToFloat64(counter),
		"a replaced-only query excludes ACTIVE rows, not replaced ones, so it must not increment a "+
			"counter documented as 'excluded at least one replaced account'")

	// Control: ReplacedOnly alone resolves to the same predicate and is equally silent. That
	// silence is a CHANGE — it used to increment, labelling a replaced-only query as the
	// accounts_list "active-only path" (a pre-existing #1109 mislabel, not from the #1537
	// all-statuses work).
	alone, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{ReplacedOnly: true, IncludeHidden: true})
	require.NoError(t, err)
	require.Len(t, alone, 1)
	assert.Equal(t, replacedAcct.AccountID, alone[0].ID)
	assert.Equal(t, before, testutil.ToFloat64(counter),
		"ReplacedOnly must not fire the replaced-accounts-filtered canary")

	// Positive control, so the two assertions above cannot pass because the counter is dead:
	// the active-only default is the canary's actual subject.
	activeOnly, err := repo.GetAccountsWithInstitutionFiltered(ctx, user,
		repository.AccountFilterOptions{IncludeHidden: true})
	require.NoError(t, err)
	require.Len(t, activeOnly, 1)
	assert.Equal(t, activeAcct.AccountID, activeOnly[0].ID)
	assert.Equal(t, before+1, testutil.ToFloat64(counter),
		"the active-only path drops the replaced row and must still fire")
}
