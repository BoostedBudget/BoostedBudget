//go:build integration

package operations

// Issue #1630, Task 10: pins the amount/expected_income precedence (budget_service.go's
// reconciliation, Issue #1048) at the HTTP layer, through the REAL budgetService (not a mock
// that merely echoes back whatever the test hands it), so a future change to the reconciliation
// order in CreateBudget/UpdateBudget fails this test instead of silently inverting which field
// wins. Declined the issue's preferred "make amount read-only" fix — amount is still
// authoritative when expected_income is absent (asserted by the fallback sub-tests below).
//
// Run with:
//
//	DATABASE_URL=postgres://... go test ./internal/huma/operations/... -tags integration \
//	  -run TestCreateBudget_AmountExpectedIncomePrecedence -p 1 -v

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateBudget_AmountExpectedIncomePrecedence pins that expected_income wins over amount
// when both are set and expected_income > 0, and that amount remains authoritative when
// expected_income is absent — through the real handler -> service chain.
func TestCreateBudget_AmountExpectedIncomePrecedence(t *testing.T) {
	pool := connectClaim1630DB(t)

	t.Run("expected_income wins when both set", func(t *testing.T) {
		tx, err := pool.Begin(context.Background())
		require.NoError(t, err)
		defer tx.Rollback(context.Background()) //nolint:errcheck

		userID := seedClaim1630User(t, tx)
		handler := newClaim1630Handler(tx)

		expectedIncome := 5000.00
		input := &CreateBudgetInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID}}
		input.Body = domain.CreateBudgetRequest{
			Name:           "Precedence Test",
			Period:         domain.BudgetPeriodMonthly,
			StartDate:      domain.FlexibleDate(time.Now()),
			Amount:         100.00, // lower value — must lose
			ExpectedIncome: &expectedIncome,
			CurrencyCode:   "USD",
		}

		out, err := handler.CreateBudget(context.Background(), input)
		require.NoError(t, err)
		require.NotNil(t, out)

		assert.Equal(t, 5000.00, out.Body.ExpectedIncome)
		assert.Equal(t, 5000.00, out.Body.Amount,
			"amount must mirror expected_income when the latter is set and > 0 — "+
				"this precedence must not silently invert")
	})

	t.Run("amount remains authoritative when expected_income is absent", func(t *testing.T) {
		tx, err := pool.Begin(context.Background())
		require.NoError(t, err)
		defer tx.Rollback(context.Background()) //nolint:errcheck

		userID := seedClaim1630User(t, tx)
		handler := newClaim1630Handler(tx)

		input := &CreateBudgetInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID}}
		input.Body = domain.CreateBudgetRequest{
			Name:         "Precedence Fallback Test",
			Period:       domain.BudgetPeriodMonthly,
			StartDate:    domain.FlexibleDate(time.Now()),
			Amount:       750.00,
			CurrencyCode: "USD",
			// ExpectedIncome deliberately omitted.
		}

		out, err := handler.CreateBudget(context.Background(), input)
		require.NoError(t, err)
		require.NotNil(t, out)

		assert.Equal(t, 750.00, out.Body.Amount,
			"amount is still authoritative when expected_income is absent — "+
				"declining the issue's preferred read-only fix depends on this staying true")
		assert.Equal(t, 750.00, out.Body.ExpectedIncome)
	})
}

// TestUpdateBudget_AmountExpectedIncomePrecedence is the UpdateBudget half of the same pin.
func TestUpdateBudget_AmountExpectedIncomePrecedence(t *testing.T) {
	pool := connectClaim1630DB(t)
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer tx.Rollback(context.Background()) //nolint:errcheck

	userID := seedClaim1630User(t, tx)
	budgetID := seedClaim1630Budget(t, tx, userID)
	handler := newClaim1630Handler(tx)

	amount := 200.00
	expectedIncome := 9000.00
	input := &UpdateBudgetInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 budgetID,
	}
	input.Body = domain.UpdateBudgetRequest{
		Amount:         &amount,
		ExpectedIncome: &expectedIncome,
	}

	out, err := handler.UpdateBudget(context.Background(), input)
	require.NoError(t, err)
	require.NotNil(t, out)

	assert.Equal(t, 9000.00, out.Body.ExpectedIncome)
	assert.Equal(t, 9000.00, out.Body.Amount,
		"UpdateBudget must apply the same expected_income-wins precedence as CreateBudget")
}
