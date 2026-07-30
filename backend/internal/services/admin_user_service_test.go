package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockAdminUserRepository is a mock implementation of AdminUserRepository
type MockAdminUserRepository struct {
	mock.Mock
}

func (m *MockAdminUserRepository) UpdateUserTier(ctx context.Context, actorUserID, userID, tier, reason string, expiresAt *time.Time) (repository.UpdateTierRow, error) {
	args := m.Called(ctx, actorUserID, userID, tier, reason, expiresAt)
	if args.Get(0) == nil {
		return repository.UpdateTierRow{}, args.Error(1)
	}
	return args.Get(0).(repository.UpdateTierRow), args.Error(1)
}

func (m *MockAdminUserRepository) GetOwnedHouseholdActiveMemberCount(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockAdminUserRepository) RemoveUserTierOverride(ctx context.Context, actorUserID, targetUserID, reason string, retentionMonths int) (repository.RemoveTierOverrideRow, error) {
	args := m.Called(ctx, actorUserID, targetUserID, reason, retentionMonths)
	if args.Get(0) == nil {
		return repository.RemoveTierOverrideRow{}, args.Error(1)
	}
	return args.Get(0).(repository.RemoveTierOverrideRow), args.Error(1)
}

// RemoveUserTierOverrideIfMatches is the CAS-guarded sibling used by the override-expiry worker,
// which must not clear a comp an admin re-applied between candidate selection and processing
// (Codex review finding H4). Returns (row, claimed, err) — claimed=false means the row no longer
// matched the expected override, so the caller must skip its follow-up work.
func (m *MockAdminUserRepository) RemoveUserTierOverrideIfMatches(ctx context.Context, actorUserID, targetUserID, reason string, retentionMonths int, expectedOverride *string, expectedEndsAt *time.Time) (repository.RemoveTierOverrideRow, bool, error) {
	args := m.Called(ctx, actorUserID, targetUserID, reason, retentionMonths, expectedOverride, expectedEndsAt)
	if args.Get(0) == nil {
		return repository.RemoveTierOverrideRow{}, args.Bool(1), args.Error(2)
	}
	return args.Get(0).(repository.RemoveTierOverrideRow), args.Bool(1), args.Error(2)
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
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

	ctx := context.Background()
	now := time.Now()
	user1ID := uuid.New().String()
	user2ID := uuid.New().String()

	expectedUsers := []domain.AdminUserListItem{
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
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

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
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

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
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

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
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

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
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

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
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

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
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

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
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

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
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

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
	service := NewAdminUserService(mockRepo, mockAdminService, nil, 0, nil)

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

// --- Issue #1413: EffectiveStatus enrichment (Task 8b) ---
//
// enrichAdminUser must populate EffectiveStatus using the single shared predicate
// (domain.User.ComputeEffectiveStatus), never a re-implementation of override-liveness.

// TestAdminUserService_EnrichAdminUser_EffectiveStatus_LiveOverride proves a live (non-expired)
// manual tier override forces EffectiveStatus to "active" even though the raw
// SubscriptionStatus is "inactive" — the whole point of Issue #1413 is that a comp must
// actually unlock access, not just relabel the tier while EffectiveStatus stays stale.
func TestAdminUserService_EnrichAdminUser_EffectiveStatus_LiveOverride(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

	ctx := context.Background()
	now := time.Now()
	override := "household_2"
	futureEnd := now.Add(24 * time.Hour)

	repoUsers := []domain.AdminUserListItem{
		{
			ID:                 uuid.New().String(),
			Email:              "comped@example.com",
			SubscriptionTier:   "premium",
			SubscriptionStatus: "inactive",
			ManualTierOverride: &override,
			ManualTierEndsAt:   &futureEnd,
			CreatedAt:          now,
			IsActive:           true,
		},
	}

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return(repoUsers, 1, nil)

	response, err := service.ListUsers(ctx, filters, 1, 20)
	require.NoError(t, err)
	require.Len(t, response.Users, 1)

	assert.Equal(t, "inactive", response.Users[0].SubscriptionStatus, "raw status must stay untouched")
	assert.Equal(t, "active", response.Users[0].EffectiveStatus, "a live override must force EffectiveStatus to active")

	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_EnrichAdminUser_EffectiveStatus_NoOverride proves EffectiveStatus equals
// the raw SubscriptionStatus when no manual tier override is set.
func TestAdminUserService_EnrichAdminUser_EffectiveStatus_NoOverride(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

	ctx := context.Background()
	now := time.Now()

	repoUsers := []domain.AdminUserListItem{
		{
			ID:                 uuid.New().String(),
			Email:              "plain@example.com",
			SubscriptionTier:   "premium",
			SubscriptionStatus: "past_due",
			CreatedAt:          now,
			IsActive:           true,
		},
	}

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return(repoUsers, 1, nil)

	response, err := service.ListUsers(ctx, filters, 1, 20)
	require.NoError(t, err)
	require.Len(t, response.Users, 1)

	assert.Equal(t, "past_due", response.Users[0].SubscriptionStatus)
	assert.Equal(t, "past_due", response.Users[0].EffectiveStatus, "no override -> EffectiveStatus mirrors the raw status")

	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_EnrichAdminUser_EffectiveStatus_ExpiredOverride proves an EXPIRED manual
// tier override does NOT force EffectiveStatus to active — it falls back to the raw status,
// mirroring domain.User.ComputeEffectiveStatus's expiry behaviour exactly (single predicate).
func TestAdminUserService_EnrichAdminUser_EffectiveStatus_ExpiredOverride(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

	ctx := context.Background()
	now := time.Now()
	override := "household_2"
	pastEnd := now.Add(-24 * time.Hour)

	repoUsers := []domain.AdminUserListItem{
		{
			ID:                 uuid.New().String(),
			Email:              "expired-comp@example.com",
			SubscriptionTier:   "premium",
			SubscriptionStatus: "cancelled",
			ManualTierOverride: &override,
			ManualTierEndsAt:   &pastEnd,
			CreatedAt:          now,
			IsActive:           true,
		},
	}

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return(repoUsers, 1, nil)

	response, err := service.ListUsers(ctx, filters, 1, 20)
	require.NoError(t, err)
	require.Len(t, response.Users, 1)

	assert.Equal(t, "cancelled", response.Users[0].EffectiveStatus, "an expired override must not force active")

	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_EnrichAdminUser_EffectiveStatus_MalformedOverride_FallsBackToRawStatus is
// the RED test for Issue #1413 (this task): manual_tier_override is an unconstrained VARCHAR(50)
// with no CHECK constraint, so it can hold a value naming no configured tier. Before this fix,
// enrichAdminUser called scratch.ComputeEffectiveStatus() unconditionally — with no tier-name
// validation at all, unlike every other representation path this branch has already fixed
// (profile.go's enrichUserProfile, GetEffectiveSubscriptionStatusByUserID) — so the admin LIST
// (and detail, which reuses this same enrichment) would report a malformed override as an active
// comp, while the user's own GET /profile correctly rejected it: the exact list-vs-profile
// disagreement about the same user this task closes.
func TestAdminUserService_EnrichAdminUser_EffectiveStatus_MalformedOverride_FallsBackToRawStatus(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, fakeTierConfig())

	ctx := context.Background()
	now := time.Now()
	override := "not-a-real-tier"
	futureEnd := now.Add(24 * time.Hour)

	repoUsers := []domain.AdminUserListItem{
		{
			ID:                 uuid.New().String(),
			Email:              "malformed-comp@example.com",
			SubscriptionTier:   "premium",
			SubscriptionStatus: "inactive",
			ManualTierOverride: &override,
			ManualTierEndsAt:   &futureEnd,
			CreatedAt:          now,
			IsActive:           true,
		},
	}

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return(repoUsers, 1, nil)

	response, err := service.ListUsers(ctx, filters, 1, 20)
	require.NoError(t, err)
	require.Len(t, response.Users, 1)

	assert.Equal(t, "inactive", response.Users[0].SubscriptionStatus, "raw status must stay untouched")
	assert.Equal(t, "inactive", response.Users[0].EffectiveStatus, "a malformed override must fall back to the raw status, not force active")

	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_EnrichAdminUser_EffectiveStatus_ValidOverride_WithTierConfig_StillActive
// is the POSITIVE CONTROL for the fix above: with a real tierConfig wired, a VALID,
// currently-configured override must still force EffectiveStatus to active. An over-tight fix
// that makes every comp fall back to the raw status would silently un-comp every legitimately
// comped user in the admin list, which is worse than the bug being fixed.
func TestAdminUserService_EnrichAdminUser_EffectiveStatus_ValidOverride_WithTierConfig_StillActive(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, fakeTierConfig())

	ctx := context.Background()
	now := time.Now()
	override := "household_2"
	futureEnd := now.Add(24 * time.Hour)

	repoUsers := []domain.AdminUserListItem{
		{
			ID:                 uuid.New().String(),
			Email:              "comped@example.com",
			SubscriptionTier:   "premium",
			SubscriptionStatus: "inactive",
			ManualTierOverride: &override,
			ManualTierEndsAt:   &futureEnd,
			CreatedAt:          now,
			IsActive:           true,
		},
	}

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return(repoUsers, 1, nil)

	response, err := service.ListUsers(ctx, filters, 1, 20)
	require.NoError(t, err)
	require.Len(t, response.Users, 1)

	assert.Equal(t, "inactive", response.Users[0].SubscriptionStatus, "raw status must stay untouched")
	assert.Equal(t, "active", response.Users[0].EffectiveStatus, "a valid override must still force EffectiveStatus to active")

	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_EnrichAdminUser_EffectiveStatus_HardcodedAdmin_ConsistentWithSubscriptionStatus
// pins the deliberate resolution for the hardcoded-admin cosmetic override: hardcoded admins are
// cosmetically forced to subscription_tier=premium/subscription_status=active for display, and
// EffectiveStatus must be consistent with that displayed SubscriptionStatus (both "active"),
// never the raw pre-override status — two adjacent fields disagreeing about the same user is
// the exact class of bug Issue #1413 exists to fix.
func TestAdminUserService_EnrichAdminUser_EffectiveStatus_HardcodedAdmin_ConsistentWithSubscriptionStatus(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	mockAdminService := new(MockAdminService)
	service := NewAdminUserService(mockRepo, mockAdminService, nil, 0, nil)

	ctx := context.Background()
	now := time.Now()

	repoUsers := []domain.AdminUserListItem{
		{
			ID:                 uuid.New().String(),
			Email:              "admin@boostedbudget.com",
			SubscriptionTier:   "trial",
			SubscriptionStatus: "trial", // raw status, pre-override
			CreatedAt:          now,
			IsActive:           true,
		},
	}

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return(repoUsers, 1, nil)
	mockAdminService.On("IsHardcodedAdmin", "admin@boostedbudget.com").Return(true)

	response, err := service.ListUsers(ctx, filters, 1, 20)
	require.NoError(t, err)
	require.Len(t, response.Users, 1)

	assert.Equal(t, "active", response.Users[0].SubscriptionStatus, "hardcoded admins are cosmetically forced to active")
	assert.Equal(t, "active", response.Users[0].EffectiveStatus,
		"EffectiveStatus must match the displayed SubscriptionStatus for hardcoded admins, not the raw pre-override status")

	mockRepo.AssertExpectations(t)
	mockAdminService.AssertExpectations(t)
}

// TestAdminUserService_GetUserDetail_EnrichesAdminStatus tests admin detail enrichment
func TestAdminUserService_GetUserDetail_EnrichesAdminStatus(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	mockAdminService := new(MockAdminService)
	service := NewAdminUserService(mockRepo, mockAdminService, nil, 0, nil)

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
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil) // nil AdminService

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

// fakeTierConfig returns a *config.TierConfig literal covering all five real tiers with their
// real users_included seat counts (config/subscription_tiers.yaml). Per this plan's standing
// rule, tests must build a literal rather than call config.LoadTierConfig() (a process-wide
// sync.Once singleton).
func fakeTierConfig() *config.TierConfig {
	return &config.TierConfig{
		Tiers: map[domain.TierName]domain.SubscriptionTier{
			domain.TierPremium:    {Name: domain.TierPremium, Pricing: domain.SubscriptionPricing{UsersIncluded: 1}},
			domain.TierHousehold2: {Name: domain.TierHousehold2, Pricing: domain.SubscriptionPricing{UsersIncluded: 2}},
			domain.TierHousehold3: {Name: domain.TierHousehold3, Pricing: domain.SubscriptionPricing{UsersIncluded: 3}},
			domain.TierHousehold4: {Name: domain.TierHousehold4, Pricing: domain.SubscriptionPricing{UsersIncluded: 4}},
			domain.TierHousehold5: {Name: domain.TierHousehold5, Pricing: domain.SubscriptionPricing{UsersIncluded: 5}},
		},
	}
}

func TestAdminUserService_UpdateUserTier_Success(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

	ctx := context.Background()
	actorID := uuid.New().String()
	userID := uuid.New().String()
	tier := "household_2"
	reason := "manual override"

	// Return empty clerk_user_id to skip Clerk API call (best-effort)
	mockRepo.On("UpdateUserTier", ctx, actorID, userID, tier, reason, (*time.Time)(nil)).
		Return(repository.UpdateTierRow{}, nil)

	result, err := service.UpdateUserTier(ctx, actorID, userID, tier, reason, nil)
	require.NoError(t, err)
	assert.Empty(t, result.Warning)

	mockRepo.AssertExpectations(t)
}

func TestAdminUserService_UpdateUserTier_MissingUserID(t *testing.T) {
	service := NewAdminUserService(nil, nil, nil, 0, nil)
	_, err := service.UpdateUserTier(context.Background(), "", "", "tier", "reason", nil)
	assert.Equal(t, ErrUserIDRequired, err)
}

func TestAdminUserService_UpdateUserTier_MissingTier(t *testing.T) {
	service := NewAdminUserService(nil, nil, nil, 0, nil)
	_, err := service.UpdateUserTier(context.Background(), "", "user", "", "reason", nil)
	assert.ErrorContains(t, err, "tier is required")
}

func TestAdminUserService_UpdateUserTier_MissingReason(t *testing.T) {
	service := NewAdminUserService(nil, nil, nil, 0, nil)
	_, err := service.UpdateUserTier(context.Background(), "", "user", "tier", "", nil)
	assert.ErrorContains(t, err, "reason is required")
}

func TestAdminUserService_UpdateUserTier_RepoError(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

	ctx := context.Background()
	userID := "user"
	tier := "premium"
	reason := "promo"
	repoErr := errors.New("db down")

	mockRepo.On("UpdateUserTier", ctx, "", userID, tier, reason, (*time.Time)(nil)).
		Return(repository.UpdateTierRow{}, repoErr)

	_, err := service.UpdateUserTier(ctx, "", userID, tier, reason, nil)
	assert.ErrorContains(t, err, "failed to update user tier in database: db down")

	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_UpdateUserTier_TruncatesExpiresAtToMicrosecond proves a caller-supplied
// expiresAt is truncated to microsecond precision before it reaches the repository (Issue #1413
// follow-up, Task JE: closing the expiry-tie gap). Postgres's timestamptz column is microsecond
// precision; Go's time.Time carries nanoseconds, and input.Body.ExpiresAt
// (huma/operations/admin_subscriptions.go) is a JSON-decoded *time.Time with no precision
// constraint of its own — so without this truncation, the value the service hands to
// AdminUserRepository.UpdateUserTier (and, via the Clerk-sync scratch user, to
// domain.User.hasActiveManualOverrideAt) could carry sub-microsecond precision that Postgres
// then silently drops on write, leaving the in-memory and persisted forms as two different
// instants.
//
// This is a genuinely discriminating test: without the Truncate call in UpdateUserTier, the mock
// argument matcher below (which requires exact microsecond alignment) does not match the raw
// nanosecond-precision fixture, so mockRepo has no matching expectation and testify's mock.Mock
// panics with "I don't know what to return" — confirmed by temporarily reverting the truncation
// during development. See admin_user_service.go's UpdateUserTier for why a companion
// cross-system (Go predicate vs SQL predicate) integration test was deliberately NOT added: it
// was attempted and found incapable of discriminating truncated from untruncated input once the
// clock-fencing rule required for CI stability is followed (see
// profile_repository_manual_override_retention_integration_test.go's comment on that finding).
func TestAdminUserService_UpdateUserTier_TruncatesExpiresAtToMicrosecond(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

	ctx := context.Background()
	actorID := uuid.New().String()
	userID := uuid.New().String()
	tier := "premium"
	reason := "comp with sub-microsecond expiry"

	// 789 nanoseconds of sub-microsecond precision — representable in Go's time.Time, not in
	// Postgres's timestamptz.
	rawExpiresAt := time.Date(2027, 3, 4, 5, 6, 7, 123456789, time.UTC)
	wantTruncated := rawExpiresAt.Truncate(time.Microsecond)
	require.NotEqual(t, rawExpiresAt, wantTruncated, "fixture must actually carry sub-microsecond precision")

	mockRepo.On("UpdateUserTier", ctx, actorID, userID, tier, reason, mock.MatchedBy(func(got *time.Time) bool {
		return got != nil && got.Equal(wantTruncated) && got.Nanosecond()%int(time.Microsecond) == 0
	})).Return(repository.UpdateTierRow{}, nil)

	_, err := service.UpdateUserTier(ctx, actorID, userID, tier, reason, &rawExpiresAt)
	require.NoError(t, err)

	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_UpdateUserTier_NilExpiresAt_StaysNil proves a nil expiresAt (a permanent,
// non-expiring override) is never coerced into a zero time.Time by the truncation added for Task
// JE above — nil must reach the repository as nil, not as &time.Time{}.
func TestAdminUserService_UpdateUserTier_NilExpiresAt_StaysNil(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

	ctx := context.Background()
	actorID := uuid.New().String()
	userID := uuid.New().String()
	tier := "premium"
	reason := "permanent comp"

	mockRepo.On("UpdateUserTier", ctx, actorID, userID, tier, reason, (*time.Time)(nil)).
		Return(repository.UpdateTierRow{}, nil)

	_, err := service.UpdateUserTier(ctx, actorID, userID, tier, reason, nil)
	require.NoError(t, err)

	mockRepo.AssertExpectations(t)
}

// --- Issue #1413 Defect 1: Clerk sync uses the injected ClerkService + canonical payload ---

// TestAdminUserService_UpdateUserTier_PushesEffectiveOverrideTier_Household4 proves the Clerk
// payload carries the ACTUAL override tier (a specific household level), not a generic value,
// and matches the canonical subscription_tier/subscription_status/tier:nil shape.
func TestAdminUserService_UpdateUserTier_PushesEffectiveOverrideTier_Household4(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	mockClerk := new(MockClerkService)
	service := NewAdminUserService(mockRepo, nil, mockClerk, 0, nil)

	ctx := context.Background()
	actorID := uuid.New().String()
	userID := uuid.New().String()
	tier := "household_4"
	reason := "comped to household_4"

	mockRepo.On("UpdateUserTier", ctx, actorID, userID, tier, reason, (*time.Time)(nil)).
		Return(repository.UpdateTierRow{
			ClerkUserID:        "clerk_household4_user",
			SubscriptionTier:   "premium", // raw base tier, unaffected by the override
			SubscriptionStatus: "cancelled",
		}, nil)
	// Task X (L3, Codex review round on PR #1443): also pin the exact key count — see the three
	// RemoveUserTierOverride matchers below for why a per-key-only check would miss a regression
	// that ADDS a key to this payload.
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_household4_user", mock.MatchedBy(func(m map[string]interface{}) bool {
		tierVal, hasTierKey := m["subscription_tier"]
		statusVal, hasStatusKey := m["subscription_status"]
		nilTierVal, hasNilTierKey := m["tier"]
		return len(m) == 3 &&
			hasTierKey && tierVal == "household_4" && // the OVERRIDE tier, not the raw "premium"
			hasStatusKey && statusVal == "active" && // override forces status to active
			hasNilTierKey && nilTierVal == nil // orphaned "tier" key retroactively cleaned up
	})).Return(nil)

	_, err := service.UpdateUserTier(ctx, actorID, userID, tier, reason, nil)
	require.NoError(t, err)

	mockRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestAdminUserService_UpdateUserTier_ClerkFailure_StillSucceeds proves Clerk sync is
// best-effort — the DB write already succeeded, so a Clerk error must not fail the call.
func TestAdminUserService_UpdateUserTier_ClerkFailure_StillSucceeds(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	mockClerk := new(MockClerkService)
	service := NewAdminUserService(mockRepo, nil, mockClerk, 0, nil)

	ctx := context.Background()
	actorID := uuid.New().String()
	userID := uuid.New().String()
	tier := "premium"
	reason := "comp"

	mockRepo.On("UpdateUserTier", ctx, actorID, userID, tier, reason, (*time.Time)(nil)).
		Return(repository.UpdateTierRow{ClerkUserID: "clerk_x", SubscriptionTier: "premium", SubscriptionStatus: "active"}, nil)
	// Issue #1413 (Codex review, PR #1443 follow-up — Finding 6): mock.Anything on a Clerk payload
	// path hides exactly the regression this whole matcher family exists to catch — see the
	// Household4 test above for the established len(m) == 3 standard. Tightened to the same exact
	// key-set check; falsified against a broken ClerkPublicMetadata() (domain/user.go) that
	// returned "tier": "premium" instead of nil, which mock.Anything did not catch.
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_x", mock.MatchedBy(func(m map[string]interface{}) bool {
		tierVal, hasTierKey := m["subscription_tier"]
		statusVal, hasStatusKey := m["subscription_status"]
		nilTierVal, hasNilTierKey := m["tier"]
		return len(m) == 3 &&
			hasTierKey && tierVal == "premium" &&
			hasStatusKey && statusVal == "active" &&
			hasNilTierKey && nilTierVal == nil
	})).Return(errors.New("clerk is down"))

	_, err := service.UpdateUserTier(ctx, actorID, userID, tier, reason, nil)
	require.NoError(t, err, "Clerk sync failure must not fail the overall call")

	mockRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestAdminUserService_UpdateUserTier_NilClerkService_NoPanic proves a nil ClerkService (not
// configured) is handled gracefully, matching the RemoveUserTierOverride precedent.
func TestAdminUserService_UpdateUserTier_NilClerkService_NoPanic(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil) // nil ClerkService

	ctx := context.Background()
	actorID := uuid.New().String()
	userID := uuid.New().String()

	mockRepo.On("UpdateUserTier", ctx, actorID, userID, "premium", "reason", (*time.Time)(nil)).
		Return(repository.UpdateTierRow{ClerkUserID: "clerk_x", SubscriptionTier: "premium", SubscriptionStatus: "active"}, nil)

	assert.NotPanics(t, func() {
		_, err := service.UpdateUserTier(ctx, actorID, userID, "premium", "reason", nil)
		require.NoError(t, err)
	})

	mockRepo.AssertExpectations(t)
}

// --- Issue #1413 Defect 2: actor threaded through to the repository, never the target ---

// TestAdminUserService_UpdateUserTier_ActorAndTargetDiffer proves the service passes
// actorUserID and targetUserID through to the repository in the right slots (not swapped).
func TestAdminUserService_UpdateUserTier_ActorAndTargetDiffer(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil)

	ctx := context.Background()
	actorID := uuid.New().String()
	targetID := uuid.New().String()
	require.NotEqual(t, actorID, targetID)
	reason := "actor/target must not swap"

	// Exact positional match: if the service ever swaps actor/target, this expectation will
	// not match and the mock will panic on an unexpected call.
	mockRepo.On("UpdateUserTier", ctx, actorID, targetID, "premium", reason, (*time.Time)(nil)).
		Return(repository.UpdateTierRow{}, nil)

	_, err := service.UpdateUserTier(ctx, actorID, targetID, "premium", reason, nil)
	require.NoError(t, err)

	mockRepo.AssertExpectations(t)
}

// --- Issue #1413 Defect 4: malformed tier fails closed ---

func TestAdminUserService_UpdateUserTier_InvalidTier_Rejected(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, fakeTierConfig())

	_, err := service.UpdateUserTier(context.Background(), uuid.New().String(), uuid.New().String(), "garbage_tier", "reason", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidTierOverride), "error must unwrap to ErrInvalidTierOverride so the handler maps it to 400")

	mockRepo.AssertNotCalled(t, "UpdateUserTier")
}

// TestAdminUserService_UpdateUserTier_ValidTiers_Accepted proves every real configured tier
// name passes validation (table test over all five tiers, not a hardcoded allowlist check).
func TestAdminUserService_UpdateUserTier_ValidTiers_Accepted(t *testing.T) {
	for _, tier := range []string{"premium", "household_2", "household_3", "household_4", "household_5"} {
		t.Run(tier, func(t *testing.T) {
			mockRepo := new(MockAdminUserRepository)
			service := NewAdminUserService(mockRepo, nil, nil, 0, fakeTierConfig())

			ctx := context.Background()
			actorID := uuid.New().String()
			userID := uuid.New().String()

			mockRepo.On("UpdateUserTier", ctx, actorID, userID, tier, "reason", (*time.Time)(nil)).
				Return(repository.UpdateTierRow{}, nil)
			mockRepo.On("GetOwnedHouseholdActiveMemberCount", ctx, userID).Return(0, nil)

			_, err := service.UpdateUserTier(ctx, actorID, userID, tier, "reason", nil)
			require.NoError(t, err)

			mockRepo.AssertExpectations(t)
		})
	}
}

// TestAdminUserService_UpdateUserTier_NilTierConfig_SkipsValidation proves a nil tierConfig
// (not configured) skips validation entirely rather than panicking — nil-safe, matching the
// ClerkService/AdminService convention elsewhere in this constructor.
func TestAdminUserService_UpdateUserTier_NilTierConfig_SkipsValidation(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, nil) // nil tierConfig

	ctx := context.Background()
	userID := uuid.New().String()

	mockRepo.On("UpdateUserTier", ctx, "", userID, "anything_at_all", "reason", (*time.Time)(nil)).
		Return(repository.UpdateTierRow{}, nil)

	_, err := service.UpdateUserTier(ctx, "", userID, "anything_at_all", "reason", nil)
	assert.NoError(t, err)

	mockRepo.AssertExpectations(t)
}

