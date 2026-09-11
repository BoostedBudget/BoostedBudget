//go:build integration

package repository

// Issue #1631: portfolio income understated dividends by $6,249.43 in production — nearly 3× what
// it reported.
//
// IncomeSummary gates its ENTIRE row universe on `it.subtype IN (domain.IncomeSubtypes)`. The
// brokerage that motivated #1631 reports a dividend as `cash/deposit` against the ETF, and
// `deposit` is not in that set, so 211 dividend rows were excluded outright — not mislabelled,
// absent. Counted today: 68 `dividend` rows ($2,249.39) + 37 `interest` ($3.00). Missing: 211
// rows worth $6,249.43.
//
// The fix must add the DEPOSIT leg only. Its paired `cash/withdrawal` reinvestment is the same
// economic dollar spent on shares, so counting both would double-count: a −$100 deposit with a
// +$100 reinvest withdrawal is $100 of income, not $0 (both counted under the existing negation)
// and not $200 (both counted with corrected signs). The withdrawal leg is excluded today and must
// stay excluded.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIncomeSummary_StructuralDividend_IsCounted_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-1631-income")

	// A cash-equivalent security, so the bill-payment shape can be asserted as NOT income.
	cashSecID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO securities (id, plaid_security_id, name, ticker_symbol, type, is_cash_equivalent)
		VALUES ($1, $2, 'US Dollar', 'CUR:USD', 'cash', true)
	`, cashSecID, "inc-sec-cash-"+cashSecID.String())
	require.NoError(t, err)

	// ⚠ QUANTITY IS NOT DECORATION ON A STRUCTURAL ROW (#1631 CX3). The structural rule has no
	// subtype literal to key on, so the amount sign and the SHARE COUNT are the only corroboration
	// it has -- and an absent quantity is deliberately not evidence of a zero one. This seeder
	// omitted the column, leaving it NULL, so every "structural" row below actually exercised the
	// plain Cash branch: the test passed for the wrong reason before CX3, and went red the moment
	// the rule started demanding the evidence it is named for.
	//
	// Production, measured: cash/deposit against a real security is amount NEGATIVE with
	// quantity == 0, cash/withdrawal is amount POSITIVE with quantity > 0, and a NULL quantity
	// does not occur in any bucket. These fixtures carry that shape.
	//
	// quantity stays a pointer so a row can seed a genuine NULL where that is the point.
	qty := func(v float64) *float64 { return &v }
	seed := func(label, typ, subtype string, security uuid.UUID, amount float64, quantity *float64) {
		id := uuid.New()
		_, e := tx.Exec(ctx, `
			INSERT INTO investment_transactions
				(id, user_id, account_id, security_id, plaid_investment_transaction_id,
				 type, subtype, name, amount, quantity, date)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '2026-03-15')
		`, id, userID, accountID, security, label+"-"+id.String(), typ, subtype, label, amount, quantity)
		require.NoError(t, e, "seed %s", label)
	}

	// Plaid's sign convention: inflow (income) is NEGATIVE, outflow (a purchase) POSITIVE.
	// The production reinvestment pair, against a real security.
	// The dividend leg: money arrived and bought nothing yet.
	seed("structural dividend credit", "cash", "deposit", secID, -100.00, qty(0))
	// The paired purchase leg: cash debited, shares delivered. Excluded from income by design --
	// a reinvested dividend is ONE dollar of income, not two -- but it must be excluded as the
	// REINVEST it is, not because a NULL quantity left it unclassifiable.
	seed("reinvestment purchase leg", "cash", "withdrawal", secID, 100.00, qty(2.5))
	// A subtype-literal dividend — already counted before #1631.
	seed("literal dividend", "cash", "dividend", secID, -25.00, qty(0))
	// Bill payments on the cash-management side: same subtypes, cash-equivalent security.
	// These are NOT income and must not be swept in by widening the universe.
	// Cash-equivalent security: the structural branch cannot apply here whatever the quantity, so
	// these two keep the shape a brokerage cash-management account really reports.
	seed("bill payment", "cash", "withdrawal", cashSecID, 400.00, nil)
	seed("paycheque", "cash", "deposit", cashSecID, -3000.00, nil)

	// ⚠ THE BANK SWEEP, RUN THROUGH INCOME AND NOT ONLY THROUGH THE FEED. The plan required this
	// specifically, and asserting it here rather than only in the partition test is the whole
	// point: a misclassified sweep inflates reported INCOME, which is worse than a mislabelled
	// chip. A chip is cosmetic; a number the user reads as dividends received is not.
	//
	// The sweep vehicle is the case the security facts alone cannot decide. MSPBNA — the Morgan
	// Stanley sweep behind the production cash/interest rows — is is_cash_equivalent=FALSE and
	// type='equity', structurally indistinguishable from an ETF, so it takes the same secID here.
	// What separates it is the share count: a sweep deposit DELIVERS SHARES of the sweep vehicle,
	// so it fails the zero-share corroboration a dividend credit carries and must fall to Cash.
	// Pinned as a row shape in TestInvestment_TradeTypePartition_RoundTrip_Integration
	// (r43-sweep-dep); pinned here as ZERO DOLLARS of income.
	seed("sweep deposit", "cash", "deposit", secID, -1500.00, qty(1500))

	repo := NewInvestmentAnalyticsRepository(tx)
	start := mustParseDate("2026-03-01")
	end := mustParseDate("2026-03-31")

	summary, err := repo.IncomeSummary(ctx, userID.String(), start, end)
	require.NoError(t, err)
	require.NotNil(t, summary)

	byType := map[string]float64{}
	for _, bt := range summary.ByType {
		byType[bt.IncomeType] = bt.TotalAmount
	}

	// $100 structural + $25 literal. Not $0 (the purchase leg cancelling the credit) and not $225
	// (the purchase leg sign-corrected and added) — the two arithmetic traps this test exists for.
	assert.InDelta(t, 125.00, summary.TotalIncome, 0.001,
		"income must be the credit legs only: a reinvested dividend is ONE dollar of income, not "+
			"zero and not two. Got by-type: %v", byType)
	assert.InDelta(t, 125.00, byType["dividend"], 0.001,
		"a structural cash/deposit against a real security is a dividend")

	// The sweep is a $1,500 credit against a non-cash-equivalent security — the exact shape that
	// would be swept into dividends by a structural rule keyed on security facts alone. If it ever
	// is, TotalIncome reads 1625.00 and this assertion is what says so.
	assert.InDelta(t, 125.00, summary.TotalIncome, 0.001,
		"a bank-sweep deposit delivers SHARES and is not income; income must not include its $1,500")

	// The cash-equivalent rows must contribute nothing at all.
	assert.NotContains(t, byType, "other_income",
		"bill payments and paycheques on a cash-management account are not investment income")
}

// TestIncomeSummary_StructuralReinvestmentPair_IsExactlyOneHundredDollars_Integration is H1 (#1631
// requirements batch H): TestIncomeSummary_StructuralDividend_IsCounted_Integration above asserts
// $125.00 — the reinvestment pair PLUS a separate $25 literal dividend row — which does not ISOLATE
// the pair. $125 is also what several WRONG combinations of those three rows would produce (e.g.
// double-counting the $100 pair as $200 minus a $75 sign error), so a combined total cannot rule
// out the two arithmetic traps the pair itself is named for.
//
// This test seeds ONLY the reinvestment pair — a -$100 deposit credit and its +$100 reinvest
// withdrawal debit, both against the same real security — and asserts TotalIncome is exactly
// $100.00: not $0.00 (both legs counted under the pre-#1631 negation, cancelling out) and not
// $200.00 (both legs counted with corrected signs, double-counting one economic dollar as two).
func TestIncomeSummary_StructuralReinvestmentPair_IsExactlyOneHundredDollars_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-1631-h4-isolated")

	qty := func(v float64) *float64 { return &v }
	seed := func(label, typ, subtype string, amount float64, quantity *float64) {
		id := uuid.New()
		_, e := tx.Exec(ctx, `
			INSERT INTO investment_transactions
				(id, user_id, account_id, security_id, plaid_investment_transaction_id,
				 type, subtype, name, amount, quantity, date)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '2026-03-15')
		`, id, userID, accountID, secID, label+"-"+id.String(), typ, subtype, label, amount, quantity)
		require.NoError(t, e, "seed %s", label)
	}

	// The dividend leg: money arrived (credit), no shares delivered yet.
	seed("isolated structural dividend credit", "cash", "deposit", -100.00, qty(0))
	// The paired purchase leg: cash debited, shares delivered — the SAME economic dollar spent on
	// shares, and must be excluded from income as the REINVEST it is.
	seed("isolated reinvestment purchase leg", "cash", "withdrawal", 100.00, qty(2.5))

	repo := NewInvestmentAnalyticsRepository(tx)
	start := mustParseDate("2026-03-01")
	end := mustParseDate("2026-03-31")

	summary, err := repo.IncomeSummary(ctx, userID.String(), start, end)
	require.NoError(t, err)
	require.NotNil(t, summary)

	byType := map[string]float64{}
	for _, bt := range summary.ByType {
		byType[bt.IncomeType] = bt.TotalAmount
	}

	assert.InDelta(t, 100.00, summary.TotalIncome, 0.001,
		"the reinvestment pair, ISOLATED, must be exactly $100 of income — not $0 (both legs "+
			"cancelling under the current negation) and not $200 (both legs counted with corrected "+
			"signs). Got by-type: %v", byType)
	assert.InDelta(t, 100.00, byType["dividend"], 0.001,
		"the credit leg alone is the $100 of dividend income")
}

// TestIncomeSummary_CapitalGainDistribution_IsIncome_Integration covers the subtypes #1631 found
// missing from every set: a capital-gain distribution is income in exactly the way a dividend is,
// yet `long-term capital gain` matched nothing and fell through to "cash".
func TestIncomeSummary_CapitalGainDistribution_IsIncome_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-1631-capgain")

	id := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id,
			 type, subtype, name, amount, quantity, date)
		VALUES ($1, $2, $3, $4, $5, 'cash', 'long-term capital gain', 'LTCG', -40.00, 0, '2026-03-15')
	`, id, userID, accountID, secID, "capgain-"+id.String())
	require.NoError(t, err)

	summary, err := NewInvestmentAnalyticsRepository(tx).IncomeSummary(
		ctx, userID.String(), mustParseDate("2026-03-01"), mustParseDate("2026-03-31"))
	require.NoError(t, err)
	assert.InDelta(t, 40.00, summary.TotalIncome, 0.001,
		"a capital-gain distribution is investment income")
}

