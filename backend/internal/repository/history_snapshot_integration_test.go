//go:build integration

package repository

// Integration tests for #1569 Task 10 (T6): append-only holdings and liability history written
// beside UNCHANGED current-state replace semantics.
//
// These have to be integration tests, not pgxmock tests. Every claim here is about what Postgres
// actually does with the capture statements — that INSERT ... SELECT reads the rows the same
// transaction just wrote, that ON CONFLICT collapses a same-day re-capture onto one row, that
// captured_at separates two captures on the same day, that to_jsonb() folds a child APR table into
// details, and that nothing in the capture disturbs holdings. pgxmock asserts query TEXT and never
// executes it, so it can confirm none of that. (This plan has already produced five compile-clean,
// execute-broken queries.)
//
// Run with:
//
//	DATABASE_URL=postgres://devuser:devpass@localhost:5434/freshcheck?sslmode=disable \
//	  go test ./internal/repository/... -run TestHistorySnapshot_ -tags integration -p 1 -v

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func f64(v float64) *float64 { return &v }
func boolPtr(v bool) *bool   { return &v }

// todayUTC is the as_of_date the capture statements compute for themselves,
// (now() AT TIME ZONE 'UTC')::date. Tests read it back from the same server rather than from the
// Go process's clock, because a test running near midnight UTC on a host in another zone would
// otherwise disagree with the server about which day "today" is.
func todayUTC(ctx context.Context, t *testing.T, db DB) time.Time {
	t.Helper()
	var d time.Time
	require.NoError(t, db.QueryRow(ctx, `SELECT (now() AT TIME ZONE 'UTC')::date`).Scan(&d))
	return d
}

// ── T6 test 1: a replace writes history and leaves current state exactly as it was ────────────

func TestHistorySnapshot_HoldingsReplace_CapturesHistoryWithoutChangingCurrentState(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-cap")
	repo := NewHoldingRepository(tx)

	_, err = repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{{
		UserID:           userID.String(),
		AccountID:        accountID.String(),
		SecurityID:       secID.String(),
		Quantity:         10,
		CostBasis:        f64(4000),
		InstitutionValue: f64(5000),
		InstitutionPrice: f64(500),
	}})
	require.NoError(t, err)

	// Current state: replace semantics untouched — exactly the row that was passed in, no extra
	// column, no filter needed to see it. (This is the property portfolio_snapshots' Upsert
	// depends on; soft-deleting holdings would have broken it.)
	live, err := repo.GetByAccountID(ctx, accountID.String())
	require.NoError(t, err)
	require.Len(t, live, 1, "the holdings replace must still leave exactly the replaced row")
	assert.Equal(t, float64(10), live[0].Quantity)

	// History: one row for today, carrying the identity task 11 needs to rebuild a
	// portfolio_snapshots row — which account, which security, as-of when, and the values.
	snapRepo := NewHoldingSnapshotRepository(tx)
	day := todayUTC(ctx, t, tx)
	snaps, err := snapRepo.GetForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)
	require.Len(t, snaps, 1, "the replace must have captured exactly one history row for today")

	got := snaps[0]
	assert.Equal(t, userID.String(), got.UserID)
	assert.Equal(t, accountID.String(), got.AccountID)
	assert.Equal(t, secID.String(), got.SecurityID)
	assert.Equal(t, day.Format("2006-01-02"), got.AsOfDate.Format("2006-01-02"))
	assert.Equal(t, float64(10), got.Quantity)
	require.NotNil(t, got.CostBasis)
	assert.Equal(t, float64(4000), *got.CostBasis)
	require.NotNil(t, got.InstitutionValue)
	assert.Equal(t, float64(5000), *got.InstitutionValue)
	require.NotNil(t, got.InstitutionPrice)
	assert.Equal(t, float64(500), *got.InstitutionPrice)
	assert.False(t, got.CapturedAt.IsZero(), "captured_at is how a later reader picks the day's final capture")
}

// ── The grain contract handed to task 11 ──────────────────────────────────────────────────────
//
// Two captures on the SAME UTC day must collapse onto one row per security (the table grows with
// elapsed days, not with sync frequency), and the day must still resolve to the SECOND capture's
// position set — including dropping a security that was sold between the two. That second half is
// what `present` is for: the sold security's row is not deleted (nothing here is), it is marked
// absent, keeping its last known values while falling out of the day's answer.

