package operations

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAdminService is a mock implementation of services.AdminService for check-role tests
type mockAdminService struct {
	hasRoleResult    bool
	hasRoleErr       error
	isHardcodedAdmin bool
	getRolesResult   []domain.AdminRole
	getRolesErr      error
}

func (m *mockAdminService) HasRole(ctx context.Context, clerkUserID string, role domain.AdminRole) (bool, error) {
	if m.hasRoleErr != nil {
		return false, m.hasRoleErr
	}
	return m.hasRoleResult, nil
}

func (m *mockAdminService) GetRoles(ctx context.Context, clerkUserID string) ([]domain.AdminRole, error) {
	if m.getRolesErr != nil {
		return nil, m.getRolesErr
	}
	return m.getRolesResult, nil
}

func (m *mockAdminService) InvalidateCache(clerkUserID string) {
	// No-op for testing
}

func (m *mockAdminService) IsHardcodedAdmin(email string) bool {
	return m.isHardcodedAdmin
}

// --- CheckRole Tests (Issue #638) ---

// TestCheckRole_ReturnsTrue_ForAdminUser tests that admin users get {"is_admin": true}.
func TestCheckRole_ReturnsTrue_ForAdminUser(t *testing.T) {
	mockAdmin := &mockAdminService{
		hasRoleResult: true,
	}
	handler := NewAdminHandler(nil, nil, nil, nil, mockAdmin)

	input := &CheckRoleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ClerkUserID:        "clerk_user_123",
		Email:              "admin@example.com",
	}

	output, err := handler.CheckRole(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.IsAdmin)
}

// TestCheckRole_ReturnsFalse_ForNonAdminUser tests that non-admin users get {"is_admin": false}.
func TestCheckRole_ReturnsFalse_ForNonAdminUser(t *testing.T) {
	mockAdmin := &mockAdminService{
		hasRoleResult:    false,
		isHardcodedAdmin: false,
	}
	handler := NewAdminHandler(nil, nil, nil, nil, mockAdmin)

	input := &CheckRoleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-456"},
		ClerkUserID:        "clerk_user_456",
		Email:              "regular@example.com",
	}

	output, err := handler.CheckRole(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Body.IsAdmin)
}

// TestCheckRole_ReturnsTrue_ForHardcodedAdmin tests that hardcoded admin emails get {"is_admin": true}.
func TestCheckRole_ReturnsTrue_ForHardcodedAdmin(t *testing.T) {
	mockAdmin := &mockAdminService{
		hasRoleResult:    false, // Not in Clerk roles
		isHardcodedAdmin: true,  // But is hardcoded admin
	}
	handler := NewAdminHandler(nil, nil, nil, nil, mockAdmin)

	input := &CheckRoleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-789"},
		ClerkUserID:        "clerk_user_789",
		Email:              "hardcoded-admin@example.com",
	}

	output, err := handler.CheckRole(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.IsAdmin)
}

// TestCheckRole_ReturnsError_WhenAdminServiceFails tests that AdminService errors are propagated.
func TestCheckRole_ReturnsError_WhenAdminServiceFails(t *testing.T) {
	mockAdmin := &mockAdminService{
		hasRoleErr:       errors.New("clerk API error"),
		isHardcodedAdmin: false,
	}
	handler := NewAdminHandler(nil, nil, nil, nil, mockAdmin)

	input := &CheckRoleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-999"},
		ClerkUserID:        "clerk_user_999",
		Email:              "user@example.com",
	}

	output, err := handler.CheckRole(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to check admin role")
}

// TestCheckRole_ChecksHardcodedAdminFirst tests that hardcoded admin check runs before Clerk API.
func TestCheckRole_ChecksHardcodedAdminFirst(t *testing.T) {
	// If IsHardcodedAdmin returns true, HasRole should NOT be called
	mockAdmin := &mockAdminService{
		isHardcodedAdmin: true,
		hasRoleErr:       errors.New("should not be called"),
	}
	handler := NewAdminHandler(nil, nil, nil, nil, mockAdmin)

	input := &CheckRoleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ClerkUserID:        "clerk_user_123",
		Email:              "hardcoded@example.com",
	}

	output, err := handler.CheckRole(context.Background(), input)

	// Should succeed because hardcoded admin check bypasses HasRole
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.True(t, output.Body.IsAdmin)
}

// TestCheckRole_RouteRegistration tests that the route is properly registered.
func TestCheckRole_RouteRegistration(t *testing.T) {
	mockAdmin := &mockAdminService{}
	handler := NewAdminHandler(nil, nil, nil, nil, mockAdmin)

	_, api := humatest.New(t)
	RegisterAdminRoutes(api, handler)

	// Test GET /api/v1/admin/check-role (without auth, should fail with 401)
	resp := api.Get("/api/v1/admin/check-role")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

// TestCheckRole_NilAdminService tests that nil AdminService returns error.
func TestCheckRole_NilAdminService(t *testing.T) {
	handler := NewAdminHandler(nil, nil, nil, nil, nil)

	input := &CheckRoleInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ClerkUserID:        "clerk_user_123",
		Email:              "user@example.com",
	}

	output, err := handler.CheckRole(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Admin service not configured")
}