// --- Issue #1413 Defect 5: household seat-capacity warning ---

// TestAdminUserService_UpdateUserTier_SeatWarning_FiresWhenBelowActiveCount proves a warning
// is surfaced (not an error, and never blocking) when the new tier's seat count is below the
// household owner's current active member count.
func TestAdminUserService_UpdateUserTier_SeatWarning_FiresWhenBelowActiveCount(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, fakeTierConfig())

	ctx := context.Background()
	actorID := uuid.New().String()
	userID := uuid.New().String()
	tier := "premium" // 1 seat

	mockRepo.On("UpdateUserTier", ctx, actorID, userID, tier, "comp down", (*time.Time)(nil)).
		Return(repository.UpdateTierRow{}, nil)
	mockRepo.On("GetOwnedHouseholdActiveMemberCount", ctx, userID).Return(4, nil) // 4 active members, only 1 seat

	result, err := service.UpdateUserTier(ctx, actorID, userID, tier, "comp down", nil)
	require.NoError(t, err, "the operation must succeed — this is a warning, never a block")
	assert.NotEmpty(t, result.Warning)
	assert.Contains(t, result.Warning, "1 seat")
	assert.Contains(t, result.Warning, "4 active member")

	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_UpdateUserTier_SeatWarning_DoesNotFire_WhenSufficientSeats proves no
