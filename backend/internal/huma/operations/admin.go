package operations

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/auth"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// TrialGranter defines the interface for granting trials to users.
// Issue #698: Used by admin endpoint to grant trial during E2E tests.
type TrialGranter interface {
	GrantTrial(ctx context.Context, userID string) error
}

// AdminHandler handles admin-only endpoints for testing and troubleshooting.
// Routes are protected by AdminMiddleware, which authorizes by identity and admin role in
// every environment (Issue #1499 removed its environment check).
type AdminHandler struct {
	revenueCat            *RevenueCatAdminDeps
	plaidService          services.PlaidService
	linkedItemRepo        repository.LinkedItemRepository
	trialService          TrialGranter
	adminService          services.AdminService
	adminUserService      services.AdminUserService
	systemHealthService   services.SystemHealthService
	subService            *services.SubscriptionService
	paddleTransactionRepo repository.PaddleTransactionRepository
	paddleWebhookRepo     repository.PaddleWebhookRepository
	deadLetterService     services.DeadLetterService
	// plaidOrphanedItemRepo backs the orphaned-Plaid-Item admin listing (Issue #1497). Read-only:
	// there is no admin re-attempt action, because the automated retry sweep already re-attempts every
	// unresolved record on its own cadence and a button would only duplicate it.
	plaidOrphanedItemRepo repository.PlaidOrphanedItemRepository
	// retentionWithheldReader backs the withheld-retention admin listing (Issue #1584). Read-only,
	// deliberately: what to do about a stuck account is an open product decision, so there is no
	// write beside this read — see ListAdminRetentionWithheld.
	retentionWithheldReader RetentionWithheldReader
	// budgetHierarchyAuditReader backs the budget-hierarchy-audit admin listing (Issue #1630 Task
	// B7). Read-only: match_logic on an escaping pair and identity fields on a duplicate-parent
	// pair are never auto-repaired (see domain/budget.go's ErrEscapedNestedChild doc comment), so
	// there is no write action for this surface to expose — only visibility.
	budgetHierarchyAuditReader BudgetHierarchyAuditReader
	jobQueue                   queue.JobQueue
	config                     *config.Config
}

// NewAdminHandler creates a new AdminHandler with required dependencies.
func NewAdminHandler(
	plaidService services.PlaidService,
	linkedItemRepo repository.LinkedItemRepository,
	trialService TrialGranter,
	adminService services.AdminService,
	adminUserService services.AdminUserService,
	systemHealthService services.SystemHealthService,
	subService *services.SubscriptionService,
	cfg *config.Config,
) *AdminHandler {
	return &AdminHandler{
		plaidService:        plaidService,
		linkedItemRepo:      linkedItemRepo,
		trialService:        trialService,
		adminService:        adminService,
		adminUserService:    adminUserService,
		systemHealthService: systemHealthService,
		subService:          subService,
		config:              cfg,
	}
}

// WithPaddleTransactionRepo sets the PaddleTransactionRepository on the handler (builder pattern).
// Used to avoid changing the constructor signature and breaking existing tests.
func (h *AdminHandler) WithPaddleTransactionRepo(repo repository.PaddleTransactionRepository) *AdminHandler {
	h.paddleTransactionRepo = repo
	return h
}

// WithPaddleWebhookRepo sets the PaddleWebhookRepository on the handler (builder pattern).
func (h *AdminHandler) WithPaddleWebhookRepo(repo repository.PaddleWebhookRepository) *AdminHandler {
	h.paddleWebhookRepo = repo
	return h
}

// WithJobQueue sets the JobQueue on the handler (builder pattern).
func (h *AdminHandler) WithJobQueue(q queue.JobQueue) *AdminHandler {
	h.jobQueue = q
	return h
}

// WithDeadLetterService sets the DeadLetterService on the handler (builder pattern).
func (h *AdminHandler) WithDeadLetterService(svc services.DeadLetterService) *AdminHandler {
	h.deadLetterService = svc
	return h
}

// WithPlaidOrphanedItemRepo sets the PlaidOrphanedItemRepository on the handler (builder pattern).
// Issue #1497.
func (h *AdminHandler) WithPlaidOrphanedItemRepo(repo repository.PlaidOrphanedItemRepository) *AdminHandler {
	h.plaidOrphanedItemRepo = repo
	return h
}

