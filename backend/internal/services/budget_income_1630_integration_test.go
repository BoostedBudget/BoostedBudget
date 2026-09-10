//go:build integration

package services

// Issue #1630, Task 0: confirm the P2 open question — is income_amount:0 with an empty
// income_sources array a real (already-explained) behavior, not a bug to independently
// re-diagnose. budget_income_service.go's CalculateBudgetIncome declares `var totalIncome
// float64` and loops over budget.IncomeSources; an empty slice never enters the loop, so the
// function returns 0 with no fallback to, e.g., ExpectedIncome or a computed sum. This test
// seeds two real income transactions (pfc_primary="INCOME", negative/inflow amount) in a past
// period and asserts CalculateBudgetIncome returns 0 for an empty income_sources budget, and a
// non-zero sum for the identical budget/period once income_sources names the same category —
// exercising CalculateBudgetIncome against real Postgres data, not a mock.
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/services/... -tags integration -p 1 \
//	  -run TestClaim1630_P2_IncomeAmountZero -v

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClaim1630_P2_IncomeAmountZero_EmptySourcesVsPFCPrimary seeds two income deposits in a
// past period, tagged with PFC primary category "INCOME", and asserts:
//   - CalculateBudgetIncome returns 0 when the budget's IncomeSources is empty (the reported
//     "income_amount: 0" symptom — CONFIRMED real, by the exact mechanism the brief cites: the
//     loop body never runs for an empty slice, and there is no fallback).
//   - CalculateBudgetIncome returns the actual non-zero deposit sum once IncomeSources names
//     the "INCOME" pfc_primary category — proving the zero above is a configuration gap
//     (no income_sources configured), not a query/data defect.
func TestClaim1630_P2_IncomeAmountZero_EmptySourcesVsPFCPrimary(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-income-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-claim1630-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-claim1630', 'Checking', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	// A PAST period: budget's own start/end aren't queried by CalculateBudgetIncome (it takes
	// startDate/endDate as explicit params), so this just has to be a period the two deposits
	// below fall inside of.
	periodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.January, 31, 23, 59, 59, 0, time.UTC)
	depositDate1 := time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)
	depositDate2 := time.Date(2026, time.January, 20, 0, 0, 0, 0, time.UTC)

	insertIncomeTxn := func(date time.Time, amount float64) {
		_, err := tx.Exec(ctx, `
			INSERT INTO transactions
				(id, user_id, account_id, linked_item_id, source, amount, date, name, pending, pfc_primary)
			VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 'Paycheck', false, 'INCOME')
		`, uuid.New().String(), userID, accountID, linkedItemID, amount, date)
		require.NoError(t, err, "seed income transaction")
	}

	// Two income deposits (inflow = negative amount under PFCv2), $1500 and $2000.
	insertIncomeTxn(depositDate1, -1500.00)
	insertIncomeTxn(depositDate2, -2000.00)

	enc := newTestFieldEncryptor(t)
	txnRepo := repository.NewTransactionRepository(tx, enc)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	incomeSvc := NewBudgetIncomeService(txnRepo.(IncomeTransactionGetter), logger)

	baseBudget := &domain.Budget{
		ID:     uuid.New().String(),
		UserID: userID,
	}

	t.Run("empty income_sources returns 0", func(t *testing.T) {
		budget := *baseBudget
		budget.IncomeSources = []domain.IncomeSource{}

		total, err := incomeSvc.CalculateBudgetIncome(ctx, &budget, periodStart, periodEnd)

		require.NoError(t, err)
		assert.Equal(t, 0.0, total,
			"CONFIRMED (#1630 P2): an empty income_sources array returns income_amount=0 "+
				"even though $3500 of real income exists in this exact period — "+
				"CalculateBudgetIncome's loop never runs for an empty slice, with no fallback")
	})

	t.Run("pfc_primary INCOME source returns real non-zero sum", func(t *testing.T) {
		budget := *baseBudget
		budget.IncomeSources = []domain.IncomeSource{
			{Type: domain.IncomeSourceTypePFCPrimary, Value: "INCOME"},
		}

		total, err := incomeSvc.CalculateBudgetIncome(ctx, &budget, periodStart, periodEnd)

		require.NoError(t, err)
		assert.Equal(t, 3500.00, total,
			"once income_sources names the pfc_primary category the two seeded deposits "+
				"were tagged with, CalculateBudgetIncome returns their real, non-zero sum")
	})
}
