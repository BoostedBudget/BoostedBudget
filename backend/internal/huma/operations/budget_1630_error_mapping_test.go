package operations

// Issue #1630, Task 7: domain.ErrInvalidBudget. Budget.Validate() / BudgetCategory.Validate() /
// IncomeSource.Validate() previously returned bare errors.New(...) with no sentinel, so
// internal/huma/errors/mapper.go's switch fell through to the default 500 case for every field
// failure. These tests drive the REAL domain validation (via mockBudgetService.createErr /
// addCategoryErr set to the actual error domain.Validate() returns) through the handler, exactly
// as TestGetCategoryTransactions_WrapsServiceError already does for status-code assertions in
// this package, and assert the mapped HTTP status + that the legal values appear in the body.
//
// The happy-path proof for P2 (a valid income_sources entry can actually be set, and after sync
// income_amount becomes non-zero) lives in TestUpdateBudget_PFCPrimaryIncomeSource_PersistsAndSyncs
// below, against real Postgres — nothing else in this ticket asserts a valid income source
// actually WORKS end-to-end.

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// budgetStatusErr drives err through the handler chain (which calls
// humaerrors.LoggingMapError) and returns the resulting huma.StatusError, failing the test if
// the error was not mapped to one.
func budgetStatusErr(t *testing.T, err error) huma.StatusError {
	t.Helper()
	require.Error(t, err)
	var statusErr huma.StatusError
	require.True(t, errors.As(err, &statusErr), "expected huma.StatusError, got %T: %v", err, err)
	return statusErr
}

// TestCreateBudget_InvalidIncomeSourceType_Returns400WithLegalValues is the RED half of Task 7:
// TestClaim1630_P2_IncomeSourceType_CategoryVsCustom_BothInvalid (task 0) proved this was a 500;
// domain.ErrInvalidBudget now makes it a 400 that names the field AND the legal values.
func TestCreateBudget_InvalidIncomeSourceType_Returns400WithLegalValues(t *testing.T) {
	badSource := domain.IncomeSource{Type: domain.IncomeSourceType("category"), Value: "whatever"}
	budget := &domain.Budget{
		UserID:         "user-123",
		Name:           "Bad Income Source",
		Period:         domain.BudgetPeriodMonthly,
		StartDate:      time.Now(),
		Amount:         1000,
		ExpectedIncome: 1000,
		CurrencyCode:   "USD",
		IncomeSources:  []domain.IncomeSource{badSource},
	}
	// Use the REAL domain validation, not a hand-written stand-in, so this test tracks
	// whatever Budget.Validate() actually returns.
	realErr := budget.Validate()
	require.Error(t, realErr, "fixture budget must fail Validate() on its bad income source")

	mockService := &mockBudgetService{createErr: realErr}
	handler := NewBudgetHandler(mockService, &mockBudgetSyncService{}, services.NewTransactionService(&mockTransactionRepoForBudget{}, &mockCustomCategoryRepoForBudget{}))

	input := &CreateBudgetInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"}}
	input.Body = domain.CreateBudgetRequest{
		Name:          "Bad Income Source",
		Period:        domain.BudgetPeriodMonthly,
		StartDate:     time.Now(),
		Amount:        1000,
		CurrencyCode:  "USD",
		IncomeSources: []domain.IncomeSource{badSource},
	}

	out, err := handler.CreateBudget(context.Background(), input)
	assert.Nil(t, out)
	statusErr := budgetStatusErr(t, err)

	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus(),
		"was 500 pre-#1630 task 7 (unwrapped errors.New falling through mapper's default case)")
	body := statusErr.Error()
	assert.Contains(t, body, "income_sources[0]")
	assert.Contains(t, body, "invalid income source type")
	assert.Contains(t, body, "pfc_primary", "legal values must be discoverable in the error body")
	assert.Contains(t, body, "tag")
	assert.Contains(t, body, "manual")
}