func TestHistorySnapshot_SecondCaptureSameDay_ResolvesToTheLaterPositionSet(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secA := seedInvestmentFixture(ctx, t, tx, "hist-sameday")

	secB := uuid.New()
	_, err = tx.Exec(ctx,
		`INSERT INTO securities (id, plaid_security_id, name, ticker_symbol) VALUES ($1, $2, 'Second Security', 'TSEC2')`,
		secB, "hist-sameday-secb-"+secB.String())
	require.NoError(t, err)

	repo := NewHoldingRepository(tx)

	// Capture 1: holds both securities.
	require.NoError(t, holdingsReplaced(repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secA.String(), Quantity: 10, InstitutionValue: f64(5000)},
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secB.String(), Quantity: 4, InstitutionValue: f64(400)},
	})))

	// Capture 2: secB sold, secA revalued.
	require.NoError(t, holdingsReplaced(repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secA.String(), Quantity: 10, InstitutionValue: f64(6000)},
	})))

	day := todayUTC(ctx, t, tx)

	// Nothing was deleted: both securities still have a row for today.
	var rowsForDay int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM holding_snapshots WHERE account_id = $1 AND as_of_date = $2`,
		accountID, day).Scan(&rowsForDay))
	assert.Equal(t, 2, rowsForDay,
		"one row per (security, day): the re-capture must UPDATE secA's row rather than append, and must not DELETE secB's")

	// secB's retained row still carries what was last known about it — the point of marking
	// absent rather than deleting.
	var bPresent bool
	var bValue *float64
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT present, institution_value FROM holding_snapshots WHERE account_id = $1 AND security_id = $2 AND as_of_date = $3`,
		accountID, secB, day).Scan(&bPresent, &bValue))
	assert.False(t, bPresent, "the security sold between the two captures must be marked absent")
	require.NotNil(t, bValue)
	assert.Equal(t, float64(400), *bValue, "an absent row keeps its last known values; it is not blanked")

	// …and the day resolves to capture 2's position set.
	snapRepo := NewHoldingSnapshotRepository(tx)
	snaps, err := snapRepo.GetForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)
	require.Len(t, snaps, 1, "the day's final capture held only secA — the security sold earlier that day must not be counted")
	assert.Equal(t, secA.String(), snaps[0].SecurityID)
	require.NotNil(t, snaps[0].InstitutionValue)
	assert.Equal(t, float64(6000), *snaps[0].InstitutionValue,
		"the surviving security's row must carry the LATER capture's value")
}

// A per-account capture must not disturb any OTHER account's rows for the same day. Both accounts
// synced on the same day must appear; an absence-marking pass scoped too widely would silently
// drop whichever was captured first — a portfolio that loses an institution.
func TestHistorySnapshot_FinalCapturesPerDay_KeepsEveryAccountSyncedAtDifferentTimes(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountA, secID := seedInvestmentFixture(ctx, t, tx, "hist-twoacct")

	accountB := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Second Brokerage', 'investment', 'active')`,
		accountB, linkedItemID, userID, "hist-twoacct-b-"+accountB.String())
	require.NoError(t, err)

	repo := NewHoldingRepository(tx)
	require.NoError(t, holdingsReplaced(repo.ReplaceForAccount(ctx, accountA.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountA.String(), SecurityID: secID.String(), Quantity: 1, InstitutionValue: f64(100)},
	})))

	require.NoError(t, holdingsReplaced(repo.ReplaceForAccount(ctx, accountB.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountB.String(), SecurityID: secID.String(), Quantity: 2, InstitutionValue: f64(200)},
	})))

	snapRepo := NewHoldingSnapshotRepository(tx)
	snaps, err := snapRepo.GetForUserOnDate(ctx, userID.String(), todayUTC(ctx, t, tx))
	require.NoError(t, err)
	require.Len(t, snaps, 2, "both accounts' captures must survive — capturing one account must not mark the other's rows absent")

	total := 0.0
	for _, s := range snaps {
		require.NotNil(t, s.InstitutionValue)
		total += *s.InstitutionValue
	}
	assert.Equal(t, float64(300), total)
}

// ── T6 test 5: disconnecting a brokerage leaves its holding history queryable ─────────────────
//
// "Disconnect" in this codebase is a status change plus, eventually, the loss of the current
// holdings rows — not a row delete. Both halves are modelled here: the account goes terminal AND
// its live holdings are gone. The history must still read back.

func TestHistorySnapshot_DisconnectedBrokerage_HistoryStaysQueryable(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-disc")
	repo := NewHoldingRepository(tx)
	require.NoError(t, holdingsReplaced(repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(), Quantity: 7, InstitutionValue: f64(700)},
	})))

	// The brokerage goes away: account marked terminal ('removed' — one of the four values
	// accounts_valid_status permits) and its live positions removed.
	_, err = tx.Exec(ctx,
		`UPDATE accounts SET status = 'removed', removed_at = now() WHERE id = $1`, accountID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `DELETE FROM holdings WHERE account_id = $1`, accountID)
	require.NoError(t, err)

	live, err := repo.GetByAccountID(ctx, accountID.String())
	require.NoError(t, err)
	require.Empty(t, live, "precondition: the current-state rows really are gone, so the assertion below is not vacuous")

	day := todayUTC(ctx, t, tx)
	snapRepo := NewHoldingSnapshotRepository(tx)
	snaps, err := snapRepo.GetByAccountDateRange(ctx, accountID.String(), day.AddDate(0, 0, -30), day)
	require.NoError(t, err)
	require.Len(t, snaps, 1, "the disconnected brokerage's history must still read back — that IS the thesis")
	assert.Equal(t, float64(7), snaps[0].Quantity)
	require.NotNil(t, snaps[0].InstitutionValue)
	assert.Equal(t, float64(700), *snaps[0].InstitutionValue)
}

// ── T6 test 7: portfolio_snapshots recompute behaviour is unchanged by this task ──────────────
//
// Task 11 depends on this not having regressed. The capture is additive and derived from the same
// rows Upsert SUMs, so Upsert must produce exactly the total it produced before — proven by
// running it against the holdings a captured replace left behind.