// WithBudgetHierarchyAuditReader sets the budget-hierarchy-audit reader on the handler (builder
// pattern). Issue #1630 Task B7.
func (h *AdminHandler) WithBudgetHierarchyAuditReader(r BudgetHierarchyAuditReader) *AdminHandler {
	h.budgetHierarchyAuditReader = r
	return h
}

// --- Input/Output Types ---

// FireWebhookInput represents the input for firing a Plaid webhook.
type FireWebhookInput struct {
	inputs.AdminAuthenticatedInput
	Body FireWebhookRequestBody
}

// FireWebhookRequestBody defines the request body for firing a Plaid webhook.
type FireWebhookRequestBody struct {
	ItemID      string `json:"item_id" validate:"required,uuid" doc:"The Plaid item ID (UUID)"`
	WebhookCode string `json:"webhook_code" validate:"required,oneof=INITIAL_UPDATE DEFAULT_UPDATE HISTORICAL_UPDATE TRANSACTIONS_REMOVED" doc:"The webhook code to fire"`
}

// FireWebhookOutput represents the response for webhook firing.
type FireWebhookOutput struct {
	Body FireWebhookResponseBody
}

// FireWebhookResponseBody defines the response body for webhook firing.
type FireWebhookResponseBody struct {
	WebhookFired bool   `json:"webhook_fired" doc:"Whether the webhook was fired successfully"`
	RequestID    string `json:"request_id" doc:"The request ID from Plaid"`
	ItemID       string `json:"item_id" doc:"The Plaid item ID"`
	WebhookCode  string `json:"webhook_code" doc:"The webhook code that was fired"`
}

// GrantTrialInput represents the input for granting trial to current user.
// Issue #698: Used by E2E tests to ensure test user has active subscription.
type GrantTrialInput struct {
	inputs.AdminAuthenticatedInput
}

// GrantTrialOutput represents the response for trial grant.
type GrantTrialOutput struct {
	Body GrantTrialResponseBody
}

// GrantTrialResponseBody defines the response body for trial grant.
type GrantTrialResponseBody struct {
	Success bool   `json:"success" doc:"Whether the trial was granted successfully"`
	Message string `json:"message" doc:"Description of the result"`
}

// CheckRoleInput represents the input for checking admin role.
// Issue #638: Allows any authenticated user to check their admin status.
type CheckRoleInput struct {
	inputs.AuthenticatedInput
	// ClerkUserID is needed to check Clerk privateMetadata for roles.
	ClerkUserID string `json:"-" doc:"Clerk user ID (resolved from JWT)"`
	// Email is needed to check hardcoded admin list.
	Email string `json:"-" doc:"User email from Clerk claims"`
}

// Resolve implements huma.Resolver to extract user context from request.
func (c *CheckRoleInput) Resolve(ctx huma.Context) []error {
	// First resolve the base AuthenticatedInput
	if errs := c.AuthenticatedInput.Resolve(ctx); len(errs) > 0 {
		return errs
	}

	// Extract ClerkUserID from context
	clerkUserID, ok := ctx.Context().Value(inputs.ClerkUserIDKey).(string)
	if !ok || clerkUserID == "" {
		return []error{huma.Error401Unauthorized("Clerk user ID not found in context")}
	}
	c.ClerkUserID = clerkUserID

	// Extract email from Clerk claims (optional for hardcoded admin check)
	claims, ok := ctx.Context().Value(inputs.ClerkClaimsKey).(*clerk.SessionClaims)
	if ok && claims != nil {
		c.Email = auth.ExtractEmailFromClaims(claims)
	}

	// FALLBACK: If email not in JWT claims, fetch from Clerk API
	// This handles cases where JWT template claims aren't populated correctly
	// (e.g., no JWT template configured, or using default Clerk token)
	if c.Email == "" && c.ClerkUserID != "" {
		var fetchErr error
		c.Email, fetchErr = auth.FetchEmailFromClerkAPI(ctx.Context(), c.ClerkUserID)
		if fetchErr != nil {
			// Log but don't fail - email is optional for admin check
			// The hardcoded admin check just won't work, but Clerk metadata check still will
			slog.Default().Warn("failed to fetch email from Clerk API for role check", "clerk_user_id", c.ClerkUserID, "error", fetchErr)
		}
	}

	return nil
}

