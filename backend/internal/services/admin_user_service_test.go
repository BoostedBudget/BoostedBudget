package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockAdminUserRepository is a mock implementation of AdminUserRepository
type MockAdminUserRepository struct {
	mock.Mock
}

func (m *MockAdminUserRepository) UpdateUserTier(ctx context.Context, userID string, tier string, reason string, expiresAt *time.Time) error {
	args := m.Called(ctx, userID, tier, reason, expiresAt)
	return args.Error(0)
}

func (m *MockAdminUserRepository) ListUsers(ctx context.Context, filters domain.AdminUserFilters, limit, offset int) ([]domain.AdminUserListItem, int, error) {
	args := m.Called(ctx, filters, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]domain.AdminUserListItem), args.Int(1), args.Error(2)
}

func (m *MockAdminUserRepository) GetUserDetail(ctx context.Context, userID string) (*domain.AdminUserDetail, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.AdminUserDetail), args.Error(1)
}

// TestAdminUserService_ListUsers_Success tests successful listing of users
func TestAdminUserService_ListUsers_Success(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil)

	ctx := context.Background()
	now := time.Now()
	user1ID := uuid.New().String()
	user2ID := uuid.New().String()

	expectedUsers := []domain.AdminUserListItem{
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
	}

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return(expectedUsers, 2, nil)

	// Execute
	response, err := service.ListUsers(ctx, filters, 1, 20)

	// Assert
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, 2, response.TotalCount)
	assert.Equal(t, 1, response.Page)
	assert.Equal(t, 20, response.PageSize)
	assert.Equal(t, 1, response.TotalPages)
	assert.False(t, response.HasMore)
	assert.Len(t, response.Users, 2)
	assert.Equal(t, user1ID, response.Users[0].ID)
	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_ListUsers_WithPagination tests pagination calculations
func TestAdminUserService_ListUsers_WithPagination(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil)

	ctx := context.Background()
	now := time.Now()

	// 50 total users, page 2 with 20 per page
	expectedUsers := []domain.AdminUserListItem{
		{
			ID:                 uuid.New().String(),
			Email:              "user21@example.com",
			SubscriptionTier:   "free",
			SubscriptionStatus: "active",
			CreatedAt:          now,
			IsActive:           true,
		},
	}

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 20).Return(expectedUsers, 50, nil)

	// Execute
	response, err := service.ListUsers(ctx, filters, 2, 20)

	// Assert
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, 50, response.TotalCount)
	assert.Equal(t, 2, response.Page)
	assert.Equal(t, 20, response.PageSize)
	assert.Equal(t, 3, response.TotalPages) // ceil(50/20) = 3
	assert.True(t, response.HasMore)        // page 2 of 3
	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_ListUsers_LastPage tests last page has no more
func TestAdminUserService_ListUsers_LastPage(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil)

	ctx := context.Background()

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 40).Return([]domain.AdminUserListItem{}, 50, nil)

	// Execute
	response, err := service.ListUsers(ctx, filters, 3, 20)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, 3, response.TotalPages)
	assert.False(t, response.HasMore)
	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_ListUsers_WithFilters tests filtering passes through
func TestAdminUserService_ListUsers_WithFilters(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil)

	ctx := context.Background()

	filters := domain.AdminUserFilters{
		Search: "test@",
		Tier:   "premium",
	}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return([]domain.AdminUserListItem{}, 0, nil)

	// Execute
	response, err := service.ListUsers(ctx, filters, 1, 20)

	// Assert
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, 0, response.TotalCount)
	assert.Empty(t, response.Users)
	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_ListUsers_RepositoryError tests error propagation
func TestAdminUserService_ListUsers_RepositoryError(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil)

	ctx := context.Background()

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return(nil, 0, errors.New("database error"))

	// Execute
	response, err := service.ListUsers(ctx, filters, 1, 20)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
	assert.Nil(t, response)
	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_ListUsers_InvalidPage tests validation of page
func TestAdminUserService_ListUsers_InvalidPage(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil)

	ctx := context.Background()
	filters := domain.AdminUserFilters{}

	// Execute with page 0 (invalid)
	response, err := service.ListUsers(ctx, filters, 0, 20)

	// Assert - should default to page 1
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "page must be at least 1")
}