func TestHistorySnapshot_PortfolioSnapshotUpsert_UnchangedByTheCapture(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-psnap")
	require.NoError(t, holdingsReplaced(NewHoldingRepository(tx).ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(),
			Quantity: 10, CostBasis: f64(4000), InstitutionValue: f64(5000)},
	})))

	day := todayUTC(ctx, t, tx)
	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), day)
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Equal(t, 5000.0, snap.TotalValue, "Upsert still SUMs the LIVE holdings rows — the capture added rows elsewhere, it did not move these")
	assert.Equal(t, 4000.0, snap.TotalCostBasis)
	assert.Equal(t, 1000.0, snap.TotalGainLoss)

	// And the history the capture wrote reproduces the same aggregate — the contract task 11 is
	// being handed. If these two ever disagree, task 11's rebuild rewrites the user's past.
	snaps, err := NewHoldingSnapshotRepository(tx).GetForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)
	rebuilt := 0.0
	for _, s := range snaps {
		require.NotNil(t, s.InstitutionValue)
		rebuilt += *s.InstitutionValue
	}
	assert.Equal(t, snap.TotalValue, rebuilt,
		"reconstructing the day from holding_snapshots must equal what Upsert computed from live holdings")
}

// ── T6 test 2: liability history, all three types ─────────────────────────────────────────────

func seedLiabilityAccount(ctx context.Context, t *testing.T, db DB, prefix string) (userID, accountID uuid.UUID) {
	t.Helper()
	u, _, a, _ := seedInvestmentFixture(ctx, t, db, prefix)
	return u, a
}

func TestHistorySnapshot_CreditLiabilityReplace_CapturesStatementIncludingAPRs(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, accountID := seedLiabilityAccount(ctx, t, tx, "hist-credit")
	repo := NewCreditLiabilityRepository(tx)

	require.NoError(t, repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), &domain.CreditLiability{
		UserID:               userID.String(),
		AccountID:            accountID.String(),
		IsOverdue:            boolPtr(true),
		LastStatementBalance: f64(1234.56),
		MinimumPaymentAmount: f64(35),
		APRs: []domain.CreditLiabilityAPR{
			{RatePercentage: 19.99, RateType: "purchase_apr", BalanceSubjectToRate: f64(1000)},
			{RatePercentage: 24.99, RateType: "cash_apr"},
		},
	}))

	day := todayUTC(ctx, t, tx)
	stmts, err := NewLiabilityStatementRepository(tx).GetByAccountDateRange(
		ctx, accountID.String(), domain.LiabilityTypeCredit, day, day)
	require.NoError(t, err)
	require.Len(t, stmts, 1)

	got := stmts[0]
	assert.Equal(t, domain.LiabilityTypeCredit, got.LiabilityType)
	assert.Equal(t, userID.String(), got.UserID)
	require.NotNil(t, got.IsOverdue)
	assert.True(t, *got.IsOverdue)
	require.NotNil(t, got.LastStatementBalance)
	assert.Equal(t, 1234.56, *got.LastStatementBalance)
	assert.Nil(t, got.InterestRatePercentage, "a credit card has APR tiers, not one rate — the tiers live in details")

	// The APR schedule is the specific thing a partial Plaid response wipes today. It must be in
	// the retained record.
	var details struct {
		APRs []struct {
			RatePercentage float64 `json:"rate_percentage"`
			RateType       string  `json:"rate_type"`
		} `json:"aprs"`
		LastStatementBalance any `json:"last_statement_balance"`
	}
	require.NoError(t, json.Unmarshal(got.Details, &details))
	require.Len(t, details.APRs, 2, "both APR tiers must be preserved in details")
	assert.Equal(t, "cash_apr", details.APRs[0].RateType, "aprs are ordered by rate_type")
	assert.Equal(t, "purchase_apr", details.APRs[1].RateType)
	assert.NotNil(t, details.LastStatementBalance, "details is the whole source row, not just the aprs")
}

func TestHistorySnapshot_MortgageLiabilityReplace_CapturesStatement(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, accountID := seedLiabilityAccount(ctx, t, tx, "hist-mort")
	require.NoError(t, NewMortgageLiabilityRepository(tx, newTestFieldEncryptor(t)).ReplaceForAccount(
		ctx, accountID.String(), userID.String(), &domain.MortgageLiability{
			UserID:                 userID.String(),
			AccountID:              accountID.String(),
			InterestRatePercentage: f64(6.125),
			LastPaymentAmount:      f64(2100),
			NextMonthlyPayment:     f64(2100),
			PropertyAddressStreet:  strPtr("1 Test Street"),
		}))

	day := todayUTC(ctx, t, tx)
	stmts, err := NewLiabilityStatementRepository(tx).GetByAccountDateRange(
		ctx, accountID.String(), domain.LiabilityTypeMortgage, day, day)
	require.NoError(t, err)
	require.Len(t, stmts, 1)

	got := stmts[0]
	require.NotNil(t, got.InterestRatePercentage)
	assert.Equal(t, 6.125, *got.InterestRatePercentage)
	require.NotNil(t, got.LastPaymentAmount)
	assert.Equal(t, 2100.0, *got.LastPaymentAmount)
	assert.Nil(t, got.MinimumPaymentAmount,
		"mortgage_liabilities has no minimum_payment_amount; next_monthly_payment is a different field and stays in details")

	var details map[string]any
	require.NoError(t, json.Unmarshal(got.Details, &details))
	assert.Contains(t, details, "next_monthly_payment")
	// The address column is encrypted at rest; details copies the stored value verbatim, so what
	// lands in history is ciphertext, never the plaintext the caller passed in.
	street, _ := details["property_address_street"].(string)
	assert.NotEmpty(t, street)
	assert.NotEqual(t, "1 Test Street", street,
		"details must carry the ciphertext as stored, not decrypted plaintext")
}

