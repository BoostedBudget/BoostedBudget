package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/dateutil"
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
		ItemID   string `json:"item_id" doc:"The Plaid provider's item id — NOT the same as any account's linked_item_id"`
		// LinkedItemID is the internal linked_items.id (Issue #1370 Task 9),
		// distinct from ItemID (the Plaid provider's own item id). It's the
		// value that matches domain.Account.LinkedItemID (set from
		// linkedItem.ID in account_service.go's LinkAccounts), so callers can
		// find just-linked accounts by filtering GetAccounts results on
		// `linked_item_id == linked_item_id` from this response.
		LinkedItemID string `json:"linked_item_id" doc:"The internal linked item id — matches an account's linked_item_id field"`
		Accounts     int    `json:"accounts_linked"`
	}
}

// GetAccountsInput represents the input for getting accounts.
type GetAccountsInput struct {
	inputs.AuthenticatedInput
	IncludeHidden   string `query:"include_hidden" doc:"If 'true', includes hidden accounts"`
	Status          string `query:"status" doc:"Filter by account status: 'active' (default), 'replaced', 'all' (active+replaced — a misnomer inherited from history, see AccountFilterOptions.StatusFilter), or 'all_statuses' (every status: active, replaced, expired, removed — for surfaces like the closed-accounts view that need the full universe)"`
	IncludeReplaced string `query:"include_replaced" doc:"If 'true', equivalent to status=all (backward-compat alias)"`
	UserAccountID   string `query:"user_account_id" doc:"When supplied, returns all accounts in the chain (active and replaced) for the given user_account_id. Status filter defaults to 'all' when this param is set."`
	// AsOfDate resolves the account universe as it REALLY WAS on that day, from
	// account_status_history, instead of guessing from current status. Mutually exclusive with
	// status=all, status=replaced, and include_replaced=true — combining them returns a 400
	// rather than reaching repository.ErrAccountFilterAsOfConflict. Issue #1569.
	AsOfDate string `query:"as_of_date" doc:"Get accounts as of a specific date (YYYY-MM-DD) — which accounts were live that day. Cannot be combined with status=all, status=replaced, or include_replaced=true."`
}

// GetAccountsOutput represents the response for getting accounts.
type GetAccountsOutput struct {
	Body struct {
		Accounts   []domain.Account `json:"accounts"`
		TotalCount int              `json:"total_count"`
	}
}

// DisconnectAccountInput represents the input for disconnecting a single account — stopping it
// from syncing while retaining all of its history. Issue #1569, Task 6.
type DisconnectAccountInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Account ID"`
}

// PurgeAccountInput represents the input for permanently destroying an account and everything
// derived from it. Issue #1569, Task 6: this is the renamed, genuinely-destructive DELETE that
// DeleteAccountInput used to back before Task 2 made that route non-destructive.
type PurgeAccountInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Account ID"`
}

// DisconnectInstitutionInput represents the input for disconnecting an institution — stopping
// Plaid sync/billing while retaining all of its accounts' history. Issue #1569, Task 6: this now
// backs POST /linked-items/{id}/disconnect rather than DELETE.
type DisconnectInstitutionInput struct {
	inputs.AuthenticatedInput
	ID string `path:"id" doc:"Linked item ID"`
}

// PurgeInstitutionInput represents the input for permanently destroying a linked item and every
// account under it. Issue #1569, Task 6.
type PurgeInstitutionInput struct {
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
		case services.ErrInvalidPublicToken:
			return nil, huma.Error400BadRequest("Invalid or expired public token")
		case services.ErrPlaidAPIFailure:
			return nil, huma.Error503ServiceUnavailable("Bank service temporarily unavailable")
		default:
			return nil, humaerrors.LoggingMapError(ctx, err)
		}
	}

	output := &ExchangePublicTokenOutput{}
	output.Body.Success = true
	output.Body.ItemID = result.LinkedItem.ItemID
	output.Body.LinkedItemID = result.LinkedItem.ID
	output.Body.Accounts = len(result.Accounts)

	return output, nil
}

