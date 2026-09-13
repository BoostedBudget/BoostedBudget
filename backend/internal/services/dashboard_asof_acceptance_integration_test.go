// dashboard_asof_acceptance_integration_test.go — issue #1569's acceptance scenario, end to end
// against a real database.
//
// THE BUG AS THE ISSUE STATES IT: an account holding $1,000 thirty days ago and disconnected since
// produced a baseline of $0 and a change_percent of 0 — "no change" for a period in which real
// tracked money left. PR #1564 patched the arithmetic with the best approximation then available
// (AllStatuses); this test asserts the fixed behaviour on RECORDED liveness
// (account_status_history), through the real repositories, with no mocks anywhere in the path.
//
// It is deliberately end-to-end rather than another service-level mock test: every mock test of
// this scenario passes by construction, because the mock returns whatever universe the test
// author believed the query would return. The defect being fixed lived precisely in the gap
// between that belief and the SQL.

package services

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/database"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

func setupDashboardAsOfDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	// Issue #1461: route through database.NewPoolFromURL so this pool pins session TimeZone=UTC
	// exactly like the production pool does.
	pool, err := database.NewPoolFromURL(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func dashboardAsOfEncryptor(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	enc, err := appcrypto.NewFieldEncryptor(nil, []byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	return enc
}

// asOfFixture is one user with one account (via a user_account chain key) and its snapshots.
type asOfFixture struct {
	UserID        uuid.UUID
	LinkedItemID  uuid.UUID
	UserAccountID uuid.UUID
	AccountID     uuid.UUID
}

func seedAsOfAccount(ctx context.Context, t *testing.T, db repository.DB, prefix string) asOfFixture {
	t.Helper()
	f := asOfFixture{UserID: uuid.New(), LinkedItemID: uuid.New(), UserAccountID: uuid.New(), AccountID: uuid.New()}

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')`,
		f.UserID, "test|"+prefix+"-"+f.UserID.String(), prefix+"-"+f.UserID.String()+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`,
		f.LinkedItemID, f.UserID, prefix+"-item-"+f.LinkedItemID.String())
	require.NoError(t, err, "seed linked_item")

	_, err = db.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'depository')`,
		f.UserAccountID, f.UserID, prefix+"-inst-"+f.UserAccountID.String())
	require.NoError(t, err, "seed user_account")

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Old Bank Checking', 'depository', 'active', $5)`,
		f.AccountID, f.LinkedItemID, f.UserID, prefix+"-acct-"+f.AccountID.String(), f.UserAccountID)
	require.NoError(t, err, "seed account")

	return f
}

func seedAsOfSnapshot(ctx context.Context, t *testing.T, db repository.DB, f asOfFixture, balance float64, at time.Time) {
	t.Helper()
	_, err := db.Exec(ctx, `
		INSERT INTO balance_snapshots (id, account_id, user_account_id, user_id, current_balance, currency_code, snapshot_at, source)
		VALUES ($1, $2, $3, $4, $5, 'USD', $6, 'sync')`,
		uuid.New(), f.AccountID, f.UserAccountID, f.UserID, balance, at)
	require.NoError(t, err, "seed balance snapshot")
}

func TestDashboard_SignedDebtSummary_PreservesRawSnapshots_Integration(t *testing.T) {
	pool := setupDashboardAsOfDB(t)
	for _, accountType := range []string{"credit", "loan"} {
		for _, currentRaw := range []float64{90000, -100} {
			t.Run(fmt.Sprintf("%s/current_%g", accountType, currentRaw), func(t *testing.T) {
				ctx := context.Background()
				tx, err := pool.Begin(ctx)
				require.NoError(t, err)
				defer tx.Rollback(ctx) //nolint:errcheck
				f := seedAsOfAccount(ctx, t, tx, "signed-debt")
				_, err = tx.Exec(ctx, "UPDATE accounts SET type = $2 WHERE id = $1", f.AccountID, accountType)
				require.NoError(t, err)
				_, err = tx.Exec(ctx, "UPDATE user_accounts SET primary_type = $2 WHERE id = $1", f.UserAccountID, accountType)
				require.NoError(t, err)
				now := time.Now().UTC()
				backdateActiveInterval(ctx, t, tx, f.AccountID, now.AddDate(0, 0, -90))
				householdID := seedAsOfHouseholdAccount(ctx, t, tx, f, now.AddDate(0, 0, -90))
				seedAsOfSnapshot(ctx, t, tx, f, 100000, now.AddDate(0, 0, -31))
				seedAsOfSnapshot(ctx, t, tx, f, currentRaw, now)
				svc := NewDashboardService(repository.NewBalanceSnapshotRepository(tx), repository.NewAccountRepository(tx, dashboardAsOfEncryptor(t)))
				for _, filters := range []*domain.DashboardFilters{nil, {HouseholdScope: &domain.HouseholdScope{HouseholdID: householdID, MemberUserIDs: []string{f.UserID.String()}}}} {
					result, err := svc.GetBalanceSummary(ctx, f.UserID.String(), filters)
					require.NoError(t, err)
					assert.Equal(t, -currentRaw, result.TotalBalance)
					require.Contains(t, result.ByType, accountType)
					assert.Equal(t, -currentRaw, result.ByType[accountType].Balance)
					require.Len(t, result.ByType[accountType].Accounts, 1)
					assert.Equal(t, -currentRaw, result.ByType[accountType].Accounts[0].Balance)
					require.NotNil(t, result.PeriodComparison)
					assert.Equal(t, -100000.0, result.PeriodComparison.PreviousBalance)
					assert.Equal(t, 100000-currentRaw, result.PeriodComparison.ChangeAmount)
					assert.InDelta(t, (100000-currentRaw)/100000*100, result.PeriodComparison.ChangePercent, 0.0001)
				}
				var raw []float64
				require.NoError(t, tx.QueryRow(ctx, "SELECT array_agg(current_balance ORDER BY snapshot_at) FROM balance_snapshots WHERE account_id = $1", f.AccountID).Scan(&raw))
				assert.Equal(t, []float64{100000, currentRaw}, raw, "summary presentation must not change stored history")
			})
		}
	}
}

