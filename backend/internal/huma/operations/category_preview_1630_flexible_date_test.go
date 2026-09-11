package operations

// Issue #1630 Task B5 (gap #21): previewCategoryMatches was the last write-shaped operation
// still carrying P3's original YYYY-MM-DD-only inconsistency — start_date/end_date were plain
// strings parsed by dateutil.ParseDate, which rejects RFC3339, with no `format` in the
// published schema. This mirrors budget_1630_flexible_date_test.go's coverage for
// createBudget/updateBudget (Issue #1630 Task 9), now that CategoryPreviewRequest's dates are
// domain.FlexibleDate too.

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
)

func newPreviewFlexibleDateTestAPI(t *testing.T) humatest.TestAPI {
	t.Helper()
	authed := func(ctx huma.Context, next func(huma.Context)) {
		next(huma.WithValue(ctx, inputs.UserIDKey, "user-123"))
	}
	_, api := humatest.New(t)
	api.UseMiddleware(authed)

	mockService := &mockCategoryPreviewService{
		response: &domain.CategoryPreviewResponse{TransactionCount: 0, TotalAmount: 0},
	}
	handler := NewCategoryPreviewHandler(mockService)
	RegisterCategoryPreviewRoutes(api, handler)
	return api
}

func TestPreviewCategoryMatches_StartDate_AcceptsPlainCalendarDay(t *testing.T) {
	api := newPreviewFlexibleDateTestAPI(t)

	resp := api.Post("/api/v1/budgets/categories/preview", map[string]interface{}{
		"category_kind": "pfc",
		"pfc_primary":   "FOOD_AND_DRINK",
		"match_logic":   "category_only",
		"start_date":    "2026-09-01",
		"end_date":      "2026-09-30",
	})

	assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
}

func TestPreviewCategoryMatches_StartDate_KeepsAcceptingRFC3339(t *testing.T) {
	api := newPreviewFlexibleDateTestAPI(t)

	resp := api.Post("/api/v1/budgets/categories/preview", map[string]interface{}{
		"category_kind": "pfc",
		"pfc_primary":   "FOOD_AND_DRINK",
		"match_logic":   "category_only",
		"start_date":    "2026-09-01T00:00:00Z",
		"end_date":      "2026-09-30T00:00:00Z",
	})

	assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
}

func TestPreviewCategoryMatches_StartDate_RejectsUnrecognizedFormat(t *testing.T) {
	api := newPreviewFlexibleDateTestAPI(t)

	resp := api.Post("/api/v1/budgets/categories/preview", map[string]interface{}{
		"category_kind": "pfc",
		"pfc_primary":   "FOOD_AND_DRINK",
		"match_logic":   "category_only",
		"start_date":    "09/01/2026",
		"end_date":      "2026-09-30",
	})

	assert.Equal(t, http.StatusUnprocessableEntity, resp.Code, resp.Body.String())
	assert.Contains(t, resp.Body.String(), "start_date",
		"a schema rejection must still name the offending field")
}

func TestPreviewCategoryMatches_EndDate_AcceptsBothFormsAlongsideStartDate(t *testing.T) {
	api := newPreviewFlexibleDateTestAPI(t)

	resp := api.Post("/api/v1/budgets/categories/preview", map[string]interface{}{
		"category_kind": "pfc",
		"pfc_primary":   "FOOD_AND_DRINK",
		"match_logic":   "category_only",
		"start_date":    "2026-09-01",
		"end_date":      "2026-09-30T00:00:00Z",
	})

	assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
}
