package domain

import "time"

// MCPAuditEntry is a record of a single MCP operation (Issue #1087 GAP-2).
// No raw financial payload columns — only operation class/id and outcome.
// Written synchronously before returning the response; durable via pgx.
type MCPAuditEntry struct {
	ID                    string    `json:"id,omitempty"`
	Timestamp             time.Time `json:"ts,omitempty"`
	ActorUserID           string    `json:"actor_user_id"`
	GrantID               string    `json:"grant_id"`
	Scopes                []string  `json:"scopes"`
	OperationClass        string    `json:"operation_class"` // "read" | "write"
	OperationID           string    `json:"operation_id"`
	ConfirmationPresented bool      `json:"confirmation_presented"`
	ConfirmationConfirmed bool      `json:"confirmation_confirmed"`
	Result                string    `json:"result"` // "success" | "denied" | "error" | "confirmation_required"
	IdempotencyKey        string    `json:"idempotency_key,omitempty"`
	RevocationState       string    `json:"revocation_state,omitempty"`
	RequestID             string    `json:"request_id,omitempty"`
}