// warning fires when the new tier has enough seats for the current active member count.
func TestAdminUserService_UpdateUserTier_SeatWarning_DoesNotFire_WhenSufficientSeats(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, fakeTierConfig())

	ctx := context.Background()
	actorID := uuid.New().String()
	userID := uuid.New().String()
	tier := "household_4" // 4 seats

	mockRepo.On("UpdateUserTier", ctx, actorID, userID, tier, "comp up", (*time.Time)(nil)).
		Return(repository.UpdateTierRow{}, nil)
	mockRepo.On("GetOwnedHouseholdActiveMemberCount", ctx, userID).Return(4, nil) // exactly at capacity

	result, err := service.UpdateUserTier(ctx, actorID, userID, tier, "comp up", nil)
	require.NoError(t, err)
	assert.Empty(t, result.Warning)

	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_UpdateUserTier_SeatWarning_DoesNotFire_WhenNotHouseholdOwner proves a
// non-owner (GetOwnedHouseholdActiveMemberCount returns 0) never triggers the warning,
// regardless of tier.
func TestAdminUserService_UpdateUserTier_SeatWarning_DoesNotFire_WhenNotHouseholdOwner(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, fakeTierConfig())

	ctx := context.Background()
	actorID := uuid.New().String()
	userID := uuid.New().String()
	tier := "premium"

	mockRepo.On("UpdateUserTier", ctx, actorID, userID, tier, "solo comp", (*time.Time)(nil)).
		Return(repository.UpdateTierRow{}, nil)
	mockRepo.On("GetOwnedHouseholdActiveMemberCount", ctx, userID).Return(0, nil)

	result, err := service.UpdateUserTier(ctx, actorID, userID, tier, "solo comp", nil)
	require.NoError(t, err)
	assert.Empty(t, result.Warning)

	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_UpdateUserTier_SeatCapacityCheckError_DoesNotBlock proves a failure in
