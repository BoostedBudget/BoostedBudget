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
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/database"
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