func TestHistorySnapshot_StudentLoanLiabilityReplace_CapturesStatement(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, accountID := seedLiabilityAccount(ctx, t, tx, "hist-sloan")
	require.NoError(t, NewStudentLoanLiabilityRepository(tx, newTestFieldEncryptor(t)).ReplaceForAccount(
		ctx, accountID.String(), userID.String(), &domain.StudentLoanLiability{
			UserID:                 userID.String(),
			AccountID:              accountID.String(),
			InterestRatePercentage: f64(4.5),
			MinimumPaymentAmount:   f64(180),
			LastStatementBalance:   f64(22000),
			IsOverdue:              boolPtr(false),
		}))

	day := todayUTC(ctx, t, tx)
	stmts, err := NewLiabilityStatementRepository(tx).GetByAccountDateRange(
		ctx, accountID.String(), domain.LiabilityTypeStudentLoan, day, day)
	require.NoError(t, err)
	require.Len(t, stmts, 1)

	got := stmts[0]
	require.NotNil(t, got.InterestRatePercentage)
	assert.Equal(t, 4.5, *got.InterestRatePercentage)
	require.NotNil(t, got.MinimumPaymentAmount)
	assert.Equal(t, 180.0, *got.MinimumPaymentAmount)
	require.NotNil(t, got.IsOverdue)
	assert.False(t, *got.IsOverdue)
	require.NotNil(t, got.LastStatementBalance)
	assert.Equal(t, 22000.0, *got.LastStatementBalance)
}

// The delete-on-absence path is the highest-frequency destroyer in the census. When it fires, the
// current row goes — and the retained statement must not. This is the liability half of the thesis.
func TestHistorySnapshot_LiabilityClear_MarksTodayAbsentButKeepsTheRecord(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, accountID := seedLiabilityAccount(ctx, t, tx, "hist-clear")
	repo := NewCreditLiabilityRepository(tx)
	require.NoError(t, repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), &domain.CreditLiability{
		UserID:               userID.String(),
		AccountID:            accountID.String(),
		LastStatementBalance: f64(999),
		APRs:                 []domain.CreditLiabilityAPR{{RatePercentage: 21.24, RateType: "purchase_apr"}},
	}))

	day := todayUTC(ctx, t, tx)

	// A statement from an EARLIER day, so this test can prove the clear is scoped to today.
	yesterday := day.AddDate(0, 0, -1)
	_, err = tx.Exec(ctx, `
		INSERT INTO liability_statements (user_id, account_id, liability_type, as_of_date, last_statement_balance, details)
		VALUES ($1, $2, 'credit', $3, 888, '{"aprs": []}'::jsonb)`, userID, accountID, yesterday)
	require.NoError(t, err)

	// Plaid confirms the liability is gone → delete-only clear.
	require.NoError(t, repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), nil))

	var liveRows int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM credit_liabilities WHERE account_id = $1`, accountID).Scan(&liveRows))
	require.Equal(t, 0, liveRows, "precondition: the clear really did remove current state")

	stmts, err := NewLiabilityStatementRepository(tx).GetByAccountDateRange(
		ctx, accountID.String(), "", yesterday, day)
	require.NoError(t, err)
	require.Len(t, stmts, 2, "the clear must not erase any retained statement — that is the whole point of T6")

	// Yesterday is untouched: the liability really did exist then, and saying otherwise would be
	// a lie about a day that already happened.
	assert.Equal(t, yesterday.Format("2006-01-02"), stmts[0].AsOfDate.Format("2006-01-02"))
	assert.True(t, stmts[0].Present, "an earlier day must not be rewritten by today's clear")

	// Today records the absence — WITHOUT throwing away what was known.
	today := stmts[1]
	assert.Equal(t, day.Format("2006-01-02"), today.AsOfDate.Format("2006-01-02"))
	assert.False(t, today.Present,
		"Critical 2: today's row must say the liability stopped being reported, not keep asserting it exists")
	require.NotNil(t, today.LastStatementBalance)
	assert.Equal(t, 999.0, *today.LastStatementBalance, "the absent row keeps its values")

	var details struct {
		APRs []map[string]any `json:"aprs"`
	}
	require.NoError(t, json.Unmarshal(today.Details, &details))
	assert.Len(t, details.APRs, 1,
		"the APR schedule the clear wiped from current state survives here — marking absent must not erase it")
}

// ── The daily job's capture: a day with no Plaid sync still gets rows ─────────────────────────
//
// portfolio_snapshot_worker.go upserts a portfolio_snapshots aggregate for every user with
// holdings once a day whether or not anything synced. If holding_snapshots were written only on a
// sync, those days would carry an aggregate with no rows behind it — and task 11 rebuilds the
// aggregates FROM the rows. CaptureForUser is what closes that, so the two tables agree day for
// day.

func TestHistorySnapshot_CaptureForUser_RecordsADayWithNoSyncAndMatchesTheAggregate(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-daily")

	// Holdings inserted directly: this models a day on which no sync ran, so
	// ReplaceForAccount — and therefore the per-account capture — never fired.
	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (user_id, account_id, security_id, quantity, cost_basis, institution_value)
		VALUES ($1, $2, $3, 3, 900.00, 1200.00)`, userID, accountID, secID)
	require.NoError(t, err)

	snapRepo := NewHoldingSnapshotRepository(tx)
	day := todayUTC(ctx, t, tx)

	before, err := snapRepo.GetForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)
	require.Empty(t, before, "precondition: nothing has captured this day yet, so the assertion below is not vacuous")

	capturedDay, err := snapRepo.CaptureForUser(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, day.Format("2006-01-02"), capturedDay.Format("2006-01-02"),
		"CaptureForUser returns the SERVER's day so the caller files its aggregate under the same one")

	after, err := snapRepo.GetForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.Equal(t, float64(3), after[0].Quantity)

	// Day-for-day parity with what the same job's Upsert writes for that user.
	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), day)
	require.NoError(t, err)
	require.NotNil(t, after[0].InstitutionValue)
	assert.Equal(t, snap.TotalValue, *after[0].InstitutionValue,
		"the captured rows must sum to the aggregate the daily job writes for the same day")

	// Idempotent within the day: a second run refreshes the same row rather than appending.
	_, err = snapRepo.CaptureForUser(ctx, userID.String())
	require.NoError(t, err)
	var rowsForDay int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM holding_snapshots WHERE user_id = $1 AND as_of_date = $2`,
		userID, day).Scan(&rowsForDay))
	assert.Equal(t, 1, rowsForDay, "re-running the daily capture must not append a second row for the same day")
}

// A sync-day capture and the daily capture must land on the SAME row, not two: they share the
// (account, security, day) grain. If they diverged, the day would double-count.
func TestHistorySnapshot_CaptureForUser_AfterASyncCapture_RefreshesTheSameDayRow(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-both")
	require.NoError(t, holdingsReplaced(NewHoldingRepository(tx).ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(), Quantity: 5, InstitutionValue: f64(500)},
	})))

	_, err = NewHoldingSnapshotRepository(tx).CaptureForUser(ctx, userID.String())
	require.NoError(t, err)

	day := todayUTC(ctx, t, tx)
	var rowsForDay int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM holding_snapshots WHERE user_id = $1 AND as_of_date = $2`,
		userID, day).Scan(&rowsForDay))
	assert.Equal(t, 1, rowsForDay, "the sync capture and the daily capture share one grain — they must collide, not accumulate")
}

