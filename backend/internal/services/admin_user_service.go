package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Sentinel errors for AdminUserService
var (
	ErrAdminUserNotFound     = repository.ErrUserNotFound
	ErrInvalidPageNumber     = errors.New("page must be at least 1")
	ErrInvalidPageSizeNumber = errors.New("page_size must be between 1 and 100")
	ErrUserIDRequired        = errors.New("user_id is required")
	// ErrInvalidTierOverride indicates a requested tier is not a configured subscription tier
	// (config/subscription_tiers.yaml is the single source of truth, not a hardcoded list).
	// manual_tier_override is an unvalidated VARCHAR(50) column with no CHECK constraint, so
	// this must be rejected here rather than reaching ComputeEffectiveTier() as a 500 at read
	// time (Issue #1413 Defect 4). The handler maps this to huma.Error400BadRequest.
	ErrInvalidTierOverride = errors.New("tier is not a configured subscription tier")
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

	// UpdateUserTier updates a user's subscription tier natively and in Clerk.
	//
	// actorUserID is the acting admin's internal UUID (for the audit row; may be "").
	// userID is the target user whose tier override is being set.
	UpdateUserTier(ctx context.Context, actorUserID, userID, tier, reason string, expiresAt *time.Time) (UpdateUserTierResult, error)

	// RemoveUserTierOverride clears an existing manual tier override for the target user and
	// re-syncs Clerk metadata. Idempotent: a user with no existing override still returns
	// RemoveTierOverrideResult{Removed: false} rather than an error, and the audit row /
	// Clerk sync still happen (this call doubles as a repair hook for stale Clerk metadata).
	//
	// actorUserID is the acting admin's internal UUID (for the audit row; may be "").
	// targetUserID is the user whose override is being cleared.
	RemoveUserTierOverride(ctx context.Context, actorUserID, targetUserID, reason string) (RemoveTierOverrideResult, error)
}

// UpdateUserTierResult communicates the outcome of setting a user's manual tier override
// back to the caller (the HTTP handler).
type UpdateUserTierResult struct {
	// Warning is a non-blocking message surfaced in the HTTP response's message field when
	// the target owns a household and the new tier's seat count is below their current
	// active member count (Issue #1413 Defect 5). Empty when there's nothing to warn about.
	Warning string
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
	tierConfig      *config.TierConfig
}