// TestAdminUserService_ListUsers_InvalidPageSize tests validation of page size
func TestAdminUserService_ListUsers_InvalidPageSize(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil)

	ctx := context.Background()
	filters := domain.AdminUserFilters{}

	// Execute with page size > 100
	response, err := service.ListUsers(ctx, filters, 1, 150)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "page_size must be between 1 and 100")
}

// TestAdminUserService_GetUserDetail_Success tests successful retrieval
func TestAdminUserService_GetUserDetail_Success(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil)

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

	mockRepo.On("GetUserDetail", ctx, userID).Return(expectedDetail, nil)

	// Execute
	detail, err := service.GetUserDetail(ctx, userID)

	// Assert
	require.NoError(t, err)
	assert.NotNil(t, detail)
	assert.Equal(t, userID, detail.ID)
	assert.Equal(t, "user@example.com", detail.Email)
	assert.Equal(t, 1500, detail.TransactionsCount)
	assert.Equal(t, 4, detail.BudgetsCount)
	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_GetUserDetail_NotFound tests user not found
func TestAdminUserService_GetUserDetail_NotFound(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil)

	ctx := context.Background()
	userID := uuid.New().String()

	mockRepo.On("GetUserDetail", ctx, userID).Return(nil, ErrAdminUserNotFound)

	// Execute
	detail, err := service.GetUserDetail(ctx, userID)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrAdminUserNotFound, err)
	assert.Nil(t, detail)
	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_GetUserDetail_EmptyUserID tests validation
func TestAdminUserService_GetUserDetail_EmptyUserID(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil)

	ctx := context.Background()

	// Execute with empty user ID
	detail, err := service.GetUserDetail(ctx, "")

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user_id is required")
	assert.Nil(t, detail)
}

// MockAdminService is a mock implementation of AdminService for testing
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

// TestAdminUserService_ListUsers_EnrichesAdminStatus tests that admin users get marked
func TestAdminUserService_ListUsers_EnrichesAdminStatus(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	mockAdminService := new(MockAdminService)
	service := NewAdminUserService(mockRepo, mockAdminService)

	ctx := context.Background()
	now := time.Now()

	// Regular user and admin user
	repoUsers := []domain.AdminUserListItem{
		{
			ID:                 uuid.New().String(),
			Email:              "regular@example.com",
			SubscriptionTier:   "trial",
			SubscriptionStatus: "trial",
			CreatedAt:          now,
			IsActive:           true,
		},
		{
			ID:                 uuid.New().String(),
			Email:              "admin@boostedbudget.com",
			SubscriptionTier:   "trial",
			SubscriptionStatus: "trial",
			CreatedAt:          now,
			IsActive:           true,
		},
	}

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return(repoUsers, 2, nil)
	mockAdminService.On("IsHardcodedAdmin", "regular@example.com").Return(false)
	mockAdminService.On("IsHardcodedAdmin", "admin@boostedbudget.com").Return(true)

	// Execute
	response, err := service.ListUsers(ctx, filters, 1, 20)

	// Assert
	require.NoError(t, err)
	assert.Len(t, response.Users, 2)

	// Regular user should NOT be admin
	assert.False(t, response.Users[0].IsAdmin)
	assert.Equal(t, "trial", response.Users[0].SubscriptionTier)
	assert.Equal(t, "trial", response.Users[0].SubscriptionStatus)

	// Admin user should be marked as admin with premium tier
	assert.True(t, response.Users[1].IsAdmin)
	assert.Equal(t, "premium", response.Users[1].SubscriptionTier)
	assert.Equal(t, "active", response.Users[1].SubscriptionStatus)

	mockRepo.AssertExpectations(t)
	mockAdminService.AssertExpectations(t)
}