// ── Fix round 1, Critical 1: the daily capture must apply the SAME liveness test as Upsert ────
//
// Reproduces the reviewer's measurement. Accounts are soft-removed and the only DELETE FROM
// holdings lives in ReplaceForAccount (which a sync only reaches for accounts Plaid returns), so a
// disconnected account's holdings live on indefinitely. The daily sweep used to be a bare
// `WHERE h.user_id = $1` and re-captured them every day:
//
//	GetForUserOnDate SUM | 1500.00 | 2 rows      <- before
//	Upsert total_value   | 1000.00 | 1 row
//
// A task-11 rebuild from that would inflate the curve by every institution the user has ever left,
// permanently and growing. Note the fixture shape that matters: ONE active account plus one
// terminal one. A single-active-account fixture cannot see this, because
// GetDistinctUserIDsWithHoldings skips users whose accounts are ALL terminal.
func TestHistorySnapshot_CaptureForUser_ExcludesAccountsNotLiveOnTheDay(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, activeAccount, secID := seedInvestmentFixture(ctx, t, tx, "hist-liveness")

	goneAccount := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Old Brokerage', 'investment', 'active')`,
		goneAccount, linkedItemID, userID, "hist-liveness-gone-"+goneAccount.String())
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		INSERT INTO holdings (user_id, account_id, security_id, quantity, institution_value)
		VALUES ($1, $2, $3, 10, 1000.00), ($1, $4, $3, 5, 500.00)`,
		userID, activeAccount, secID, goneAccount)
	require.NoError(t, err)

	// The second account was disconnected ten days ago. Its account_status_history 'active'
	// interval is closed then; its holdings are RETAINED, which is the whole point of #1569 and
	// is exactly why a bare user-scoped capture kept picking them up.
	tenDaysAgo := time.Now().UTC().AddDate(0, 0, -10)
	// valid_from is backdated too: the fixture's interval is created by the accounts INSERT
	// trigger and opens at test time, and chk_ash_interval requires valid_to > valid_from.
	ct, err := tx.Exec(ctx, `
		UPDATE account_status_history SET valid_from = $2::timestamptz - INTERVAL '30 days', valid_to = $2::timestamptz
		WHERE account_id = $1 AND status = 'active' AND valid_to IS NULL`, goneAccount, tenDaysAgo)
	require.NoError(t, err)
	require.EqualValues(t, 1, ct.RowsAffected(), "the trigger-created 'active' interval must exist to close")
	_, err = tx.Exec(ctx,
		`UPDATE accounts SET status = 'removed', removed_at = $2 WHERE id = $1`, goneAccount, tenDaysAgo)
	require.NoError(t, err)

	// Precondition: the retained holdings really are still there, so this is not vacuous.
	var liveHoldingRows int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM holdings WHERE account_id = $1`, goneAccount).Scan(&liveHoldingRows))
	require.Equal(t, 1, liveHoldingRows, "the disconnected account's holdings are retained — that is #1569's whole thesis")

	snapRepo := NewHoldingSnapshotRepository(tx)
	day, err := snapRepo.CaptureForUser(ctx, userID.String())
	require.NoError(t, err)

	snaps, err := snapRepo.GetForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)

	captured := 0.0
	for _, s := range snaps {
		require.NotNil(t, s.InstitutionValue)
		captured += *s.InstitutionValue
		assert.NotEqual(t, goneAccount.String(), s.AccountID,
			"an account that was not live on this day must not be captured for it")
	}

	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), day)
	require.NoError(t, err)

	assert.Len(t, snaps, 1)
	assert.Equal(t, 1000.0, captured, "Critical 1: was 1500.00 — the disconnected institution was being re-captured daily")
	assert.Equal(t, snap.TotalValue, captured,
		"the capture and Upsert must ask the SAME liveness question, or a task-11 rebuild rewrites the curve")
}

