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

// PlaidHandler handles Plaid-related HTTP requests.
type PlaidHandler struct {
	plaidService   services.PlaidService
	accountService services.AccountService
}

// NewPlaidHandler creates a new PlaidHandler instance.
func NewPlaidHandler(plaidService services.PlaidService, accountService services.AccountService) *PlaidHandler {
	return &PlaidHandler{
		plaidService:   plaidService,
		accountService: accountService,
	}
}

// --- Input/Output Types ---

// CreateLinkTokenInput represents the input for creating a link token.
type CreateLinkTokenInput struct {
	inputs.AuthenticatedInput
}

// CreateLinkTokenOutput represents the response for creating a link token.
type CreateLinkTokenOutput struct {
	Body *domain.LinkTokenResponse
}

// ExchangePublicTokenInput represents the input for exchanging a public token.
type ExchangePublicTokenInput struct {
	inputs.AuthenticatedInput
	Body struct {
		PublicToken     string `json:"public_token" required:"true" doc:"The public token from Plaid Link"`
		InstitutionID   string `json:"institution_id" required:"true" doc:"The institution ID"`
		InstitutionName string `json:"institution_name" required:"true" doc:"The institution name"`
	}
}

// ExchangePublicTokenOutput represents the response for exchanging a public token.
type ExchangePublicTokenOutput struct {
	Body struct {
		Success  bool   `json:"success"`
		ItemID   string `json:"item_id"`
		Accounts int    `json:"accounts_linked"`
	}
}

// GetAccountsInput represents the input for getting accounts.
type GetAccountsInput struct {
	inputs.AuthenticatedInput
	IncludeHidden string `query:"include_hidden" doc:"If 'true', includes hidden accounts"`
}

// GetAccountsOutput represents the response for getting accounts.
type GetAccountsOutput struct {
	Body struct {
		Accounts   []domain.Account `json:"accounts"`
		TotalCount int              `json:"total_count"`
	}
}

// DeleteAccountInput represents the input for deleting an account.
type DeleteAccountInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Account ID"`
}

// DisconnectInstitutionInput represents the input for disconnecting an institution.
type DisconnectInstitutionInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Linked item ID"`
}

// UpdateAccountInput represents the input for updating an account.
type UpdateAccountInput struct {
	inputs.AuthenticatedInput
	ID   string `path:"id" doc:"Account ID"`
	Body domain.UpdateAccountRequest
}

// UpdateAccountOutput represents the response for updating an account.
type UpdateAccountOutput struct {
	Body struct {
		Account domain.Account `json:"account"`
	}
}

// ReorderAccountsInput represents the input for reordering accounts.
type ReorderAccountsInput struct {
	inputs.AuthenticatedInput
	Body domain.ReorderAccountsRequest
}

// ReorderAccountsOutput represents the response for reordering accounts.
type ReorderAccountsOutput struct {
	Body struct {
		Success bool `json:"success"`
	}
}

// --- Handler Methods ---

// CreateLinkToken handles POST /api/v1/plaid/link-token requests.
// Generates a Plaid Link token for initiating bank account connection flow.
func (h *PlaidHandler) CreateLinkToken(ctx context.Context, input *CreateLinkTokenInput) (*CreateLinkTokenOutput, error) {
	response, err := h.plaidService.CreateLinkToken(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to create link token")
	}

	return &CreateLinkTokenOutput{Body: response}, nil
}

// ExchangePublicToken handles POST /api/v1/plaid/exchange-token requests.
// Exchanges a Plaid public token for an access token and links bank accounts.
func (h *PlaidHandler) ExchangePublicToken(ctx context.Context, input *ExchangePublicTokenInput) (*ExchangePublicTokenOutput, error) {
	result, err := h.accountService.LinkAccounts(
		ctx,
		input.UserID,
		input.Body.PublicToken,
		input.Body.InstitutionID,
		input.Body.InstitutionName,
	)

	if err != nil {
		switch err {
		case services.ErrAccountLimitExceeded:
			return nil, huma.Error403Forbidden("Account limit exceeded for your subscription tier")
		case services.ErrInvalidPublicToken:
			return nil, huma.Error400BadRequest("Invalid or expired public token")
		case services.ErrPlaidAPIFailure:
			return nil, huma.Error503ServiceUnavailable("Bank service temporarily unavailable")
		default:
			return nil, humaerrors.MapError(err)
		}
	}

	output := &ExchangePublicTokenOutput{}
	output.Body.Success = true
	output.Body.ItemID = result.LinkedItem.ItemID
	output.Body.Accounts = len(result.Accounts)

	return output, nil
}

// GetAccounts handles GET /api/v1/accounts requests.
// Retrieves all linked bank accounts for the authenticated user.
func (h *PlaidHandler) GetAccounts(ctx context.Context, input *GetAccountsInput) (*GetAccountsOutput, error) {
	includeHidden := input.IncludeHidden == "true"

	accounts, err := h.accountService.GetAccountsFiltered(ctx, input.UserID, includeHidden)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	output := &GetAccountsOutput{}
	output.Body.Accounts = accounts
	output.Body.TotalCount = len(accounts)

	return output, nil
}