// CheckRoleOutput represents the response for role check.
type CheckRoleOutput struct {
	Body CheckRoleResponseBody
}

// CheckRoleResponseBody defines the response body for role check.
type CheckRoleResponseBody struct {
	IsAdmin bool `json:"is_admin" doc:"Whether the user has admin role"`
}

// --- Admin User Management Types (Issue #639) ---

// ListUsersInput represents the input for listing users with pagination and filters.
type ListUsersInput struct {
	inputs.AdminAuthenticatedInput
	inputs.PagePaginationInput
	Search  string `query:"search"   doc:"Search by email prefix, starts-with match (min 3 chars)"`
	Tier    string `query:"tier"     doc:"Filter by subscription tier"`
	SortBy  string `query:"sort_by"  enum:"created_at,last_login_at,subscription_tier,subscription_status" default:"created_at"  doc:"Column to sort by"`
	SortDir string `query:"sort_dir" enum:"asc,desc"                                                      default:"desc"         doc:"Sort direction"`
}

// ListUsersOutput represents the response for user listing.
type ListUsersOutput struct {
	Body *domain.AdminUserListResponse
}

// GetUserDetailInput represents the input for getting user details.
type GetUserDetailInput struct {
	inputs.AdminAuthenticatedInput
	UserID string `path:"user_id" doc:"User UUID"`
}

// GetUserDetailOutput represents the response for user details.
type GetUserDetailOutput struct {
	Body *domain.AdminUserDetail
}

// --- System Health Types (Issue #640) ---

// GetSystemHealthInput represents the input for getting system health metrics.
type GetSystemHealthInput struct {
	inputs.AdminAuthenticatedInput
}

// GetSystemHealthOutput represents the response for system health metrics.
type GetSystemHealthOutput struct {
	Body *domain.SystemHealthResponse
}

// --- Handler Methods ---

// FireWebhook fires a Plaid webhook for testing.
//
// POST /api/v1/admin/plaid/fire-webhook
//
// Issue #1498: this is registered and admin-reachable in EVERY environment — #1499 removed
// the admin middleware's environment gate. What actually confines it is
// plaidService.FireWebhook's PlaidEnv != "sandbox" check, so in production (PLAID_ENV=production)
// it returns an error rather than firing anything. Do not describe it as "QA only".
//
// Error Responses:
//   - 400: Invalid request body or validation failure
//   - 401: Not authenticated
//   - 403: Not an admin (checked by AdminMiddleware)
//   - 404: Linked item not found
//   - 500: Decryption failure or Plaid API error
//   - 503: Plaid service not configured
func (h *AdminHandler) FireWebhook(ctx context.Context, input *FireWebhookInput) (*FireWebhookOutput, error) {
	// Issue #1499: admin routes are now registered even when Plaid is unconfigured, so that a
	// non-Plaid deployment still gets system health, user management, subscription admin, the
	// dead-letter queue and the orphaned-items view. This is the one admin handler with a
	// Plaid dependency, so it refuses the same way the other under-configured ones do.
	if h.plaidService == nil || h.linkedItemRepo == nil {
		return nil, huma.Error503ServiceUnavailable("Plaid service not configured")
	}

	// 1. Fetch linked item by Plaid item ID (not database primary key)
	// Issue #331: item_id is the Plaid identifier, not the database primary key
	linkedItem, err := h.linkedItemRepo.GetByItemID(ctx, input.Body.ItemID)
	if err != nil {
		if err == repository.ErrLinkedItemNotFound {
			return nil, huma.Error404NotFound("Item not found")
		}
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to fetch linked item: %w", err))
	}

	// 2. Fire webhook via Plaid service using plaintext access token from repo layer
	response, err := h.plaidService.FireWebhook(ctx, linkedItem.AccessToken, input.Body.WebhookCode)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to fire webhook: %w", err))
	}

	// 4. Build and return success response
	return &FireWebhookOutput{
		Body: FireWebhookResponseBody{
			WebhookFired: response.WebhookFired,
			RequestID:    response.RequestId,
			ItemID:       input.Body.ItemID,
			WebhookCode:  input.Body.WebhookCode,
		},
	}, nil
}