// ── Fix round 1, Critical 2: a confirmed liquidation must not leave the morning standing ──────
//
// Reproduces the reviewer's measurement. The morning sync records 5000; in the afternoon a
// confirmed-complete Plaid response clears the account. INSERT ... SELECT selects zero rows, so
// before the fix nothing was written and the morning capture remained the day's answer:
//
//	live_total          | 0        <- what Upsert writes
//	history_final_total | 5000.00  <- what the reconstruction returned
//
// It never self-healed: the daily sweep also selected zero rows. Task 11 would have restored a
// portfolio the user had actually liquidated.
func TestHistorySnapshot_ConfirmedLiquidation_ZeroesTheDayRatherThanKeepingTheMorning(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-liquidation")
	repo := NewHoldingRepository(tx)

	// Morning sync.
	require.NoError(t, holdingsReplaced(repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(), Quantity: 10, InstitutionValue: f64(5000)},
	})))

	snapRepo := NewHoldingSnapshotRepository(tx)
	day := todayUTC(ctx, t, tx)
	morning, err := snapRepo.GetForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)
	require.Len(t, morning, 1, "precondition: the morning really was recorded, so the afternoon assertion is not vacuous")

	// Afternoon: Plaid returns the account with zero holdings. The T1 roster gate has already
	// confirmed the response is complete, so this reaches the repository as a genuine liquidation.
	require.NoError(t, holdingsReplaced(repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{})))

	var liveTotal float64
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(institution_value), 0) FROM holdings WHERE user_id = $1`, userID).Scan(&liveTotal))
	require.Equal(t, 0.0, liveTotal, "precondition: current state really is empty")

	afternoon, err := snapRepo.GetForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)
	historyTotal := 0.0
	for _, s := range afternoon {
		require.NotNil(t, s.InstitutionValue)
		historyTotal += *s.InstitutionValue
	}
	assert.Empty(t, afternoon, "Critical 2: the morning capture must not still be returned as the day's positions")
	assert.Equal(t, 0.0, historyTotal, "Critical 2: was 5000.00 against a live total of 0")

	// Upsert agrees, which is the property task 11 depends on.
	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), day)
	require.NoError(t, err)
	assert.Equal(t, snap.TotalValue, historyTotal)

	// The record is retained, not deleted: the row still says what was held this morning.
	var present bool
	var value *float64
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT present, institution_value FROM holding_snapshots WHERE account_id = $1 AND as_of_date = $2`,
		accountID, day).Scan(&present, &value))
	assert.False(t, present)
	require.NotNil(t, value)
	assert.Equal(t, 5000.0, *value, "marking absent must not blank the values — a liquidation is history, not an erasure")

	// And it does not self-unheal: the daily sweep must reach the same answer.
	sweepDay, err := snapRepo.CaptureForUser(ctx, userID.String())
	require.NoError(t, err)
	afterSweep, err := snapRepo.GetForUserOnDate(ctx, userID.String(), sweepDay)
	require.NoError(t, err)
	assert.Empty(t, afterSweep, "the daily sweep must not resurrect the liquidated position")
}

// An earlier day must survive both absence-marking paths untouched. This is the immutability claim
// the whole task rests on, and both fixes are UPDATEs that could in principle reach too far.
func TestHistorySnapshot_AbsenceMarking_LeavesEarlierDaysAlone(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-priorday")

	day := todayUTC(ctx, t, tx)
	yesterday := day.AddDate(0, 0, -1)
	_, err = tx.Exec(ctx, `
		INSERT INTO holding_snapshots (user_id, account_id, security_id, as_of_date, quantity, institution_value)
		VALUES ($1, $2, $3, $4, 10, 5000.00)`, userID, accountID, secID, yesterday)
	require.NoError(t, err)

	repo := NewHoldingRepository(tx)
	// Today: the account holds nothing at all. Both the per-account capture and the whole-user
	// sweep run their absence-marking pass.
	require.NoError(t, holdingsReplaced(repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{})))
	_, err = NewHoldingSnapshotRepository(tx).CaptureForUser(ctx, userID.String())
	require.NoError(t, err)

	snaps, err := NewHoldingSnapshotRepository(tx).GetForUserOnDate(ctx, userID.String(), yesterday)
	require.NoError(t, err)
	require.Len(t, snaps, 1, "yesterday must still read as held — a past day is immutable")
	assert.True(t, snaps[0].Present)
	require.NotNil(t, snaps[0].InstitutionValue)
	assert.Equal(t, 5000.0, *snaps[0].InstitutionValue)
}

