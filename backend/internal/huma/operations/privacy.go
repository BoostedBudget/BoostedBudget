package operations

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// PrivacyHandler handles privacy settings-related HTTP requests.
type PrivacyHandler struct {
	service services.PrivacyService
}

// NewPrivacyHandler creates a new PrivacyHandler instance.
func NewPrivacyHandler(service services.PrivacyService) *PrivacyHandler {
	return &PrivacyHandler{service: service}
}

// --- Input/Output Types ---

// GetPrivacySettingsInput represents the input for getting privacy settings.
type GetPrivacySettingsInput struct {
	inputs.AuthenticatedInput
}

// GetPrivacySettingsOutput represents the response for getting privacy settings.
type GetPrivacySettingsOutput struct {
	Body *domain.PrivacySettings
}

// UpdatePrivacySettingsInput represents the input for updating privacy settings.
type UpdatePrivacySettingsInput struct {
	inputs.AuthenticatedInput
	Body domain.UpdatePrivacySettingsRequest
}

// UpdatePrivacySettingsOutput represents the response for updating privacy settings.
type UpdatePrivacySettingsOutput struct {
	Body *domain.PrivacySettings
}

// --- Handler Methods ---

// GetPrivacySettings handles GET /api/v1/user/privacy requests.
// Retrieves the authenticated user's privacy settings, creating defaults if none exist.
func (h *PrivacyHandler) GetPrivacySettings(ctx context.Context, input *GetPrivacySettingsInput) (*GetPrivacySettingsOutput, error) {
	settings, err := h.service.GetPrivacySettings(ctx, input.UserID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to retrieve privacy settings")
	}

	return &GetPrivacySettingsOutput{Body: settings}, nil
}

// UpdatePrivacySettings handles PUT /api/v1/user/privacy requests.
// Updates the authenticated user's privacy settings with GDPR validation.
func (h *PrivacyHandler) UpdatePrivacySettings(ctx context.Context, input *UpdatePrivacySettingsInput) (*UpdatePrivacySettingsOutput, error) {
	// Fail closed: the PostHog person identity is the Clerk subject, so a request that reached the
	// handler without one is refused rather than updating settings whose opt-out plumbing cannot
	// name the person to update.
	clerkSubject, ok := ctx.Value(inputs.ClerkUserIDKey).(string)
	if !ok || clerkSubject == "" {
		return nil, huma.Error401Unauthorized("User not authenticated")
	}

	settings, err := h.service.UpdatePrivacySettings(ctx, input.UserID, clerkSubject, &input.Body)
	if err != nil {
		// Dedicated branch, deliberately ABOVE the necessary_consent branch below: the merged-state
		// invariant (interaction capture requires analytics consent) is a semantic refusal — 422 —
		// not a malformed field (400) and not a server fault (500).
		if errors.Is(err, domain.ErrInteractionCaptureRequiresAnalytics) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		// Handle specific GDPR validation errors
		if strings.Contains(err.Error(), "necessary_consent") {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to update privacy settings")
	}

	return &UpdatePrivacySettingsOutput{Body: settings}, nil
}

// RegisterPrivacyRoutes registers privacy routes with the Huma API.
// Note: These routes require the Clerk middleware to be applied.
func RegisterPrivacyRoutes(api huma.API, handler *PrivacyHandler) {
	// GET /api/v1/user/privacy - Get privacy settings
	huma.Register(api, huma.Operation{
		OperationID: "getPrivacySettings",
		Method:      http.MethodGet,
		Path:        "/api/v1/user/privacy",
		Summary:     "Get privacy settings",
		Description: "Retrieves the authenticated user's privacy settings, creating defaults if none exist.",
		Tags:        []string{"Privacy"},
		Security:    []map[string][]string{{"clerk": {}}},
		// #1610: this GET may lazily create the caller's own default row on first access — it has
		// a caller-scoped read side effect despite being admitted by readExecute. This flows into
		// api/openapi.yaml as a sibling of operationId (huma.Operation.Extensions is marshaled,
		// unlike Metadata) so apps/mcp-server can assert on it instead of only a by-name allowlist.
		Extensions: map[string]any{"x-mcp-read-side-effect": true},
	}, handler.GetPrivacySettings)

	// PUT /api/v1/user/privacy - Update privacy settings
	huma.Register(api, huma.Operation{
		OperationID: "updatePrivacySettings",
		Method:      http.MethodPut,
		Path:        "/api/v1/user/privacy",
		Summary:     "Update privacy settings",
		Description: "Updates the authenticated user's privacy settings with GDPR validation.",
		Tags:        []string{"Privacy"},
		Security:    []map[string][]string{{"clerk": {}}},
	}, handler.UpdatePrivacySettings)
}