// GrantTrial grants a 60-day trial subscription to the authenticated user.
// Issue #698: Used by E2E tests to ensure test user has active subscription.
//
// POST /api/v1/admin/subscription/grant-trial
//
// Issue #1498 — DELIBERATE DECISION, do not "fix" this by adding an environment gate.
// Issue #1499 removed the admin middleware's environment gate, so this endpoint is now
// reachable in production. That was reviewed and accepted: granting a trial is a
// legitimate admin action, and any real admin can already change a user's subscription
// through the other admin subscription endpoints (see AdminUserService's tier-override
// and subscription-management paths). Gating this one endpoint would buy no security
// while breaking the E2E flow. The OpenAPI summary used to say "QA only"; it now says
// "(admin)" and states the every-environment availability, so the contract matches reality.
// Recorded in docs/decisions/2026-08-06-admin-authorization-posture.md.
//
// Error Responses:
//   - 401: Not authenticated
//   - 403: Not an admin (checked by AdminMiddleware)
//   - 409: User has already had a trial
//   - 500: Failed to grant trial
//   - 503: Trial service not configured
func (h *AdminHandler) GrantTrial(ctx context.Context, input *GrantTrialInput) (*GrantTrialOutput, error) {
	// Check if trial service is configured
	if h.trialService == nil {
		return nil, huma.Error503ServiceUnavailable("Trial service not configured")
	}

	// Grant trial to the authenticated user
	err := h.trialService.GrantTrial(ctx, input.UserID)
	if err != nil {
		// Check if user already had a trial (not an error for E2E purposes)
		if strings.Contains(err.Error(), "has already had a trial") {
			return &GrantTrialOutput{
				Body: GrantTrialResponseBody{
					Success: true,
					Message: "User already has or had a trial",
				},
			}, nil
		}
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to grant trial: %w", err))
	}

	return &GrantTrialOutput{
		Body: GrantTrialResponseBody{
			Success: true,
			Message: "Trial granted successfully",
		},
	}, nil
}

// CheckRole checks if the current user has admin role.
// Issue #638: Allows frontend to check admin status since roles are in Clerk privateMetadata.
//
// GET /api/v1/admin/check-role
//
// Unlike other admin endpoints, this is callable by ANY authenticated user (not just admins).
// The middleware is modified to skip role verification for this specific endpoint.
//
// Error Responses:
//   - 401: Not authenticated
//   - 500: Failed to check admin role
//   - 503: Admin service not configured
func (h *AdminHandler) CheckRole(ctx context.Context, input *CheckRoleInput) (*CheckRoleOutput, error) {
	// Check if admin service is configured
	if h.adminService == nil {
		return nil, huma.Error503ServiceUnavailable("Admin service not configured")
	}

	// 1. Check hardcoded admin list first (avoids Clerk API call)
	if input.Email != "" && h.adminService.IsHardcodedAdmin(input.Email) {
		return &CheckRoleOutput{
			Body: CheckRoleResponseBody{
				IsAdmin: true,
			},
		}, nil
	}

	// 2. Check admin role via Clerk privateMetadata
	hasAdminRole, err := h.adminService.HasRole(ctx, input.ClerkUserID, domain.RoleAdmin)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to check admin role: %w", err))
	}

	return &CheckRoleOutput{
		Body: CheckRoleResponseBody{
			IsAdmin: hasAdminRole,
		},
	}, nil
}

// --- Admin User Management Handlers (Issue #639) ---