// the best-effort seat-count lookup is logged and swallowed, never surfaced as an error.
func TestAdminUserService_UpdateUserTier_SeatCapacityCheckError_DoesNotBlock(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 0, fakeTierConfig())

	ctx := context.Background()
	actorID := uuid.New().String()
	userID := uuid.New().String()
	tier := "premium"

	mockRepo.On("UpdateUserTier", ctx, actorID, userID, tier, "reason", (*time.Time)(nil)).
		Return(repository.UpdateTierRow{}, nil)
	mockRepo.On("GetOwnedHouseholdActiveMemberCount", ctx, userID).Return(0, errors.New("db hiccup"))

	result, err := service.UpdateUserTier(ctx, actorID, userID, tier, "reason", nil)
	require.NoError(t, err, "a failed seat-capacity check must never block the tier change")
	assert.Empty(t, result.Warning)

	mockRepo.AssertExpectations(t)
}

// --- RemoveUserTierOverride tests (Issue #1414) ---

func TestAdminUserService_RemoveUserTierOverride_Success(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	mockClerk := new(MockClerkService)
	service := NewAdminUserService(mockRepo, nil, mockClerk, 6, nil)

	ctx := context.Background()
	actorID := uuid.New().String()
	targetID := uuid.New().String()
	reason := "no longer needed"
	priorOverride := "household_3"

	mockRepo.On("RemoveUserTierOverride", ctx, actorID, targetID, reason, 6).Return(repository.RemoveTierOverrideRow{
		PriorManualTierOverride: &priorOverride,
		Removed:                 true,
		RevertedTier:            "premium",
		RevertedStatus:          "active",
		ClerkUserID:             "clerk_target_123",
	}, nil)
	// #1413 AC-15: assert the Clerk payload key-by-key (mirroring the grant path's matcher
	// above and manual_tier_override_expiry_check_test.go's expiry-path matcher) — mock.Anything
	// here would let a regression that re-emits a stale premium claim, or drops the "tier": nil
	// poisoned-user cleanup, pass silently.
	//
	// Task X (L3, Codex review round on PR #1443): also assert the exact key COUNT, not just
	// these three keys' values — otherwise a regression that ADDS a key (e.g. reintroducing a
	// stale tier claim under a different name) would still satisfy every check above and pass
	// silently. len(m) == 3 pins the payload to exactly {subscription_tier, subscription_status,
	// tier} — domain.User.ClerkPublicMetadata's full key set, no more, no less.
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_target_123", mock.MatchedBy(func(m map[string]interface{}) bool {
		tierVal, hasTierKey := m["subscription_tier"]
		statusVal, hasStatusKey := m["subscription_status"]
		nilTierVal, hasNilTierKey := m["tier"]
		return len(m) == 3 &&
			hasTierKey && tierVal == "premium" &&
			hasStatusKey && statusVal == "active" &&
			hasNilTierKey && nilTierVal == nil
	})).Return(nil)

	result, err := service.RemoveUserTierOverride(ctx, actorID, targetID, reason)
	require.NoError(t, err)
	assert.True(t, result.Removed)
	assert.Equal(t, "premium", result.RevertedTier)
	assert.Equal(t, "active", result.RevertedStatus)

	mockRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