// ── Fix round 2, item 2: the history side's liveness BOUNDARY ─────────────────────────────────
//
// accountLiveOnDateClause is a re-derivation of the predicate Upsert carries as its own inline
// literal (portfolio_snapshot_repository.go ~:96-108). They are two copies of one rule and CAN
// drift. Re-review measured exactly how far the existing tests guarded that, and the two answers
// were not the same. Deleting the valid_to bound from UPSERT made
// CaptureForUser_ExcludesAccountsNotLiveOnTheDay fail, so coarse drift was caught. But changing
// `valid_to >` to `>=` on the HISTORY side only left the entire ./internal/repository integration
// package green — nothing guarded the history side's boundary at all.
//
// The mixed-liveness fixture uses an account terminated ten days ago, nowhere near a boundary, so
// it structurally cannot see an off-by-one. This test is the boundary pin, the counterpart of
// Upsert's own OverlapBoundaryIsUTC.
//
// The rule is a half-open interval [valid_from, valid_to) overlapping the half-open calendar day
// [D, D+1): live iff valid_from < D+1 AND (valid_to IS NULL OR valid_to > D). Each probe below
// sits exactly ON one of those two bounds, one on each side.
//
// These fixtures manipulate account_status_history ONLY and never touch accounts.status: the
// predicate reads recorded liveness, not current status (that is #1569's Fix A design), and
// leaving status alone keeps the accounts trigger from firing again and adding intervals that
// would muddy what is being measured.
func TestHistorySnapshot_CaptureForUser_LivenessBoundaryIsTheHalfOpenUTCDay(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountOpen, secID := seedInvestmentFixture(ctx, t, tx, "hist-boundary")

	mkAccount := func(label string) uuid.UUID {
		id := uuid.New()
		_, err := tx.Exec(ctx, `
			INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
			VALUES ($1, $2, $3, $4, $5, 'investment', 'active')`,
			id, linkedItemID, userID, "hist-boundary-"+label+"-"+id.String(), label)
		require.NoError(t, err)
		return id
	}
	endsAtMidnight := mkAccount("ends-at-midnight")
	endsJustAfterMidnight := mkAccount("ends-just-after-midnight")
	startsTomorrowMidnight := mkAccount("starts-tomorrow-midnight")

	for _, seed := range []struct {
		account uuid.UUID
		value   float64
	}{
		{accountOpen, 1000},
		{endsAtMidnight, 500},
		{endsJustAfterMidnight, 250},
		{startsTomorrowMidnight, 125},
	} {
		_, err = tx.Exec(ctx, `
			INSERT INTO holdings (user_id, account_id, security_id, quantity, institution_value)
			VALUES ($1, $2, $3, 1, $4)`, userID, seed.account, secID, seed.value)
		require.NoError(t, err)
	}

	// setInterval rewrites the account's single trigger-created 'active' interval. Bounds are
	// computed SERVER-side off the same (now() AT TIME ZONE 'UTC')::date the capture uses, so the
	// probes land exactly on the boundary rather than near it.
	setInterval := func(account uuid.UUID, fromExpr, toExpr string) {
		t.Helper()
		ct, err := tx.Exec(ctx, `
			WITH d AS (SELECT ((now() AT TIME ZONE 'UTC')::date)::timestamp AT TIME ZONE 'UTC' AS midnight)
			UPDATE account_status_history ash
			SET valid_from = `+fromExpr+`, valid_to = `+toExpr+`
			FROM d
			WHERE ash.account_id = $1 AND ash.status = 'active' AND ash.valid_to IS NULL`, account)
		require.NoError(t, err)
		require.EqualValues(t, 1, ct.RowsAffected(), "the trigger-created 'active' interval must exist to rewrite")
	}

	// Ends EXACTLY at this UTC day's midnight → the interval is [.., D) and does NOT overlap
	// [D, D+1). Not live today. This is the probe the `>` → `>=` mutation flips.
	setInterval(endsAtMidnight, "d.midnight - INTERVAL '30 days'", "d.midnight")
	// Ends one microsecond into the day → overlaps by that microsecond. Live today.
	setInterval(endsJustAfterMidnight, "d.midnight - INTERVAL '30 days'", "d.midnight + INTERVAL '1 microsecond'")
	// Starts EXACTLY at tomorrow's midnight → [D+1, ..) does not overlap [D, D+1). Not live today.
	// This is the probe a `<` → `<=` mutation on the valid_from bound flips.
	setInterval(startsTomorrowMidnight, "d.midnight + INTERVAL '1 day'", "NULL")

	snapRepo := NewHoldingSnapshotRepository(tx)
	day, err := snapRepo.CaptureForUser(ctx, userID.String())
	require.NoError(t, err)

	snaps, err := snapRepo.GetForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)

	captured := map[string]float64{}
	total := 0.0
	for _, s := range snaps {
		require.NotNil(t, s.InstitutionValue)
		captured[s.AccountID] = *s.InstitutionValue
		total += *s.InstitutionValue
	}

	assert.Contains(t, captured, accountOpen.String(), "an open interval is live")
	assert.Contains(t, captured, endsJustAfterMidnight.String(),
		"an interval ending one microsecond INTO the day overlaps it — valid_to > D must be strict-greater, and this is the true side of it")
	assert.NotContains(t, captured, endsAtMidnight.String(),
		"an interval ending EXACTLY at the day's midnight is [.., D) and does not overlap [D, D+1) — this is what `valid_to >= D` would wrongly include")
	assert.NotContains(t, captured, startsTomorrowMidnight.String(),
		"an interval starting EXACTLY at tomorrow's midnight does not overlap today — this is what `valid_from <= D+1` would wrongly include")
	assert.Equal(t, 1250.0, total)

	// And the boundary answer must be the SAME one Upsert reaches. This is the only thing standing
	// between the two copies of the predicate and a silent divergence.
	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), day)
	require.NoError(t, err)
	assert.Equal(t, snap.TotalValue, total,
		"accountLiveOnDateClause and Upsert's inline copy are two derivations of one rule; if they disagree at the boundary a task-11 rebuild rewrites the curve")
}

