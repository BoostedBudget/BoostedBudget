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
	service := NewAdminUserService(mockRepo, nil, nil)

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
	response, err := service.ListUsers(ctx, filters, 1, 20, time.Now())

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
	service := NewAdminUserService(mockRepo, nil, nil)

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
	response, err := service.ListUsers(ctx, filters, 2, 20, time.Now())

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
	service := NewAdminUserService(mockRepo, nil, nil)

	ctx := context.Background()

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 40).Return([]domain.AdminUserListItem{}, 50, nil)

	// Execute
	response, err := service.ListUsers(ctx, filters, 3, 20, time.Now())

	// Assert
	require.NoError(t, err)
	assert.Equal(t, 3, response.TotalPages)
	assert.False(t, response.HasMore)
	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_ListUsers_WithFilters tests filtering passes through
func TestAdminUserService_ListUsers_WithFilters(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil)

	ctx := context.Background()

	filters := domain.AdminUserFilters{
		Search: "test@",
		Tier:   "premium",
	}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return([]domain.AdminUserListItem{}, 0, nil)

	// Execute
	response, err := service.ListUsers(ctx, filters, 1, 20, time.Now())

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
	service := NewAdminUserService(mockRepo, nil, nil)

	ctx := context.Background()

	filters := domain.AdminUserFilters{}
	mockRepo.On("ListUsers", ctx, filters, 20, 0).Return(nil, 0, errors.New("database error"))

	// Execute
	response, err := service.ListUsers(ctx, filters, 1, 20, time.Now())

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
	assert.Nil(t, response)
	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_ListUsers_InvalidPage tests validation of page
func TestAdminUserService_ListUsers_InvalidPage(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil)

	ctx := context.Background()
	filters := domain.AdminUserFilters{}

	// Execute with page 0 (invalid)
	response, err := service.ListUsers(ctx, filters, 0, 20, time.Now())

	// Assert - should default to page 1
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "page must be at least 1")
}

// TestAdminUserService_ListUsers_InvalidPageSize tests validation of page size
func TestAdminUserService_ListUsers_InvalidPageSize(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil)

	ctx := context.Background()
	filters := domain.AdminUserFilters{}

	// Execute with page size > 100
	response, err := service.ListUsers(ctx, filters, 1, 150, time.Now())

	// Assert
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "page_size must be between 1 and 100")
}

// TestAdminUserService_GetUserDetail_Success tests successful retrieval
func TestAdminUserService_GetUserDetail_Success(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil)

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
	detail, err := service.GetUserDetail(ctx, userID, time.Now())

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
	service := NewAdminUserService(mockRepo, nil, nil)

	ctx := context.Background()
	userID := uuid.New().String()

	mockRepo.On("GetUserDetail", ctx, userID).Return(nil, ErrAdminUserNotFound)

	// Execute
	detail, err := service.GetUserDetail(ctx, userID, time.Now())

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrAdminUserNotFound, err)
	assert.Nil(t, detail)
	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_GetUserDetail_EmptyUserID tests validation
func TestAdminUserService_GetUserDetail_EmptyUserID(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil)

	ctx := context.Background()

	// Execute with empty user ID
	detail, err := service.GetUserDetail(ctx, "", time.Now())

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
	service := NewAdminUserService(mockRepo, mockAdminService, nil)

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
	response, err := service.ListUsers(ctx, filters, 1, 20, time.Now())

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
	service := NewAdminUserService(mockRepo, nil, nil)

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

	response, err := service.ListUsers(ctx, filters, 1, 20, time.Now())
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
	service := NewAdminUserService(mockRepo, nil, nil)

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

	response, err := service.ListUsers(ctx, filters, 1, 20, time.Now())
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
	service := NewAdminUserService(mockRepo, nil, nil)

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

	response, err := service.ListUsers(ctx, filters, 1, 20, time.Now())
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
	service := NewAdminUserService(mockRepo, nil, fakeTierConfig())

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

	response, err := service.ListUsers(ctx, filters, 1, 20, time.Now())
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
	service := NewAdminUserService(mockRepo, nil, fakeTierConfig())

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

	response, err := service.ListUsers(ctx, filters, 1, 20, time.Now())
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
	service := NewAdminUserService(mockRepo, mockAdminService, nil)

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

	response, err := service.ListUsers(ctx, filters, 1, 20, time.Now())
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
	service := NewAdminUserService(mockRepo, mockAdminService, nil)

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
	detail, err := service.GetUserDetail(ctx, userID, time.Now())

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

