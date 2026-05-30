package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// MCPAuditRepository writes security audit entries for MCP operations (Issue #1087 GAP-2).
type MCPAuditRepository interface {
	Record(ctx context.Context, entry *domain.MCPAuditEntry) error
	RecordInTx(ctx context.Context, tx pgx.Tx, entry *domain.MCPAuditEntry) error
}

// MCPAuditRepositoryImpl is the concrete implementation (exported for compile-time checks).
type MCPAuditRepositoryImpl = mcpAuditRepository

type mcpAuditRepository struct {
	db DB
}

// NewMCPAuditRepository creates a new MCPAuditRepository.
func NewMCPAuditRepository(db DB) MCPAuditRepository {
	return &mcpAuditRepository{db: db}
}

// Record writes a single audit entry to mcp_audit_log.
// pgx commits synchronously, making this durable before the caller returns.
func (r *mcpAuditRepository) Record(ctx context.Context, entry *domain.MCPAuditEntry) error {
	return recordMCPAuditEntry(ctx, r.db, entry)
}

// RecordInTx writes a single audit entry within an existing transaction.
func (r *mcpAuditRepository) RecordInTx(ctx context.Context, tx pgx.Tx, entry *domain.MCPAuditEntry) error {
	return recordMCPAuditEntry(ctx, tx, entry)
}

func recordMCPAuditEntry(ctx context.Context, db DB, entry *domain.MCPAuditEntry) error {
	query := `
		INSERT INTO mcp_audit_log (
			actor_user_id, grant_id, scopes, operation_class, operation_id,
			confirmation_presented, confirmation_confirmed, result,
			idempotency_key, revocation_state, request_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	scopes := entry.Scopes
	if scopes == nil {
		scopes = []string{}
	}

	_, err := db.Exec(ctx, query,
		entry.ActorUserID,
		entry.GrantID,
		scopes,
		entry.OperationClass,
		entry.OperationID,
		entry.ConfirmationPresented,
		entry.ConfirmationConfirmed,
		entry.Result,
		nilIfEmpty(entry.IdempotencyKey),
		nilIfEmpty(entry.RevocationState),
		nilIfEmpty(entry.RequestID),
	)
	if err != nil {
		return fmt.Errorf("failed to record MCP audit entry: %w", err)
	}
	return nil
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
