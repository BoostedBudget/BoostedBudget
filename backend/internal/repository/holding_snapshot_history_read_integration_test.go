//go:build integration

package repository

// Integration tests for the READ side of holding_snapshots (Issue #1569).
//
// These have to run against real Postgres. Every claim here is about what the database does with
// the read statements — that the securities LEFT JOIN resolves, that a DATE comes back as the day
// it was stored under, that `present` filtering excludes a liquidated position, and above all that
// the read returns rows for an account the CURRENT-STATE read excludes. pgxmock asserts query text
// and never executes it, so it can confirm none of that.
//
// FIXTURES GO THROUGH PRODUCTION WRITE PATHS. Every history row below is written by
// holdingRepository.ReplaceForAccount (which captures inside its own transaction) and every
// account removal by accountRepository.MarkRemovedByLinkedItemID. Hand-INSERTing history rows
// would validate rows production cannot produce.
//
// Run with:
//
//	DATABASE_URL=postgres://devuser:devpass@localhost:5434/freshcheck?sslmode=disable \
//	  go test ./internal/repository/... -run TestHoldingHistoryRead_ -tags integration -p 1 -v

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// backdateCaptures moves an account's already-captured rows back one UTC day, so a test can hold
// two distinct as_of_date values.
//
// It edits rows PRODUCTION WROTE rather than inserting fabricated ones — the shape, the presence
// flag and the unique-index grain all come from the real capture. Nothing else can produce a
// second day inside one test: the capture takes its day from the server clock.
func backdateCaptures(ctx context.Context, t *testing.T, db DB, accountID uuid.UUID) {
	t.Helper()
	_, err := db.Exec(ctx,
		`UPDATE holding_snapshots SET as_of_date = as_of_date - 1 WHERE account_id = $1`, accountID)
	require.NoError(t, err)
}

// ── The defect this closes: a closed brokerage's positions are unreachable ─────────────────────

func TestHoldingHistoryRead_ClosedAccount_HistoryStaysReadableWhileCurrentStateIsEmpty(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-read-closed")

	holdingRepo := NewHoldingRepository(tx)
	require.NoError(t, holdingsReplaced(holdingRepo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{{
		UserID:           userID.String(),
		AccountID:        accountID.String(),
		SecurityID:       secID.String(),
		Quantity:         10,
		CostBasis:        f64(4000),
		InstitutionValue: f64(5000),
		InstitutionPrice: f64(500),
	}})))

	// The user disconnects the bank. Production path, with the cause stamped.
	accountRepo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	require.NoError(t, accountRepo.MarkRemovedByLinkedItemID(ctx, linkedItemID.String(), domain.RemovalReasonUserDisconnect))

	// CURRENT-STATE universe: empty, and that stays exactly as it was. The endpoint added here does
	// not widen it by one row.
	acct := accountID.String()
	live, err := holdingRepo.GetByUserIDWithSecurities(ctx, userID.String(), &acct, nil)
	require.NoError(t, err)
	assert.Empty(t, live, "a removed account must still hold nothing in the current-state read")

	// HISTORICAL universe: intact. This is the sentence privacy policy 8.2a promises.
	snapRepo := NewHoldingSnapshotRepository(tx)
	dates, err := snapRepo.ListHistoryDatesForAccount(ctx, userID.String(), accountID.String())
	require.NoError(t, err)
	require.Len(t, dates, 1, "the closed account's captured day must still be listed")

	positions, err := snapRepo.GetHistoryForAccountOnDate(ctx, userID.String(), accountID.String(), dates[0])
	require.NoError(t, err)
	require.Len(t, positions, 1, "the closed account's retained position must be readable")

	got := positions[0]
	assert.Equal(t, secID.String(), got.SecurityID)
	assert.Equal(t, dates[0], got.AsOfDate, "the row must carry the day it was filed under")
	assert.Equal(t, float64(10), got.Quantity)
	require.NotNil(t, got.InstitutionValue)
	assert.Equal(t, float64(5000), *got.InstitutionValue)

	// The securities LEFT JOIN resolved — without it the UI can only show a UUID.
	require.NotNil(t, got.SecurityName)
	assert.Equal(t, "Test Security", *got.SecurityName)
	require.NotNil(t, got.TickerSymbol)
	assert.Equal(t, "TSEC", *got.TickerSymbol)

	// Gain/loss is computed from THIS row's own two historical figures.
	require.NotNil(t, got.GainLoss)
	assert.InDelta(t, 1000.0, *got.GainLoss, 0.001)
	require.NotNil(t, got.GainLossPct)
	assert.InDelta(t, 25.0, *got.GainLossPct, 0.001)
}