// TestAdminUserService_GetUserDetail_EffectiveStatus_DerivedFromPassedInstant_NotLaterClockRead is
// the regression test for a Codex adversarial review finding on Issue #1462/#1413: GetUserDetail
// must compute EffectiveStatus from the SAME instant its caller captured as `as_of`
// (admin.go / admin_subscriptions.go each do `asOf := time.Now()` BEFORE calling this method), not
// from a second, independent time.Now() read taken later inside enrichAdminUser. Before this fix,
// enrichAdminUser called the ambient domain.User.EffectiveStatusWithActiveOverride() (which reads
// its own fresh clock); a request whose processing crossed the override's expiry boundary between
// the handler's as_of capture and enrichAdminUser's internal read would return an `as_of` and an
// `effective_status` describing two different moments in time — the exact "two clocks describing
// one user" defect #1462 exists to fix, reintroduced one layer up.
//
// This is made deterministic (no flaky timing race) as follows: `asOf` is captured FIRST, the
// override is set to expire shortly after `asOf`, and only THEN — after an artificial sleep well
// past that expiry — is GetUserDetail called, passing the ORIGINAL `asOf`. As of `asOf`, the
// override was still live, so a correct implementation must report EffectiveStatus "active". A
// service that instead reads a fresh time.Now() internally (the pre-fix behavior) would run that
// read AFTER the sleep, see an EXPIRED override, and wrongly report the raw "inactive" status —
// so this test fails on the pre-fix code and passes on the fix.
func TestAdminUserService_GetUserDetail_EffectiveStatus_DerivedFromPassedInstant_NotLaterClockRead(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil)

	ctx := context.Background()
	userID := uuid.New().String()

	asOf := time.Now()
	override := "household_2"
	// Still live AT asOf, but will have expired by the time the sleep below elapses.
	overrideEndsAt := asOf.Add(20 * time.Millisecond)

	repoDetail := &domain.AdminUserDetail{
		AdminUserListItem: domain.AdminUserListItem{
			ID:                 userID,
			Email:              "boundary-crossing@example.com",
			SubscriptionTier:   "premium",
			SubscriptionStatus: "inactive",
			ManualTierOverride: &override,
			ManualTierEndsAt:   &overrideEndsAt,
			IsActive:           true,
		},
	}
	mockRepo.On("GetUserDetail", ctx, userID).Return(repoDetail, nil)

	// Simulate request-processing time crossing the override's expiry boundary BETWEEN the
	// caller's as_of capture (asOf, above) and this call into the service.
	time.Sleep(60 * time.Millisecond)

	detail, err := service.GetUserDetail(ctx, userID, asOf)
	require.NoError(t, err)
	require.NotNil(t, detail)

	assert.Equal(t, "inactive", detail.SubscriptionStatus, "raw status must stay untouched")
	assert.Equal(t, "active", detail.EffectiveStatus,
		"EffectiveStatus must be derived from asOf (when the override was still live), not a later internal clock read taken after it had already expired")

	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_GetOwnedHouseholdActiveMemberCount_PassesThroughToRepo proves
// AdminUserService exposes AdminUserRepository.GetOwnedHouseholdActiveMemberCount unchanged
// (Issue #1452) — the admin subscription-detail handler needs this count to warn BEFORE an
// override is applied (rather than only after, in UpdateUserTier's existing post-apply
// warning), and the handler only has access to AdminUserService, not the repository directly.
func TestAdminUserService_GetOwnedHouseholdActiveMemberCount_PassesThroughToRepo(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil)

	ctx := context.Background()
	userID := uuid.New().String()

	mockRepo.On("GetOwnedHouseholdActiveMemberCount", ctx, userID).Return(3, nil)

	count, err := service.GetOwnedHouseholdActiveMemberCount(ctx, userID)

	require.NoError(t, err)
	assert.Equal(t, 3, count)
	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_GetOwnedHouseholdActiveMemberCount_PropagatesRepoError proves a repository
// failure is surfaced to the caller rather than silently swallowed — unlike UpdateUserTier's
// best-effort seat-capacity check (which logs and skips on error since it must never block a
// tier change), this is a direct read the admin subscription-detail GET needs to render
// accurately, so a failure here should fail the request rather than silently show 0 members.
func TestAdminUserService_GetOwnedHouseholdActiveMemberCount_PropagatesRepoError(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil)

	ctx := context.Background()
	userID := uuid.New().String()

	mockRepo.On("GetOwnedHouseholdActiveMemberCount", ctx, userID).Return(0, errors.New("db error"))

	count, err := service.GetOwnedHouseholdActiveMemberCount(ctx, userID)

	assert.Error(t, err)
	assert.Equal(t, 0, count)
	mockRepo.AssertExpectations(t)
}

// TestAdminUserService_ListUsers_NilAdminService tests graceful handling of nil AdminService
func TestAdminUserService_ListUsers_NilAdminService(t *testing.T) {
	mockRepo := new(MockAdminUserRepository)
	service := NewAdminUserService(mockRepo, nil, nil) // nil AdminService

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
	response, err := service.ListUsers(ctx, filters, 1, 20, time.Now())

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
