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
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockAdminUserService is a mock implementation of services.AdminUserService
type mockAdminUserService struct {
	mock.Mock
}

func (m *mockAdminUserService) ListUsers(ctx context.Context, filters domain.AdminUserFilters, page, pageSize int) (*domain.AdminUserListResponse, error) {
	args := m.Called(ctx, filters, page, pageSize)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AdminUserListResponse), args.Error(1)
}

func (m *mockAdminUserService) GetUserDetail(ctx context.Context, userID string) (*domain.AdminUserDetail, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AdminUserDetail), args.Error(1)
}

func (m *mockAdminUserService) UpdateUserTier(ctx context.Context, clerkUserID string, targetUserID string, tier string, reason string, expiresAt *time.Time) error {
	args := m.Called(ctx, clerkUserID, targetUserID, tier, reason, expiresAt)
	return args.Error(0)
}

// TestListUsers_Success tests successful user listing
func TestListUsers_Success(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()
	now := time.Now()
	user1ID := uuid.New().String()
	user2ID := uuid.New().String()

	expectedResponse := &domain.AdminUserListResponse{
		Users: []domain.AdminUserListItem{
			{
				ID:                   user1ID,
				Email:                "user1@example.com",
				SubscriptionTier:     "premium",
				SubscriptionStatus:   "active",
				PrimaryRegion:        "iad",
				CreatedAt:            now,
				LastLoginAt:          &now,
				IsActive:             true,
				LinkedAccountsCount:  3,
				AutomationRulesCount: 5,
			},
			{
				ID:                   user2ID,
				Email:                "user2@example.com",
				SubscriptionTier:     "free",
				SubscriptionStatus:   "trial",
				PrimaryRegion:        "lhr",
				CreatedAt:            now,
				IsActive:             true,
				LinkedAccountsCount:  1,
				AutomationRulesCount: 2,
			},
		},
		TotalCount: 2,
		Page:       1,
		PageSize:   20,
		TotalPages: 1,
		HasMore:    false,
	}

	mockService.On("ListUsers", ctx, domain.AdminUserFilters{}, 1, 20).Return(expectedResponse, nil)

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
	handler := NewAdminHandler(nil, nil, nil, nil, nil, mockService, nil, nil, cfg)

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
	mockService.On("ListUsers", ctx, expectedFilters, 1, 20).Return(expectedResponse, nil)

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
	handler := NewAdminHandler(nil, nil, nil, nil, nil, mockService, nil, nil, cfg)

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
	mockService.On("ListUsers", ctx, expectedFilters, 1, 20).Return(expectedResponse, nil)

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
	handler := NewAdminHandler(nil, nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()

	mockService.On("ListUsers", ctx, domain.AdminUserFilters{}, 1, 20).Return(nil, errors.New("database error"))

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
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, nil, cfg) // adminUserService is nil

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
	handler := NewAdminHandler(nil, nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()

	expectedDetail := &domain.AdminUserDetail{
		AdminUserListItem: domain.AdminUserListItem{
			ID:                   userID,
			Email:                "user@example.com",
			SubscriptionTier:     "premium",
			SubscriptionStatus:   "active",
			PrimaryRegion:        "iad",
			CreatedAt:            now,
			LastLoginAt:          &now,
			IsActive:             true,
			LinkedAccountsCount:  5,
			AutomationRulesCount: 10,
		},
		TransactionsCount: 1500,
		BudgetsCount:      4,
	}

	mockService.On("GetUserDetail", ctx, userID).Return(expectedDetail, nil)

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

// TestGetUserDetail_NotFound tests 404 when user not found
func TestGetUserDetail_NotFound(t *testing.T) {
	mockService := new(mockAdminUserService)
	cfg := &config.Config{Environment: "qa"}
	handler := NewAdminHandler(nil, nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()
	userID := uuid.New().String()

	mockService.On("GetUserDetail", ctx, userID).Return(nil, services.ErrAdminUserNotFound)

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
	handler := NewAdminHandler(nil, nil, nil, nil, nil, mockService, nil, nil, cfg)

	ctx := context.Background()
	userID := uuid.New().String()

	mockService.On("GetUserDetail", ctx, userID).Return(nil, errors.New("database error"))

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
	handler := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, nil, cfg) // adminUserService is nil

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
