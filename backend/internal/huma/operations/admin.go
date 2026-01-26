package operations

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/auth"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// TrialGranter defines the interface for granting trials to users.
// Issue #698: Used by admin endpoint to grant trial during E2E tests.
type TrialGranter interface {
	GrantTrial(ctx context.Context, userID string) error
}

// AdminHandler handles admin-only endpoints for testing and troubleshooting.
// Routes are protected by AdminMiddleware with role validation and environment checks.
type AdminHandler struct {
	plaidService      services.PlaidService
	linkedItemRepo    repository.LinkedItemRepository
	encryptionService services.EncryptionService
	trialService      TrialGranter
	adminService      services.AdminService
	adminUserService  services.AdminUserService
}

// NewAdminHandler creates a new AdminHandler with required dependencies.
// The trialService, adminService, and adminUserService parameters can be nil if not needed.
func NewAdminHandler(
	plaidService services.PlaidService,
	linkedItemRepo repository.LinkedItemRepository,
	encryptionService services.EncryptionService,
	trialService TrialGranter,
	adminService services.AdminService,
	adminUserService services.AdminUserService,
) *AdminHandler {
	return &AdminHandler{
		plaidService:      plaidService,
		linkedItemRepo:    linkedItemRepo,
		encryptionService: encryptionService,
		trialService:      trialService,
		adminService:      adminService,
		adminUserService:  adminUserService,
	}
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
			log.Printf("[CheckRoleInput] Failed to fetch email from Clerk API: %v", fetchErr)
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
	Search string `query:"search" doc:"Search by email (partial match)"`
	Tier   string `query:"tier" doc:"Filter by subscription tier"`
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

// --- Handler Methods ---

// FireWebhook fires a Plaid webhook for testing in QA environment.
//
// POST /api/v1/admin/plaid/fire-webhook
//
// Error Responses:
//   - 400: Invalid request body or validation failure
//   - 401: Not authenticated
//   - 403: Not an admin (checked by AdminMiddleware)
//   - 404: Linked item not found
//   - 500: Decryption failure or Plaid API error
func (h *AdminHandler) FireWebhook(ctx context.Context, input *FireWebhookInput) (*FireWebhookOutput, error) {
	// 1. Fetch linked item by Plaid item ID (not database primary key)
	// Issue #331: item_id is the Plaid identifier, not the database primary key
	linkedItem, err := h.linkedItemRepo.GetByItemID(ctx, input.Body.ItemID)
	if err != nil {
		if err == repository.ErrLinkedItemNotFound {
			return nil, huma.Error404NotFound("Item not found")
		}
		return nil, huma.Error500InternalServerError("Failed to fetch linked item")
	}

	// 2. Decrypt access token
	accessToken, err := h.encryptionService.Decrypt(linkedItem.AccessTokenEncrypted)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to decrypt access token")
	}

	// 3. Fire webhook via Plaid service
	response, err := h.plaidService.FireWebhook(ctx, accessToken, input.Body.WebhookCode)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to fire webhook")
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
		return nil, huma.Error500InternalServerError("Failed to grant trial: " + err.Error())
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
		return nil, huma.Error500InternalServerError("Failed to check admin role")
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
	// Check if admin user service is configured
	if h.adminUserService == nil {
		return nil, huma.Error503ServiceUnavailable("Admin user service not configured")
	}

	// Build filters from input
	filters := domain.AdminUserFilters{
		Search: input.Search,
		Tier:   input.Tier,
	}

	// Get users from service
	response, err := h.adminUserService.ListUsers(ctx, filters, input.Page, input.PageSize)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list users: " + err.Error())
	}

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
	// Check if admin user service is configured
	if h.adminUserService == nil {
		return nil, huma.Error503ServiceUnavailable("Admin user service not configured")
	}

	// Get user detail from service
	detail, err := h.adminUserService.GetUserDetail(ctx, input.UserID)
	if err != nil {
		if err == services.ErrAdminUserNotFound {
			return nil, huma.Error404NotFound("User not found")
		}
		return nil, huma.Error500InternalServerError("Failed to get user detail: " + err.Error())
	}

	return &GetUserDetailOutput{Body: detail}, nil
}

// RegisterAdminRoutes registers admin routes with the Huma API.
// Routes are protected by AdminMiddleware (environment and role validation).
func RegisterAdminRoutes(api huma.API, handler *AdminHandler) {
	// POST /api/v1/admin/plaid/fire-webhook - Fire a Plaid webhook (QA only)
	huma.Register(api, huma.Operation{
		OperationID: "fireWebhook",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/plaid/fire-webhook",
		Summary:     "Fire a Plaid webhook (QA only)",
		Description: "Fires a Plaid webhook for testing in QA environment. Requires Clerk authentication.",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.FireWebhook)

	// POST /api/v1/admin/subscription/grant-trial - Grant trial to current user (QA only)
	// Issue #698: Used by E2E tests to ensure test user has active subscription.
	huma.Register(api, huma.Operation{
		OperationID: "grantTrial",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/subscription/grant-trial",
		Summary:     "Grant trial subscription (QA only)",
		Description: "Grants a 60-day trial to the authenticated user. For E2E testing. Returns success if user already has trial.",
		Tags:        []string{"Admin"},
		Security:    []map[string][]string{{"clerk": {}}},
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
}
