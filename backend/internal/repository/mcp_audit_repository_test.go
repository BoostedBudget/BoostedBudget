package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

func TestMCPAuditRepository_RecordInTxUsesExistingTransaction(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO mcp_audit_log`)).
		WithArgs(
			"user-123",
			"grant-456",
			[]string{},
			"write",
			"createUserTag",
			true,
			true,
			"success",
			nil,
			nil,
			nil,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	ctx := context.Background()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	repo := NewMCPAuditRepository(mock)
	err = repo.RecordInTx(ctx, tx, &domain.MCPAuditEntry{
		ActorUserID:           "user-123",
		GrantID:               "grant-456",
		OperationClass:        "write",
		OperationID:           "createUserTag",
		ConfirmationPresented: true,
		ConfirmationConfirmed: true,
		Result:                "success",
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, mock.ExpectationsWereMet())
}
