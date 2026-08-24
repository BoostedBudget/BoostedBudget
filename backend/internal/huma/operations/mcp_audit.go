package operations

// MCP audit log endpoint (Issue #1087 GAP-2)
// Records security audit entries for MCP operations.
//
// Authentication: this endpoint is NOT secured with Clerk JWT. It is called
// by the trusted MCP Cloudflare Worker (service call), not end-users.
// Security: CloudflareOnly (CF IP + X-Origin-Secret) is layer 1 — it gates all routes.
// Layer 2: this handler also requires X-MCP-Worker-Secret (a separate Worker-only secret)
// so that any caller that happens to know the host-wide X-Origin-Secret cannot forge audit rows.
// The Worker sends X-MCP-Worker-Secret from its MCP_WORKER_SECRET binding.

import (
	"context"
	"crypto/subtle"
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

// UserIdentityResolver resolves a Clerk user ID to the internal user identity.
// Satisfied by repository.UserIdentityRepository.
type UserIdentityResolver interface {
	GetByClerkID(ctx context.Context, clerkUserID string) (*domain.UserIdentity, error)
}

// MCPAuditHandler handles POST /api/v1/mcp/audit
type MCPAuditHandler struct {
	repo             MCPAuditRepository
	mcpWorkerSecret  string               // required: proves call came from MCP Worker, not any CF-origin caller
	identityResolver UserIdentityResolver // resolves the Worker-asserted Clerk user ID to the internal UUID (#1247)
}

// NewMCPAuditHandler creates a new MCPAuditHandler.
// mcpWorkerSecret must match the value in MCP_WORKER_SECRET config (fail-closed when empty).
// identityResolver maps the Worker-asserted Clerk user ID to the internal user UUID
// (the actor_user_id column is a UUID; the Worker only knows the Clerk ID — #1247).
func NewMCPAuditHandler(repo MCPAuditRepository, mcpWorkerSecret string, identityResolver UserIdentityResolver) *MCPAuditHandler {
	return &MCPAuditHandler{repo: repo, mcpWorkerSecret: mcpWorkerSecret, identityResolver: identityResolver}
}

// Ensure repository.MCPAuditRepository satisfies our interface (compile-time check).
var _ MCPAuditRepository = (*repository.MCPAuditRepositoryImpl)(nil)

// RecordMCPAuditBody is the request body for POST /api/v1/mcp/audit.
type RecordMCPAuditBody struct {
	ActorUserID           string   `json:"actor_user_id" doc:"Clerk user ID of the actor; resolved to the internal user UUID server-side"`
	GrantID               string   `json:"grant_id" doc:"OAuth grant ID from the Clerk token"`
	Scopes                []string `json:"scopes,omitempty" doc:"OAuth scopes granted"`
	OperationClass        string   `json:"operation_class" enum:"read,write,destructive" doc:"Operation class: read, write, or destructive (permanently destroys data — #1569)"`
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
// not an end-user request. Authentication: CloudflareOnly (layer 1) + X-MCP-Worker-Secret (layer 2).
type RecordMCPAuditInput struct {
	// X-MCP-Worker-Secret proves the request came from the MCP Cloudflare Worker
	// and not from an arbitrary caller that happens to know the host-wide X-Origin-Secret.
	MCPWorkerSecret string `header:"X-MCP-Worker-Secret" doc:"Worker-only shared secret; validated constant-time before recording"`
	Body            RecordMCPAuditBody
}

// RecordMCPAuditOutput is the response for POST /api/v1/mcp/audit.
type RecordMCPAuditOutput struct{}

// RecordAuditEntry handles POST /api/v1/mcp/audit.
func (h *MCPAuditHandler) RecordAuditEntry(ctx context.Context, input *RecordMCPAuditInput) (*RecordMCPAuditOutput, error) {
	// Layer 2 auth: constant-time comparison of the Worker-only shared secret.
	// Fail-closed when unconfigured (empty expected secret blocks all callers).
	if h.mcpWorkerSecret == "" || subtle.ConstantTimeCompare([]byte(input.MCPWorkerSecret), []byte(h.mcpWorkerSecret)) != 1 {
		return nil, huma.Error403Forbidden("invalid or missing Worker secret")
	}

	// Resolve the Worker-asserted Clerk user ID to the internal user UUID.
	// The actor_user_id column is a UUID; the Worker only knows the Clerk ID (#1247).
	// We do not trust a client-supplied internal UUID — the actor is always derived
	// server-side from the Clerk identity.
	if h.identityResolver == nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("mcp audit: identity resolver not configured"))
	}
	identity, err := h.identityResolver.GetByClerkID(ctx, input.Body.ActorUserID)
	if err != nil {
		return nil, humaerrors.LoggingMapError(ctx, fmt.Errorf("failed to resolve actor identity %q: %w", input.Body.ActorUserID, err))
	}

	entry := &domain.MCPAuditEntry{
		ActorUserID:           identity.UserID,
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