// backdateActiveInterval makes the account look as though it has been live since `from`, the way a
// real account of that age would. The interval itself was written by migration 000152's INSERT
// trigger; only its start is adjusted.
func backdateActiveInterval(ctx context.Context, t *testing.T, db repository.DB, accountID uuid.UUID, from time.Time) {
	t.Helper()
	ct, err := db.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2
		WHERE account_id = $1 AND status = 'active' AND valid_to IS NULL`, accountID, from)
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "the INSERT trigger must have opened one active interval")
}

// TestDashboard_DisconnectedAccount_ReportsARealDecline_Integration is the issue's acceptance test.
func TestDashboard_DisconnectedAccount_ReportsARealDecline_Integration(t *testing.T) {
	pool := setupDashboardAsOfDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedAsOfAccount(ctx, t, tx, "asof-accept")
	now := time.Now().UTC()

	// The account has existed for 90 days and held $1,000 thirty-one days ago. Then the user
	// disconnected it, and no snapshot has been written since — exactly the issue's setup.
	backdateActiveInterval(ctx, t, tx, f.AccountID, now.AddDate(0, 0, -90))
	seedAsOfSnapshot(ctx, t, tx, f, 1000.00, now.AddDate(0, 0, -31))
	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = NOW(), updated_at = NOW() WHERE id = $1`, f.AccountID)
	require.NoError(t, err, "soft-remove the account (Task 2 retains it rather than deleting)")

	svc := NewDashboardService(
		repository.NewBalanceSnapshotRepository(tx),
		repository.NewAccountRepository(tx, dashboardAsOfEncryptor(t)),
	)

	netWorth, err := svc.GetNetWorth(ctx, f.UserID.String(), nil)
	require.NoError(t, err)
	require.NotNil(t, netWorth.PeriodComparison,
		"a comparison must be produced at all — silently omitting it is the other way this bug hides")

	assert.Equal(t, 0.00, netWorth.NetWorth,
		"the retained account's frozen snapshot must NOT count toward CURRENT net worth")
	assert.InDelta(t, 1000.00, netWorth.PeriodComparison.PreviousNetWorth, 0.001,
		"the money really was there 30 days ago; a $0 baseline is the reported bug")
	assert.Negative(t, netWorth.PeriodComparison.NetWorthChange,
		"tracked money left, so the change must be negative")
	assert.NotZero(t, netWorth.PeriodComparison.NetWorthChangePercent,
		"reporting 0%% — 'no change' — for a period in which $1,000 left is the exact symptom "+
			"issue #1569 was filed for")
	assert.InDelta(t, -100.0, netWorth.PeriodComparison.NetWorthChangePercent, 0.001)

	summary, err := svc.GetBalanceSummary(ctx, f.UserID.String(), nil)
	require.NoError(t, err)
	require.NotNil(t, summary.PeriodComparison)
	assert.Equal(t, 0.00, summary.TotalBalance)
	assert.InDelta(t, 1000.00, summary.PeriodComparison.PreviousBalance, 0.001)
	assert.InDelta(t, -100.0, summary.PeriodComparison.ChangePercent, 0.001)
}

// TestDashboard_AccountOpenedAfterTheCutoff_IsAbsentFromTheBaseline_Integration is the exclusion
// half, and the one AllStatuses could never have satisfied: an account that did not exist at the
// baseline instant must not be part of the baseline's universe.
//
// The number it protects is the baseline TYPE attribution, not the sum — the balance always comes
// from a snapshot, so an account with no pre-cutoff snapshot contributes nothing either way. What
// this test pins is that the universe is honest: a snapshot written before the account's own
// recorded existence (a backdated import, a repaired row) must not be silently adopted by it.
func TestDashboard_AccountOpenedAfterTheCutoff_IsAbsentFromTheBaseline_Integration(t *testing.T) {
	pool := setupDashboardAsOfDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedAsOfAccount(ctx, t, tx, "asof-newacct")
	now := time.Now().UTC()

	// Account created just now (so its recorded active interval starts today), but carrying a
	// snapshot dated before the 30-day cutoff.
	seedAsOfSnapshot(ctx, t, tx, f, 500.00, now.AddDate(0, 0, -31))
	seedAsOfSnapshot(ctx, t, tx, f, 700.00, now)

	accountRepo := repository.NewAccountRepository(tx, dashboardAsOfEncryptor(t))
	cutoff := now.AddDate(0, 0, -30)
	universe, err := accountRepo.GetAccountsWithInstitutionFiltered(ctx, f.UserID.String(),
		repository.AccountFilterOptions{AsOf: &cutoff, IncludeHidden: true})
	require.NoError(t, err)
	assert.Empty(t, universe,
		"the account did not exist at the cutoff, so the baseline universe at that instant is "+
			"empty. AllStatuses returned it, which is why the old universe was an approximation.")

	svc := NewDashboardService(repository.NewBalanceSnapshotRepository(tx), accountRepo)
	netWorth, err := svc.GetNetWorth(ctx, f.UserID.String(), nil)
	require.NoError(t, err)
	assert.InDelta(t, 700.00, netWorth.NetWorth, 0.001,
		"the current side is unaffected — it reads live accounts, and this one is live")
	require.NotNil(t, netWorth.PeriodComparison)
	assert.InDelta(t, 500.00, netWorth.PeriodComparison.PreviousNetWorth, 0.001,
		"the $500 snapshot still resolves — through the CURRENT map, because the account is active "+
			"today. That is the established precedence (active map first, historical as fallback) "+
			"and it is why the emptiness asserted above does not, on its own, change this number. "+
			"The as-of universe is the honest answer to 'what existed then', not a second filter "+
			"on the balances.")
}