// TestAdminUserService_GetUserDetail_EnrichesAdminStatus tests admin detail enrichment
func TestAdminUserService_GetUserDetail_EnrichesAdminStatus(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	mockAdminService := new(MockAdminService)
	service := NewAdminUserService(mockRepo, mockAdminService)

	ctx := context.Background()
	now := time.Now()
	userID := uuid.New().String()

	repoDetail := &domain.AdminUserDetail{
		AdminUserListItem: domain.AdminUserListItem{
			ID:                 userID,
			Email:              "admin@boostedbudget.com",
			SubscriptionTier:   "trial",
			SubscriptionStatus: "trial",
			CreatedAt:          now,
			IsActive:           true,
		},
		TransactionsCount: 100,
		BudgetsCount:      5,
	}

	mockRepo.On("GetUserDetail", ctx, userID).Return(repoDetail, nil)
	mockAdminService.On("IsHardcodedAdmin", "admin@boostedbudget.com").Return(true)

	// Execute
	detail, err := service.GetUserDetail(ctx, userID)

	// Assert
	require.NoError(t, err)
	assert.True(t, detail.IsAdmin)
	assert.Equal(t, "premium", detail.SubscriptionTier)
	assert.Equal(t, "active", detail.SubscriptionStatus)
	// Other fields unchanged
	assert.Equal(t, 100, detail.TransactionsCount)
	assert.Equal(t, 5, detail.BudgetsCount)

	mockRepo.AssertExpectations(t)
	mockAdminService.AssertExpectations(t)
}

// TestAdminUserService_ListUsers_NilAdminService tests graceful handling of nil AdminService
func TestAdminUserService_ListUsers_NilAdminService(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil) // nil AdminService

	ctx := context.Background()
	now := time.Now()

	repoUsers := []domain.AdminUserListItem{
		{
			ID:                 uuid.New().String(),
			Email:              "admin@boostedbudget.com",
			SubscriptionTier:   "trial",
			SubscriptionStatus: "trial",
			CreatedAt:          now,
			IsActive:           true,
		},
	}

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return(repoUsers, 1, nil)

	// Execute
	response, err := service.ListUsers(ctx, filters, 1, 20)

	// Assert - should work without enrichment
	require.NoError(t, err)
	assert.Len(t, response.Users, 1)
	assert.False(t, response.Users[0].IsAdmin)                   // Not enriched
	assert.Equal(t, "trial", response.Users[0].SubscriptionTier) // Not overridden

	mockRepo.AssertExpectations(t)
}

func TestAdminUserService_UpdateUserTier_Success(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil)

	ctx := context.Background()
	userID := uuid.New().String()
	tier := "household_2"
	reason := "manual override"

	// Using empty clerk_user_id to skip clerk API call
	mockRepo.On("UpdateUserTier", ctx, userID, tier, reason, (*time.Time)(nil)).Return(nil)

	err := service.UpdateUserTier(ctx, "", userID, tier, reason, nil)
	require.NoError(t, err)

	mockRepo.AssertExpectations(t)
}

func TestAdminUserService_UpdateUserTier_MissingUserID(t *testing.T) {
	service := NewAdminUserService(nil, nil)
	err := service.UpdateUserTier(context.Background(), "", "", "tier", "reason", nil)
	assert.Equal(t, ErrUserIDRequired, err)
}

func TestAdminUserService_UpdateUserTier_MissingTier(t *testing.T) {
	service := NewAdminUserService(nil, nil)
	err := service.UpdateUserTier(context.Background(), "", "user", "", "reason", nil)
	assert.ErrorContains(t, err, "tier is required")
}

func TestAdminUserService_UpdateUserTier_MissingReason(t *testing.T) {
	service := NewAdminUserService(nil, nil)
	err := service.UpdateUserTier(context.Background(), "", "user", "tier", "", nil)
	assert.ErrorContains(t, err, "reason is required")
}

func TestAdminUserService_UpdateUserTier_RepoError(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil)

	ctx := context.Background()
	userID := "user"
	tier := "premium"
	reason := "promo"
	repoErr := errors.New("db down")

	mockRepo.On("UpdateUserTier", ctx, userID, tier, reason, (*time.Time)(nil)).Return(repoErr)

	err := service.UpdateUserTier(ctx, "", userID, tier, reason, nil)
	assert.ErrorContains(t, err, "failed to update user tier in database: db down")

	mockRepo.AssertExpectations(t)
}
