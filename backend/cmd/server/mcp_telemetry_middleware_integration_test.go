package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaconfig "github.com/maxnussbaum/BoostedBudget/internal/huma"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/operations"
	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

type telemetryHouseholdLookup struct{ calls int }

func (l *telemetryHouseholdLookup) GetHouseholdForUser(context.Context, string) (*domain.Household, bool, bool, error) {
	l.calls++
	return &domain.Household{ID: "household-1"}, false, true, nil
}

func buildMCPMiddlewareAPI(t *testing.T) (*echo.Echo, *redisclient.Fake, *telemetryHouseholdLookup, *int) {
	t.Helper()
	e := echo.New()
	api := humaecho.NewV4(e, humaconfig.NewAPIConfig("test", "test"))
	fake := redisclient.NewFake("cache")
	household := &telemetryHouseholdLookup{}
	controlCalls := 0

	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		requestContext := ctx.Context()
		if userID := ctx.Header("X-Test-User-Id"); userID != "" {
			requestContext = context.WithValue(requestContext, inputs.UserIDKey, userID)
		}
		if status := ctx.Header("X-Test-Subscription-Status"); status != "" {
			requestContext = context.WithValue(requestContext, inputs.EffectiveSubscriptionStatusKey, status)
		}
		if ctx.Header("X-Test-Subscription-Unresolved") == "true" {
			requestContext = context.WithValue(requestContext, inputs.SubscriptionStatusUnresolvedKey, true)
		}
		next(huma.WithContext(ctx, requestContext))
	})
	api.UseMiddleware(humamiddleware.NewReadOnlyMiddleware(nil))
	api.UseMiddleware(humamiddleware.NewCacheInvalidationMiddleware(cache.NewGenerations(fake), household, nil))

	operations.RegisterMCPToolTelemetryRoutes(api, operations.NewMCPToolTelemetryHandler(nil, "worker-secret"))
	huma.Register(api, huma.Operation{
		OperationID: "mcpTelemetryMutationControl",
		Method:      http.MethodPost,
		Path:        "/api/v1/control/mutation",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(context.Context, *struct{}) (*struct{ Body struct{} }, error) {
		controlCalls++
		return &struct{ Body struct{} }{}, nil
	})

	return e, fake, household, &controlCalls
}

func postMCPMiddlewareTelemetry(t *testing.T, e *echo.Echo, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/telemetry", strings.NewReader(`{
		"tool_surface":"search",
		"tool_family":"discovery",
		"outcome":"succeeded",
		"duration_bucket":"under_250ms"
	}`))
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	e.ServeHTTP(response, request)
	return response
}

func TestMCPToolTelemetryProductionMiddlewareDoesNotInvalidateCaches(t *testing.T) {
	e, fake, household, _ := buildMCPMiddlewareAPI(t)

	response := postMCPMiddlewareTelemetry(t, e, map[string]string{
		"X-Test-User-Id":             "user-1",
		"X-Test-Subscription-Status": "active",
		"X-MCP-Worker-Secret":        "worker-secret",
	})

	require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
	assert.Zero(t, fake.SetCalls()+fake.SetNXCalls(), "telemetry must not bump the user or household generation")
	assert.Zero(t, household.calls, "telemetry must not perform a household lookup")
}

func TestMCPToolTelemetryProductionMiddlewareAllowsReadOnlyAndUnresolvedUsers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
	}{
		{name: "inactive", headers: map[string]string{"X-Test-Subscription-Status": "inactive"}},
		{name: "cancelled", headers: map[string]string{"X-Test-Subscription-Status": "cancelled"}},
		{name: "past due", headers: map[string]string{"X-Test-Subscription-Status": "past_due"}},
		{name: "status unresolved", headers: map[string]string{"X-Test-Subscription-Unresolved": "true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, fake, household, _ := buildMCPMiddlewareAPI(t)
			tc.headers["X-Test-User-Id"] = "user-1"
			tc.headers["X-MCP-Worker-Secret"] = "worker-secret"

			response := postMCPMiddlewareTelemetry(t, e, tc.headers)

			require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
			assert.Zero(t, fake.SetCalls()+fake.SetNXCalls())
			assert.Zero(t, household.calls)
		})
	}
}

func TestMCPToolTelemetryProductionMiddlewarePreservesAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name       string
		headers    map[string]string
		wantStatus int
	}{
		{name: "missing user", headers: map[string]string{"X-MCP-Worker-Secret": "worker-secret"}, wantStatus: http.StatusUnauthorized},
		{name: "missing secret", headers: map[string]string{"X-Test-User-Id": "user-1"}, wantStatus: http.StatusUnprocessableEntity},
		{name: "wrong secret", headers: map[string]string{"X-Test-User-Id": "user-1", "X-MCP-Worker-Secret": "wrong"}, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, fake, household, _ := buildMCPMiddlewareAPI(t)

			response := postMCPMiddlewareTelemetry(t, e, tc.headers)

			assert.Equal(t, tc.wantStatus, response.Code, response.Body.String())
			assert.Zero(t, fake.SetCalls()+fake.SetNXCalls())
			assert.Zero(t, household.calls)
		})
	}
}

func TestMCPToolTelemetryProductionMiddlewareLeavesDomainMutationPoliciesUnchanged(t *testing.T) {
	t.Run("active mutation still bumps", func(t *testing.T) {
		e, fake, household, controlCalls := buildMCPMiddlewareAPI(t)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/control/mutation", nil)
		request.Header.Set("X-Test-User-Id", "user-1")
		request.Header.Set("X-Test-Subscription-Status", "active")
		response := httptest.NewRecorder()

		e.ServeHTTP(response, request)

		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Equal(t, 1, *controlCalls)
		assert.Positive(t, fake.SetCalls()+fake.SetNXCalls())
		assert.Positive(t, household.calls)
	})

	t.Run("read-only mutation stays blocked", func(t *testing.T) {
		e, fake, household, controlCalls := buildMCPMiddlewareAPI(t)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/control/mutation", nil)
		request.Header.Set("X-Test-User-Id", "user-1")
		request.Header.Set("X-Test-Subscription-Status", "inactive")
		response := httptest.NewRecorder()

		e.ServeHTTP(response, request)

		assert.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		assert.Zero(t, *controlCalls)
		assert.Zero(t, fake.SetCalls()+fake.SetNXCalls())
		assert.Zero(t, household.calls)
	})
}
