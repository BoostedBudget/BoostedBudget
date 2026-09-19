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
)

const (
	// DefaultAdminPageSize is the default number of users per page
	DefaultAdminPageSize = 20
	// MaxAdminPageSize is the maximum number of users per page
	MaxAdminPageSize = 100
)

// AdminUserService handles admin user management operations
type AdminUserService interface {
	// ListUsers returns a paginated list of users with optional filtering.
	//
	// now is the single instant every row's EffectiveStatus on this page must be computed from —
	// the caller's own already-captured `as_of` snapshot (see AdminUserListResponse.AsOf's doc
	// comment) — NOT a fresh time.Now() read inside this method or enrichAdminUser. Passing the
	// caller's own instant keeps the returned `as_of` and every row's `effective_status` describing
	// the identical moment (Codex review finding on Issue #1462/#1413).
	ListUsers(ctx context.Context, filters domain.AdminUserFilters, page, pageSize int, now time.Time) (*domain.AdminUserListResponse, error)

	// GetUserDetail returns detailed user information with statistics.
	//
	// now is the single instant EffectiveStatus must be computed from — the caller's own
	// already-captured `as_of` snapshot (see AdminUserDetail.AsOf's doc comment), for the same
	// reason documented on ListUsers above.
	GetUserDetail(ctx context.Context, userID string, now time.Time) (*domain.AdminUserDetail, error)

	// GetOwnedHouseholdActiveMemberCount returns the number of active members in the household
	// owned by userID (0 if the user owns no household) — a direct passthrough to
	// AdminUserRepository.GetOwnedHouseholdActiveMemberCount, exposed here because the admin
	// subscription-detail handler only has access to this service, not the repository directly
	// (Issue #1452), and used by GetUserSubscription (admin_subscriptions.go) to surface the
	// count BEFORE a comp change is requested.
	GetOwnedHouseholdActiveMemberCount(ctx context.Context, userID string) (int, error)

	SetRevenueCatSummary(RevenueCatSummaryReader)
}

// adminUserService implements AdminUserService
type adminUserService struct {
	repo              repository.AdminUserRepository
	adminService      AdminService
	tierConfig        *config.TierConfig
	revenueCatSummary RevenueCatSummaryReader
}

// NewAdminUserService creates a new AdminUserService instance.
// adminService is used to check if users are admins (can be nil if not needed).
// tierConfig is the subscription tier configuration (config/subscription_tiers.yaml, the
// single source of truth) used to reject a malformed manual_tier_override name at read time
// (Issue #1413 Defect 4). Nil-safe: a nil tierConfig skips that validation rather than
// panicking.
func NewAdminUserService(repo repository.AdminUserRepository, adminService AdminService, tierConfig *config.TierConfig) AdminUserService {
	return &adminUserService{
		repo:         repo,
		adminService: adminService,
		tierConfig:   tierConfig,
	}
}

func (s *adminUserService) SetRevenueCatSummary(summary RevenueCatSummaryReader) {
	s.revenueCatSummary = summary
}