// ── Fix round 2, item 3: all FOUR portfolio_snapshots columns, not just total_value ───────────
//
// Task 11 rebuilds total_value, total_cost_basis, total_gain_loss AND total_gain_loss_pct. Only
// the first was contracted. The gain/loss pair is computed under
// FILTER (WHERE cost_basis IS NOT NULL AND institution_value IS NOT NULL) — NULL cost_basis is
// excluded to prevent phantom gains, NULL institution_value to prevent phantom losses — while
// total_value and total_cost_basis are plain SUMs over ALL holdings. That asymmetry is exactly the
// sort of rule a rebuild gets silently wrong, so it is pinned here against real Postgres rather
// than described in a report.
//
// Fixture deliberately contains all three shapes: both values known, cost_basis NULL, and
// institution_value NULL.
func TestHistorySnapshot_ReconstructsAllFourPortfolioSnapshotColumns(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secBoth := seedInvestmentFixture(ctx, t, tx, "hist-fourcol")

	mkSecurity := func(label string) uuid.UUID {
		id := uuid.New()
		_, err := tx.Exec(ctx,
			`INSERT INTO securities (id, plaid_security_id, name) VALUES ($1, $2, $3)`,
			id, "hist-fourcol-"+label+"-"+id.String(), label)
		require.NoError(t, err)
		return id
	}
	secNoCostBasis := mkSecurity("no-cost-basis")
	secNoValue := mkSecurity("no-value")

	require.NoError(t, holdingsReplaced(NewHoldingRepository(tx).ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secBoth.String(),
			Quantity: 10, CostBasis: f64(4000), InstitutionValue: f64(5000)},
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secNoCostBasis.String(),
			Quantity: 3, CostBasis: nil, InstitutionValue: f64(1000)},
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secNoValue.String(),
			Quantity: 7, CostBasis: f64(2000), InstitutionValue: nil},
	})))

	day := todayUTC(ctx, t, tx)
	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), day)
	require.NoError(t, err)

	// Sanity-pin Upsert's own arithmetic, so the equalities below cannot both be wrong in the
	// same direction and still agree.
	require.Equal(t, 6000.0, snap.TotalValue, "total_value is a plain SUM over ALL holdings; the NULL-value row contributes nothing")
	require.Equal(t, 6000.0, snap.TotalCostBasis, "total_cost_basis is a plain SUM over ALL holdings; the NULL-cost-basis row contributes nothing")
	require.Equal(t, 1000.0, snap.TotalGainLoss, "gain/loss counts ONLY the row where both values are known: 5000 - 4000")
	require.NotNil(t, snap.TotalGainLossPct)
	require.InDelta(t, 25.0, *snap.TotalGainLossPct, 1e-9, "1000 / 4000 * 100")

	snaps, err := NewHoldingSnapshotRepository(tx).GetForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)
	require.Len(t, snaps, 3, "history must carry the NULL-bearing rows too, or the FILTER cannot be reproduced")

	var value, costBasis, filteredValue, filteredBasis float64
	for _, s := range snaps {
		if s.InstitutionValue != nil {
			value += *s.InstitutionValue
		}
		if s.CostBasis != nil {
			costBasis += *s.CostBasis
		}
		if s.CostBasis != nil && s.InstitutionValue != nil {
			filteredValue += *s.InstitutionValue
			filteredBasis += *s.CostBasis
		}
	}
	gainLoss := filteredValue - filteredBasis

	assert.Equal(t, snap.TotalValue, value)
	assert.Equal(t, snap.TotalCostBasis, costBasis,
		"total_cost_basis must be reconstructible — task 11 rebuilds it and nothing asserted it before")
	assert.Equal(t, snap.TotalGainLoss, gainLoss,
		"the both-non-NULL FILTER must be reproducible from history; a rebuild that COALESCEd instead would invent gains")
	require.Greater(t, filteredBasis, 0.0)
	assert.InDelta(t, *snap.TotalGainLossPct, gainLoss/filteredBasis*100, 1e-9,
		"the pct denominator is the FILTERED basis, not total_cost_basis — 4000 here, not 6000")
	assert.NotEqual(t, costBasis, filteredBasis,
		"guard against a fixture that makes the two denominators coincide, which would let a wrong rebuild pass")
}

// TestHistorySnapshot_ReplaceForAccount_ReturnsTheDayItFiledUnder pins the contract #1569 T9.1
// item C's fix depends on: the day ReplaceForAccount hands back is the SERVER's UTC date, and it is
// the same as_of_date the rows it just wrote carry. investment_sync_service files its
// portfolio_snapshots aggregate under this value instead of consulting the Go clock, so a return
// value that drifted from the rows would move the bug rather than fix it.
func TestHistorySnapshot_ReplaceForAccount_ReturnsTheDayItFiledUnder(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "hist-retday")

	returned, err := NewHoldingRepository(tx).ReplaceForAccount(ctx, accountID.String(), userID.String(),
		[]domain.Holding{{
			UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(),
			Quantity: 1, CostBasis: f64(10), InstitutionValue: f64(12),
		}})
	require.NoError(t, err)

	var rowDay time.Time
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT as_of_date FROM holding_snapshots WHERE account_id = $1`, accountID).Scan(&rowDay))

	require.Equal(t, rowDay, returned,
		"the returned day must be the as_of_date the capture actually wrote — an aggregate filed "+
			"under anything else is the orphan item C exists to prevent")
	require.Equal(t, todayUTC(ctx, t, tx), returned, "and it must come from the SERVER, not the Go process")
}

// holdingsReplaced adapts holdingRepository.ReplaceForAccount's (day, error) return for the many
// call sites that assert only on the error. The value it drops is the UTC day the history capture
// filed under (#1569 T9.1 item C); a test that cares about that day binds both results instead of
// going through here.
func holdingsReplaced(_ time.Time, err error) error { return err }