// ── Tenancy, in SQL, on both statements ───────────────────────────────────────────────────────

func TestHoldingHistoryRead_ForeignUser_SeesNothing(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-read-ownerA")
	otherUserID, _, _, _ := seedInvestmentFixture(ctx, t, tx, "hist-read-ownerB")

	holdingRepo := NewHoldingRepository(tx)
	require.NoError(t, holdingsReplaced(holdingRepo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{{
		UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(),
		Quantity: 1, InstitutionValue: f64(100),
	}})))

	snapRepo := NewHoldingSnapshotRepository(tx)

	// The owner can see it, so the fixture is real rather than the read being broken for everyone
	// — which is how a tenancy test passes for the wrong reason.
	ownerDates, err := snapRepo.ListHistoryDatesForAccount(ctx, userID.String(), accountID.String())
	require.NoError(t, err)
	require.Len(t, ownerDates, 1)

	dates, err := snapRepo.ListHistoryDatesForAccount(ctx, otherUserID.String(), accountID.String())
	require.NoError(t, err)
	assert.Empty(t, dates, "another user must not learn that this account has history")

	positions, err := snapRepo.GetHistoryForAccountOnDate(ctx, otherUserID.String(), accountID.String(), ownerDates[0])
	require.NoError(t, err)
	assert.Empty(t, positions, "another user must not read this account's positions")
}

// ── present: a sold position is retained but is not a holding ─────────────────────────────────

