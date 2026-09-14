package operations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockAdminUserService is a mock implementation of services.AdminUserService
type mockAdminUserService struct {
	mock.Mock
}

func (m *mockAdminUserService) ListUsers(ctx context.Context, filters domain.AdminUserFilters, page, pageSize int, now time.Time) (*domain.AdminUserListResponse, error) {
	args := m.Called(ctx, filters, page, pageSize, now)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AdminUserListResponse), args.Error(1)
}

func (m *mockAdminUserService) GetUserDetail(ctx context.Context, userID string, now time.Time) (*domain.AdminUserDetail, error) {
	args := m.Called(ctx, userID, now)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AdminUserDetail), args.Error(1)
}

// GetOwnedHouseholdActiveMemberCount satisfies services.AdminUserService for tests in this file
// that don't exercise the subscription-detail endpoint (Issue #1452 — see
// admin_subscriptions_test.go for coverage).
func (m *mockAdminUserService) GetOwnedHouseholdActiveMemberCount(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

// UpdateUserTier satisfies services.AdminUserService for tests in this file that don't
// exercise tier-override updates (Issue #1413 — see admin_subscriptions_test.go for coverage).
func (m *mockAdminUserService) UpdateUserTier(ctx context.Context, actorUserID string, targetUserID string, tier string, reason string, expiresAt *time.Time) (services.UpdateUserTierResult, error) {
	args := m.Called(ctx, actorUserID, targetUserID, tier, reason, expiresAt)
	if args.Get(0) == nil {
		return services.UpdateUserTierResult{}, args.Error(1)
	}
	return args.Get(0).(services.UpdateUserTierResult), args.Error(1)
}

// RemoveUserTierOverride satisfies services.AdminUserService for tests in this file that don't
// exercise tier-override removal (Issue #1414 — see admin_subscriptions_test.go for coverage).
func (m *mockAdminUserService) RemoveUserTierOverride(ctx context.Context, actorUserID, targetUserID, reason string) (services.RemoveTierOverrideResult, error) {
	args := m.Called(ctx, actorUserID, targetUserID, reason)
	if args.Get(0) == nil {
		return services.RemoveTierOverrideResult{}, args.Error(1)
	}
	return args.Get(0).(services.RemoveTierOverrideResult), args.Error(1)
}

// SetGDPRAnalytics (Issue #1413 Task NB, replacing Task MB's raw SetAnalytics) is not exercised
// by these handler-level tests — analytics wiring is tested at the service layer
// (admin_user_service_test.go); stubbed here only to satisfy the services.AdminUserService
// interface.
func (m *mockAdminUserService) SetGDPRAnalytics(a phclient.GDPRAnalytics) {}

func (m *mockAdminUserService) SetRevenueCatSummary(services.RevenueCatSummaryReader) {}

// TestListUsers_Success tests successful user listing
func TestListUsers_Success(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()
	now := time.Now()
	user1ID := uuid.New().String()
	user2ID := uuid.New().String()

	expectedResponse := &domain.AdminUserListResponse{
		Users: []domain.AdminUserListItem{
			{
				ID:                  user1ID,
				Email:               "user1@example.com",
				SubscriptionTier:    "premium",
				SubscriptionStatus:  "active",
				PrimaryRegion:       "iad",
				CreatedAt:           now,
				LastLoginAt:         &now,
				IsActive:            true,
				LinkedAccountsCount: 3,
				BoostsCount:         5,
			},
			{
				ID:                  user2ID,
				Email:               "user2@example.com",
				SubscriptionTier:    "free",
				SubscriptionStatus:  "trial",
				PrimaryRegion:       "iad",
				CreatedAt:           now,
				IsActive:            true,
				LinkedAccountsCount: 1,
				BoostsCount:         2,
			},
		},
		TotalCount: 2,
		Page:       1,
		PageSize:   20,
		TotalPages: 1,
		HasMore:    false,
	}

	mockService.On("ListUsers", ctx, domain.AdminUserFilters{}, 1, 20, mock.Anything).Return(expectedResponse, nil)

	input := &ListUsersInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		PagePaginationInput: inputs.PagePaginationInput{
			Page:     1,
			PageSize: 20,
		},
	}

	output, err := handler.ListUsers(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 2, output.Body.TotalCount)
	assert.Len(t, output.Body.Users, 2)
	assert.Equal(t, user1ID, output.Body.Users[0].ID)
	assert.Equal(t, "user1@example.com", output.Body.Users[0].Email)
	mockService.AssertExpectations(t)
}

