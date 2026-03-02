package operations

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// QAAuthBypassHandler handles the QA auth bypass endpoint.
// Issue #821: Allows Claude Code agents to authenticate in QA/Staging without Clerk OAuth.
type QAAuthBypassHandler struct {
	service     services.QAAuthBypassService
	secret      string
	environment string
}

// NewQAAuthBypassHandler creates a new QAAuthBypassHandler.
func NewQAAuthBypassHandler(service services.QAAuthBypassService, secret, environment string) *QAAuthBypassHandler {
	return &QAAuthBypassHandler{
		service:     service,
		secret:      secret,
		environment: environment,
	}
}

// QAAuthBypassRequestBody is the request body for the QA auth bypass endpoint.
type QAAuthBypassRequestBody struct {
	Role   string `json:"role" validate:"required,oneof=admin premium" doc:"QA test user role: admin or premium"`
	Secret string `json:"secret" validate:"required" doc:"QA bypass secret (must match QA_BYPASS_SECRET env var)"`
}

// QAAuthBypassResponseBody is the response body for the QA auth bypass endpoint.
type QAAuthBypassResponseBody struct {
	URL   string `json:"url" doc:"Clerk sign-in token URL - navigate here to authenticate"`
	Email string `json:"email" doc:"Email address of the QA test user"`
	Tier  string `json:"tier" doc:"Subscription tier of the QA test user"`
}

// QAAuthBypassInput is the Huma input for the QA auth bypass endpoint.
type QAAuthBypassInput struct {
	Body QAAuthBypassRequestBody
}

// QAAuthBypassOutput is the Huma output for the QA auth bypass endpoint.
type QAAuthBypassOutput struct {
	Body QAAuthBypassResponseBody
}

// Bypass handles POST /api/v1/qa/auth/bypass.
// Guard #1: Rejects production environment with 404.
// Guard #2: Route never registered in production (enforced in main.go).
func (h *QAAuthBypassHandler) Bypass(ctx context.Context, input *QAAuthBypassInput) (*QAAuthBypassOutput, error) {
	// Guard #1: Never serve this endpoint in production.
	if h.environment == "production" {
		return nil, huma.Error404NotFound("not found")
	}

	// Constant-time secret comparison to prevent timing attacks.
	if subtle.ConstantTimeCompare([]byte(input.Body.Secret), []byte(h.secret)) != 1 {
		return nil, huma.Error401Unauthorized("invalid secret")
	}

	result, err := h.service.CreateSignInToken(ctx, input.Body.Role)
	if err != nil {
		if errors.Is(err, services.ErrInvalidQARole) {
			return nil, huma.Error400BadRequest("invalid role: " + err.Error())
		}
		return nil, huma.Error500InternalServerError("failed to create sign-in token")
	}

	return &QAAuthBypassOutput{
		Body: QAAuthBypassResponseBody{
			URL:   result.URL,
			Email: result.Email,
			Tier:  result.Tier,
		},
	}, nil
}

// RegisterQAAuthBypassRoutes registers the QA auth bypass route.
// No Security annotation - Clerk middleware auto-skips unauthenticated routes.
func RegisterQAAuthBypassRoutes(api huma.API, handler *QAAuthBypassHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "qaAuthBypass",
		Method:      http.MethodPost,
		Path:        "/api/v1/qa/auth/bypass",
		Summary:     "QA auth bypass",
		Description: "Creates a Clerk sign-in token URL for QA test users. Only available in QA/Staging environments.",
		Tags:        []string{"QA"},
	}, handler.Bypass)
}
