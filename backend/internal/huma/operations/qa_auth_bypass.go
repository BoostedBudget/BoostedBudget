package operations

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
//
// Two independent guards stand in front of it:
//   - Guard 1, registration: cmd/server/main.go registers this route only when
//     config.Config.QAAuthBypassRefusalReason returns "" — an explicitly-set ENVIRONMENT on a
//     two-value allowlist, the QA_AUTH_BYPASS_ENABLED opt-in, the three QA settings, and no
//     production-only signal (a Clerk sk_live_ key, PADDLE_ENVIRONMENT=production).
//   - Guard 2, below: the handler re-checks the environment itself, so the route cannot serve
//     even if it is somehow registered.
func (h *QAAuthBypassHandler) Bypass(ctx context.Context, input *QAAuthBypassInput) (*QAAuthBypassOutput, error) {
	// Guard 2 of 2.
	//
	// #1432 batch ED, finding B2-1: this used to read `h.environment == "production"`, which
	// fails OPEN — "Production", " production ", "prod", a typo, and "" all fell through to
	// minting a five-minute admin sign-in token. It is now an allowlist, so an unrecognised
	// value gets the production treatment.
	if !domain.IsQAAuthBypassEnvironment(h.environment) {
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
