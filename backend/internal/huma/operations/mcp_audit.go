package operations

// MCP audit log endpoint (Issue #1087 GAP-2)
// Records security audit entries for MCP operations.
//
// Authentication: this endpoint is NOT secured with Clerk JWT. It is called
// by the trusted MCP Cloudflare Worker (service call), not end-users.
// Security is provided by the existing CloudflareOnly middleware (X-Origin-Secret
// + Cloudflare IP CIDR check) that applies to all routes.

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	humaerrors "github.com/maxnussbaum/BoostedBudget/internal/huma/errors"
"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// MCPAuditRepository is the interface the handler uses (subset of repository.MCPAuditRepository).
type MCPAuditRepository interface {
	Record(ctx context.Context, entry *domain.MCPAuditEntry) error
}

// MCPAuditHandler handles POST /api/v1/mcp/audit
type MCPAuditHandler struct {
	repo MCPAuditRepository
}

// NewMCPAuditHandler creates a new MCPAuditHandler.
func NewMCPAuditHandler(repo MCPAuditRepository) *MCPAuditHandler {
	return &MCPAuditHandler{repo: repo}
}

// Ensure repository.MCPAuditRepository satisfies our interface (compile-time check).
var _ MCPAuditRepository = (*repository.MCPAuditRepositoryImpl)(nil)

// RecordMCPAuditBody is the request body for POST /api/v1/mcp/audit.
type RecordMCPAuditBody struct {
	ActorUserID           string   `json:"actor_user_id" doc:"Internal user UUID"`
	GrantID               string   `json:"grant_id" doc:"OAuth grant ID from the Clerk token"`
	Scopes                []string `json:"scopes,omitempty" doc:"OAuth scopes granted"`
	OperationClass        string   `json:"operation_class" enum:"read,write" doc:"Operation class: read or write"`
	OperationID           string   `json:"operation_id" doc:"OpenAPI operationId"`
	ConfirmationPresented bool     `json:"confirmation_presented" doc:"Whether confirmation was required"`
	ConfirmationConfirmed bool     `json:"confirmation_confirmed" doc:"Whether confirmation was provided and valid"`
	Result                string   `json:"result" enum:"success,denied,error,confirmation_required" doc:"Outcome"`
	IdempotencyKey        string   `json:"idempotency_key,omitempty" doc:"Idempotency key if provided"`
	RevocationState       string   `json:"revocation_state,omitempty" doc:"Revocation check outcome"`
	RequestID             string   `json:"request_id,omitempty" doc:"Trace request ID"`
}

// RecordMCPAuditInput is the full input for POST /api/v1/mcp/audit.
// No AuthenticatedInput — this is a service call from the trusted MCP Worker,
// not an end-user request. CloudflareOnly middleware provides auth.
type RecordMCPAuditInput struct {
	Body RecordMCPAuditBody
}

// RecordMCPAuditOutput is the response for POST /api/v1/mcp/audit.
type RecordMCPAuditOutput struct{}

// RecordAuditEntry handles POST /api/v1/mcp/audit.
func (h *MCPAuditHandler) RecordAuditEntry(ctx context.Context, input *RecordMCPAuditInput) (*RecordMCPAuditOutput, error) {
	entry := &domain.MCPAuditEntry{
		ActorUserID:           input.Body.ActorUserID,
		GrantID:               input.Body.GrantID,
		Scopes:                input.Body.Scopes,
		OperationClass:        input.Body.OperationClass,
		OperationID:           input.Body.OperationID,
		ConfirmationPresented: input.Body.ConfirmationPresented,
		ConfirmationConfirmed: input.Body.ConfirmationConfirmed,
		Result:                input.Body.Result,
		IdempotencyKey:        input.Body.IdempotencyKey,
		RevocationState:       input.Body.RevocationState,
		RequestID:             input.Body.RequestID,
	}

	if err := h.repo.Record(ctx, entry); err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to record MCP audit entry: %w", err))
	}

	return &RecordMCPAuditOutput{}, nil
}

// RegisterMCPAuditRoutes registers the MCP audit endpoint.
// Note: NO Security declaration — authenticated by CloudflareOnly middleware via
// X-Origin-Secret (service call from the trusted MCP Worker, not end-user Clerk JWT).
func RegisterMCPAuditRoutes(api huma.API, handler *MCPAuditHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "recordMCPAuditEntry",
		Method:      http.MethodPost,
		Path:        "/api/v1/mcp/audit",
		Summary:     "Record MCP audit entry",
		Description: "Records a security audit entry for an MCP operation. Called by the trusted MCP Worker; authenticated by CloudflareOnly middleware (X-Origin-Secret), not end-user Clerk JWT.",
		Tags:        []string{"MCP Audit"},
		// No Security declaration — CloudflareOnly middleware protects this path
	}, handler.RecordAuditEntry)
}
