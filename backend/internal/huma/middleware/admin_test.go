package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	humamiddleware "github.com/maxnussbaum/BoostedBudget/internal/huma/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockAdminService is a mock implementation of AdminService for testing.
type MockAdminService struct {
	mock.Mock
}

func (m *MockAdminService) HasRole(ctx context.Context, clerkUserID string, role domain.AdminRole) (bool, error) {
	args := m.Called(ctx, clerkUserID, role)
	return args.Bool(0), args.Error(1)
}

func (m *MockAdminService) GetRoles(ctx context.Context, clerkUserID string) ([]domain.AdminRole, error) {
	args := m.Called(ctx, clerkUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.AdminRole), args.Error(1)
}

func (m *MockAdminService) InvalidateCache(clerkUserID string) {
	m.Called(clerkUserID)
}

func (m *MockAdminService) IsHardcodedAdmin(email string) bool {
	args := m.Called(email)
	return args.Bool(0)
}

// Ensure mock implements the interface
var _ services.AdminService = (*MockAdminService)(nil)

// setupAdminTestAPIBase creates a test API with admin middleware and simulated Clerk middleware.
// The returned server can be used to test admin endpoints.
func setupAdminTestAPIBase(t *testing.T, cfg *config.Config, adminService services.AdminService) (*httptest.Server, huma.API) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))

	// Add middleware that simulates Clerk by setting context values from test headers
	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		clerkUserID := ctx.Header("X-Test-Clerk-User-ID")
		email := ctx.Header("X-Test-Email")

		if clerkUserID != "" {
			newCtx := context.WithValue(ctx.Context(), inputs.ClerkUserIDKey, clerkUserID)

			if email != "" {
				claims := &clerk.SessionClaims{
					Custom: map[string]interface{}{"email": email},
				}
				newCtx = context.WithValue(newCtx, inputs.ClerkClaimsKey, claims)
			}

			next(huma.WithContext(ctx, newCtx))
		} else {
			next(ctx)
		}
	})

	// Add the Admin middleware
	adminMiddleware := humamiddleware.NewAdminMiddleware(cfg, adminService)
	api.UseMiddleware(adminMiddleware)

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return server, api
}