func TestHoldingHistoryRead_LiquidatedPosition_IsExcludedFromTheDay(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secA := seedInvestmentFixture(ctx, t, tx, "hist-read-sold")
	secB := uuid.New()
	_, err = tx.Exec(ctx,
		`INSERT INTO securities (id, plaid_security_id, name, ticker_symbol) VALUES ($1, $2, 'Second Security', 'SEC2')`,
		secB, "hist-read-sold-"+secB.String())
	require.NoError(t, err)

	holdingRepo := NewHoldingRepository(tx)
	require.NoError(t, holdingsReplaced(holdingRepo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secA.String(), Quantity: 1, InstitutionValue: f64(100)},
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secB.String(), Quantity: 2, InstitutionValue: f64(200)},
	})))

	// Second sync the same day: secB was sold.
	require.NoError(t, holdingsReplaced(holdingRepo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secA.String(), Quantity: 1, InstitutionValue: f64(100)},
	})))

	snapRepo := NewHoldingSnapshotRepository(tx)
	day := todayUTC(ctx, t, tx).Format("2006-01-02")

	positions, err := snapRepo.GetHistoryForAccountOnDate(ctx, userID.String(), accountID.String(), day)
	require.NoError(t, err)
	require.Len(t, positions, 1, "the sold position must not be rendered as still held")
	assert.Equal(t, secA.String(), positions[0].SecurityID)

	// It was RETAINED, not destroyed — the absent row is still in the table.
	var retained int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*) FROM holding_snapshots WHERE account_id = $1 AND security_id = $2 AND NOT present`,
		accountID, secB).Scan(&retained))
	assert.Equal(t, 1, retained, "#1569: nothing is deleted; the row is marked absent")
}

// ── The day picker ────────────────────────────────────────────────────────────────────────────

func TestHoldingHistoryRead_MultipleDays_AreListedNewestFirstAndReadIndependently(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-read-days")
	holdingRepo := NewHoldingRepository(tx)

	require.NoError(t, holdingsReplaced(holdingRepo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{{
		UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(),
		Quantity: 10, InstitutionValue: f64(1000),
	}})))
	backdateCaptures(ctx, t, tx, accountID)

	require.NoError(t, holdingsReplaced(holdingRepo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{{
		UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(),
		Quantity: 12, InstitutionValue: f64(1500),
	}})))

	snapRepo := NewHoldingSnapshotRepository(tx)
	dates, err := snapRepo.ListHistoryDatesForAccount(ctx, userID.String(), accountID.String())
	require.NoError(t, err)
	require.Len(t, dates, 2)
	assert.Greater(t, dates[0], dates[1], "newest first — the caller treats dates[0] as 'the latest'")

	newest, err := snapRepo.GetHistoryForAccountOnDate(ctx, userID.String(), accountID.String(), dates[0])
	require.NoError(t, err)
	require.Len(t, newest, 1)
	assert.Equal(t, float64(12), newest[0].Quantity)
	assert.Equal(t, dates[0], newest[0].AsOfDate)

	older, err := snapRepo.GetHistoryForAccountOnDate(ctx, userID.String(), accountID.String(), dates[1])
	require.NoError(t, err)
	require.Len(t, older, 1)
	assert.Equal(t, float64(10), older[0].Quantity, "the earlier day must answer with the earlier position, not the latest one")
	assert.Equal(t, dates[1], older[0].AsOfDate)
}

// A day the account has no rows for returns EMPTY from the repository. The refusal that turns
// that into a 404 is the service's job (see holding_history_service_test.go) — pinned here so the
// two halves cannot both assume the other does it.
func TestHoldingHistoryRead_UnretainedDay_ReturnsEmptyFromTheRepository(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-read-gap")
	holdingRepo := NewHoldingRepository(tx)
	require.NoError(t, holdingsReplaced(holdingRepo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{{
		UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(), Quantity: 1,
	}})))

	snapRepo := NewHoldingSnapshotRepository(tx)
	positions, err := snapRepo.GetHistoryForAccountOnDate(ctx, userID.String(), accountID.String(), "2001-01-01")
	require.NoError(t, err)
	assert.Empty(t, positions)
	assert.NotNil(t, positions, "an empty result is a slice, never nil — nil marshals as null")
}

// A position whose provider never sent a value is still a position. The read must return it
// rather than dropping it, and must leave gain/loss nil rather than inventing a zero.
func TestHoldingHistoryRead_MissingValues_SurviveAsNullNotZero(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-read-nulls")
	holdingRepo := NewHoldingRepository(tx)
	require.NoError(t, holdingsReplaced(holdingRepo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{{
		UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(), Quantity: 7,
	}})))

	snapRepo := NewHoldingSnapshotRepository(tx)
	day := todayUTC(ctx, t, tx).Format("2006-01-02")
	positions, err := snapRepo.GetHistoryForAccountOnDate(ctx, userID.String(), accountID.String(), day)
	require.NoError(t, err)
	require.Len(t, positions, 1)

	assert.Equal(t, float64(7), positions[0].Quantity)
	assert.Nil(t, positions[0].InstitutionValue)
	assert.Nil(t, positions[0].CostBasis)
	assert.Nil(t, positions[0].GainLoss, "no value and no cost basis is not a gain of zero")
	assert.Nil(t, positions[0].GainLossPct)
}

// ── Account scope: one user, two brokerages ───────────────────────────────────────────────────
//
// Tenancy alone is not enough. Both reads are per-ACCOUNT — the UI renders them under one closed
// account's detail page — so a second brokerage belonging to the SAME user must contribute
// neither a day to the picker nor a position to the day. A user_id-only filter passes every
// tenancy test and still shows the wrong portfolio.
func TestHoldingHistoryRead_SecondAccountOfSameUser_DoesNotBleedIn(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountA, secA := seedInvestmentFixture(ctx, t, tx, "hist-read-scopeA")

	// A second investment account under the SAME user and linked item.
	accountB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Second Brokerage', 'investment', 'active')
	`, accountB, linkedItemID, userID, "hist-read-scopeB-"+accountB.String())
	require.NoError(t, err)

	secB := uuid.New()
	_, err = tx.Exec(ctx,
		`INSERT INTO securities (id, plaid_security_id, name, ticker_symbol) VALUES ($1, $2, 'Other Security', 'OTHR')`,
		secB, "hist-read-scope-"+secB.String())
	require.NoError(t, err)

	holdingRepo := NewHoldingRepository(tx)
	require.NoError(t, holdingsReplaced(holdingRepo.ReplaceForAccount(ctx, accountA.String(), userID.String(), []domain.Holding{{
		UserID: userID.String(), AccountID: accountA.String(), SecurityID: secA.String(),
		Quantity: 1, InstitutionValue: f64(100),
	}})))
	// Account B gets TWO days, and the older one is a day account A does not have at all. Both
	// halves are load-bearing:
	//   - the backdated day catches a date listing that is not account-scoped (it would show up in
	//     A's picker);
	//   - B's row on TODAY catches a position read that is not account-scoped (it would show up
	//     among A's positions). A backdated-only fixture proves only the first, and a mutant that
	//     dropped the account filter from the position read survived exactly that gap.
	replaceB := func(qty float64, value float64) {
		require.NoError(t, holdingsReplaced(holdingRepo.ReplaceForAccount(ctx, accountB.String(), userID.String(), []domain.Holding{{
			UserID: userID.String(), AccountID: accountB.String(), SecurityID: secB.String(),
			Quantity: qty, InstitutionValue: f64(value),
		}})))
	}
	replaceB(9, 900)
	backdateCaptures(ctx, t, tx, accountB)
	replaceB(11, 1100)

	snapRepo := NewHoldingSnapshotRepository(tx)

	datesA, err := snapRepo.ListHistoryDatesForAccount(ctx, userID.String(), accountA.String())
	require.NoError(t, err)
	assert.Len(t, datesA, 1, "account A has exactly one captured day; account B's backdated day must not appear")

	positionsA, err := snapRepo.GetHistoryForAccountOnDate(ctx, userID.String(), accountA.String(), datesA[0])
	require.NoError(t, err)
	require.Len(t, positionsA, 1, "only account A's own position belongs on account A's day")
	assert.Equal(t, secA.String(), positionsA[0].SecurityID)

	// And the same read from B's seat sees only B — proving the scope discriminates rather than
	// the fixture simply being empty.
	datesB, err := snapRepo.ListHistoryDatesForAccount(ctx, userID.String(), accountB.String())
	require.NoError(t, err)
	require.Len(t, datesB, 2, "account B owns both days")
	positionsB, err := snapRepo.GetHistoryForAccountOnDate(ctx, userID.String(), accountB.String(), datesB[0])
	require.NoError(t, err)
	require.Len(t, positionsB, 1)
	assert.Equal(t, secB.String(), positionsB[0].SecurityID)
}