// TestAdminUserService_RemoveUserTierOverride_NoExistingOverride_StillCallsRepoAndSyncsClerk
// (#1413 G7: renamed from "...StillAuditsAndSyncs" — that name claimed an audit-log assertion
// this test never made and, at this layer, cannot make: auditing is written by
// AdminUserRepository.RemoveUserTierOverride's own SQL transaction, entirely inside the mocked
// call below. What this test actually proves is (a) the service calls the repo unconditionally
// even when Removed=false — i.e. does not short-circuit before the repo's audit-writing call —
// via mockRepo.AssertExpectations, and (b) the Clerk re-sync still fires on that no-op path. The
// real DB-level proof that a row lands in audit_logs lives at
// TestRemoveUserTierOverride_Integration_AuditRow_ActorAndTarget
// (internal/repository/admin_user_repository_remove_tier_override_integration_test.go:364).
func TestAdminUserService_RemoveUserTierOverride_NoExistingOverride_StillCallsRepoAndSyncsClerk(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	mockClerk := new(MockClerkService)
	service := NewAdminUserService(mockRepo, nil, mockClerk, 6, nil)

	ctx := context.Background()
	actorID := uuid.New().String()
	targetID := uuid.New().String()
	reason := "repair stale metadata"

	// No prior override (nil) -> Removed must be false, but the repo call (which writes the
	// audit row internally — proven at the repo integration test referenced above, not here) and
	// the Clerk sync both still happen.
	mockRepo.On("RemoveUserTierOverride", ctx, actorID, targetID, reason, 6).Return(repository.RemoveTierOverrideRow{
		PriorManualTierOverride: nil,
		Removed:                 false,
		RevertedTier:            "premium",
		RevertedStatus:          "active",
		ClerkUserID:             "clerk_target_456",
	}, nil)
	// #1413 AC-15: assert key-by-key, same as the grant path and TestAdminUserService_
	// RemoveUserTierOverride_Success above — mock.Anything would hide a stale-premium-claim or
	// dropped "tier": nil regression on this no-op repair path too.
	//
	// Task X (L3): also pin the exact key count (len(m) == 3) — see the Success test above for
	// why a per-key-only check would miss a regression that ADDS a key.
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_target_456", mock.MatchedBy(func(m map[string]interface{}) bool {
		tierVal, hasTierKey := m["subscription_tier"]
		statusVal, hasStatusKey := m["subscription_status"]
		nilTierVal, hasNilTierKey := m["tier"]
		return len(m) == 3 &&
			hasTierKey && tierVal == "premium" &&
			hasStatusKey && statusVal == "active" &&
			hasNilTierKey && nilTierVal == nil
	})).Return(nil)

	result, err := service.RemoveUserTierOverride(ctx, actorID, targetID, reason)
	require.NoError(t, err)
	assert.False(t, result.Removed)
	assert.Equal(t, "premium", result.RevertedTier)
	assert.Equal(t, "active", result.RevertedStatus)

	mockRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