// DeleteAccount handles DELETE /api/v1/accounts/:id requests.
// Deletes a single account with ownership validation.
func (h *PlaidHandler) DeleteAccount(ctx context.Context, input *DeleteAccountInput) (*struct{}, error) {
	err := h.accountService.DeleteAccount(ctx, input.ID, input.UserID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return nil, nil
}

// DisconnectInstitution handles DELETE /api/v1/linked-items/:id requests.
// Disconnects an institution and all associated accounts.
func (h *PlaidHandler) DisconnectInstitution(ctx context.Context, input *DisconnectInstitutionInput) (*struct{}, error) {
	err := h.accountService.DisconnectInstitution(ctx, input.ID, input.UserID)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	return nil, nil
}

// UpdateAccount handles PATCH /api/v1/accounts/:id requests.
// Updates account customization fields.
func (h *PlaidHandler) UpdateAccount(ctx context.Context, input *UpdateAccountInput) (*UpdateAccountOutput, error) {
	account, err := h.accountService.UpdateAccount(ctx, input.ID, input.UserID, &input.Body)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	output := &UpdateAccountOutput{}
	output.Body.Account = *account

	return output, nil
}

// ReorderAccounts handles PATCH /api/v1/accounts/reorder requests.
// Bulk updates display_order for multiple accounts.
func (h *PlaidHandler) ReorderAccounts(ctx context.Context, input *ReorderAccountsInput) (*ReorderAccountsOutput, error) {
	err := h.accountService.ReorderAccounts(ctx, input.UserID, &input.Body)
	if err != nil {
		return nil, humaerrors.MapError(err)
	}

	output := &ReorderAccountsOutput{}
	output.Body.Success = true

	return output, nil
}

// RegisterPlaidRoutes registers Plaid routes with the Huma API.
// Note: These routes require the Auth0 middleware to be applied.
func RegisterPlaidRoutes(api huma.API, handler *PlaidHandler) {
	// POST /api/v1/plaid/link-token - Create link token
	huma.Register(api, huma.Operation{
		OperationID: "createLinkToken",
		Method:      http.MethodPost,
		Path:        "/api/v1/plaid/link-token",
		Summary:     "Create Plaid link token",
		Description: "Generates a Plaid Link token for initiating bank account connection flow.",
		Tags:        []string{"Plaid"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.CreateLinkToken)

	// POST /api/v1/plaid/exchange-token - Exchange public token
	huma.Register(api, huma.Operation{
		OperationID: "exchangePublicToken",
		Method:      http.MethodPost,
		Path:        "/api/v1/plaid/exchange-token",
		Summary:     "Exchange public token",
		Description: "Exchanges a Plaid public token for an access token and links bank accounts.",
		Tags:        []string{"Plaid"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.ExchangePublicToken)

	// GET /api/v1/accounts - Get accounts
	huma.Register(api, huma.Operation{
		OperationID: "getAccounts",
		Method:      http.MethodGet,
		Path:        "/api/v1/accounts",
		Summary:     "Get accounts",
		Description: "Retrieves all linked bank accounts for the authenticated user.",
		Tags:        []string{"Accounts"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.GetAccounts)

	// PATCH /api/v1/accounts/reorder - Reorder accounts (must be before :id route)
	huma.Register(api, huma.Operation{
		OperationID: "reorderAccounts",
		Method:      http.MethodPatch,
		Path:        "/api/v1/accounts/reorder",
		Summary:     "Reorder accounts",
		Description: "Bulk updates display_order for multiple accounts.",
		Tags:        []string{"Accounts"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.ReorderAccounts)

	// PATCH /api/v1/accounts/:id - Update account
	huma.Register(api, huma.Operation{
		OperationID: "updateAccount",
		Method:      http.MethodPatch,
		Path:        "/api/v1/accounts/{id}",
		Summary:     "Update account",
		Description: "Updates account customization fields.",
		Tags:        []string{"Accounts"},
		Security:    []map[string][]string{{"auth0": {}}},
	}, handler.UpdateAccount)

	// DELETE /api/v1/accounts/:id - Delete account
	huma.Register(api, huma.Operation{
		OperationID:   "deleteAccount",
		Method:        http.MethodDelete,
		Path:          "/api/v1/accounts/{id}",
		Summary:       "Delete account",
		Description:   "Deletes a single account with ownership validation.",
		Tags:          []string{"Accounts"},
		Security:      []map[string][]string{{"auth0": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DeleteAccount)

	// DELETE /api/v1/linked-items/:id - Disconnect institution
	huma.Register(api, huma.Operation{
		OperationID:   "disconnectInstitution",
		Method:        http.MethodDelete,
		Path:          "/api/v1/linked-items/{id}",
		Summary:       "Disconnect institution",
		Description:   "Disconnects an institution and all associated accounts.",
		Tags:          []string{"Accounts"},
		Security:      []map[string][]string{{"auth0": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DisconnectInstitution)
}
