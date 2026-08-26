//go:build integration

package repository

// Integration tests for the per-account READ side of balance_snapshots (Issue #1569).
//
// These have to run against real Postgres. Every claim here is about what the database does with
// the read statement — that DISTINCT ON collapses a busy day to its freshest row, that the UTC day
// derivation buckets an instant the way the aggregated reads do, that the inclusive bounds behave,
// and above all that the read returns rows for an account the CURRENT-STATE reads exclude.
// pgxmock asserts query text and never executes it, so it can confirm none of that.
//
// FIXTURES GO THROUGH PRODUCTION WRITE PATHS where one exists: every snapshot below is written by
// balanceSnapshotRepository.Create (the same method the sync calls) and every account removal by
// accountRepository.MarkRemovedByLinkedItemID.
//
// Run with:
//
//	DATABASE_URL=postgres://devuser:devpass@localhost:5434/freshcheck?sslmode=disable \
//	  go test ./internal/repository/... -run TestAccountBalanceHistoryRead_ -tags integration -p 1 -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// seedDepositoryFixture creates a user, a linked item and ONE checking account. Deliberately not
// seedInvestmentFixture: privacy policy §8.2a's balance clause is about every account type, and
// the surface this feeds is reached for a closed checking account too — which is the case with no
// holdings history at all.
func seedDepositoryFixture(ctx context.Context, t *testing.T, db DB, prefix string) (userID, linkedItemID, accountID uuid.UUID) {
	t.Helper()
	userID, linkedItemID, accountID = uuid.New(), uuid.New(), uuid.New()

	_, err := db.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')`,
		userID, "test|"+prefix+"-"+userID.String(), prefix+"-"+userID.String()+"@example.com")
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')`,
		linkedItemID, userID, prefix+"-item-"+linkedItemID.String())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, subtype, status)
		VALUES ($1, $2, $3, $4, 'Test Checking', 'depository', 'checking', 'active')`,
		accountID, linkedItemID, userID, prefix+"-acct-"+accountID.String())
	require.NoError(t, err)

	return
}

func balSnap(userID, accountID uuid.UUID, at time.Time, current, available float64) *domain.BalanceSnapshot {
	c, a := current, available
	return &domain.BalanceSnapshot{
		UserID:           userID.String(),
		AccountID:        accountID.String(),
		CurrentBalance:   &c,
		AvailableBalance: &a,
		CurrencyCode:     "USD",
		SnapshotAt:       at,
		Source:           domain.SnapshotSourcePlaidWebhook,
	}
}

// ── The defect this closes: a closed account's day-by-day balances are unreachable ────────────

func TestAccountBalanceHistoryRead_ClosedAccount_HistoryStaysReadableWhileCurrentStateIsEmpty(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountID := seedDepositoryFixture(ctx, t, tx, "bal-hist-closed")

	repo := NewBalanceSnapshotRepository(tx)
	d1 := time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 3, 3, 14, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Create(ctx, balSnap(userID, accountID, d1, 1000, 900)))
	require.NoError(t, repo.Create(ctx, balSnap(userID, accountID, d2, 1200, 1100)))
	require.NoError(t, repo.Create(ctx, balSnap(userID, accountID, d3, 800, 700)))

	// The user disconnects the bank. Production path, with the cause stamped.
	accountRepo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	require.NoError(t, accountRepo.MarkRemovedByLinkedItemID(ctx, linkedItemID.String(), domain.RemovalReasonUserDisconnect))

	// CURRENT-STATE universe: the account carries no balance at all, and that stays exactly as it
	// was. This endpoint does not widen it by one row.
	accounts, err := accountRepo.GetByUserID(ctx, userID.String())
	require.NoError(t, err)
	for _, a := range accounts {
		if a.ID == accountID.String() {
			assert.Nil(t, a.CurrentBalance, "a terminal account must still present NO current balance")
		}
	}

	// HISTORICAL universe: intact. This is the sentence privacy policy §8.2a promises.
	points, err := repo.GetAccountBalanceHistory(ctx, userID.String(), accountID.String(), nil, nil)
	require.NoError(t, err)
	require.Len(t, points, 3, "every retained day must still be readable for a disconnected account")

	assert.Equal(t, []string{"2026-03-01", "2026-03-02", "2026-03-03"},
		[]string{points[0].Date, points[1].Date, points[2].Date}, "oldest day first")
	require.NotNil(t, points[1].CurrentBalance)
	assert.Equal(t, float64(1200), *points[1].CurrentBalance)
	require.NotNil(t, points[1].AvailableBalance)
	assert.Equal(t, float64(1100), *points[1].AvailableBalance)
	assert.Equal(t, "USD", points[2].CurrencyCode)
}

// ── Tenancy, in SQL, on the one statement ─────────────────────────────────────────────────────

func TestAccountBalanceHistoryRead_ForeignUser_SeesNothing(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	ownerID, _, accountID := seedDepositoryFixture(ctx, t, tx, "bal-hist-owner")
	intruderID, _, _ := seedDepositoryFixture(ctx, t, tx, "bal-hist-intruder")

	repo := NewBalanceSnapshotRepository(tx)
	require.NoError(t, repo.Create(ctx, balSnap(ownerID, accountID,
		time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC), 1000, 900)))

	mine, err := repo.GetAccountBalanceHistory(ctx, ownerID.String(), accountID.String(), nil, nil)
	require.NoError(t, err)
	require.Len(t, mine, 1)

	theirs, err := repo.GetAccountBalanceHistory(ctx, intruderID.String(), accountID.String(), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, theirs,
		"account_id alone must never be enough: user_id is on the statement, and a foreign account "+
			"answers the same way an account with no history does")
}

// ── One point per UTC day, freshest row winning ───────────────────────────────────────────────

// balance_snapshots has no per-day uniqueness key on purpose (see Create's doc comment), so a busy
// day holds several rows. The series must carry ONE point per day, chosen the same way
// GetAggregatedHistory's second stage chooses — otherwise the per-account chart and the dashboard
// chart disagree about the same day.
func TestAccountBalanceHistoryRead_CollapsesABusyDayToItsFreshestRow(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID := seedDepositoryFixture(ctx, t, tx, "bal-hist-busy")
	repo := NewBalanceSnapshotRepository(tx)

	day := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Create(ctx, balSnap(userID, accountID, day.Add(2*time.Hour), 100, 100)))
	require.NoError(t, repo.Create(ctx, balSnap(userID, accountID, day.Add(9*time.Hour), 250, 250)))
	require.NoError(t, repo.Create(ctx, balSnap(userID, accountID, day.Add(23*time.Hour), 375, 375)))

	points, err := repo.GetAccountBalanceHistory(ctx, userID.String(), accountID.String(), nil, nil)
	require.NoError(t, err)
	require.Len(t, points, 1, "three snapshots on one day are one point, not three")
	assert.Equal(t, "2026-03-04", points[0].Date)
	require.NotNil(t, points[0].CurrentBalance)
	assert.Equal(t, float64(375), *points[0].CurrentBalance, "the day's freshest snapshot wins")
}

// The day is derived as (snapshot_at AT TIME ZONE 'UTC')::date, matching every other daily read in
// this file. A bare ::date cast would follow the session TimeZone and re-bucket instants near
// midnight — so a 23:30Z instant must file under its UTC day, not the next or previous one.
func TestAccountBalanceHistoryRead_BucketsByUTCDayNotSessionTimeZone(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID := seedDepositoryFixture(ctx, t, tx, "bal-hist-tz")
	repo := NewBalanceSnapshotRepository(tx)
	require.NoError(t, repo.Create(ctx, balSnap(userID, accountID,
		time.Date(2026, 3, 5, 23, 30, 0, 0, time.UTC), 500, 500)))

	// A session in a zone behind UTC would put that instant on 2026-03-05 under a bare ::date
	// cast, and one ahead of UTC on 2026-03-06. It must be 2026-03-05 under both.
	for _, zone := range []string{"America/New_York", "Asia/Tokyo"} {
		_, err = tx.Exec(ctx, `SET LOCAL TimeZone = '`+zone+`'`)
		require.NoError(t, err)

		points, err := repo.GetAccountBalanceHistory(ctx, userID.String(), accountID.String(), nil, nil)
		require.NoError(t, err)
		require.Len(t, points, 1, "zone %s", zone)
		assert.Equal(t, "2026-03-05", points[0].Date, "the UTC day must not move with the session zone (%s)", zone)
	}
	_, err = tx.Exec(ctx, `SET LOCAL TimeZone = 'UTC'`)
	require.NoError(t, err)
}

// ── Inclusive bounds, and NULL meaning unbounded ──────────────────────────────────────────────

func TestAccountBalanceHistoryRead_BoundsAreInclusiveAndNilIsUnbounded(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID := seedDepositoryFixture(ctx, t, tx, "bal-hist-bounds")
	repo := NewBalanceSnapshotRepository(tx)
	for i := 1; i <= 5; i++ {
		require.NoError(t, repo.Create(ctx, balSnap(userID, accountID,
			time.Date(2026, 4, i, 12, 0, 0, 0, time.UTC), float64(100*i), float64(100*i))))
	}

	all, err := repo.GetAccountBalanceHistory(ctx, userID.String(), accountID.String(), nil, nil)
	require.NoError(t, err)
	require.Len(t, all, 5, "nil bounds must bind SQL NULL and read everything")

	start := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC)
	windowed, err := repo.GetAccountBalanceHistory(ctx, userID.String(), accountID.String(), &start, &end)
	require.NoError(t, err)
	require.Len(t, windowed, 3, "both bounds are INCLUSIVE — dropping either endpoint loses a real day")
	assert.Equal(t, "2026-04-02", windowed[0].Date)
	assert.Equal(t, "2026-04-04", windowed[2].Date)

	openEnded, err := repo.GetAccountBalanceHistory(ctx, userID.String(), accountID.String(), &start, nil)
	require.NoError(t, err)
	require.Len(t, openEnded, 4, "one nil bound must leave that side unbounded rather than empty")
}

// ── A missing balance stays missing ───────────────────────────────────────────────────────────

func TestAccountBalanceHistoryRead_NullBalanceIsNotZero(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID := seedDepositoryFixture(ctx, t, tx, "bal-hist-null")
	repo := NewBalanceSnapshotRepository(tx)

	s := balSnap(userID, accountID, time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), 0, 0)
	s.AvailableBalance = nil // a credit-card style account reports no available balance
	require.NoError(t, repo.Create(ctx, s))

	points, err := repo.GetAccountBalanceHistory(ctx, userID.String(), accountID.String(), nil, nil)
	require.NoError(t, err)
	require.Len(t, points, 1)
	assert.Nil(t, points[0].AvailableBalance,
		"an unreported balance must stay unreported: rendering 0 claims the account held nothing")
	require.NotNil(t, points[0].CurrentBalance)
	assert.Equal(t, float64(0), *points[0].CurrentBalance)
}

// ── The read must carry NO liveness predicate ─────────────────────────────────────────────────

// The single most repeated regression on this branch: applying currentStateAccountLeg or an
// accountLiveOnDateSQL leg to a HISTORICAL read returns nothing for exactly the accounts the read
// exists to serve. Asserted here as behaviour — snapshots recorded on days the account was NOT
// live (it was removed before any of them) must still come back.
func TestAccountBalanceHistoryRead_SnapshotsAfterRemovalAreStillReturned(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountID := seedDepositoryFixture(ctx, t, tx, "bal-hist-noliveness")

	accountRepo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	require.NoError(t, accountRepo.MarkRemovedByLinkedItemID(ctx, linkedItemID.String(), domain.RemovalReasonUserDisconnect))

	// Written AFTER the removal, so the account's status-history interval does not overlap the
	// snapshot's day at all. A liveness leg of either shape excludes this row.
	repo := NewBalanceSnapshotRepository(tx)
	require.NoError(t, repo.Create(ctx, balSnap(userID, accountID,
		time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), 42, 42)))

	points, err := repo.GetAccountBalanceHistory(ctx, userID.String(), accountID.String(), nil, nil)
	require.NoError(t, err)
	require.Len(t, points, 1,
		"the historical read must carry no liveness predicate — one here returns nothing for the "+
			"closed accounts this endpoint exists to serve")
	require.NotNil(t, points[0].CurrentBalance)
	assert.Equal(t, float64(42), *points[0].CurrentBalance)
}