// TestAddCategory_InvalidMatchLogicCombination_Returns400WithFieldName is the RED half of Task 7
// for P6 (task 0's TestClaim1630_P6_MatchLogicAnd_EmptyTags_Is500NotStruct400): match_logic="and"
// with no tags is a legal-looking value combined into an illegal state, and used to 500.
func TestAddCategory_InvalidMatchLogicCombination_Returns400WithFieldName(t *testing.T) {
	category := &domain.BudgetCategory{
		BudgetID:     "budget-123",
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		MatchLogic:   domain.MatchLogicAnd,
		// Tags deliberately omitted.
	}
	realErr := category.Validate()
	require.Error(t, realErr)

	mockService := &mockBudgetService{addCategoryErr: realErr}
	handler := NewBudgetHandler(mockService, &mockBudgetSyncService{}, services.NewTransactionService(&mockTransactionRepoForBudget{}, &mockCustomCategoryRepoForBudget{}))

	input := &AddCategoryInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"}, ID: "budget-123"}
	input.Body = domain.CreateBudgetCategoryRequest{
		CategoryKind:    "pfc",
		PFCPrimary:      ptrStr("FOOD_AND_DRINK"),
		AllocatedAmount: 100,
		MatchLogic:      domain.MatchLogicAnd,
	}

	out, err := handler.AddCategory(context.Background(), input)
	assert.Nil(t, out)
	statusErr := budgetStatusErr(t, err)

	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus(),
		"was 500 pre-#1630 task 7")
	body := statusErr.Error()
	assert.Contains(t, body, "tags")
	assert.Contains(t, body, "match_logic")
}

// TestAddCategory_InvalidCategoryKind_Returns400WithLegalValues covers the fourth Task 7 claim:
// an invalid category_kind.
func TestAddCategory_InvalidCategoryKind_Returns400WithLegalValues(t *testing.T) {
	category := &domain.BudgetCategory{
		BudgetID:     "budget-123",
		CategoryKind: "not-a-real-kind",
	}
	realErr := category.Validate()
	require.Error(t, realErr)

	mockService := &mockBudgetService{addCategoryErr: realErr}
	handler := NewBudgetHandler(mockService, &mockBudgetSyncService{}, services.NewTransactionService(&mockTransactionRepoForBudget{}, &mockCustomCategoryRepoForBudget{}))

	input := &AddCategoryInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"}, ID: "budget-123"}
	input.Body = domain.CreateBudgetCategoryRequest{
		CategoryKind:    "not-a-real-kind",
		AllocatedAmount: 100,
	}

	out, err := handler.AddCategory(context.Background(), input)
	assert.Nil(t, out)
	statusErr := budgetStatusErr(t, err)

	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
	body := statusErr.Error()
	assert.Contains(t, body, "category_kind")
	assert.Contains(t, body, "pfc")
	assert.Contains(t, body, "custom")
	assert.Contains(t, body, "savings")
}

// TestCreateBudget_NegativeAmount_Returns400WithFieldName covers the last Task 7 claim: a
// negative `amount`.
func TestCreateBudget_NegativeAmount_Returns400WithFieldName(t *testing.T) {
	budget := &domain.Budget{
		UserID:         "user-123",
		Name:           "Negative Amount",
		Period:         domain.BudgetPeriodMonthly,
		StartDate:      time.Now(),
		Amount:         -50,
		ExpectedIncome: 100,
		CurrencyCode:   "USD",
	}
	realErr := budget.Validate()
	require.Error(t, realErr)

	mockService := &mockBudgetService{createErr: realErr}
	handler := NewBudgetHandler(mockService, &mockBudgetSyncService{}, services.NewTransactionService(&mockTransactionRepoForBudget{}, &mockCustomCategoryRepoForBudget{}))

	input := &CreateBudgetInput{AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"}}
	input.Body = domain.CreateBudgetRequest{
		Name:         "Negative Amount",
		Period:       domain.BudgetPeriodMonthly,
		StartDate:    time.Now(),
		Amount:       -50,
		CurrencyCode: "USD",
	}

	out, err := handler.CreateBudget(context.Background(), input)
	assert.Nil(t, out)
	statusErr := budgetStatusErr(t, err)

	assert.Equal(t, http.StatusBadRequest, statusErr.GetStatus())
	assert.Contains(t, statusErr.Error(), "amount")
}