// GetAccounts handles GET /api/v1/accounts requests.
// Retrieves all linked bank accounts for the authenticated user.
func (h *PlaidHandler) GetAccounts(ctx context.Context, input *GetAccountsInput) (*GetAccountsOutput, error) {
	// Issue #1109: C.2 — When user_account_id is supplied, default status filter to "all"
	statusFilter := input.Status
	if input.UserAccountID != "" && input.Status == "" {
		statusFilter = "all"
	}
	includeReplaced := input.IncludeReplaced == "true"

	// Issue #1569: as_of_date and a "widen/narrow current status" filter ask contradictory
	// questions (see repository.ErrAccountFilterAsOfConflict / resolveAccountStatusScope). The
	// API layer rejects the combination itself rather than letting the resolver's error leak
	// through as an opaque 500.
	asOf, err := dateutil.ParseOptionalDate(input.AsOfDate, "as_of_date")
	if err != nil {
		return nil, err
	}
	if asOf != nil && (statusFilter == "all" || statusFilter == "replaced" || statusFilter == "all_statuses" || includeReplaced) {
		return nil, huma.Error400BadRequest("as_of_date cannot be combined with status=all, status=replaced, status=all_statuses, or include_replaced=true")
	}

	opts := services.AccountFilterOptions{
		IncludeHidden:   input.IncludeHidden == "true",
		StatusFilter:    statusFilter, // "active" (default), "replaced", "all"
		IncludeReplaced: includeReplaced,
		UserAccountID:   input.UserAccountID,
		AsOf:            asOf,
	}

	accounts, err := h.accountService.GetAccountsFiltered(ctx, input.UserID, opts)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	output := &GetAccountsOutput{}
	output.Body.Accounts = accounts
	output.Body.TotalCount = len(accounts)

	return output, nil
}