// ── A day on which the account held NOTHING is a day we kept ──────────────────────────────────
//
// The fully-liquidated day. An account whose only position is sold has that day's WHOLE row set
// marked present = false and nothing inserted (see markHoldingSnapshotsAbsentForAccountSQL) — the
// rows are retained, exactly as #1569 requires. Until this test the date listing filtered on
// `present`, so the retained day disappeared from available_dates and every surface above it said
// no day-by-day positions were recorded for the account. That is a false statement about data we
// still hold, on the surface built to stop making false statements about retained data.
//
// The two reads are deliberately ASYMMETRIC and this test pins both halves:
//   - the DATE listing counts retained ROWS, because "we observed you on day D" is what a day in
//     the picker means;
//   - the POSITION read still counts only PRESENT rows, because an absent row records a position
//     that STOPPED being held and rendering it would state a sold position as an owned one.
//
// So the day is offered and it renders empty — "on D this account held nothing" — which is a
// different fact from "we kept nothing for D" (that one is still the service's 404).
func TestHoldingHistoryRead_FullyLiquidatedDay_StaysInTheDateListingAndRendersEmpty(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-read-liquidated")

	holdingRepo := NewHoldingRepository(tx)
	require.NoError(t, holdingsReplaced(holdingRepo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{{
		UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(),
		Quantity: 10, CostBasis: f64(4000), InstitutionValue: f64(5000), InstitutionPrice: f64(500),
	}})))

	// The one holding is sold, confirmed complete: Plaid returned the account with no positions.
	require.NoError(t, holdingsReplaced(holdingRepo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{})))

	// Nothing was destroyed by the liquidation — the row is still there, marked absent.
	var retained, present int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE present) FROM holding_snapshots WHERE account_id = $1`,
		accountID).Scan(&retained, &present))
	require.Equal(t, 1, retained, "the liquidated day's row must be RETAINED")
	require.Equal(t, 0, present, "and every one of its rows must be absent — that is the case under test")

	// The bank connection then goes away, which is when the user meets this surface.
	accountRepo := NewAccountRepository(tx, newTestFieldEncryptor(t))
	require.NoError(t, accountRepo.MarkRemovedByLinkedItemID(ctx, linkedItemID.String(), domain.RemovalReasonUserDisconnect))

	snapRepo := NewHoldingSnapshotRepository(tx)
	day := todayUTC(ctx, t, tx).Format("2006-01-02")

	dates, err := snapRepo.ListHistoryDatesForAccount(ctx, userID.String(), accountID.String())
	require.NoError(t, err)
	require.Len(t, dates, 1, "the retained day must be offered — the user is otherwise told it does not exist")
	assert.Equal(t, day, dates[0])

	positions, err := snapRepo.GetHistoryForAccountOnDate(ctx, userID.String(), accountID.String(), day)
	require.NoError(t, err)
	assert.Empty(t, positions, "an absent row is not a position and must never render as one")
	assert.NotNil(t, positions, "an empty result is a slice, never nil")
}
