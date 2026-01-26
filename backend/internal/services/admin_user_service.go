package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Sentinel errors for AdminUserService
var (
	ErrAdminUserNotFound     = repository.ErrUserNotFound
	ErrInvalidPageNumber     = errors.New("page must be at least 1")
	ErrInvalidPageSizeNumber = errors.New("page_size must be between 1 and 100")
	ErrUserIDRequired        = errors.New("user_id is required")
)

const (
	// DefaultAdminPageSize is the default number of users per page
	DefaultAdminPageSize = 20
	// MaxAdminPageSize is the maximum number of users per page
	MaxAdminPageSize = 100
)

// AdminUserService handles admin user management operations
type AdminUserService interface {
	// ListUsers returns a paginated list of users with optional filtering
	ListUsers(ctx context.Context, filters domain.AdminUserFilters, page, pageSize int) (*domain.AdminUserListResponse, error)

	// GetUserDetail returns detailed user information with statistics
	GetUserDetail(ctx context.Context, userID string) (*domain.AdminUserDetail, error)
}

// adminUserService implements AdminUserService
type adminUserService struct {
	repo         repository.AdminUserRepository
	adminService AdminService
}

// NewAdminUserService creates a new AdminUserService instance.
// adminService is used to check if users are admins (can be nil if not needed).
func NewAdminUserService(repo repository.AdminUserRepository, adminService AdminService) AdminUserService {
	return &adminUserService{repo: repo, adminService: adminService}
}

// ListUsers returns a paginated list of users with optional filtering
func (s *adminUserService) ListUsers(ctx context.Context, filters domain.AdminUserFilters, page, pageSize int) (*domain.AdminUserListResponse, error) {
	// Validate page
	if page < 1 {
		return nil, ErrInvalidPageNumber
	}

	// Validate page size
	if pageSize < 1 || pageSize > MaxAdminPageSize {
		return nil, ErrInvalidPageSizeNumber
	}

	// Convert page to offset
	offset := (page - 1) * pageSize

	// Get users from repository
	users, totalCount, err := s.repo.ListUsers(ctx, filters, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	// Ensure users is never nil
	if users == nil {
		users = []domain.AdminUserListItem{}
	}

	// Enrich users with admin status and override tier for admins
	s.enrichUsersWithAdminStatus(users)

	// Calculate pagination metadata
	totalPages := 0
	if totalCount > 0 {
		totalPages = (totalCount + pageSize - 1) / pageSize
	}

	return &domain.AdminUserListResponse{
		Users:      users,
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		HasMore:    page*pageSize < totalCount,
	}, nil
}

// GetUserDetail returns detailed user information with statistics
func (s *adminUserService) GetUserDetail(ctx context.Context, userID string) (*domain.AdminUserDetail, error) {
	// Validate user ID
	if userID == "" {
		return nil, ErrUserIDRequired
	}

	detail, err := s.repo.GetUserDetail(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Enrich with admin status and override tier for admins
	s.enrichAdminUser(&detail.AdminUserListItem)

	return detail, nil
}

// enrichUsersWithAdminStatus sets IsAdmin flag and overrides tier for admin users.
// Admins always show as "premium" tier with "active" status.
func (s *adminUserService) enrichUsersWithAdminStatus(users []domain.AdminUserListItem) {
	for i := range users {
		s.enrichAdminUser(&users[i])
	}
}

// enrichAdminUser checks if user is admin and updates their info accordingly.
func (s *adminUserService) enrichAdminUser(user *domain.AdminUserListItem) {
	if s.adminService == nil {
		return
	}
	if s.adminService.IsHardcodedAdmin(user.Email) {
		user.IsAdmin = true
		user.SubscriptionTier = "premium"
		user.SubscriptionStatus = "active"
	}
}
