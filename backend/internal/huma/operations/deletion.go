package operations

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// DeletionHandler handles account deletion HTTP requests.
type DeletionHandler struct {
	service services.DeletionService
}

// NewDeletionHandler creates a new DeletionHandler instance.
func NewDeletionHandler(service services.DeletionService) *DeletionHandler {
	return &DeletionHandler{service: service}
}

// --- Input/Output Types ---

// RequestDeletionInput represents the input for requesting account deletion.
type RequestDeletionInput struct {
	inputs.AuthenticatedWithEmailInput
}

// RequestDeletionOutput represents the response for deletion requests.
type RequestDeletionOutput struct {
	Body RequestDeletionResponseBody
}

// RequestDeletionResponseBody is the response body for deletion requests.
type RequestDeletionResponseBody struct {
	Message           string `json:"message" doc:"Status message"`
	ScheduledAt       string `json:"scheduled_at" doc:"When deletion is scheduled (ISO 8601)"`
	CancellationToken string `json:"cancellation_token" doc:"Token to cancel deletion"`
}

// CancelDeletionInput represents the input for cancelling account deletion.
type CancelDeletionInput struct {
	Token string `path:"token" doc:"Cancellation token from email"`
}

// CancelDeletionOutput represents the response for cancellation requests.
type CancelDeletionOutput struct {
	Body CancelDeletionResponseBody
}

// CancelDeletionResponseBody is the response body for cancellation requests.
type CancelDeletionResponseBody struct {
	Message string `json:"message" doc:"Status message"`
}

// --- Handler Methods ---

// RequestDeletion handles POST /api/v1/user/delete requests.
// Initiates account deletion with 30-day grace period.
func (h *DeletionHandler) RequestDeletion(ctx context.Context, input *RequestDeletionInput) (*RequestDeletionOutput, error) {
	cancellationToken, scheduledAt, err := h.service.RequestDeletion(ctx, input.UserID, input.Email)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to request account deletion")
	}

	return &RequestDeletionOutput{
		Body: RequestDeletionResponseBody{
			Message:           "Account deletion scheduled",
			ScheduledAt:       scheduledAt.Format("2006-01-02T15:04:05Z07:00"),
			CancellationToken: cancellationToken,
		},
	}, nil
}

// CancelDeletion handles GET /api/v1/user/delete/cancel/{token} requests.
// Cancels a pending deletion using the cancellation token.
// Note: This is a public endpoint - the token validates the request.
func (h *DeletionHandler) CancelDeletion(ctx context.Context, input *CancelDeletionInput) (*CancelDeletionOutput, error) {
	if input.Token == "" {
		return nil, huma.Error400BadRequest("Cancellation token required")
	}

	err := h.service.CancelDeletion(ctx, input.Token)
	if err != nil {
		return nil, huma.Error400BadRequest("Invalid or expired cancellation token")
	}

	return &CancelDeletionOutput{
		Body: CancelDeletionResponseBody{
			Message: "Account deletion cancelled",
		},
	}, nil
}

// RegisterDeletionRoutes registers deletion routes with the Huma API.
func RegisterDeletionRoutes(api huma.API, handler *DeletionHandler) {
	// POST /api/v1/user/delete - Request account deletion (requires auth)
	huma.Register(api, huma.Operation{
		OperationID: "requestDeletion",
		Method:      http.MethodPost,
		Path:        "/api/v1/user/delete",
		Summary:     "Request account deletion",
		Description: "Initiates account deletion with 30-day grace period. User will receive email with cancellation link.",
		Tags:        []string{"User"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.RequestDeletion)

	// GET /api/v1/user/delete/cancel/{token} - Cancel deletion (public, token-based)
	huma.Register(api, huma.Operation{
		OperationID: "cancelDeletion",
		Method:      http.MethodGet,
		Path:        "/api/v1/user/delete/cancel/{token}",
		Summary:     "Cancel account deletion",
		Description: "Cancels a pending account deletion using the cancellation token from the email.",
		Tags:        []string{"User"},
		// No Security - this is a public endpoint validated by the token
	}, handler.CancelDeletion)
}