func TestAdminUserService_RemoveUserTierOverride_UserNotFound(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 6, nil)

	ctx := context.Background()
	actorID := uuid.New().String()
	targetID := uuid.New().String()
	reason := "cleanup"

	mockRepo.On("RemoveUserTierOverride", ctx, actorID, targetID, reason, 6).Return(nil, ErrAdminUserNotFound)

	_, err := service.RemoveUserTierOverride(ctx, actorID, targetID, reason)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAdminUserNotFound), "error must unwrap to ErrAdminUserNotFound so the handler maps it to 404")

	mockRepo.AssertExpectations(t)
}

func TestAdminUserService_RemoveUserTierOverride_EmptyReason(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 6, nil)

	_, err := service.RemoveUserTierOverride(context.Background(), uuid.New().String(), uuid.New().String(), "")
	assert.ErrorContains(t, err, "reason is required")
	mockRepo.AssertNotCalled(t, "RemoveUserTierOverride")
}

func TestAdminUserService_RemoveUserTierOverride_MissingTargetUserID(t *testing.T) {
	service := NewAdminUserService(nil, nil, nil, 6, nil)
	_, err := service.RemoveUserTierOverride(context.Background(), uuid.New().String(), "", "reason")
	assert.Equal(t, ErrUserIDRequired, err)
}