// ListUsers returns a paginated list of users with optional search and tier filter.
// Issue #639: Admin user management API
//
// GET /api/v1/admin/users
//
// Error Responses:
//   - 401: Not authenticated
//   - 403: Not an admin (checked by AdminMiddleware)
//   - 500: Internal server error
//   - 503: Admin user service not configured
func (h *AdminHandler) ListUsers(ctx context.Context, input *ListUsersInput) (*ListUsersOutput, error) {
	// Issue #1462 follow-up G2: captured before the rest of this handler's work, mirroring
	// GetUserSubscription's `as_of` capture in admin_subscriptions.go — a single server instant
	// for the WHOLE page, so UsersPage.tsx can anchor every row's isOverrideActive/
	// getOverrideStatus liveness check to one shared, trusted clock instead of the browser's.
	asOf := time.Now()

	// Check if admin user service is configured
	if h.adminUserService == nil {
		return nil, huma.Error503ServiceUnavailable("Admin user service not configured")
	}

	// Validate search length: min 3 runes (matches prefix index minimum).
	// Frontend suppresses API calls for search.length < 3, but defend server-side too.
	search := strings.TrimSpace(input.Search)
	if search != "" && len([]rune(search)) < 3 {
		return nil, huma.Error400BadRequest("search must be at least 3 characters")
	}

	// Build filters from input
	filters := domain.AdminUserFilters{
		Search:  search,
		Tier:    input.Tier,
		SortBy:  input.SortBy,
		SortDir: input.SortDir,
	}

	// Get users from service, computed from the SAME instant (asOf) that will be reported below —
	// never a fresh clock read inside the service (Codex review finding on Issue #1462/#1413).
	response, err := h.adminUserService.ListUsers(ctx, filters, input.Page, input.PageSize, asOf)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to list users: %w", err))
	}
	response.AsOf = asOf

	return &ListUsersOutput{Body: response}, nil
}

// GetUserDetail returns detailed user information with statistics.
// Issue #639: Admin user management API
//
// GET /api/v1/admin/users/{user_id}
//
// Error Responses:
//   - 401: Not authenticated
//   - 403: Not an admin (checked by AdminMiddleware)
//   - 404: User not found
//   - 500: Internal server error
//   - 503: Admin user service not configured
func (h *AdminHandler) GetUserDetail(ctx context.Context, input *GetUserDetailInput) (*GetUserDetailOutput, error) {
	// Issue #1462 follow-up G2: captured before the rest of this handler's work, mirroring
	// GetUserSubscription's `as_of` capture in admin_subscriptions.go — lets UserDetailModal.tsx
	// anchor its own isOverrideActive/getOverrideStatus liveness check to this server instant
	// instead of the browser's clock.
	asOf := time.Now()

	// Check if admin user service is configured
	if h.adminUserService == nil {
		return nil, huma.Error503ServiceUnavailable("Admin user service not configured")
	}

	// Get user detail from service, computed from the SAME instant (asOf) that will be reported
	// below — never a fresh clock read inside the service (Codex review finding on Issue
	// #1462/#1413).
	detail, err := h.adminUserService.GetUserDetail(ctx, input.UserID, asOf)
	if err != nil {
		if err == services.ErrAdminUserNotFound {
			return nil, huma.Error404NotFound("User not found")
		}
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get user detail: %w", err))
	}
	detail.AsOf = asOf

	return &GetUserDetailOutput{Body: detail}, nil
}

// GetSystemHealth returns comprehensive system health metrics.
// Issue #640: System health dashboard API
//
// GET /api/v1/admin/system/health
//
// Error Responses:
//   - 401: Not authenticated
//   - 403: Not an admin (checked by AdminMiddleware)
//   - 500: Internal server error
//   - 503: System health service not configured
func (h *AdminHandler) GetSystemHealth(ctx context.Context, input *GetSystemHealthInput) (*GetSystemHealthOutput, error) {
	// Check if system health service is configured
	if h.systemHealthService == nil {
		return nil, huma.Error503ServiceUnavailable("System health service not configured")
	}

	// Get system health from service
	health, err := h.systemHealthService.GetSystemHealth(ctx)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to get system health: %w", err))
	}

	return &GetSystemHealthOutput{Body: health}, nil
}