// registerTestEndpoints registers test endpoints for admin middleware testing.
func registerTestEndpoints(api huma.API) {
	// Admin endpoint
	huma.Register(api, huma.Operation{
		OperationID: "admin-endpoint",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/test",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct {
			Message string `json:"message"`
		}
	}, error) {
		return &struct {
			Body struct {
				Message string `json:"message"`
			}
		}{Body: struct {
			Message string `json:"message"`
		}{Message: "admin success"}}, nil
	})

	// Non-admin endpoint
	huma.Register(api, huma.Operation{
		OperationID: "normal-endpoint",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/me",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct {
			Message string `json:"message"`
		}
	}, error) {
		return &struct {
			Body struct {
				Message string `json:"message"`
			}
		}{Body: struct {
			Message string `json:"message"`
		}{Message: "user success"}}, nil
	})

	// Check-role endpoint (Issue #638)
	huma.Register(api, huma.Operation{
		OperationID: "checkRole",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/check-role",
		Security:    []map[string][]string{{"clerk": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct {
		Body struct {
			IsAdmin bool `json:"is_admin"`
		}
	}, error) {
		return &struct {
			Body struct {
				IsAdmin bool `json:"is_admin"`
			}
		}{Body: struct {
			IsAdmin bool `json:"is_admin"`
		}{IsAdmin: true}}, nil
	})
}

// Helper to create test API with admin middleware
func setupAdminTestAPI(t *testing.T, cfg *config.Config, adminService services.AdminService) (*httptest.Server, huma.API) {
	server, api := setupAdminTestAPIBase(t, cfg, adminService)
	registerTestEndpoints(api)
	return server, api
}

// TestAdminMiddleware_QAEnvironment_AdminRole_Allows tests that admin users can access admin endpoints in QA
func TestAdminMiddleware_QAEnvironment_AdminRole_Allows(t *testing.T) {
	// GIVEN: QA environment and user with admin role via Clerk privateMetadata
	cfg := &config.Config{Environment: "qa"}
	mockAdminService := new(MockAdminService)
	mockAdminService.On("IsHardcodedAdmin", "user@example.com").Return(false)
	mockAdminService.On("HasRole", mock.Anything, "user_admin", domain.RoleAdmin).Return(true, nil)

	server, _ := setupAdminTestAPI(t, cfg, mockAdminService)

	// WHEN: Admin user requests admin endpoint
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/test", nil)
	require.NoError(t, err)
	req.Header.Set("X-Test-Clerk-User-ID", "user_admin")
	req.Header.Set("X-Test-Email", "user@example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// THEN: Should allow access
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	mockAdminService.AssertExpectations(t)
}

// TestAdminMiddleware_QAEnvironment_HardcodedEmail_Allows tests that hardcoded admin emails bypass Clerk check
func TestAdminMiddleware_QAEnvironment_HardcodedEmail_Allows(t *testing.T) {
	// GIVEN: QA environment and user with hardcoded admin email
	cfg := &config.Config{Environment: "qa"}
	mockAdminService := new(MockAdminService)
	mockAdminService.On("IsHardcodedAdmin", "owner@company.com").Return(true)
	// HasRole should NOT be called because hardcoded email check passes first

	server, _ := setupAdminTestAPI(t, cfg, mockAdminService)

	// WHEN: Hardcoded admin requests admin endpoint
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/test", nil)
	require.NoError(t, err)
	req.Header.Set("X-Test-Clerk-User-ID", "user_owner")
	req.Header.Set("X-Test-Email", "owner@company.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// THEN: Should allow access without checking Clerk privateMetadata
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	mockAdminService.AssertNotCalled(t, "HasRole")
}

// TestAdminMiddleware_QAEnvironment_NoAdminRole_Returns403 tests that non-admin users get 403
func TestAdminMiddleware_QAEnvironment_NoAdminRole_Returns403(t *testing.T) {
	// GIVEN: QA environment and user without admin role
	cfg := &config.Config{Environment: "qa"}
	mockAdminService := new(MockAdminService)
	mockAdminService.On("IsHardcodedAdmin", "regular@example.com").Return(false)
	mockAdminService.On("HasRole", mock.Anything, "user_regular", domain.RoleAdmin).Return(false, nil)

	server, _ := setupAdminTestAPI(t, cfg, mockAdminService)

	// WHEN: Non-admin user requests admin endpoint
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/test", nil)
	require.NoError(t, err)
	req.Header.Set("X-Test-Clerk-User-ID", "user_regular")
	req.Header.Set("X-Test-Email", "regular@example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// THEN: Should return 403 Forbidden
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	mockAdminService.AssertExpectations(t)
}

// TestAdminMiddleware_ProductionEnvironment_Returns404 tests that admin endpoints return 404 in production
func TestAdminMiddleware_ProductionEnvironment_Returns404(t *testing.T) {
	// GIVEN: Production environment and admin user
	cfg := &config.Config{Environment: "production"}
	mockAdminService := new(MockAdminService)
	// Admin service should NOT be called because environment check fails first

	server, _ := setupAdminTestAPI(t, cfg, mockAdminService)

	// WHEN: Admin user requests admin endpoint in production
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/test", nil)
	require.NoError(t, err)
	req.Header.Set("X-Test-Clerk-User-ID", "user_admin")
	req.Header.Set("X-Test-Email", "admin@example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// THEN: Should return 404 (security through obscurity)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	mockAdminService.AssertNotCalled(t, "HasRole")
	mockAdminService.AssertNotCalled(t, "IsHardcodedAdmin")
}

// TestAdminMiddleware_DevelopmentEnvironment_Returns404 tests that admin endpoints return 404 in development
func TestAdminMiddleware_DevelopmentEnvironment_Returns404(t *testing.T) {
	// GIVEN: Development environment
	cfg := &config.Config{Environment: "development"}
	mockAdminService := new(MockAdminService)

	server, _ := setupAdminTestAPI(t, cfg, mockAdminService)

	// WHEN: User requests admin endpoint in development
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/test", nil)
	require.NoError(t, err)
	req.Header.Set("X-Test-Clerk-User-ID", "user_admin")
	req.Header.Set("X-Test-Email", "admin@example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// THEN: Should return 404
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestAdminMiddleware_StagingEnvironment_AdminRole_Allows tests that admin users can access admin endpoints in staging
func TestAdminMiddleware_StagingEnvironment_AdminRole_Allows(t *testing.T) {
	// GIVEN: Staging environment and user with admin role via Clerk privateMetadata
	cfg := &config.Config{Environment: "staging"}
	mockAdminService := new(MockAdminService)
	mockAdminService.On("IsHardcodedAdmin", "user@example.com").Return(false)
	mockAdminService.On("HasRole", mock.Anything, "user_admin", domain.RoleAdmin).Return(true, nil)

	server, _ := setupAdminTestAPI(t, cfg, mockAdminService)

	// WHEN: Admin user requests admin endpoint
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/test", nil)
	require.NoError(t, err)
	req.Header.Set("X-Test-Clerk-User-ID", "user_admin")
	req.Header.Set("X-Test-Email", "user@example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// THEN: Should allow access
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	mockAdminService.AssertExpectations(t)
}

// TestAdminMiddleware_NoClerkUserID_Returns401 tests that missing Clerk user ID returns 401
func TestAdminMiddleware_NoClerkUserID_Returns401(t *testing.T) {
	// GIVEN: QA environment but no Clerk user ID in context
	cfg := &config.Config{Environment: "qa"}
	mockAdminService := new(MockAdminService)

	server, _ := setupAdminTestAPI(t, cfg, mockAdminService)

	// WHEN: Request without Clerk user ID
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/test", nil)
	require.NoError(t, err)
	// Not setting X-Test-Clerk-User-ID header

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// THEN: Should return 401 Unauthorized
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestAdminMiddleware_AdminServiceError_Returns500 tests that admin service errors return 500
func TestAdminMiddleware_AdminServiceError_Returns500(t *testing.T) {
	// GIVEN: QA environment but admin service returns an error
	cfg := &config.Config{Environment: "qa"}
	mockAdminService := new(MockAdminService)
	mockAdminService.On("IsHardcodedAdmin", "user@example.com").Return(false)
	mockAdminService.On("HasRole", mock.Anything, "user_error", domain.RoleAdmin).Return(false, services.ErrClerkAPIError)

	server, _ := setupAdminTestAPI(t, cfg, mockAdminService)

	// WHEN: Admin service encounters an error
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/test", nil)
	require.NoError(t, err)
	req.Header.Set("X-Test-Clerk-User-ID", "user_error")
	req.Header.Set("X-Test-Email", "user@example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// THEN: Should return 500 Internal Server Error
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	mockAdminService.AssertExpectations(t)
}

// --- Issue #638: Check-Role Endpoint Tests ---

// TestAdminMiddleware_CheckRole_SkipsEnvironmentCheck tests that check-role works in all environments
func TestAdminMiddleware_CheckRole_SkipsEnvironmentCheck(t *testing.T) {
	// Test in multiple environments - check-role should work in all of them
	envs := []string{"production", "development", "staging", "qa"}

	for _, env := range envs {
		t.Run("Environment_"+env, func(t *testing.T) {
			// GIVEN: Any environment and authenticated non-admin user
			cfg := &config.Config{Environment: env}
			mockAdminService := new(MockAdminService)
			// Admin service should NOT be called by middleware (only by handler)

			server, _ := setupAdminTestAPI(t, cfg, mockAdminService)

			// WHEN: Non-admin user requests check-role endpoint
			req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/check-role", nil)
			require.NoError(t, err)
			req.Header.Set("X-Test-Clerk-User-ID", "user_regular")
			req.Header.Set("X-Test-Email", "regular@example.com")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			// THEN: Should allow access (middleware skips env and role checks for this endpoint)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			// Middleware should NOT call admin service - that's the handler's job
			mockAdminService.AssertNotCalled(t, "HasRole")
			mockAdminService.AssertNotCalled(t, "IsHardcodedAdmin")
		})
	}
}

// TestAdminMiddleware_CheckRole_RequiresAuthentication tests that check-role still requires auth
func TestAdminMiddleware_CheckRole_RequiresAuthentication(t *testing.T) {
	// GIVEN: Any environment but no authentication
	cfg := &config.Config{Environment: "production"}
	mockAdminService := new(MockAdminService)

	server, _ := setupAdminTestAPI(t, cfg, mockAdminService)

	// WHEN: Unauthenticated user requests check-role endpoint
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/check-role", nil)
	require.NoError(t, err)
	// Not setting X-Test-Clerk-User-ID header

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// THEN: Should return 401 Unauthorized
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestAdminMiddleware_NonAdminEndpoint_Passthrough tests that non-admin endpoints are not affected
func TestAdminMiddleware_NonAdminEndpoint_Passthrough(t *testing.T) {
	// GIVEN: QA environment and regular user accessing non-admin endpoint
	cfg := &config.Config{Environment: "qa"}
	mockAdminService := new(MockAdminService)
	// Admin service should NOT be called for non-admin endpoints

	server, _ := setupAdminTestAPI(t, cfg, mockAdminService)

	// WHEN: Regular user requests non-admin endpoint
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/users/me", nil)
	require.NoError(t, err)
	req.Header.Set("X-Test-Clerk-User-ID", "user_regular")
	req.Header.Set("X-Test-Email", "regular@example.com")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// THEN: Should allow access (admin middleware should not interfere)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	mockAdminService.AssertNotCalled(t, "HasRole")
	mockAdminService.AssertNotCalled(t, "IsHardcodedAdmin")
}
