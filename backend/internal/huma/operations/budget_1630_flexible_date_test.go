package operations

// Issue #1630, Task 9: every READ endpoint documents and accepts a plain "YYYY-MM-DD" query
// string, but CreateBudgetRequest.StartDate/EndDate were bare time.Time, which Huma publishes as
// format:"date-time" and validates strictly against RFC3339 — so "2026-09-01" alone 422'd and
// only the RFC3339 timestamp form worked. domain.FlexibleDate fixes this by publishing
// `anyOf: [{type: string, format: date}, {type: string, format: date-time}]` via
// huma.SchemaProvider and accepting both forms in its own UnmarshalJSON.
//
// These tests go through the real router (humatest + RegisterBudgetRoutes), not a direct handler
// call, exactly like TestCreateGoalSchema_RejectsAtTheFrameworkForSingleFieldConstraints in
// savings_goal_test.go — the anyOf schema validation this task adds only runs on the actual HTTP
// path (huma/v2 huma.go validateBody), never on a direct Go struct literal handler call.

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// newFlexibleDateTestAPI builds a real router with an authenticated middleware, mirroring
// TestCreateGoalSchema_RejectsAtTheFrameworkForSingleFieldConstraints's setup.
func newFlexibleDateTestAPI(t *testing.T) humatest.TestAPI {
	t.Helper()
	authed := func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithValue(ctx, inputs.UserIDKey, "user-123"))
	}
	_, api := humatest.New(t)
	api.UseMiddleware(authed)

	mockService := &mockBudgetService{budget: createTestBudget()}
	mockSyncService := &mockBudgetSyncService{}
	mockTxRepo := &mockTransactionRepoForBudget{}
	handler := NewBudgetHandler(mockService, mockSyncService,
		services.NewTransactionService(mockTxRepo, &mockCustomCategoryRepoForBudget{}))
	RegisterBudgetRoutes(api, handler)
	return api
}

// TestCreateBudget_StartDate_AcceptsPlainCalendarDay is the RED half of Task 9: pre-fix,
// StartDate was a bare time.Time (format:"date-time"), so "2026-09-01" 422'd.
func TestCreateBudget_StartDate_AcceptsPlainCalendarDay(t *testing.T) {
	api := newFlexibleDateTestAPI(t)

	resp := api.Post("/api/v1/budgets", map[string]interface{}{
		"name":       "Plain Date Budget",
		"period":     "monthly",
		"start_date": "2026-09-01",
		"amount":     100.00,
	})

	assert.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())
}

// TestCreateBudget_StartDate_KeepsAcceptingRFC3339 pins the regression risk this task's brief
// calls out explicitly: the existing frontend already sends the RFC3339 timestamp form, and it
// must keep working.
func TestCreateBudget_StartDate_KeepsAcceptingRFC3339(t *testing.T) {
	api := newFlexibleDateTestAPI(t)

	resp := api.Post("/api/v1/budgets", map[string]interface{}{
		"name":       "RFC3339 Date Budget",
		"period":     "monthly",
		"start_date": "2026-09-01T00:00:00Z",
		"amount":     100.00,
	})

	assert.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())
}

// TestCreateBudget_StartDate_RejectsUnrecognizedFormat proves a shape that is neither
// YYYY-MM-DD nor RFC3339 still 422s, naming the offending field, rather than silently coercing
// or falling through to an unhelpful 500.
func TestCreateBudget_StartDate_RejectsUnrecognizedFormat(t *testing.T) {
	api := newFlexibleDateTestAPI(t)

	resp := api.Post("/api/v1/budgets", map[string]interface{}{
		"name":       "Bad Date Budget",
		"period":     "monthly",
		"start_date": "09/01/2026",
		"amount":     100.00,
	})

	assert.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
	assert.Contains(t, resp.Body.String(), "start_date",
		"a schema rejection must still name the offending field")
}

// TestCreateBudget_EndDate_AcceptsBothFormsAlongsideStartDate covers the pointer field
// (*domain.FlexibleDate) with both dates set, one in each accepted form, proving the two
// fields are independent and either form is accepted regardless of what the sibling field used.
func TestCreateBudget_EndDate_AcceptsBothFormsAlongsideStartDate(t *testing.T) {
	api := newFlexibleDateTestAPI(t)

	resp := api.Post("/api/v1/budgets", map[string]interface{}{
		"name":       "Custom Period Budget",
		"period":     "custom",
		"start_date": "2026-09-01",
		"end_date":   "2026-09-30T00:00:00Z",
		"amount":     100.00,
	})

	assert.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())
}

// TestFlexibleDate_UnmarshalJSON round-trips both accepted UnmarshalJSON forms directly against the type,
// independent of the HTTP layer, and confirms MarshalJSON always emits RFC3339 (no response
// shape change from the pre-existing time.Time wire format).
func TestFlexibleDate_UnmarshalJSON(t *testing.T) {
	t.Run("plain calendar day", func(t *testing.T) {
		var d domain.FlexibleDate
		err := d.UnmarshalJSON([]byte(`"2026-09-01"`))
		assert.NoError(t, err)
		assert.Equal(t, 2026, d.Time().Year())
		assert.Equal(t, 9, int(d.Time().Month()))
		assert.Equal(t, 1, d.Time().Day())
	})

	t.Run("RFC3339 timestamp", func(t *testing.T) {
		var d domain.FlexibleDate
		err := d.UnmarshalJSON([]byte(`"2026-09-01T00:00:00Z"`))
		assert.NoError(t, err)
		assert.Equal(t, 2026, d.Time().Year())
	})

	t.Run("unrecognized format", func(t *testing.T) {
		var d domain.FlexibleDate
		err := d.UnmarshalJSON([]byte(`"09/01/2026"`))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "09/01/2026")
	})

	t.Run("marshal always emits RFC3339", func(t *testing.T) {
		var d domain.FlexibleDate
		assert.NoError(t, d.UnmarshalJSON([]byte(`"2026-09-01"`)))
		out, err := d.MarshalJSON()
		assert.NoError(t, err)
		assert.Equal(t, `"2026-09-01T00:00:00Z"`, string(out))
	})
}