// RegisterAdminRoutes registers admin routes with the Huma API.
// Routes are protected by AdminMiddleware (role validation, all environments — Issue #1499).
// Safe to call with a nil plaidService/linkedItemRepo: FireWebhook is the only handler that
// needs them and it returns 503 when they are absent.
func RegisterAdminRoutes(api huma.API, handler *AdminHandler) {
	// POST /api/v1/admin/plaid/fire-webhook - Fire a Plaid webhook
	// Issue #1498: the summary/description used to say "QA only", which misstated the security
	// boundary in the generated contract (api/openapi.yaml, and from there the MCP capability
	// manifest and the TS client). This endpoint is admin-reachable in every environment; what
	// confines it is the sandbox-only check in plaidService.FireWebhook.
	huma.Register(api, huma.Operation{
		OperationID: "fireWebhook",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/plaid/fire-webhook",
		Summary:     "Fire a Plaid webhook (sandbox Plaid only)",
		Description: "Fires a Plaid webhook for testing. Requires Clerk authentication and admin " +
			"authorization. Reachable in every environment, but only functions when PLAID_ENV=sandbox; " +
			"it returns an error otherwise.",
		Tags:     []string{"Admin"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.FireWebhook)

	// POST /api/v1/admin/subscription/grant-trial - Grant trial to current user
	// Issue #698: Used by E2E tests to ensure test user has active subscription.
	// Issue #1498: the summary used to say "QA only". This endpoint is DELIBERATELY reachable in
	// production — see the decision note on AdminHandler.GrantTrial — so that wording misstated
	// the security boundary in the generated contract. Do not re-add it.
	huma.Register(api, huma.Operation{
		OperationID: "grantTrial",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/subscription/grant-trial",
		Summary:     "Grant trial subscription (admin)",
		Description: "Grants a 60-day trial to the authenticated admin user. Used by E2E testing, but " +
			"deliberately available in every environment — any admin can already change subscriptions " +
			"through the other admin endpoints. Returns success if the user already has or had a trial.",
		Tags:     []string{"Admin"},
		Security: []map[string][]string{{"clerk": {}}},
	}, handler.GrantTrial)

	// GET /api/v1/admin/check-role - Check if current user is admin
	// Issue #638: Available to all authenticated users (not just admins).
	huma.Register(api, huma.Operation{
		OperationID: "checkRole",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/check-role",
		Summary:     "Check if current user is admin",
		Description: "Returns whether the authenticated user has admin role. Callable by any authenticated user, available in all environments.",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.CheckRole)

	// --- Admin User Management Routes (Issue #639) ---

	// GET /api/v1/admin/users - List users with pagination and filters
	huma.Register(api, huma.Operation{
		OperationID: "listAdminUsers",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/users",
		Summary:     "List users (admin)",
		Description: "Returns a paginated list of users with optional email search and tier filtering. Requires admin role.",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.ListUsers)

	// GET /api/v1/admin/users/{user_id} - Get user details
	huma.Register(api, huma.Operation{
		OperationID: "getAdminUserDetail",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/users/{user_id}",
		Summary:     "Get user details (admin)",
		Description: "Returns detailed user information including statistics. Requires admin role.",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetUserDetail)

	// GET /api/v1/admin/system/health - Get system health metrics (Issue #640)
	huma.Register(api, huma.Operation{
		OperationID: "getSystemHealth",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/system/health",
		Summary:     "Get system health metrics (admin)",
		Description: "Returns comprehensive system health metrics including database, job queues, and sync status. Requires admin role.",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetSystemHealth)

	// --- Admin Subscription Management Routes (Issue #641) ---
	RegisterAdminSubscriptionsOperations(api, handler)

	// --- Admin Payment Failures Route (Issues #747 gap) ---
	RegisterAdminPaymentFailuresOperations(api, handler)

	// --- Admin Paddle Webhooks Routes (Issue #762) ---
	RegisterAdminPaddleWebhooksOperations(api, handler)

	// --- Admin Dead Letter Queue Routes (Issue #633) ---
	RegisterAdminDeadLetterOperations(api, handler)

	// --- Orphaned Plaid Items surviving a GDPR erasure (Issue #1497) ---
	RegisterAdminPlaidOrphanedItemOperations(api, handler)

	// Issue #1584: the enumerating read over accounts the #1569 T1 notice gate is holding.
	RegisterAdminRetentionWithheldOperations(api, handler)

	// Issue #1630 Task B7: the enumerating read over budgets the hierarchy audit has flagged.
	RegisterAdminBudgetHierarchyAuditOperations(api, handler)
}