// TestListUsers_WithSearch tests user listing with email search filter
func TestListUsers_WithSearch(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()

	expectedResponse := &domain.AdminUserListResponse{
		Users: []domain.AdminUserListItem{
			{
				ID:               userID,
				Email:            "test@example.com",
				SubscriptionTier: "premium",
				CreatedAt:        now,
				IsActive:         true,
			},
		},
		TotalCount: 1,
		Page:       1,
		PageSize:   20,
		TotalPages: 1,
		HasMore:    false,
	}

	expectedFilters := domain.AdminUserFilters{Search: "test@"}
	mockService.On("ListUsers", ctx, expectedFilters, 1, 20, mock.Anything).Return(expectedResponse, nil)

	input := &ListUsersInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		PagePaginationInput: inputs.PagePaginationInput{
			Page:     1,
			PageSize: 20,
		},
		Search: "test@",
	}

	output, err := handler.ListUsers(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, 1, output.Body.TotalCount)
	assert.Equal(t, "test@example.com", output.Body.Users[0].Email)
	mockService.AssertExpectations(t)
}

// TestListUsers_WithTierFilter tests user listing with tier filter
func TestListUsers_WithTierFilter(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()

	expectedResponse := &domain.AdminUserListResponse{
		Users:      []domain.AdminUserListItem{},
		TotalCount: 0,
		Page:       1,
		PageSize:   20,
		TotalPages: 0,
		HasMore:    false,
	}

	expectedFilters := domain.AdminUserFilters{Tier: "premium"}
	mockService.On("ListUsers", ctx, expectedFilters, 1, 20, mock.Anything).Return(expectedResponse, nil)

	input := &ListUsersInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		PagePaginationInput: inputs.PagePaginationInput{
			Page:     1,
			PageSize: 20,
		},
		Tier: "premium",
	}

	output, err := handler.ListUsers(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Empty(t, output.Body.Users)
	mockService.AssertExpectations(t)
}

// TestListUsers_ServiceError tests error propagation from service
func TestListUsers_ServiceError(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()

	mockService.On("ListUsers", ctx, domain.AdminUserFilters{}, 1, 20, mock.Anything).Return(nil, errors.New("database error"))

	input := &ListUsersInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		PagePaginationInput: inputs.PagePaginationInput{
			Page:     1,
			PageSize: 20,
		},
	}

	output, err := handler.ListUsers(ctx, input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Internal server error")
	mockService.AssertExpectations(t)
}

// TestListUsers_ServiceNotConfigured tests 503 when service is nil
func TestListUsers_ServiceNotConfigured(t *testing.T) {
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg) // adminUserService is nil

	ctx := context.Background()

	input := &ListUsersInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		PagePaginationInput: inputs.PagePaginationInput{
			Page:     1,
			PageSize: 20,
		},
	}

	output, err := handler.ListUsers(ctx, input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Admin user service not configured")
}

// TestGetUserDetail_Success tests successful user detail retrieval
func TestGetUserDetail_Success(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()

	expectedDetail := &domain.AdminUserDetail{
		AdminUserListItem: domain.AdminUserListItem{
			ID:                  userID,
			Email:               "user@example.com",
			SubscriptionTier:    "premium",
			SubscriptionStatus:  "active",
			PrimaryRegion:       "iad",
			CreatedAt:           now,
			LastLoginAt:         &now,
			IsActive:            true,
			LinkedAccountsCount: 5,
			BoostsCount:         10,
		},
		TransactionsCount: 1500,
		BudgetsCount:      4,
	}

	mockService.On("GetUserDetail", ctx, userID, mock.Anything).Return(expectedDetail, nil)

	input := &GetUserDetailInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		UserID: userID,
	}

	output, err := handler.GetUserDetail(ctx, input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, userID, output.Body.ID)
	assert.Equal(t, "user@example.com", output.Body.Email)
	assert.Equal(t, 1500, output.Body.TransactionsCount)
	assert.Equal(t, 4, output.Body.BudgetsCount)
	mockService.AssertExpectations(t)
}

// TestGetUserDetail_IncludesAsOf proves the response carries an `as_of` timestamp (Issue #1462
// follow-up G2) — the server instant EffectiveStatus (on the embedded AdminUserListItem) was
// computed from, which UserDetailModal.tsx anchors its own isOverrideActive/getOverrideStatus
// calls to instead of the browser's clock. Mirrors TestGetUserSubscription_IncludesAsOf in
// admin_subscriptions_test.go.
func TestGetUserDetail_IncludesAsOf(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()
	userID := uuid.New().String()

	expectedDetail := &domain.AdminUserDetail{
		AdminUserListItem: domain.AdminUserListItem{
			ID: userID,
		},
	}
	mockService.On("GetUserDetail", ctx, userID, mock.Anything).Return(expectedDetail, nil)

	input := &GetUserDetailInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		UserID: userID,
	}

	before := time.Now()
	output, err := handler.GetUserDetail(ctx, input)
	after := time.Now()

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Body.AsOf.IsZero(), "as_of must always be populated")
	assert.False(t, output.Body.AsOf.Before(before.Add(-time.Second)), "as_of must be captured at request time, not a fixed/zero value")
	assert.False(t, output.Body.AsOf.After(after.Add(time.Second)), "as_of must be captured at request time, not a fixed/zero value")
	mockService.AssertExpectations(t)
}

