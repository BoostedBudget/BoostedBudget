package operations

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Task 9 gave CreateBudgetRequest.StartDate/EndDate a domain.FlexibleDate accepting both
// YYYY-MM-DD and RFC3339 (see budget_1630_flexible_date_test.go). UpdateBudgetRequest was left
// as *time.Time, so PUT /api/v1/budgets/{id} still 422'd "2026-09-01" — issue #1630's P3
// complaint verbatim, one verb over, and a create-vs-update asymmetry on the same resource.

func TestUpdateBudget_StartDate_AcceptsPlainCalendarDay(t *testing.T) {
	api := newFlexibleDateTestAPI(t)

	resp := api.Put("/api/v1/budgets/budget-123", map[string]interface{}{
		"start_date": "2026-09-01",
	})

	assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
}

func TestUpdateBudget_StartDate_KeepsAcceptingRFC3339(t *testing.T) {
	api := newFlexibleDateTestAPI(t)

	resp := api.Put("/api/v1/budgets/budget-123", map[string]interface{}{
		"start_date": "2026-09-01T00:00:00Z",
	})

	assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
}

func TestUpdateBudget_StartDate_RejectsUnrecognizedFormat(t *testing.T) {
	api := newFlexibleDateTestAPI(t)

	resp := api.Put("/api/v1/budgets/budget-123", map[string]interface{}{
		"start_date": "09/01/2026",
	})

	assert.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
	assert.Contains(t, resp.Body.String(), "start_date",
		"a schema rejection must still name the offending field")
}

func TestUpdateBudget_EndDate_AcceptsPlainCalendarDay(t *testing.T) {
	api := newFlexibleDateTestAPI(t)

	resp := api.Put("/api/v1/budgets/budget-123", map[string]interface{}{
		"end_date": "2026-09-30",
	})

	assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
}