func TestAdminUserService_RemoveUserTierOverride_ClerkFailure_StillSucceeds(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	mockClerk := new(MockClerkService)
	service := NewAdminUserService(mockRepo, nil, mockClerk, 6, nil)

	ctx := context.Background()
	actorID := uuid.New().String()
	targetID := uuid.New().String()
	reason := "comp expired"
	priorOverride := "premium"

	mockRepo.On("RemoveUserTierOverride", ctx, actorID, targetID, reason, 6).Return(repository.RemoveTierOverrideRow{
		PriorManualTierOverride: &priorOverride,
		Removed:                 true,
		RevertedTier:            "premium",
		RevertedStatus:          "cancelled",
		ClerkUserID:             "clerk_target_789",
	}, nil)
	// #1413 AC-15: assert key-by-key even though this call errors — the payload sent must still
	// be correct (a failed sync should not mask what would have gone out), same key set as the
	// grant path and the two tests above.
	//
	// Task X (L3): also pin the exact key count (len(m) == 3) — see the Success test above for
	// why a per-key-only check would miss a regression that ADDS a key.
	mockClerk.On("UpdateUserMetadata", ctx, "clerk_target_789", mock.MatchedBy(func(m map[string]interface{}) bool {
		tierVal, hasTierKey := m["subscription_tier"]
		statusVal, hasStatusKey := m["subscription_status"]
		nilTierVal, hasNilTierKey := m["tier"]
		return len(m) == 3 &&
			hasTierKey && tierVal == "premium" &&
			hasStatusKey && statusVal == "cancelled" &&
			hasNilTierKey && nilTierVal == nil
	})).Return(errors.New("clerk is down"))

	result, err := service.RemoveUserTierOverride(ctx, actorID, targetID, reason)
	require.NoError(t, err, "Clerk sync failure must not fail the overall call — DB update already succeeded")
	assert.True(t, result.Removed)

	mockRepo.AssertExpectations(t)
	mockClerk.AssertExpectations(t)
}