// TestIncomeSummary_TransferTypeDepositAgainstRealSecurity_IsNotIncome_Integration pins #1631 I1:
// the structural rule (and the subtype-literal rule beside it) must be gated on it.type = 'cash',
// exactly as NormalizeTradeType and TradeTypeToSQLPredicate gate the same rule. Without the gate,
// an ACATS/in-kind movement posted as type='transfer', subtype='deposit' against a real security
// was counted as a dividend here while the feed labelled the same row Transfer — and with the
// -SUM(amount) negation, a large transfer IN read as that much dividend income.
func TestIncomeSummary_TransferTypeDepositAgainstRealSecurity_IsNotIncome_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, _, accountID, secID := seedInvestmentFixture(ctx, t, tx, "inv-1631-i1")

	// quantity 0 with a NEGATIVE amount is deliberate: this row satisfies EVERY fact the
	// structural dividend rule requires EXCEPT the type gate (#1631 CX3), which is what makes it
	// adversarial -- it isolates type = the cash literal as the only thing keeping an ACATS in-kind
	// transfer out of reported income. Omitting quantity let a NULL do that work instead, so the
	// test passed without ever exercising the gate it is named for.
	id := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id,
			 type, subtype, name, amount, quantity, date)
		VALUES ($1, $2, $3, $4, $5, 'transfer', 'deposit', 'ACATS in-kind transfer', -50000.00, 0, '2026-03-15')
	`, id, userID, accountID, secID, "acats-transfer-"+id.String())
	require.NoError(t, err)

	summary, err := NewInvestmentAnalyticsRepository(tx).IncomeSummary(
		ctx, userID.String(), mustParseDate("2026-03-01"), mustParseDate("2026-03-31"))
	require.NoError(t, err)
	require.NotNil(t, summary)

	assert.InDelta(t, 0, summary.TotalIncome, 0.001,
		"a type='transfer' deposit against a real security is a Transfer everywhere else in this "+
			"codebase (investment_trade_type.go); IncomeSummary must not count it as a dividend "+
			"just because its subtype happens to be 'deposit'")
}