// TestDashboard_PastAsOfDate_ResolvesTheAccountUniverseAtThatInstant_Integration is #1569's
// founding symptom, in the half of the response nobody re-derived: the CURRENT figure.
//
// THE DEFECT. GetBalanceSummary and GetNetWorth read snapshots AS OF the requested date and then
// interpret them through GetByUserID — `WHERE user_id = $1 AND status = 'active'`, which is the
// universe TODAY. The BASELINE half of the very same response already does this correctly, via
// historicalAccountMap + AccountFilterOptions.AsOf. So an account that was live on the requested
// date and has since been disconnected has its snapshot silently dropped from the figure the
// response is labelled with, while the same account is correctly present in the comparison.
//
// Measured on this fixture before the fix:
//
//	(A) as_of = today,  active-today map:  1 row -> 1000.00   correct, and unchanged by this fix
//	(B) as_of = -20d,   active-today map:  0 rows ->      0   what shipped
//	(C) as_of = -20d,   as-of-live map:    1 row -> 1000.00   what the baseline already did
//
// WHY IT MATTERS MORE THAN THE NUMBER. `as_of_date` is an unvalidated public query param on
// /dashboard/balance-summary and /dashboard/net-worth, and BOTH are MCP `read` capabilities. An
// agent asked "what was my net worth on <date>" got $0 and a -100% PeriodComparison — a confident,
// well-formed, wrong answer about the user's money.
//
// THE SEAM, stated because getting it wrong is the OTHER failure mode: the as-of universe is used
// ONLY when the requested date is strictly in the past. For today it would be actively wrong —
// GetForHouseholdScopeLiveAsOf's doc comment says so directly ("passing asOf=now here would
// readmit an account removed moments ago"), and the product rule is that CURRENT totals include
// only linked/active accounts. Both halves are asserted below.
func TestDashboard_PastAsOfDate_ResolvesTheAccountUniverseAtThatInstant_Integration(t *testing.T) {
	pool := setupDashboardAsOfDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedAsOfAccount(ctx, t, tx, "asof-past-universe")
	now := time.Now().UTC()

	// Live for 90 days, holding $1,000 twenty days ago, disconnected ten days ago. So on the
	// requested date the account was live and the money was tracked; today it is neither.
	backdateActiveInterval(ctx, t, tx, f.AccountID, now.AddDate(0, 0, -90))
	// TWO snapshots, and the older one is not decoration. The baseline for an as_of of -20d is
	// -50d, so with only the -20d snapshot GetSnapshotsAsOfForUser(endOfDay(-50d)) returns nothing,
	// PeriodComparison comes back NIL, and any assertion about ChangePercent guarded by a nil check
	// never runs. That is precisely how the first draft of this test hid the headline claim behind
	// an `if` that was always false — the seventh vacuity class this plan has turned up, in the
	// test for the defect it was written to pin. The -60d snapshot gives the baseline something to
	// resolve so the comparison is non-nil and can be asserted unconditionally.
	seedAsOfSnapshot(ctx, t, tx, f, 500.00, now.AddDate(0, 0, -60))
	seedAsOfSnapshot(ctx, t, tx, f, 1000.00, now.AddDate(0, 0, -20))
	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = $2, updated_at = $2 WHERE id = $1`,
		f.AccountID, now.AddDate(0, 0, -10))
	require.NoError(t, err, "soft-remove the account ten days ago")
	// Close the recorded interval at the same instant, so the account is genuinely live at -20d and
	// genuinely not live today. Without this the fixture would pass on "has any active interval"
	// rather than on point-in-time liveness, which is a weaker claim than the one being made.
	ct, err := tx.Exec(ctx, `
		UPDATE account_status_history SET valid_to = $2
		WHERE account_id = $1 AND status = 'active'`,
		f.AccountID, now.AddDate(0, 0, -10))
	require.NoError(t, err)
	// `valid_to IS NULL` is NOT in that predicate, and the omission is deliberate: the UPDATE above
	// already fired the status trigger, which closed the 'active' interval at clock_timestamp(). A
	// `valid_to IS NULL` clause here matches nothing and updates zero rows — silently, since Exec
	// reports no error for it — leaving the interval closed at NOW rather than at -10d. The test
	// would still pass (now > -20d, so the account is still live at the as-of instant), but it
	// would be passing on "has any active interval" rather than on point-in-time liveness, which is
	// a weaker claim than the one this test makes. RowsAffected is what stops that being silent.
	require.EqualValues(t, 1, ct.RowsAffected(), "the trigger must have recorded exactly one active interval")

	svc := NewDashboardService(
		repository.NewBalanceSnapshotRepository(tx),
		repository.NewAccountRepository(tx, dashboardAsOfEncryptor(t)),
	)

	// ── (B)/(C): a PAST as-of date must be interpreted by the universe of that date. ──────
	pastDate := now.AddDate(0, 0, -20)
	past := &domain.DashboardFilters{AsOfDate: &pastDate}

	summary, err := svc.GetBalanceSummary(ctx, f.UserID.String(), past)
	require.NoError(t, err)
	assert.InDelta(t, 1000.00, summary.TotalBalance, 0.001,
		"the money WAS there on the requested date; answering 0 for a past date is the bug")
	// assert, not require: a fatal here would abort before the PeriodComparison assertions below,
	// which are the ones carrying the MCP claim. Under the pre-fix mutant every one of these fails,
	// and all of them must be SEEN to fail — a require would hide the interesting half.
	assert.NotEmpty(t, summary.ByType,
		"the breakdown must carry the account too — an empty ByType beside a non-zero total would "+
			"mean the account was counted without being interpreted")

	netWorth, err := svc.GetNetWorth(ctx, f.UserID.String(), past)
	require.NoError(t, err)
	assert.InDelta(t, 1000.00, netWorth.NetWorth, 0.001,
		"net worth on a past date is a question about that date's universe")
	// The MCP symptom, asserted rather than guarded away. $500 at the -50d baseline and $1,000 on
	// the requested date is a REAL +100% rise. Before the fix the current side read 0 against the
	// same $500 baseline, so an agent asked "what was my net worth on <date>" was told -100% —
	// everything gone — about an account that was simply disconnected afterwards.
	require.NotNil(t, netWorth.PeriodComparison,
		"a comparison must be produced at all; a nil one is how this assertion silently stopped "+
			"running in the first draft of this test")
	require.NotNil(t, summary.PeriodComparison)
	assert.InDelta(t, 500.00, netWorth.PeriodComparison.PreviousNetWorth, 0.001,
		"the baseline is the -60d snapshot, unaffected by this fix")
	assert.InDelta(t, 100.0, netWorth.PeriodComparison.NetWorthChangePercent, 0.001,
		"a REAL +100%% rise. -100%% is the artefact of the current side reading 0")

	assert.InDelta(t, 100.0, summary.PeriodComparison.ChangePercent, 0.001,
		"the balance-summary comparison carries the same symptom and the same fix")

	// ── (A): TODAY is unchanged. The as-of universe must NOT readmit the removed account. ──
	today, err := svc.GetBalanceSummary(ctx, f.UserID.String(), nil)
	require.NoError(t, err)
	assert.InDelta(t, 0.00, today.TotalBalance, 0.001,
		"a disconnected account must stay out of the CURRENT total — the product rule task 20 "+
			"confirmed. If this flips, the fix has been applied at the wrong seam.")

	todayExplicit := now
	todaySummary, err := svc.GetBalanceSummary(ctx, f.UserID.String(), &domain.DashboardFilters{AsOfDate: &todayExplicit})
	require.NoError(t, err)
	assert.InDelta(t, 0.00, todaySummary.TotalBalance, 0.001,
		"asking for TODAY explicitly is still a current-figure question, not a historical one")
}

// seedAsOfHouseholdAccount extends seedAsOfAccount with the household linkage the household-scope
// dashboard reads require: a household the user owns and is an active member of, and an active
// 'shared' grant over their user_account. Returns the household id.
func seedAsOfHouseholdAccount(ctx context.Context, t *testing.T, db repository.DB, f asOfFixture, sharedAt time.Time) string {
	t.Helper()
	var householdID string
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO households (owner_user_id, name) VALUES ($1, 'As-Of Household') RETURNING id::text
	`, f.UserID).Scan(&householdID), "seed household")

	_, err := db.Exec(ctx, `
		INSERT INTO household_members (household_id, user_id, role, status)
		VALUES ($1, $2, 'owner', 'active')`, householdID, f.UserID)
	require.NoError(t, err, "seed household_member")

	_, err = db.Exec(ctx, `
		INSERT INTO household_account_shares (household_id, user_account_id, visibility, shared_at)
		VALUES ($1, $2, 'shared', $3)`, householdID, f.UserAccountID, sharedAt)
	require.NoError(t, err, "seed household_account_share")

	return householdID
}