// DisconnectAccount handles POST /api/v1/accounts/:id/disconnect requests.
// Stops a single account from syncing while retaining all of its history. Issue #1569, Task 6.
func (h *PlaidHandler) DisconnectAccount(ctx context.Context, input *DisconnectAccountInput) (*struct{}, error) {
	err := h.accountService.DisconnectAccount(ctx, input.ID, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return nil, nil
}

// PurgeAccount handles DELETE /api/v1/accounts/:id requests.
// Permanently destroys a single account and everything derived from it, with ownership
// validation. Issue #1569, Task 6: renamed from DeleteAccount, which this route backed before
// Task 2 made it non-destructive; the non-destructive verb now lives at DisconnectAccount.
func (h *PlaidHandler) PurgeAccount(ctx context.Context, input *PurgeAccountInput) (*struct{}, error) {
	err := h.accountService.PurgeAccount(ctx, input.ID, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return nil, nil
}

// DisconnectInstitution handles POST /api/v1/linked-items/:id/disconnect requests.
// Disconnects an institution (stops Plaid sync/billing) while retaining all of its accounts'
// history. Issue #1569, Task 6: moved from DELETE to POST; the DELETE route is now the
// genuinely-destructive PurgeInstitution below.
func (h *PlaidHandler) DisconnectInstitution(ctx context.Context, input *DisconnectInstitutionInput) (*struct{}, error) {
	err := h.accountService.DisconnectInstitution(ctx, input.ID, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return nil, nil
}

// PurgeInstitution handles DELETE /api/v1/linked-items/:id requests.
// Permanently destroys a linked item and every account under it, with ownership validation.
// Issue #1569, Task 6.
func (h *PlaidHandler) PurgeInstitution(ctx context.Context, input *PurgeInstitutionInput) (*struct{}, error) {
	err := h.accountService.PurgeInstitution(ctx, input.ID, input.UserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return nil, nil
}

// UpdateAccount handles PATCH /api/v1/accounts/:id requests.
// Updates account customization fields.
func (h *PlaidHandler) UpdateAccount(ctx context.Context, input *UpdateAccountInput) (*UpdateAccountOutput, error) {
	account, err := h.accountService.UpdateAccount(ctx, input.ID, input.UserID, &input.Body)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
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
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	output := &ReorderAccountsOutput{}
	output.Body.Success = true

	return output, nil
}

// RegisterPlaidRoutes registers Plaid routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterPlaidRoutes(api huma.API, handler *PlaidHandler) {
	// POST /api/v1/plaid/link-token - Create link token
	huma.Register(api, huma.Operation{
		OperationID: "createLinkToken",
		Method:      http.MethodPost,
		Path:        "/api/v1/plaid/link-token",
		Summary:     "Create Plaid link token",
		Description: "Generates a Plaid Link token for initiating bank account connection flow.",
		Tags:        []string{"Plaid"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.CreateLinkToken)

	// POST /api/v1/plaid/exchange-token - Exchange public token
	huma.Register(api, huma.Operation{
		OperationID: "exchangePublicToken",
		Method:      http.MethodPost,
		Path:        "/api/v1/plaid/exchange-token",
		Summary:     "Exchange public token",
		Description: "Exchanges a Plaid public token for an access token and links bank accounts.",
		Tags:        []string{"Plaid"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.ExchangePublicToken)

	// GET /api/v1/accounts - Get accounts
	huma.Register(api, huma.Operation{
		OperationID: "getAccounts",
		Method:      http.MethodGet,
		Path:        "/api/v1/accounts",
		Summary:     "Get accounts",
		Description: "Retrieves all linked bank accounts for the authenticated user.",
		Tags:        []string{"Accounts"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.GetAccounts)

	// PATCH /api/v1/accounts/reorder - Reorder accounts (must be before :id route)
	huma.Register(api, huma.Operation{
		OperationID: "reorderAccounts",
		Method:      http.MethodPatch,
		Path:        "/api/v1/accounts/reorder",
		Summary:     "Reorder accounts",
		Description: "Bulk updates display_order for multiple accounts.",
		Tags:        []string{"Accounts"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.ReorderAccounts)

	// PATCH /api/v1/accounts/:id - Update account
	huma.Register(api, huma.Operation{
		OperationID: "updateAccount",
		Method:      http.MethodPatch,
		Path:        "/api/v1/accounts/{id}",
		Summary:     "Update account",
		Description: "Updates account customization fields.",
		Tags:        []string{"Accounts"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdateAccount)

	// POST /api/v1/accounts/:id/disconnect - Disconnect account (Issue #1569, Task 6)
	huma.Register(api, huma.Operation{
		OperationID:   "disconnectAccount",
		Method:        http.MethodPost,
		Path:          "/api/v1/accounts/{id}/disconnect",
		Summary:       "Disconnect account",
		Description:   "Stops a single account from syncing and retains its financial history. Does not destroy any data — use DELETE to permanently purge an account and its history.",
		Tags:          []string{"Accounts"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DisconnectAccount)

	// DELETE /api/v1/accounts/:id - Purge account (Issue #1569, Task 6: renamed from deleteAccount)
	huma.Register(api, huma.Operation{
		OperationID:   "purgeAccount",
		Method:        http.MethodDelete,
		Path:          "/api/v1/accounts/{id}",
		Summary:       "Purge account",
		Description:   "Permanently destroys a single account and everything derived from it — balance history, holdings, liabilities, AND investment transactions (buys, sells, dividends) for an investment account — with ownership validation. This cannot be undone. Bank transactions (from a checking/savings/credit account) are NOT destroyed; investment transactions ARE, since they have no independent existence apart from the account. Refuses if a savings goal still references this account, and refuses unless the account was already disconnected (POST .../disconnect first) — a live account cannot be purged, since the next Plaid sync would recreate it.",
		Tags:          []string{"Accounts"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.PurgeAccount)

	// POST /api/v1/linked-items/:id/disconnect - Disconnect institution (Issue #1569, Task 6)
	huma.Register(api, huma.Operation{
		OperationID:   "disconnectInstitution",
		Method:        http.MethodPost,
		Path:          "/api/v1/linked-items/{id}/disconnect",
		Summary:       "Disconnect institution",
		Description:   "Disconnects an institution (stops Plaid sync/billing) and retains all of its accounts' financial history. Does not destroy any data — use DELETE to permanently purge the institution and its accounts.",
		Tags:          []string{"Accounts"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.DisconnectInstitution)

	// DELETE /api/v1/linked-items/:id - Purge institution (Issue #1569, Task 6: renamed from disconnectInstitution)
	huma.Register(api, huma.Operation{
		OperationID:   "purgeInstitution",
		Method:        http.MethodDelete,
		Path:          "/api/v1/linked-items/{id}",
		Summary:       "Purge institution",
		Description:   "Permanently destroys a linked item and every account under it — balance history, holdings, liabilities, AND investment transactions (buys, sells, dividends) for any investment account — with ownership validation. This cannot be undone. Bank transactions are NOT destroyed; investment transactions ARE, since they have no independent existence apart from the account. Refuses if a savings goal still references one of its accounts, and refuses unless the institution was already disconnected. Use POST .../disconnect first.",
		Tags:          []string{"Accounts"},
		Security:      []map[string][]string{{"clerk": {}}},
		DefaultStatus: http.StatusNoContent,
	}, handler.PurgeInstitution)
}