// TestGetUserDetail_NotFound tests 404 when user not found
func TestGetUserDetail_NotFound(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()
	userID := uuid.New().String()

	mockService.On("GetUserDetail", ctx, userID, mock.Anything).Return(nil, services.ErrAdminUserNotFound)

	input := &GetUserDetailInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		UserID: userID,
	}

	output, err := handler.GetUserDetail(ctx, input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "User not found")
	mockService.AssertExpectations(t)
}

// TestGetUserDetail_ServiceError tests error propagation from service
func TestGetUserDetail_ServiceError(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()
	userID := uuid.New().String()

	mockService.On("GetUserDetail", ctx, userID, mock.Anything).Return(nil, errors.New("database error"))

	input := &GetUserDetailInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		UserID: userID,
	}

	output, err := handler.GetUserDetail(ctx, input)

	assert.Nil(t, output)
	require.Error(t, err)
	mockService.AssertExpectations(t)
}

// TestGetUserDetail_ServiceNotConfigured tests 503 when service is nil
func TestGetUserDetail_ServiceNotConfigured(t *testing.T) {
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, cfg) // adminUserService is nil

	ctx := context.Background()
	userID := uuid.New().String()

	input := &GetUserDetailInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		UserID: userID,
	}

	output, err := handler.GetUserDetail(ctx, input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Admin user service not configured")
}

// TestListUsers_SearchTooShort_Returns400 verifies that a search term shorter
// than 3 characters (after trimming) is rejected with 400 Bad Request.
func TestListUsers_SearchTooShort_Returns400(t *testing.T) {
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, new(mockAdminUserService), nil, nil, cfg)
	ctx := context.Background()

	for _, tc := range []struct {
		name   string
		search string
	}{
		{"one char", "a"},
		{"two chars", "ab"},
		{"two chars with spaces that trim to two", "  ab  "},
		{"one char unicode", "é"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := &ListUsersInput{
				AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
					AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
					ClerkUserID:        "clerk_admin_123",
				},
				PagePaginationInput: inputs.PagePaginationInput{Page: 1, PageSize: 20},
				Search:              tc.search,
			}
			_, err := handler.ListUsers(ctx, input)
			require.Error(t, err)
			// Project pattern: check err.Error() contains the expected message
			assert.Contains(t, err.Error(), "search must be at least 3 characters")
		})
	}
}

// TestListUsers_IncludesAsOf proves the response carries an `as_of` timestamp (Issue #1462
// follow-up G2) — a single server instant for the whole page that UsersPage.tsx anchors its
// per-row isOverrideActive/getOverrideStatus calls to, instead of the browser's clock. Mirrors
// TestGetUserSubscription_IncludesAsOf in admin_subscriptions_test.go.
func TestListUsers_IncludesAsOf(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()
	expectedResponse := &domain.AdminUserListResponse{
		Users:      []domain.AdminUserListItem{},
		TotalCount: 0,
		Page:       1,
		PageSize:   20,
	}
	mockService.On("ListUsers", ctx, domain.AdminUserFilters{}, 1, 20, mock.Anything).Return(expectedResponse, nil)

	input := &ListUsersInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		PagePaginationInput: inputs.PagePaginationInput{
			Page:     1,
			PageSize: 20,
		},
	}

	before := time.Now()
	output, err := handler.ListUsers(ctx, input)
	after := time.Now()

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.False(t, output.Body.AsOf.IsZero(), "as_of must always be populated")
	assert.False(t, output.Body.AsOf.Before(before.Add(-time.Second)), "as_of must be captured at request time, not a fixed/zero value")
	assert.False(t, output.Body.AsOf.After(after.Add(time.Second)), "as_of must be captured at request time, not a fixed/zero value")
	mockService.AssertExpectations(t)
}

// TestListUsers_SearchExactlyThreeChars_Succeeds verifies that exactly 3 characters passes.
func TestListUsers_SearchExactlyThreeChars_Succeeds(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, mockService, nil, nil, cfg)
	ctx := context.Background()

	mockService.On("ListUsers", ctx, domain.AdminUserFilters{Search: "abc"}, 1, 20, mock.Anything).
		Return(&domain.AdminUserListResponse{Users: []domain.AdminUserListItem{}, TotalCount: 0, Page: 1, PageSize: 20, TotalPages: 0}, nil)

	input := &ListUsersInput{
		AdminAuthenticatedInput: inputs.AdminAuthenticatedInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: "admin-123"},
			ClerkUserID:        "clerk_admin_123",
		},
		PagePaginationInput: inputs.PagePaginationInput{Page: 1, PageSize: 20},
		Search:              "abc",
	}
	_, err := handler.ListUsers(ctx, input)
	require.NoError(t, err)
	mockService.AssertExpectations(t)
}
