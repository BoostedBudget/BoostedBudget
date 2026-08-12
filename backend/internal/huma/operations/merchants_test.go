package operations_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/operations"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testAuthMiddleware reads ClerkUserIDHeader and injects inputs.UserIDKey into the
// request context, mirroring what the production Clerk JWT middleware does.
func testAuthMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if uid := c.Request().Header.Get(inputs.ClerkUserIDHeader); uid != "" {
			ctx := context.WithValue(c.Request().Context(), inputs.UserIDKey, uid)
			c.SetRequest(c.Request().WithContext(ctx))
		}
		return next(c)
	}
}

// MerchantServiceInterface aliases the operations package type for test convenience.
type MerchantServiceInterface = operations.MerchantServiceInterface

// ── Mock MerchantService ───────────────────────────────────────────────────

type mockMerchantSvc struct {
	listMerchantsResult          []domain.MerchantSummary
	listMerchantsErr             error
	duplicateSuggestionsResult   [][]domain.MerchantSummary
	duplicateSuggestionsErr      error
	setOverrideResult            *domain.MerchantNameOverride
	setOverrideErr               error
	clearOverrideErr             error
	bulkSetOverridesResult       []domain.MerchantNameOverride
	bulkSetOverridesErr          error
	mergeMerchantsResult         []domain.MerchantNameOverride
	mergeMerchantsErr            error
	getMerchantOverridesResult   []domain.MerchantNameOverride
	getMerchantOverridesErr      error
}

func (m *mockMerchantSvc) ListMerchants(ctx context.Context, userID string) ([]domain.MerchantSummary, error) {
	return m.listMerchantsResult, m.listMerchantsErr
}
func (m *mockMerchantSvc) GetDuplicateSuggestions(ctx context.Context, userID string) ([][]domain.MerchantSummary, error) {
	return m.duplicateSuggestionsResult, m.duplicateSuggestionsErr
}
func (m *mockMerchantSvc) SetOverride(ctx context.Context, userID, merchantID string, displayName *string, mergedIntoMerchantID *string) (*domain.MerchantNameOverride, error) {
	return m.setOverrideResult, m.setOverrideErr
}
func (m *mockMerchantSvc) ClearOverride(ctx context.Context, userID, merchantID string) error {
	return m.clearOverrideErr
}
func (m *mockMerchantSvc) BulkSetOverrides(ctx context.Context, userID string, reqs []services.SetOverrideRequest) ([]domain.MerchantNameOverride, error) {
	return m.bulkSetOverridesResult, m.bulkSetOverridesErr
}
func (m *mockMerchantSvc) MergeMerchants(ctx context.Context, userID string, merchantIDs []string, targetMerchantID string) ([]domain.MerchantNameOverride, error) {
	return m.mergeMerchantsResult, m.mergeMerchantsErr
}
func (m *mockMerchantSvc) GetMerchantOverrides(ctx context.Context, userID string) ([]domain.MerchantNameOverride, error) {
	return m.getMerchantOverridesResult, m.getMerchantOverridesErr
}

// ── Test helpers ───────────────────────────────────────────────────────────

func setupMerchantAPI(t *testing.T, svc MerchantServiceInterface) huma.API {
	t.Helper()
	e := echo.New()
	e.Use(testAuthMiddleware)
	// NewV4, not New: huma v2.39.0 repointed humaecho.New at Echo v5. NewV4 is the same
	// adapter for Echo v4, which this app still runs on.
	api := humaecho.NewV4(e, huma.DefaultConfig("Test API", "1.0.0"))
	handler := operations.NewMerchantHandler(svc)
	operations.RegisterMerchantRoutes(api, handler)
	return api
}

func merchantRequest(t *testing.T, api huma.API, method, path, body string) (int, string) {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set(inputs.ClerkUserIDHeader, "user-123")
	w := httptest.NewRecorder()
	api.Adapter().ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// ── Tests ──────────────────────────────────────────────────────────────────

func TestMerchantHandler_Unauthenticated(t *testing.T) {
	svc := &mockMerchantSvc{}
	api := setupMerchantAPI(t, svc)
	// No auth header → UserIDKey not injected → must 401, not 500
	req := httptest.NewRequest(http.MethodGet, "/api/v1/merchants", nil)
	w := httptest.NewRecorder()
	api.Adapter().ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMerchantHandler_GetMerchants_OK(t *testing.T) {
	svc := &mockMerchantSvc{
		listMerchantsResult: []domain.MerchantSummary{
			{MerchantID: "m-1", EffectiveDisplayName: "Starbucks", TotalSpend: 42.5, TransactionCount: 3},
		},
	}
	api := setupMerchantAPI(t, svc)
	code, body := merchantRequest(t, api, http.MethodGet, "/api/v1/merchants", "")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "Starbucks")
}

func TestMerchantHandler_GetMerchants_Empty(t *testing.T) {
	svc := &mockMerchantSvc{listMerchantsResult: []domain.MerchantSummary{}}
	api := setupMerchantAPI(t, svc)
	code, body := merchantRequest(t, api, http.MethodGet, "/api/v1/merchants", "")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "[]")
}

func TestMerchantHandler_GetMerchants_ServiceError(t *testing.T) {
	svc := &mockMerchantSvc{listMerchantsErr: errors.New("db error")}
	api := setupMerchantAPI(t, svc)
	code, _ := merchantRequest(t, api, http.MethodGet, "/api/v1/merchants", "")
	assert.Equal(t, http.StatusInternalServerError, code)
}

