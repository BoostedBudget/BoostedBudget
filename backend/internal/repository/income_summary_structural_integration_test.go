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

	seed := func(label, typ, subtype string, security uuid.UUID, amount float64) {
		id := uuid.New()
		_, e := tx.Exec(ctx, `
			INSERT INTO investment_transactions
				(id, user_id, account_id, security_id, plaid_investment_transaction_id,
				 type, subtype, name, amount, date)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '2026-03-15')
		`, id, userID, accountID, security, label+"-"+id.String(), typ, subtype, label, amount)
		require.NoError(t, e, "seed %s", label)
	}

	// Plaid's sign convention: inflow (income) is NEGATIVE, outflow (a purchase) POSITIVE.
	// The production reinvestment pair, against a real security.
	seed("structural dividend credit", "cash", "deposit", secID, -100.00)
	seed("reinvestment purchase leg", "cash", "withdrawal", secID, 100.00)
	// A subtype-literal dividend — already counted before #1631.
	seed("literal dividend", "cash", "dividend", secID, -25.00)
	// Bill payments on the cash-management side: same subtypes, cash-equivalent security.
	// These are NOT income and must not be swept in by widening the universe.
	seed("bill payment", "cash", "withdrawal", cashSecID, 400.00)
	seed("paycheque", "cash", "deposit", cashSecID, -3000.00)

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

	// The cash-equivalent rows must contribute nothing at all.
	assert.NotContains(t, byType, "other_income",
		"bill payments and paycheques on a cash-management account are not investment income")
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
			 type, subtype, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'cash', 'long-term capital gain', 'LTCG', -40.00, '2026-03-15')
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

	id := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO investment_transactions
			(id, user_id, account_id, security_id, plaid_investment_transaction_id,
			 type, subtype, name, amount, date)
		VALUES ($1, $2, $3, $4, $5, 'transfer', 'deposit', 'ACATS in-kind transfer', -50000.00, '2026-03-15')
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