// NewAdminUserService creates a new AdminUserService instance.
// adminService is used to check if users are admins (can be nil if not needed).
// clerkService is used to re-sync Clerk metadata on tier-override removal (can be nil —
// existing tests already pass nil for adminService, and this parameter is nil-safe the same
// way).
// retentionMonths sizes the retention-clock re-arm window on tier-override removal
// (config.Config.RetentionPeriodMonths — the same value trialService.SetRetentionMonths gets).
// tierConfig is the subscription tier configuration (config/subscription_tiers.yaml, the
// single source of truth) used to validate manual tier overrides and to look up seat counts
// for the household-downgrade warning (Issue #1413 Defects 4 and 5). Nil-safe: a nil
// tierConfig skips both checks rather than panicking.
func NewAdminUserService(repo repository.AdminUserRepository, adminService AdminService, clerkService ClerkService, retentionMonths int, tierConfig *config.TierConfig) AdminUserService {
	return &adminUserService{
		repo:            repo,
		adminService:    adminService,
		clerkService:    clerkService,
		retentionMonths: retentionMonths,
		tierConfig:      tierConfig,
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

// enrichAdminUser checks if user is admin and updates their info accordingly, and populates
// EffectiveStatus (Issue #1413) via the single shared domain predicate
// (domain.User.ComputeEffectiveStatus) — never reimplemented here.
func (s *adminUserService) enrichAdminUser(user *domain.AdminUserListItem) {
	// Build a scratch domain.User from this row's raw tier/status/override fields so
	// EffectiveStatus is computed by the one canonical predicate everything else uses.
	scratch := &domain.User{
		SubscriptionTier:   user.SubscriptionTier,
		SubscriptionStatus: user.SubscriptionStatus,
		ManualTierOverride: user.ManualTierOverride,
		ManualTierEndsAt:   user.ManualTierEndsAt,
	}
	user.EffectiveStatus = scratch.ComputeEffectiveStatus()

	if s.adminService == nil {
		return
	}
	if s.adminService.IsHardcodedAdmin(user.Email) {
		user.IsAdmin = true
		user.SubscriptionTier = "premium"
		user.SubscriptionStatus = "active"
		// Hardcoded admins are cosmetically forced to subscription_status=active for display;
		// EffectiveStatus must stay consistent with that displayed status rather than the raw
		// pre-override status computed above — two adjacent fields disagreeing about the same
		// user is the exact class of bug Issue #1413 exists to fix.
		user.EffectiveStatus = "active"
	}
}

// UpdateUserTier manually overrides a user's subscription tier locally and updates Clerk JWT
// claims. actorUserID is the acting admin's internal UUID, threaded through to the repository
// for the audit row (Issue #1413 Defect 2) — never the target.
func (s *adminUserService) UpdateUserTier(ctx context.Context, actorUserID, userID, tier, reason string, expiresAt *time.Time) (UpdateUserTierResult, error) {
	// Validate inputs
	if userID == "" {
		return UpdateUserTierResult{}, ErrUserIDRequired
	}
	if tier == "" {
		return UpdateUserTierResult{}, errors.New("tier is required")
	}
	if reason == "" {
		return UpdateUserTierResult{}, errors.New("reason is required")
	}

	// Defect 4: reject a tier that isn't a configured tier name, using the tier config (the
	// single source of truth) rather than a hardcoded list. manual_tier_override is an
	// unvalidated VARCHAR(50) column with no CHECK constraint, so a malformed value must fail
	// closed here rather than reaching ComputeEffectiveTier() as a 500 at read time.
	var tierCfg domain.SubscriptionTier
	if s.tierConfig != nil {
		cfg, cfgErr := s.tierConfig.GetTier(domain.TierName(tier))
		if cfgErr != nil {
			return UpdateUserTierResult{}, fmt.Errorf("%w: %q", ErrInvalidTierOverride, tier)
		}
		tierCfg = cfg
	}

	// 1. Update the database via AdminUserRepository. The repo returns the target's clerk_user_id
	// plus its raw (base) subscription_tier/subscription_status for step 2 below.
	row, err := s.repo.UpdateUserTier(ctx, actorUserID, userID, tier, reason, expiresAt)
	if err != nil {
		return UpdateUserTierResult{}, fmt.Errorf("failed to update user tier in database: %w", err)
	}

	result := UpdateUserTierResult{}

	// Defect 5: warn (never block) when the target owns a household and the new tier's seat
	// count is below their current active member count. Task 5 made a comped owner's
	// effective tier reach the seat-capacity machine, so comping someone down to a smaller
	// tier can silently start the over-capacity grace clock and eventually LIFO-offboard
	// members — a destructive side effect of what the admin intends as a gift. Best-effort:
	// a failure here logs and is skipped, it never blocks the tier change itself.
	if s.tierConfig != nil {
		activeMembers, cErr := s.repo.GetOwnedHouseholdActiveMemberCount(ctx, userID)
		if cErr != nil {
			slog.ErrorContext(ctx, "failed to check household seat capacity for tier override",
				"user_id", userID, "error", cErr)
		} else if activeMembers > tierCfg.Pricing.UsersIncluded {
			slog.WarnContext(ctx, "admin tier override drops household owner below active seat count",
				"user_id", userID, "tier", tier,
				"seats_included", tierCfg.Pricing.UsersIncluded, "active_members", activeMembers)
			result.Warning = fmt.Sprintf(
				"Warning: %q includes %d seat(s), but this household has %d active member(s); "+
					"excess members may eventually be offboarded unless the tier is increased or members are removed.",
				tier, tierCfg.Pricing.UsersIncluded, activeMembers)
		}
	}

	// 2. Update Clerk publicMetadata to reflect the new EFFECTIVE tier/status in the target
	// user's JWT claims, via the canonical merge-semantics helper (Issue #1413 Defect 1). The
	// scratch user carries the just-applied override so ClerkPublicMetadata() computes the
	// actual override tier (e.g. "household_4"), not a generic/raw value.
	if s.clerkService != nil && row.ClerkUserID != "" {
		scratch := &domain.User{
			SubscriptionTier:   row.SubscriptionTier,
			SubscriptionStatus: row.SubscriptionStatus,
			ManualTierOverride: &tier,
			ManualTierEndsAt:   expiresAt,
		}
		if syncErr := s.clerkService.UpdateUserMetadata(ctx, row.ClerkUserID, scratch.ClerkPublicMetadata()); syncErr != nil {
			// Clerk sync is best-effort: the DB update already succeeded, so log and continue.
			slog.ErrorContext(ctx, "failed to sync tier to clerk metadata", "clerk_user_id", row.ClerkUserID, "tier", tier, "error", syncErr)
		}
	}

	return result, nil
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
