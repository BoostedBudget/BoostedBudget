package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
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
	// (Issue #1452). Reused by UpdateUserTier's own post-apply seat-capacity warning below, and
	// by GetUserSubscription (admin_subscriptions.go) to surface the same count BEFORE an
	// override is applied — same underlying query, two different call sites.
	GetOwnedHouseholdActiveMemberCount(ctx context.Context, userID string) (int, error)

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

	// SetGDPRAnalytics wires in the optional, nil-safe GDPR-consent-gated PostHog analytics
	// client (Issue #1413 Task NB, replacing the raw phclient.Analytics wiring Task MB added).
	//
	// Both events fire with the TARGET user (userID/targetUserID) as distinct_id — the event and
	// that user's UUID land on THEIR PostHog profile — so GDPR consent governs this regardless of
	// who triggered the change: the target user did not act and may not have consented. This is
	// why these use CaptureIfConsented (like transfer_suggestion_service.go's user-initiated
	// events) rather than treating the action as system telemetry. The production gateway rejects
	// operational PostHog events entirely. audit_logs already
	// durably records both actions (admin_user_repository.go) as the compliance record; this
	// governs only the separate PostHog product-analytics stream.
	//
	// Never called in some tests/environments (PostHog unconfigured in local dev/QA) — both
	// UpdateUserTier and RemoveUserTierOverride must remain no-ops for analytics in that case.
	SetGDPRAnalytics(a phclient.GDPRAnalytics)
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
	gdprAnalytics   phclient.GDPRAnalytics // Optional GDPR-consent-gated analytics client (Issue #1413 Task NB)
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

// SetGDPRAnalytics wires in the optional, nil-safe GDPR-consent-gated PostHog analytics client
// (Issue #1413 Task NB). See the AdminUserService interface doc for why these events (unlike most
// admin-triggered events) go through the consent gate.
func (s *adminUserService) SetGDPRAnalytics(a phclient.GDPRAnalytics) {
	s.gdprAnalytics = a
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
	s.enrichUsersWithAdminStatus(users, now)

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
	s.enrichAdminUser(&detail.AdminUserListItem, now)

	return detail, nil
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

	// Truncate expiresAt to microsecond precision once, here, at the point it enters the
	// service — before it is used for BOTH the persisted write (step 1 below, via
	// AdminUserRepository.UpdateUserTier) and the in-memory Go predicate evaluation (the
	// Clerk-sync scratch user in step 2 below, via domain.User.hasActiveManualOverrideAt).
	// Postgres's timestamptz column is microsecond precision; Go's time.Time carries
	// nanoseconds. Left untruncated, a caller-supplied expiresAt with sub-microsecond precision
	// (input.Body.ExpiresAt is a JSON-decoded *time.Time with no precision constraint) would
	// evaluate as a very slightly later expiry in memory than what Postgres actually stores and
	// later evaluates via noActiveManualOverridePredicate — a narrow but real disagreement
	// window between the two systems at the exact tie. Doing it here, rather than at each call
	// site, means both uses below are guaranteed to see the identical instant.
	//
	// nil is left untouched: nil means a permanent (non-expiring) override, a distinct,
	// meaningful state — never coerce it into a zero time.
	//
	// RemoveUserTierOverride (below) has no mirrored concern: it only clears an override
	// (manual_tier_ends_at -> NULL) and never receives or persists a caller-supplied expiresAt,
	// so there is no second write/predicate pair for this field on that path.
	if expiresAt != nil {
		truncated := expiresAt.Truncate(time.Microsecond)
		expiresAt = &truncated
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

	// Issue #1413 Task NB (Codex High finding, replacing Task MB's raw phClient.Capture): fire
	// the product-analytics event on the success path only, exactly like the Clerk sync above —
	// best-effort, nil-safe. GDPR-gated via CaptureIfConsented, not raw Capture: this event's
	// distinct_id is userID (the TARGET user), so it and their UUID land on THEIR PostHog
	// profile — consent governs data about a data subject regardless of who triggered the
	// processing (see the AdminUserService interface doc for the full reasoning). Payload mirrors
	// the frontend's own trackEvent call (SubscriptionOverrideModal.tsx:142-145,
	// {tier, had_expiry}) so the two sides are joinable in PostHog, plus actor_user_id for the
	// backend's own value (nil, not "", when there is no acting admin — mirrors the repository's
	// audit-row NULL-actor convention just above).
	if s.gdprAnalytics != nil {
		var actorParam interface{}
		if actorUserID != "" {
			actorParam = actorUserID
		}
		s.gdprAnalytics.CaptureIfConsented(ctx, userID, phclient.EventAdminSubscriptionOverrideAppliedServer, map[string]interface{}{
			"tier":          tier,
			"had_expiry":    expiresAt != nil,
			"actor_user_id": actorParam,
		})
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

	// Issue #1413 Task NB (Codex High + Medium findings, replacing Task MB's raw phClient.Capture):
	// fire the product-analytics event only when an override was actually removed (row.Removed) —
	// an idempotent no-op call (nothing to clear; see this method's own doc comment) is not a
	// "removal" a product funnel should count, unlike the audit row above which always writes as
	// a repair hook. Best-effort/nil-safe, same as the Clerk sync above. GDPR-gated via
	// CaptureIfConsented for the same reason as the Applied event above — distinct_id is
	// targetUserID, so consent governs regardless of the acting admin.
	//
	// had_expiry is now included (Task NB, Part 2): RemoveTierOverrideRow threads
	// prior.manual_tier_ends_at through the repository's RETURNING clause
	// (admin_user_repository.go's removeUserTierOverrideUpdate), so this call site can report
	// whether the just-cleared override had an expiry — matching the Applied event and the
	// frontend's {tier, had_expiry} payload shape. actor_user_id is included and is nil, not "",
	// when there is no acting admin — mirrors the repository's own audit-row NULL-actor
	// convention (removeTierOverride's actorParam, just above the audit INSERT).
	if s.gdprAnalytics != nil && row.Removed {
		var actorParam interface{}
		if actorUserID != "" {
			actorParam = actorUserID
		}
		s.gdprAnalytics.CaptureIfConsented(ctx, targetUserID, phclient.EventAdminSubscriptionOverrideRemovedServer, map[string]interface{}{
			"tier":          *row.PriorManualTierOverride,
			"had_expiry":    row.PriorManualTierEndsAt != nil,
			"actor_user_id": actorParam,
		})
	}

	return RemoveTierOverrideResult{
		Removed:        row.Removed,
		RevertedTier:   row.RevertedTier,
		RevertedStatus: row.RevertedStatus,
	}, nil
}
