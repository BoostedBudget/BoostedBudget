package operations

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

func newMCPWriteAuditEntry(userID, grantID, operationID, idempotencyKey string) *domain.MCPAuditEntry {
	return &domain.MCPAuditEntry{
		ActorUserID:           userID,
		GrantID:               grantID,
		OperationClass:        "write",
		OperationID:           operationID,
		ConfirmationPresented: true,
		ConfirmationConfirmed: true,
		Result:                "success",
		IdempotencyKey:        idempotencyKey,
	}
}

func rollbackMCPAuditTx(ctx context.Context, tx pgx.Tx) {
	tx.Rollback(ctx) // error ignored — rollback on failure
}

func recordMCPWriteAuditInTx(
	ctx context.Context,
	repo repository.MCPAuditRepository,
	tx pgx.Tx,
	userID string,
	grantID string,
	operationID string,
	idempotencyKey string,
) error {
	return repo.RecordInTx(ctx, tx, newMCPWriteAuditEntry(userID, grantID, operationID, idempotencyKey))
}

func recordMCPWriteAudit(ctx context.Context, repo repository.MCPAuditRepository, userID, grantID, operationID, idempotencyKey string) error {
	return repo.Record(ctx, newMCPWriteAuditEntry(userID, grantID, operationID, idempotencyKey))
}