func TestAdminUserService_RemoveUserTierOverride_NilClerkService_NoPanic(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 6, nil) // nil ClerkService

	ctx := context.Background()
	actorID := uuid.New().String()
	targetID := uuid.New().String()
	reason := "cleanup"
	priorOverride := "premium"

	mockRepo.On("RemoveUserTierOverride", ctx, actorID, targetID, reason, 6).Return(repository.RemoveTierOverrideRow{
		PriorManualTierOverride: &priorOverride,
		Removed:                 true,
		RevertedTier:            "premium",
		RevertedStatus:          "active",
		ClerkUserID:             "clerk_target_999",
	}, nil)

	assert.NotPanics(t, func() {
		result, err := service.RemoveUserTierOverride(ctx, actorID, targetID, reason)
		require.NoError(t, err)
		assert.True(t, result.Removed)
	})

	mockRepo.AssertExpectations(t)
}

func TestAdminUserService_RemoveUserTierOverride_RepoError(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 6, nil)

	ctx := context.Background()
	actorID := uuid.New().String()
	targetID := uuid.New().String()
	reason := "cleanup"
	repoErr := errors.New("db down")

	mockRepo.On("RemoveUserTierOverride", ctx, actorID, targetID, reason, 6).Return(nil, repoErr)

	_, err := service.RemoveUserTierOverride(ctx, actorID, targetID, reason)
	assert.ErrorContains(t, err, "failed to remove user tier override: db down")

	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_RemoveUserTierOverride_ActorAndTargetDiffer proves the service passes
// actorUserID and targetUserID through to the repository in the right slots (not swapped).
// The shadowing between AdminSubscriptionTierInput's own path-bound UserID and the promoted
// AdminAuthenticatedInput.UserID is invisible on a skim at the handler layer; this test pins
// the service-level contract the handler must honor.
func TestAdminUserService_RemoveUserTierOverride_ActorAndTargetDiffer(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil, 6, nil)

	ctx := context.Background()
	actorID := uuid.New().String()
	targetID := uuid.New().String()
	require.NotEqual(t, actorID, targetID)
	reason := "actor/target must not swap"

	// mock.On with exact positional args: if the service ever swaps actor/target, this
	// expectation will not match and the mock will panic on an unexpected call.
	mockRepo.On("RemoveUserTierOverride", ctx, actorID, targetID, reason, 6).Return(repository.RemoveTierOverrideRow{
		Removed:        false,
		RevertedTier:   "premium",
		RevertedStatus: "active",
	}, nil)

	_, err := service.RemoveUserTierOverride(ctx, actorID, targetID, reason)
	require.NoError(t, err)

	mockRepo.AssertExpectations(t)
}
