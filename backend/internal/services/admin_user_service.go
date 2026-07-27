package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/clerk/clerk-sdk-go/v2/user"
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

	// UpdateUserTier updates a user's subscription tier natively and in Clerk
	UpdateUserTier(ctx context.Context, clerkUserID, userID, tier, reason string, expiresAt *time.Time) error

	// RemoveUserTierOverride clears an existing manual tier override for the target user and
	// re-syncs Clerk metadata. Idempotent: a user with no existing override still returns
	// RemoveTierOverrideResult{Removed: false} rather than an error, and the audit row /
	// Clerk sync still happen (this call doubles as a repair hook for stale Clerk metadata).
	//
	// actorUserID is the acting admin's internal UUID (for the audit row; may be "").
	// targetUserID is the user whose override is being cleared.
	RemoveUserTierOverride(ctx context.Context, actorUserID, targetUserID, reason string) (RemoveTierOverrideResult, error)
}

// RemoveTierOverrideResult communicates the outcome of clearing a user's manual tier
// override back to the caller (the HTTP handler).
type RemoveTierOverrideResult struct {
	// Removed is true iff the user had a non-empty manual tier override before this call.
	Removed bool
	// RevertedTier is the user's base subscription tier, now effective again.
	RevertedTier string
	// RevertedStatus is the user's raw subscription status, now effective again.
	RevertedStatus string
}

// adminUserService implements AdminUserService
type adminUserService struct {
	repo            repository.AdminUserRepository
	adminService    AdminService
	clerkService    ClerkService
	retentionMonths int
}

// NewAdminUserService creates a new AdminUserService instance.
// adminService is used to check if users are admins (can be nil if not needed).
// clerkService is used to re-sync Clerk metadata on tier-override removal (can be nil —
// existing tests already pass nil for adminService, and this parameter is nil-safe the same
// way).
// retentionMonths sizes the retention-clock re-arm window on tier-override removal
// (config.Config.RetentionPeriodMonths — the same value trialService.SetRetentionMonths gets).
func NewAdminUserService(repo repository.AdminUserRepository, adminService AdminService, clerkService ClerkService, retentionMonths int) AdminUserService {
	return &adminUserService{
		repo:            repo,
		adminService:    adminService,
		clerkService:    clerkService,
		retentionMonths: retentionMonths,
	}
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

// UpdateUserTier manually overrides a user's subscription tier locally and updates Clerk JWT claims.
func (s *adminUserService) UpdateUserTier(ctx context.Context, _, userID, tier, reason string, expiresAt *time.Time) error {
	// Validate inputs
	if userID == "" {
		return ErrUserIDRequired
	}
	if tier == "" {
		return errors.New("tier is required")
	}
	if reason == "" {
		return errors.New("reason is required")
	}

	// 1. Update the database via AdminUserRepository.
	// The repo returns the target user's clerk_user_id for use in step 2.
	targetClerkUserID, err := s.repo.UpdateUserTier(ctx, userID, tier, reason, expiresAt)
	if err != nil {
		return fmt.Errorf("failed to update user tier in database: %w", err)
	}

	// 2. Update Clerk publicMetadata to reflect the new tier in the target user's JWT claims.
	if targetClerkUserID != "" {
		metadata := map[string]interface{}{
			"tier": tier,
		}
		metadataJSON, err := json.Marshal(metadata)
		if err != nil {
			slog.ErrorContext(ctx, "failed to marshal clerk metadata", "user_id", userID, "error", err)
			return nil
		}
		rawMetadata := json.RawMessage(metadataJSON)

		var params user.UpdateParams
		params.PublicMetadata = &rawMetadata

		_, err = user.Update(ctx, targetClerkUserID, &params)
		if err != nil {
			// Clerk sync is best-effort: the DB update already succeeded, so log and continue.
			slog.ErrorContext(ctx, "failed to sync tier to clerk metadata", "clerk_user_id", targetClerkUserID, "tier", tier, "error", err)
		}
	}

	return nil
}

// RemoveUserTierOverride clears an existing manual tier override for the target user,
// restarting the retention clock when applicable, and best-effort re-syncs Clerk metadata.
// See the AdminUserService interface doc for the idempotency contract.
func (s *adminUserService) RemoveUserTierOverride(ctx context.Context, actorUserID, targetUserID, reason string) (RemoveTierOverrideResult, error) {
	if targetUserID == "" {
		return RemoveTierOverrideResult{}, ErrUserIDRequired
	}
	if reason == "" {
		return RemoveTierOverrideResult{}, errors.New("reason is required")
	}

	row, err := s.repo.RemoveUserTierOverride(ctx, actorUserID, targetUserID, reason, s.retentionMonths)
	if err != nil {
		return RemoveTierOverrideResult{}, fmt.Errorf("failed to remove user tier override: %w", err)
	}

	// Clerk re-sync is best-effort and happens even when Removed is false — this call is the
	// natural repair hook for metadata #1413 may have corrupted. A scratch domain.User carries
	// the reverted (post-clear) tier/status so ClerkPublicMetadata() computes the correct
	// "no override" payload without a second DB read.
	if s.clerkService != nil && row.ClerkUserID != "" {
		scratch := &domain.User{
			SubscriptionTier:   row.RevertedTier,
			SubscriptionStatus: row.RevertedStatus,
		}
		if syncErr := s.clerkService.UpdateUserMetadata(ctx, row.ClerkUserID, scratch.ClerkPublicMetadata()); syncErr != nil {
			// Clerk sync is best-effort: the DB update already succeeded, so log and continue.
			slog.ErrorContext(ctx, "failed to sync reverted tier to clerk metadata",
				"clerk_user_id", row.ClerkUserID, "reverted_tier", row.RevertedTier, "error", syncErr)
		}
	}

	return RemoveTierOverrideResult{
		Removed:        row.Removed,
		RevertedTier:   row.RevertedTier,
		RevertedStatus: row.RevertedStatus,
	}, nil
}
