package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// UserAccountHandler handles user account-related HTTP requests.
// Issue #171: Provides stable account references that survive Plaid re-links.
type UserAccountHandler struct {
	service services.UserAccountService
}

// NewUserAccountHandler creates a new UserAccountHandler instance.
func NewUserAccountHandler(service services.UserAccountService) *UserAccountHandler {
	return &UserAccountHandler{service: service}
}

// --- Input/Output Types ---

// GetUserAccountsInput represents the input for listing user accounts.
type GetUserAccountsInput struct {
	inputs.AuthenticatedInput
	IncludeHidden bool `query:"include_hidden" doc:"Include hidden accounts (default false)"`
}

// GetUserAccountsOutput represents the response for listing user accounts.
type GetUserAccountsOutput struct {
	Body []domain.UserAccount
}

// GetUserAccountByIDInput represents the input for getting a user account by ID.
type GetUserAccountByIDInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"User Account UUID"`
}

// GetUserAccountByIDOutput represents the response for getting a user account.
type GetUserAccountByIDOutput struct {
	Body *domain.UserAccount
}

// UpdateUserAccountInput represents the input for updating user account customization.
type UpdateUserAccountInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"User Account UUID"`
	Body domain.UpdateUserAccountRequest
}

// UpdateUserAccountOutput represents the response for updating a user account.
type UpdateUserAccountOutput struct {
	Body *domain.UserAccount
}

// --- Handler Methods ---

// GetUserAccounts handles GET /api/v1/user-accounts requests.
// Returns all stable user accounts for the authenticated user.
// These survive Plaid re-links and provide a consistent reference for transactions.
func (h *UserAccountHandler) GetUserAccounts(ctx context.Context, input *GetUserAccountsInput) (*GetUserAccountsOutput, error) {
	accounts, err := h.service.GetByUserID(ctx, input.UserID, input.IncludeHidden)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	// Return empty array instead of null if no accounts
	if accounts == nil {
		accounts = []domain.UserAccount{}
	}

	return &GetUserAccountsOutput{Body: accounts}, nil
}

// GetUserAccountByID handles GET /api/v1/user-accounts/:id requests.
// Retrieves a specific user account by ID with ownership validation.
func (h *UserAccountHandler) GetUserAccountByID(ctx context.Context, input *GetUserAccountByIDInput) (*GetUserAccountByIDOutput, error) {
	account, err := h.service.GetByID(ctx, input.ID, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &GetUserAccountByIDOutput{Body: account}, nil
}

// UpdateUserAccount handles PATCH /api/v1/user-accounts/:id requests.
// Updates customization fields (nickname, color, etc.) for a user account.
func (h *UserAccountHandler) UpdateUserAccount(ctx context.Context, input *UpdateUserAccountInput) (*UpdateUserAccountOutput, error) {
	account, err := h.service.UpdateCustomization(ctx, input.ID, input.UserID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &UpdateUserAccountOutput{Body: account}, nil
}

// RegisterUserAccountRoutes registers user account routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
// Issue #171: These endpoints provide access to stable account references.
func RegisterUserAccountRoutes(api huma.API, handler *UserAccountHandler) {
	// GET /api/v1/user-accounts - List all user accounts
	huma.Register(api, huma.Operation{
		OperationID: "getUserAccounts",
		Method:      http.MethodGet,
		Path:        "/api/v1/user-accounts",
		Summary:     "List user accounts",
		Description: "Returns all stable user accounts for the authenticated user. These survive Plaid re-links and provide consistent references for querying transactions.",
		Tags:        []string{"User Accounts"},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, handler.GetUserAccounts)

	// GET /api/v1/user-accounts/:id - Get user account by ID
	huma.Register(api, huma.Operation{
		OperationID: "getUserAccountById",
		Method:      http.MethodGet,
		Path:        "/api/v1/user-accounts/{id}",
		Summary:     "Get user account by ID",
		Description: "Retrieves a specific user account by ID. Returns 404 if not found or not owned by the user.",
		Tags:        []string{"User Accounts"},
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, handler.GetUserAccountByID)

	// PATCH /api/v1/user-accounts/:id - Update user account customization
	huma.Register(api, huma.Operation{
		OperationID:   "updateUserAccount",
		Method:        http.MethodPatch,
		Path:          "/api/v1/user-accounts/{id}",
		Summary:       "Update user account",
		Description:   "Updates customization fields (nickname, color, icon, emoji, display order, hidden status) for a user account. Only provided fields are updated.",
		Tags:          []string{"User Accounts"},
		DefaultStatus: http.StatusOK,
		Security: []map[string][]string{
			{"bearerAuth": {}},
		},
	}, handler.UpdateUserAccount)
}