// TestDashboardHousehold_PastAsOfDate_ResolvesTheAccountUniverseAtThatInstant_Integration is C2's
// household half.
//
// It is a SEPARATE defect site, not a corollary of the personal one: the household paths do not
// call GetByUserID at all. They call GetForHouseholdScopeActiveNow, whose own doc comment states
// that its date argument scopes the SHARE GRANTS and the joint-account winner and NOT account
// status — "an account disconnected since grantsAsOf is absent however live it was then". So the
// same wrong number arrives by a different route, and fixing only the personal path would have
// left every household user with it.
//
// The correct source already existed and was already used by the BASELINE leg of these same two
// responses (GetForHouseholdScopeLiveAsOf, reached via the historical fallback). As with the
// personal path, the current leg was the half nobody re-derived.
func TestDashboardHousehold_PastAsOfDate_ResolvesTheAccountUniverseAtThatInstant_Integration(t *testing.T) {
	pool := setupDashboardAsOfDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedAsOfAccount(ctx, t, tx, "asof-hh-universe")
	now := time.Now().UTC()
	householdID := seedAsOfHouseholdAccount(ctx, t, tx, f, now.AddDate(0, 0, -90))

	backdateActiveInterval(ctx, t, tx, f.AccountID, now.AddDate(0, 0, -90))
	seedAsOfSnapshot(ctx, t, tx, f, 1000.00, now.AddDate(0, 0, -20))
	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = $2, updated_at = $2 WHERE id = $1`,
		f.AccountID, now.AddDate(0, 0, -10))
	require.NoError(t, err)
	ct, err := tx.Exec(ctx, `
		UPDATE account_status_history SET valid_to = $2
		WHERE account_id = $1 AND status = 'active'`,
		f.AccountID, now.AddDate(0, 0, -10))
	require.NoError(t, err)
	// `valid_to IS NULL` is NOT in that predicate, and the omission is deliberate: the UPDATE above
	// already fired the status trigger, which closed the 'active' interval at clock_timestamp(). A
	// `valid_to IS NULL` clause here matches nothing and updates zero rows — silently, since Exec
	// reports no error for it — leaving the interval closed at NOW rather than at -10d. The test
	// would still pass (now > -20d, so the account is still live at the as-of instant), but it
	// would be passing on "has any active interval" rather than on point-in-time liveness, which is
	// a weaker claim than the one this test makes. RowsAffected is what stops that being silent.
	require.EqualValues(t, 1, ct.RowsAffected(), "the trigger must have recorded exactly one active interval")

	svc := NewDashboardService(
		repository.NewBalanceSnapshotRepository(tx),
		repository.NewAccountRepository(tx, dashboardAsOfEncryptor(t)),
	)
	scope := &domain.HouseholdScope{HouseholdID: householdID, MemberUserIDs: []string{f.UserID.String()}}

	pastDate := now.AddDate(0, 0, -20)
	past := &domain.DashboardFilters{AsOfDate: &pastDate, HouseholdScope: scope}

	summary, err := svc.GetBalanceSummary(ctx, f.UserID.String(), past)
	require.NoError(t, err)
	assert.InDelta(t, 1000.00, summary.TotalBalance, 0.001,
		"the household held this money on the requested date")

	netWorth, err := svc.GetNetWorth(ctx, f.UserID.String(), past)
	require.NoError(t, err)
	assert.InDelta(t, 1000.00, netWorth.NetWorth, 0.001,
		"household net worth on a past date is a question about that date's universe")

	// The seam holds here too: TODAY must not readmit the disconnected account.
	todayFilters := &domain.DashboardFilters{HouseholdScope: scope}
	today, err := svc.GetBalanceSummary(ctx, f.UserID.String(), todayFilters)
	require.NoError(t, err)
	assert.InDelta(t, 0.00, today.TotalBalance, 0.001,
		"a disconnected account stays out of the household's CURRENT total")
}

// TestDashboard_PastAsOfDate_MergeDirection_DecidesInclusionUnderATypeFilter pins the ADD-ONLY
// merge direction of augmentWithAsOfUniverse, at the one place where the direction changes a
// TOTAL rather than a label.
//
// I originally graded this as "type attribution only, changes no total, therefore not pinned". That
// was wrong, and the review caught it. The account map does not merely decorate a snapshot: it
// gates INCLUSION, because `filters.AccountTypes` is applied to `account.Type` after the lookup
// (GetBalanceSummary / GetNetWorth). For a chain that re-linked AFTER the requested date, the
// current map holds the SUCCESSOR and the as-of map holds the PREDECESSOR — and if their types
// differ, the merge direction decides whether that chain's money is counted at all.
//
// WHY THE CASE IS REACHABLE AT ALL, since it is narrow enough to look impossible. The augmentation
// short-circuits when every snapshot already resolves in the current map, so a lone re-linked chain
// never triggers a fetch. It takes a SECOND chain — one fully disconnected, resolving nowhere — to
// force the as-of read that then also returns the re-linked chain's predecessor. Both chains are in
// this fixture for that reason; deleting the disconnected one would make this test pass under
// either direction and prove nothing.
//
// THE DIRECTION KEPT, and its cost stated plainly. Add-only: an account that is active today keeps
// its current-map entry, so the chain is interpreted by its SUCCESSOR. That is the same precedence
// baselineBalanceForComparison already applies to the baseline leg of this same response, and
// having the two legs resolve one chain differently is a class of bug this file has been bitten by
// twice (#1369 G1, #1537 round 10). The cost is real: a historical figure is attributed to the
// successor's type, ID and name. Preferring the member that was live THEN is arguably more honest
// for a historical read, but it is a change that must move BOTH legs together, and it is not this
// task's to make. It is stated here rather than left for the next reader to discover.
func TestDashboard_PastAsOfDate_MergeDirection_DecidesInclusionUnderATypeFilter(t *testing.T) {
	pool := setupDashboardAsOfDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	now := time.Now().UTC()
	asOf := now.AddDate(0, 0, -20)

	// ── Chain X: fully disconnected. Resolves in NO current map, so it forces the as-of fetch. ──
	x := seedAsOfAccount(ctx, t, tx, "merge-dir-x")
	backdateActiveInterval(ctx, t, tx, x.AccountID, now.AddDate(0, 0, -90))
	seedAsOfSnapshot(ctx, t, tx, x, 1000.00, asOf)
	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'removed', removed_at = $2, updated_at = $2 WHERE id = $1`,
		x.AccountID, now.AddDate(0, 0, -10))
	require.NoError(t, err)
	ct, err := tx.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2, valid_to = $3
		WHERE account_id = $1 AND status = 'active'`,
		x.AccountID, now.AddDate(0, 0, -90), now.AddDate(0, 0, -10))
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected())

	// ── Chain Y, same user: re-linked FIVE days ago, i.e. AFTER the requested date. ──────────
	// Y1 ('investment') was live at asOf; Y2 ('depository') is active today. They share a chain key,
	// so the snapshot below resolves to Y2 through the current map and to Y1 through the as-of map.
	yUserAccountID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, $3, 'depository')`, yUserAccountID, x.UserID, "merge-dir-y-inst-"+yUserAccountID.String())
	require.NoError(t, err)

	y1, y2 := uuid.New(), uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Old Brokerage', 'investment', 'active', $5)`,
		y1, x.LinkedItemID, x.UserID, "merge-dir-y1-"+y1.String(), yUserAccountID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'New Checking', 'depository', 'active', $5)`,
		y2, x.LinkedItemID, x.UserID, "merge-dir-y2-"+y2.String(), yUserAccountID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `
		INSERT INTO balance_snapshots (id, account_id, user_account_id, user_id, current_balance, currency_code, snapshot_at, source)
		VALUES ($1, $2, $3, $4, 300.00, 'USD', $5, 'sync')`,
		uuid.New(), y1, yUserAccountID, x.UserID, asOf)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		UPDATE accounts SET status = 'replaced', replaced_at = $2, replaced_by_id = $3, updated_at = $2
		WHERE id = $1`, y1, now.AddDate(0, 0, -5), y2)
	require.NoError(t, err)
	ct, err = tx.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2, valid_to = $3
		WHERE account_id = $1 AND status = 'active'`,
		y1, now.AddDate(0, 0, -90), now.AddDate(0, 0, -5))
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "Y1 must have exactly one recorded active interval")

	svc := NewDashboardService(
		repository.NewBalanceSnapshotRepository(tx),
		repository.NewAccountRepository(tx, dashboardAsOfEncryptor(t)),
	)

	// Precondition: unfiltered, both chains count either way. If this is not 1300 the fixture is
	// wrong and every assertion below would be measuring something else.
	unfiltered, err := svc.GetBalanceSummary(ctx, x.UserID.String(), &domain.DashboardFilters{AsOfDate: &asOf})
	require.NoError(t, err)
	require.InDelta(t, 1300.00, unfiltered.TotalBalance, 0.001,
		"fixture precondition: both chains resolve at the requested date")

	// The assertion the direction decides. Add-only -> Y is interpreted by the SUCCESSOR Y2
	// ('depository'), so it passes the filter: 1000 + 300. Overwrite -> Y is interpreted by the
	// PREDECESSOR Y1 ('investment'), the type filter drops it, and the total is 1000.
	filtered, err := svc.GetBalanceSummary(ctx, x.UserID.String(), &domain.DashboardFilters{
		AsOfDate:     &asOf,
		AccountTypes: []string{"depository"},
	})
	require.NoError(t, err)
	assert.InDelta(t, 1300.00, filtered.TotalBalance, 0.001,
		"add-only: a chain that is active today keeps its current-map entry, so the type filter "+
			"sees the successor's type. An overwriting merge would resolve it to the predecessor "+
			"and drop $300 from the total — the merge direction is not a labelling choice")
}

// TestDashboard_RemovedThenRestoredAccount_KeepsItsPreRemovalBaseline_Integration is the INVERSE of
// #1569's founding symptom, and nothing covered it: an account that is active TODAY, carries a
// LEGITIMATE pre-removal snapshot, and has a RECORDED GAP IN LIVENESS that straddles the baseline
// instant.
//
// It is materially different from
// TestDashboard_AccountOpenedAfterTheCutoff_IsAbsentFromTheBaseline_Integration, which shares the
// same map precedence but has no legitimate pre-cutoff history (that test says so itself: the
// account "contributes nothing either way"). Here the pre-cutoff snapshot is real money, really
// held, by an account that really existed — and the recorded as-of universe at the baseline instant
// is nevertheless EMPTY, because the account was in a 'removed' interval right then.
//
// ── THE EXPECTED VALUE, DECIDED AND WRITTEN DOWN BEFORE THE FIRST RUN ────────────────────────────
// Predicted: the as-of universe at the baseline instant is EMPTY, and PreviousNetWorth is
// nonetheless $1,000 — the pre-removal snapshot — for a real +20% against today's $1,200.
//
// And that is the CORRECT answer, not merely the one the code produces. Three reasons, in
// descending order of how load-bearing they are:
//
//  1. THE REMOVAL WAS NOT A FACT ABOUT THE MONEY. `removed_reason='provider_absent'` means the
//     bank stopped reporting the account; RestoreProviderRemovedAccount exists precisely to say
//     "that removal was never real" and REFUSES every other cause (a 'user_disconnect' or a legacy
//     NULL gets ErrAccountNotRestorable and is never undone by a sync). The gap in
//     account_status_history is a gap in OUR OBSERVATION, not in the account's existence. Excluding
//     the balance would let a provider outage erase the user's money from their own history.
//  2. THE BALANCE NEVER COMES FROM THE MAP. baselineBalanceForComparison and GetNetWorth's baseline
//     loop take every figure from the snapshot row; the account map decides only inclusion and
//     TYPE. So the question this test asks is genuinely "should a snapshot be dropped because the
//     account was mid-outage at the cutoff", and the answer is no — the snapshot is a fact about
//     -60d, an instant at which the account was indisputably live.
//  3. EXCLUDING WOULD RE-CREATE THE FOUNDING BUG WITH THE SIGN FLIPPED. Baseline $0 against a
//     current $1,200 is not "+20%"; it is an infinite rise, and the divide-by-zero guard would
//     report 0% — "no change" — which is the exact string of characters issue #1569 was filed over.
//
// So a RED here would be a defect in my prediction, not in the product: the failure mode worth
// fearing on this path is the baseline going to ZERO, not it including too much.
//
// ⚠ WHAT IS PINNED IS BROADER THAN THE JUSTIFICATION GIVEN FOR IT. Reason 1 leans on
// 'provider_absent', but the baseline path never reads removed_reason OR account_status_history for
// this account: the current map hits, historicalAccountMap short-circuits (dashboard_service.go
// :1061-1063), and sfAccountsAsOf is never issued. The retention asserted below therefore holds for
// ANY account that is status='active' today, whatever cause removed it. Unreachable now —
// RestoreProviderRemovedAccount is the only production write returning accounts.status to 'active'
// (account_repository.go:1980; no production caller passes AccountStatusActive to UpdateStatus), and
// it refuses every cause but 'provider_absent'. But a NEW write path to 'active' would inherit this
// retention WITHOUT inheriting the cause check, and this test would keep passing.
//
// What this test therefore pins is a JOINT property that no single existing test states: the as-of
// universe and the baseline SUM are allowed to disagree about this account, and must. The universe
// is the honest answer to "what did we know then"; the baseline is the honest answer to "how much
// money was there". Anything that turns the first into a filter on the second breaks this — and it
// breaks it toward ZERO, not toward over-counting: with the restore skipped (the same account, not
// active today) the short-circuit stops firing, sfAccountsAsOf(previousCutoff) IS issued, returns
// empty because the account was mid-outage, and every figure below goes to 0.00. Measured, not
// reasoned.
//
// ⚠ LIMIT, stated so nobody over-reads the mutant set: assertion 2 is INVARIANT to the liveness gap.
// Because the current-map hit short-circuits historicalAccountMap, no account_status_history
// mutation can move PreviousNetWorth — only assertion 1 responds to the gap. The restore's necessity
// is guarded by the require.Equal("active", status) check below, not by any history mutation.
func TestDashboard_RemovedThenRestoredAccount_KeepsItsPreRemovalBaseline_Integration(t *testing.T) {
	pool := setupDashboardAsOfDB(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	f := seedAsOfAccount(ctx, t, tx, "asof-restored")
	now := time.Now().UTC()

	// Live for 90 days, holding $1,000 sixty days ago (well before the 30-day baseline instant, and
	// at an instant the account was unambiguously live), $1,200 today.
	backdateActiveInterval(ctx, t, tx, f.AccountID, now.AddDate(0, 0, -90))
	seedAsOfSnapshot(ctx, t, tx, f, 1000.00, now.AddDate(0, 0, -60))
	seedAsOfSnapshot(ctx, t, tx, f, 1200.00, now)

	// ── The provider stopped reporting the account 45 days ago. ──────────────────────────────
	// removed_reason MUST be 'provider_absent': RestoreProviderRemovedAccount accepts nothing else.
	_, err = tx.Exec(ctx, `
		UPDATE accounts
		SET status = 'removed', removed_at = $2, removed_reason = 'provider_absent', updated_at = $2
		WHERE id = $1`, f.AccountID, now.AddDate(0, 0, -45))
	require.NoError(t, err, "provider-absent removal")

	// Backdate the recorded transition; the trigger stamped clock_timestamp() on both halves.
	//
	// ⚠ STATEMENT 1 IS THE LOAD-BEARING ONE, and an earlier version of this comment said otherwise.
	// Closing the 'active' interval at -45d is what stops it covering the baseline instant, and
	// accountLiveAsOfPredicate (account_repository.go:1567-1569) tests ACTIVE intervals ONLY — so
	// the unrecorded hole statement 1 opens reads "not live" exactly as strongly as a 'removed' row
	// would. VERIFIED BY DELETION, both directions: drop statement 1 and require.False(liveAtBaseline)
	// below fires; drop statement 2 and the whole test still passes, unchanged. That precondition is
	// the guard that stops a future edit quietly removing statement 1 — which is precisely the edit
	// the old comment made look safe, by crediting statement 2 with the property statement 1
	// supplies.
	//
	// STATEMENT 2 buys contiguity and realism, NOT the property under test: with it the recorded
	// history is gapless (active[-90d,-45d) -> removed[-45d,now) -> active[now,inf)); without it the
	// -45d..now stretch is simply unrecorded. Nothing in the schema wants contiguity — 000151
	// declares only chk_ash_interval and uq_ash_open, and no overlap or gap constraint exists. It is
	// kept because a fixture modelling a provider outage ought to record the outage.
	//
	// Their ORDER is immaterial, verified by reversing them: chk_ash_interval is a PER-ROW CHECK
	// (000151_create_account_status_history.up.sql:49) and statement 2's target row has
	// valid_to IS NULL, which short-circuits it. The previous claim here — that closing before
	// opening avoided a momentary violation — was invented; neither ordering can violate anything.
	ct, err := tx.Exec(ctx, `
		UPDATE account_status_history SET valid_to = $2
		WHERE account_id = $1 AND status = 'active'`, f.AccountID, now.AddDate(0, 0, -45))
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "exactly one recorded active interval to close")
	ct, err = tx.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2
		WHERE account_id = $1 AND status = 'removed' AND valid_to IS NULL`,
		f.AccountID, now.AddDate(0, 0, -45))
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "exactly one open removed interval to backdate")

	// ── The bank starts reporting it again today, and the sync restores it. ──────────────────
	// This runs the REAL repository method, not hand-written SQL, because the cause check
	// ('provider_absent' and nothing else) is the reason this scenario is reachable at all.
	// r.db.Begin(ctx) on a pgx.Tx opens a SAVEPOINT, so the nesting inside this test's outer tx is
	// legitimate — the restore commits to the savepoint and stays inside the rollback below.
	accountRepo := repository.NewAccountRepository(tx, dashboardAsOfEncryptor(t))
	require.NoError(t,
		accountRepo.RestoreProviderRemovedAccount(ctx, f.AccountID.String(), f.UserID.String()),
		"a provider_absent removal is restorable; if this errors the fixture is not the scenario")

	var status string
	var removedAt *time.Time
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT status, removed_at FROM accounts WHERE id = $1`, f.AccountID).Scan(&status, &removedAt))
	require.Equal(t, "active", status, "restored, so the CURRENT-map precedence is the one under test")
	require.Nil(t, removedAt, "the restore clears removed_at; a live row must not claim a removal date")

	// The gap is real and recorded: three intervals, and none of the 'active' ones covers -30d.
	baselineInstant := endOfDay(now.Truncate(24*time.Hour).AddDate(0, 0, -30))
	var liveAtBaseline bool
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM account_status_history h
		               WHERE h.account_id = $1 AND h.status = 'active'
		                 AND h.valid_from <= $2 AND (h.valid_to IS NULL OR h.valid_to > $2))`,
		f.AccountID, baselineInstant).Scan(&liveAtBaseline))
	require.False(t, liveAtBaseline,
		"fixture precondition: the removal interval must straddle the baseline instant, or this "+
			"test is the already-covered always-live case wearing a different name")

	// ── Assertion 1: the as-of universe is EMPTY at the baseline instant. ────────────────────
	universe, err := accountRepo.GetAccountsWithInstitutionFiltered(ctx, f.UserID.String(),
		repository.AccountFilterOptions{AsOf: &baselineInstant, IncludeHidden: true})
	require.NoError(t, err)
	assert.Empty(t, universe,
		"recorded liveness says the account was mid-outage at the baseline instant, and the as-of "+
			"universe reports that honestly")

	// ── Assertion 2: the baseline SUM keeps the money anyway. ────────────────────────────────
	svc := NewDashboardService(repository.NewBalanceSnapshotRepository(tx), accountRepo)

	netWorth, err := svc.GetNetWorth(ctx, f.UserID.String(), nil)
	require.NoError(t, err)
	assert.InDelta(t, 1200.00, netWorth.NetWorth, 0.001,
		"the account is live again, so today's figure is today's snapshot")
	require.NotNil(t, netWorth.PeriodComparison,
		"a nil comparison would silently skip every assertion below — the vacuity class this file "+
			"has already been bitten by")
	assert.InDelta(t, 1000.00, netWorth.PeriodComparison.PreviousNetWorth, 0.001,
		"the $1,000 was really held at -60d by an account that was really live then. A provider "+
			"outage across the baseline instant is a gap in what we OBSERVED, not in what the user "+
			"OWNED, and RestoreProviderRemovedAccount is the system stating exactly that. A $0 "+
			"here would be #1569's founding bug with the sign flipped")
	assert.InDelta(t, 200.00, netWorth.PeriodComparison.NetWorthChange, 0.001)
	assert.InDelta(t, 20.0, netWorth.PeriodComparison.NetWorthChangePercent, 0.001,
		"a real +20%. The 0% a zero baseline would produce via the divide-by-zero guard is the "+
			"exact symptom this issue was filed for")

	summary, err := svc.GetBalanceSummary(ctx, f.UserID.String(), nil)
	require.NoError(t, err)
	require.NotNil(t, summary.PeriodComparison)
	assert.InDelta(t, 1200.00, summary.TotalBalance, 0.001)
	assert.InDelta(t, 1000.00, summary.PeriodComparison.PreviousBalance, 0.001,
		"the balance-summary baseline resolves through the same precedence and must agree — the "+
			"two endpoints disagreeing about one account is its own bug class here")
	assert.InDelta(t, 20.0, summary.PeriodComparison.ChangePercent, 0.001)
}
