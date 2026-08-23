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

	err = repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{{
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
	snaps, err := snapRepo.GetFinalCapturesForUserOnDate(ctx, userID.String(), day)
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
// the whole reason captured_at exists: the sold security's row is not deleted (nothing here is),
// it simply keeps the earlier captured_at and so falls outside the day's final capture.

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
	require.NoError(t, repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secA.String(), Quantity: 10, InstitutionValue: f64(5000)},
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secB.String(), Quantity: 4, InstitutionValue: f64(400)},
	}))

	// The two captures must land on distinguishable transaction timestamps. now() is the
	// TRANSACTION timestamp, so both ReplaceForAccount calls inside this one outer test
	// transaction would otherwise share it — they each open their own tx via tx.Begin (a
	// savepoint here), and Postgres gives a subtransaction its parent's now(). Advance the
	// recorded captured_at by hand to model two syncs at different times of the same day; the
	// PRODUCTION path gets distinct values for free because each sync is its own top-level
	// transaction.
	_, err = tx.Exec(ctx,
		`UPDATE holding_snapshots SET captured_at = captured_at - INTERVAL '1 hour' WHERE account_id = $1`,
		accountID)
	require.NoError(t, err)

	// Capture 2: secB sold, secA revalued.
	require.NoError(t, repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secA.String(), Quantity: 10, InstitutionValue: f64(6000)},
	}))

	day := todayUTC(ctx, t, tx)

	// Nothing was deleted: both securities still have a row for today.
	var rowsForDay int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM holding_snapshots WHERE account_id = $1 AND as_of_date = $2`,
		accountID, day).Scan(&rowsForDay))
	assert.Equal(t, 2, rowsForDay,
		"one row per (security, day): the re-capture must UPDATE secA's row rather than append, and must not delete secB's")

	// …but the day resolves to capture 2's position set.
	snapRepo := NewHoldingSnapshotRepository(tx)
	snaps, err := snapRepo.GetFinalCapturesForUserOnDate(ctx, userID.String(), day)
	require.NoError(t, err)
	require.Len(t, snaps, 1, "the day's FINAL capture held only secA — the security sold earlier that day must not be counted twice")
	assert.Equal(t, secA.String(), snaps[0].SecurityID)
	require.NotNil(t, snaps[0].InstitutionValue)
	assert.Equal(t, float64(6000), *snaps[0].InstitutionValue,
		"the surviving security's row must carry the LATER capture's value")
}

// GetFinalCapturesForUserOnDate takes MAX(captured_at) per (account, day), not per user. Two
// accounts synced at different times on the same day must BOTH appear; a user-wide MAX would
// silently drop whichever was captured first — a portfolio that loses an institution.
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
	require.NoError(t, repo.ReplaceForAccount(ctx, accountA.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountA.String(), SecurityID: secID.String(), Quantity: 1, InstitutionValue: f64(100)},
	}))
	// Age account A's capture so the two accounts have genuinely different captured_at values.
	_, err = tx.Exec(ctx,
		`UPDATE holding_snapshots SET captured_at = captured_at - INTERVAL '2 hours' WHERE account_id = $1`, accountA)
	require.NoError(t, err)

	require.NoError(t, repo.ReplaceForAccount(ctx, accountB.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountB.String(), SecurityID: secID.String(), Quantity: 2, InstitutionValue: f64(200)},
	}))

	snapRepo := NewHoldingSnapshotRepository(tx)
	snaps, err := snapRepo.GetFinalCapturesForUserOnDate(ctx, userID.String(), todayUTC(ctx, t, tx))
	require.NoError(t, err)
	require.Len(t, snaps, 2, "both accounts' final captures must survive — MAX(captured_at) is per account, not per user")

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
	require.NoError(t, repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(), Quantity: 7, InstitutionValue: f64(700)},
	}))

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
	require.NoError(t, NewHoldingRepository(tx).ReplaceForAccount(ctx, accountID.String(), userID.String(), []domain.Holding{
		{UserID: userID.String(), AccountID: accountID.String(), SecurityID: secID.String(),
			Quantity: 10, CostBasis: f64(4000), InstitutionValue: f64(5000)},
	}))

	day := todayUTC(ctx, t, tx)
	snap, err := NewPortfolioSnapshotRepository(tx).Upsert(ctx, userID.String(), day)
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Equal(t, 5000.0, snap.TotalValue, "Upsert still SUMs the LIVE holdings rows — the capture added rows elsewhere, it did not move these")
	assert.Equal(t, 4000.0, snap.TotalCostBasis)
	assert.Equal(t, 1000.0, snap.TotalGainLoss)

	// And the history the capture wrote reproduces the same aggregate — the contract task 11 is
	// being handed. If these two ever disagree, task 11's rebuild rewrites the user's past.
	snaps, err := NewHoldingSnapshotRepository(tx).GetFinalCapturesForUserOnDate(ctx, userID.String(), day)
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
func TestHistorySnapshot_LiabilityClear_DestroysCurrentRowButNotHistory(t *testing.T) {
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

	// Plaid confirms the liability is gone → delete-only clear.
	require.NoError(t, repo.ReplaceForAccount(ctx, accountID.String(), userID.String(), nil))

	var liveRows int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM credit_liabilities WHERE account_id = $1`, accountID).Scan(&liveRows))
	require.Equal(t, 0, liveRows, "precondition: the clear really did remove current state")

	day := todayUTC(ctx, t, tx)
	stmts, err := NewLiabilityStatementRepository(tx).GetByAccountDateRange(ctx, accountID.String(), "", day, day)
	require.NoError(t, err)
	require.Len(t, stmts, 1, "the clear must not erase the retained statement — that is the whole point of T6")
	require.NotNil(t, stmts[0].LastStatementBalance)
	assert.Equal(t, 999.0, *stmts[0].LastStatementBalance)

	var details struct {
		APRs []map[string]any `json:"aprs"`
	}
	require.NoError(t, json.Unmarshal(stmts[0].Details, &details))
	assert.Len(t, details.APRs, 1, "the APR schedule the clear wiped from current state survives in history")
}