func TestMerchantHandler_GetDuplicateSuggestions_OK(t *testing.T) {
	svc := &mockMerchantSvc{
		duplicateSuggestionsResult: [][]domain.MerchantSummary{
			{
				{MerchantID: "m-1", EffectiveDisplayName: "Starbucks"},
				{MerchantID: "m-2", EffectiveDisplayName: "Starbucks Coffee"},
			},
		},
	}
	api := setupMerchantAPI(t, svc)
	code, body := merchantRequest(t, api, http.MethodGet, "/api/v1/merchants/duplicate-suggestions", "")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "Starbucks Coffee")
}

func TestMerchantHandler_SetMerchantOverride_OK(t *testing.T) {
	displayName := "Whole Foods"
	svc := &mockMerchantSvc{
		setOverrideResult: &domain.MerchantNameOverride{
			ID:          "ov-1",
			MerchantID:  "m-1",
			DisplayName: &displayName,
		},
	}
	api := setupMerchantAPI(t, svc)
	code, body := merchantRequest(t, api, http.MethodPut, "/api/v1/merchants/m-1/override",
		`{"display_name":"Whole Foods"}`)
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "Whole Foods")
}

func TestMerchantHandler_SetMerchantOverride_ValidationError(t *testing.T) {
	svc := &mockMerchantSvc{setOverrideErr: errors.New("at least one of display_name or merged_into_merchant_id must be set")}
	api := setupMerchantAPI(t, svc)
	code, _ := merchantRequest(t, api, http.MethodPut, "/api/v1/merchants/m-1/override", `{}`)
	// Empty body (no display_name, no merged_into) → 400
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestMerchantHandler_ClearMerchantOverride_OK(t *testing.T) {
	svc := &mockMerchantSvc{}
	api := setupMerchantAPI(t, svc)
	code, _ := merchantRequest(t, api, http.MethodDelete, "/api/v1/merchants/m-1/override", "")
	assert.Equal(t, http.StatusNoContent, code)
}

func TestMerchantHandler_ClearMerchantOverride_ServiceError(t *testing.T) {
	svc := &mockMerchantSvc{clearOverrideErr: errors.New("not found")}
	api := setupMerchantAPI(t, svc)
	code, _ := merchantRequest(t, api, http.MethodDelete, "/api/v1/merchants/m-1/override", "")
	assert.Equal(t, http.StatusInternalServerError, code)
}

func TestMerchantHandler_BulkSetOverrides_OK(t *testing.T) {
	displayName := "Amazon"
	svc := &mockMerchantSvc{
		bulkSetOverridesResult: []domain.MerchantNameOverride{
			{ID: "ov-1", MerchantID: "m-1", DisplayName: &displayName},
		},
	}
	api := setupMerchantAPI(t, svc)
	code, body := merchantRequest(t, api, http.MethodPost, "/api/v1/merchants/overrides/bulk",
		`{"overrides":[{"merchant_id":"m-1","display_name":"Amazon"}]}`)
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "Amazon")
}

func TestMerchantHandler_MergeMerchants_OK(t *testing.T) {
	svc := &mockMerchantSvc{
		mergeMerchantsResult: []domain.MerchantNameOverride{
			{ID: "ov-1", MerchantID: "m-2"},
		},
	}
	api := setupMerchantAPI(t, svc)
	code, body := merchantRequest(t, api, http.MethodPost, "/api/v1/merchants/merge",
		`{"merchant_ids":["m-2"],"target_merchant_id":"m-1"}`)
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "m-2")
}

func TestMerchantHandler_MergeMerchants_EmptyIDs(t *testing.T) {
	svc := &mockMerchantSvc{}
	api := setupMerchantAPI(t, svc)
	code, _ := merchantRequest(t, api, http.MethodPost, "/api/v1/merchants/merge",
		`{"merchant_ids":[],"target_merchant_id":"m-1"}`)
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestMerchantHandler_GetMerchantOverrides_OK(t *testing.T) {
	displayName := "Whole Foods"
	svc := &mockMerchantSvc{
		getMerchantOverridesResult: []domain.MerchantNameOverride{
			{ID: "ov-1", MerchantID: "m-1", DisplayName: &displayName},
		},
	}
	api := setupMerchantAPI(t, svc)
	code, body := merchantRequest(t, api, http.MethodGet, "/api/v1/merchants/overrides", "")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "Whole Foods")
}

func TestMerchantHandler_GetMerchantOverrides_Empty(t *testing.T) {
	svc := &mockMerchantSvc{getMerchantOverridesResult: []domain.MerchantNameOverride{}}
	api := setupMerchantAPI(t, svc)
	code, body := merchantRequest(t, api, http.MethodGet, "/api/v1/merchants/overrides", "")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "[]")
}

func TestMerchantHandler_GetMerchantOverrides_ServiceError(t *testing.T) {
	svc := &mockMerchantSvc{getMerchantOverridesErr: errors.New("db error")}
	api := setupMerchantAPI(t, svc)
	code, _ := merchantRequest(t, api, http.MethodGet, "/api/v1/merchants/overrides", "")
	assert.Equal(t, http.StatusInternalServerError, code)
}
