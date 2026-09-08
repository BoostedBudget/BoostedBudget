// Package operations provides Huma API operation handlers.
// This file implements the reconnect flow for a broken Plaid connection (Issue #1609).
//
// Historically, ItemErrorService's Handle* methods minted an update-mode Plaid Link token and
// embedded it directly in a notification's action_url (e.g.
// "/accounts/reconnect?link_token=...&item_id=..."). That route never existed on the frontend, so
// clicking "Reconnect Bank" always led to a broken page. Fixing the route alone was not enough:
// the embedded token could be hours stale by the time a user acted on the notification (Plaid
// Link tokens expire in ~4h; the notification row lives forever), so this handler mints a FRESH
// token on demand instead, and RepairItem clears the item's error state once Plaid Link succeeds.
//
// PlaidLinkButton's normal token+exchange flow is NOT reused here (and must never be): exchanging
// a public token calls AccountService.LinkAccounts, which creates a brand-new linked_items row.
// For a user re-authenticating an EXISTING broken item, that would leave the broken item in place
// AND create a duplicate — worse than the original bug. This handler's flow never calls
// ExchangePublicToken.
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

// PlaidReconnectHandler handles the reconnect flow for a broken Plaid linked item.
// Deliberately a separate handler/file from PlaidHandler: it depends on ItemErrorService, not
// AccountService, and keeping it separate avoids touching PlaidHandler's ~40 existing test
// call-sites for an unrelated dependency.
type PlaidReconnectHandler struct {
	itemErrorService services.ItemErrorService
}

// NewPlaidReconnectHandler creates a new PlaidReconnectHandler instance.
func NewPlaidReconnectHandler(itemErrorService services.ItemErrorService) *PlaidReconnectHandler {
	return &PlaidReconnectHandler{itemErrorService: itemErrorService}
}

// --- Input/Output Types ---

// CreateReconnectLinkTokenInput represents the input for minting an update-mode Plaid Link token
// for an existing broken linked item.
type CreateReconnectLinkTokenInput struct {
	inputs.AuthenticatedInput
	Body struct {
		ItemID string `json:"item_id" required:"true" doc:"The Plaid item_id of the linked item to reconnect (from the notification's action_url)"`
	}
}

// CreateReconnectLinkTokenOutput represents the response for minting a reconnect link token.
type CreateReconnectLinkTokenOutput struct {
	Body *domain.LinkTokenResponse
}

// RepairLinkedItemInput represents the input for clearing a linked item's error state after a
// successful update-mode Plaid Link flow.
type RepairLinkedItemInput struct {
	inputs.AuthenticatedInput
	Body struct {
		ItemID string `json:"item_id" required:"true" doc:"The Plaid item_id of the linked item that was just repaired via Plaid Link"`
	}
}

// RepairLinkedItemOutput represents the response for repairing a linked item.
type RepairLinkedItemOutput struct {
	Body struct {
		Success bool `json:"success"`
	}
}

// --- Handler Methods ---

// CreateReconnectLinkToken handles POST /api/v1/plaid/reconnect-link-token requests.
// Mints a fresh update-mode Plaid Link token for a user reconnecting a broken bank connection.
func (h *PlaidReconnectHandler) CreateReconnectLinkToken(ctx context.Context, input *CreateReconnectLinkTokenInput) (*CreateReconnectLinkTokenOutput, error) {
	response, err := h.itemErrorService.CreateReconnectLinkToken(ctx, input.UserID, input.Body.ItemID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	return &CreateReconnectLinkTokenOutput{Body: response}, nil
}

// RepairLinkedItem handles POST /api/v1/plaid/reconnect requests.
// Clears a linked item's error state and marks it active again after the user has successfully
// completed the update-mode Plaid Link flow.
func (h *PlaidReconnectHandler) RepairLinkedItem(ctx context.Context, input *RepairLinkedItemInput) (*RepairLinkedItemOutput, error) {
	if err := h.itemErrorService.RepairItem(ctx, input.UserID, input.Body.ItemID); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, err)
	}

	output := &RepairLinkedItemOutput{}
	output.Body.Success = true
	return output, nil
}

// RegisterPlaidReconnectRoutes registers the reconnect-flow routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterPlaidReconnectRoutes(api huma.API, handler *PlaidReconnectHandler) {
	// POST /api/v1/plaid/reconnect-link-token - Mint an update-mode link token for a broken item
	huma.Register(api, huma.Operation{
		OperationID: "createReconnectLinkToken",
		Method:      http.MethodPost,
		Path:        "/api/v1/plaid/reconnect-link-token",
		Summary:     "Create a Plaid update-mode link token to repair a broken connection",
		Description: "Mints a fresh update-mode Plaid Link token for the caller's own linked item, scoped to items in a repairable error state (login_required, error, pending_expiration). A 'revoked' item is deliberately excluded — Plaid documents revoked access as unrecoverable via update mode; see ErrLinkedItemNotRepairable in item_error_service.go.",
		Tags:        []string{"Plaid"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.CreateReconnectLinkToken)

	// POST /api/v1/plaid/reconnect - Clear a linked item's error state after Link succeeds
	huma.Register(api, huma.Operation{
		OperationID: "repairLinkedItem",
		Method:      http.MethodPost,
		Path:        "/api/v1/plaid/reconnect",
		Summary:     "Clear a linked item's error state after a successful reconnect",
		Description: "Marks the caller's own linked item active again and clears its error tracking fields. Call only after the update-mode Plaid Link flow (using the token from createReconnectLinkToken) has succeeded.",
		Tags:        []string{"Plaid"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.RepairLinkedItem)
}