// ListUsers returns a paginated list of users with optional filtering
func (s *adminUserService) ListUsers(ctx context.Context, filters domain.AdminUserFilters, page, pageSize int, now time.Time) (*domain.AdminUserListResponse, error) {
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

	// Enrich users with admin status and override tier for admins, from the SAME instant the
	// caller will report as `as_of` — never a fresh clock read here.
	if s.revenueCatSummary != nil {
		for i := range users {
			s.enrichRevenueCatAdminUser(ctx, &users[i])
		}
	} else {
		s.enrichUsersWithAdminStatus(users, now)
	}

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
func (s *adminUserService) GetUserDetail(ctx context.Context, userID string, now time.Time) (*domain.AdminUserDetail, error) {
	// Validate user ID
	if userID == "" {
		return nil, ErrUserIDRequired
	}

	detail, err := s.repo.GetUserDetail(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Enrich with admin status and override tier for admins, from the SAME instant the caller
	// will report as `as_of` — never a fresh clock read here.
	if s.revenueCatSummary != nil {
		s.enrichRevenueCatAdminUser(ctx, &detail.AdminUserListItem)
	} else {
		s.enrichAdminUser(&detail.AdminUserListItem, now)
	}

	return detail, nil
}

func (s *adminUserService) enrichRevenueCatAdminUser(ctx context.Context, user *domain.AdminUserListItem) {
	summary := s.revenueCatSummary.Summary(ctx, &domain.User{ID: user.ID})
	user.AccessResolved = &summary.AccessResolved
	user.EffectiveStatus = "unresolved"
	user.EffectiveTier = "premium"
	// Preserve the query's observed own-access projection for its filter/sort
	// contract. It is maintained by RevenueCat Save, never an authorization input.
	// Live aggregate access is reported separately in EffectiveTier/Status.
	user.ManualTierOverride = nil
	user.ManualTierEndsAt = nil
	if summary.AccessResolved && summary.Effective != nil {
		user.EffectiveStatus = summary.Effective.Status
		user.EffectiveTier = summary.Effective.Tier
	}
	if s.adminService != nil && s.adminService.IsHardcodedAdmin(user.Email) {
		user.IsAdmin = true
	}
	// These compatibility display fields represent a confirmed comp only. They
	// are never persisted or used to authorize access.
	if summary.Comp != nil {
		user.ManualTierOverride = &summary.Comp.Tier
		user.ManualTierEndsAt = summary.Comp.ExpiresAt
	}
}

// GetOwnedHouseholdActiveMemberCount is a direct passthrough to
// AdminUserRepository.GetOwnedHouseholdActiveMemberCount — see the interface doc comment for why
// this exists as its own method rather than being folded into GetUserDetail's response (Issue
// #1452): GetUserSubscription needs it as a standalone value, and GetUserDetail's own
// domain.AdminUserDetail type is shared with the plain (non-subscription) admin user-detail
// endpoint, which has no use for a household seat count.
func (s *adminUserService) GetOwnedHouseholdActiveMemberCount(ctx context.Context, userID string) (int, error) {
	return s.repo.GetOwnedHouseholdActiveMemberCount(ctx, userID)
}

// enrichUsersWithAdminStatus sets IsAdmin flag and overrides tier for admin users.
// Admins always show as "premium" tier with "active" status.
//
// now is threaded straight through to enrichAdminUser for every row — see that method's doc
// comment for why this must be the caller's single already-captured instant, not time.Now().
func (s *adminUserService) enrichUsersWithAdminStatus(users []domain.AdminUserListItem, now time.Time) {
	for i := range users {
		s.enrichAdminUser(&users[i], now)
	}
}

// enrichAdminUser checks if user is admin and updates their info accordingly, and populates
// EffectiveStatus (Issue #1413) via the single shared domain predicate
// (domain.User.EffectiveStatusWithActiveOverrideAt) — never reimplemented here.
//
// Issue #1413 (this task): manual_tier_override is an unconstrained VARCHAR(50) with no CHECK
// constraint, so it can hold a value naming no configured tier. This site used to call the bare,
// unvalidated ComputeEffectiveStatus() — the only representation path in this codebase that
// never inherited the tier-name validation profile.go's enrichUserProfile and
// GetEffectiveSubscriptionStatusByUserID already enforce — so the admin list (and detail, which
// reuses this same enrichment) would report a malformed override as an active comp while the
// user's own GET /profile correctly rejected it. Uses this service's own tierConfig field
// directly as the validator (mechanism 2 — "a handler's own tierConfig" — matching
// PaddleCheckoutHandler.isConfiguredOverrideTier) rather than adding a third seam; nil-safe: a
// nil tierConfig skips validation, matching every other SetOverrideTierValidator call site's
// documented nil semantics.
//
// now MUST be the caller's own already-captured `as_of` snapshot instant, threaded all the way
// from the HTTP handler (admin.go / admin_subscriptions.go). Calling the ambient
// domain.User.EffectiveStatusWithActiveOverride() here (which reads its own time.Now()) would let
// this enrichment disagree with the `as_of` the response also carries whenever a request happens
// to straddle the override's expiry boundary between the handler's clock read and this one — the
// exact "two clocks describing one user" defect Issue #1462 exists to fix, reintroduced one layer
// up (Codex review finding).
func (s *adminUserService) enrichAdminUser(user *domain.AdminUserListItem, now time.Time) {
	// Build a scratch domain.User from this row's raw tier/status/override fields so
	// EffectiveStatus is computed by the one canonical predicate everything else uses.
	scratch := &domain.User{
		SubscriptionTier:   user.SubscriptionTier,
		SubscriptionStatus: user.SubscriptionStatus,
		ManualTierOverride: user.ManualTierOverride,
		ManualTierEndsAt:   user.ManualTierEndsAt,
	}
	status, overrideValue, hasOverride := scratch.EffectiveStatusWithActiveOverrideAt(now)
	if hasOverride && s.tierConfig != nil {
		if _, err := s.tierConfig.GetTier(domain.TierName(overrideValue)); err != nil {
			slog.Warn("rejecting manual tier override in admin list: value does not name a configured tier",
				"user_id", user.ID, "manual_tier_override", overrideValue)
			status = scratch.SubscriptionStatus
		}
	}
	user.EffectiveStatus = status

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
