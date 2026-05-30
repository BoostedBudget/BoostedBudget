package operations

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMCPAuditRepository is a test double for MCPAuditRepository (Issue #1087 GAP-2)
type mockMCPAuditRepository struct {
	recordedEntry *domain.MCPAuditEntry
	shouldError   bool
}

func (m *mockMCPAuditRepository) Record(ctx context.Context, entry *domain.MCPAuditEntry) error {
	if m.shouldError {
		return assert.AnError
	}
	m.recordedEntry = entry
	return nil
}

func (m *mockMCPAuditRepository) RecordInTx(_ context.Context, _ pgx.Tx, entry *domain.MCPAuditEntry) error {
	if m.shouldError {
		return assert.AnError
	}
	m.recordedEntry = entry
	return nil
}

func TestMCPAuditHandler_RecordAuditEntry(t *testing.T) {
	ctx := context.Background()

	t.Run("records a valid read-class audit entry", func(t *testing.T) {
		repo := &mockMCPAuditRepository{}
		handler := NewMCPAuditHandler(repo)

		input := &RecordMCPAuditInput{
			Body: RecordMCPAuditBody{
				ActorUserID:            "user-123",
				GrantID:                "grant-456",
				Scopes:                 []string{"email", "profile"},
				OperationClass:         "read",
				OperationID:            "getTransactions",
				ConfirmationPresented:  false,
				ConfirmationConfirmed:  false,
				Result:                 "success",
			},
		}

		output, err := handler.RecordAuditEntry(ctx, input)
		require.NoError(t, err)
		assert.NotNil(t, output)

		require.NotNil(t, repo.recordedEntry)
		assert.Equal(t, "user-123", repo.recordedEntry.ActorUserID)
		assert.Equal(t, "grant-456", repo.recordedEntry.GrantID)
		assert.Equal(t, "read", repo.recordedEntry.OperationClass)
		assert.Equal(t, "getTransactions", repo.recordedEntry.OperationID)
		assert.Equal(t, "success", repo.recordedEntry.Result)
	})

	t.Run("records a write-class audit entry with confirmation confirmed", func(t *testing.T) {
		repo := &mockMCPAuditRepository{}
		handler := NewMCPAuditHandler(repo)

		input := &RecordMCPAuditInput{
			Body: RecordMCPAuditBody{
				ActorUserID:           "user-123",
				GrantID:               "grant-789",
				OperationClass:        "write",
				OperationID:           "createBudget",
				ConfirmationPresented: true,
				ConfirmationConfirmed: true,
				Result:                "success",
				IdempotencyKey:        "idem-key-abc",
			},
		}

		_, err := handler.RecordAuditEntry(ctx, input)
		require.NoError(t, err)

		require.NotNil(t, repo.recordedEntry)
		assert.Equal(t, "write", repo.recordedEntry.OperationClass)
		assert.True(t, repo.recordedEntry.ConfirmationPresented)
		assert.True(t, repo.recordedEntry.ConfirmationConfirmed)
		assert.Equal(t, "idem-key-abc", repo.recordedEntry.IdempotencyKey)
	})

	t.Run("propagates repository error", func(t *testing.T) {
		repo := &mockMCPAuditRepository{shouldError: true}
		handler := NewMCPAuditHandler(repo)

		input := &RecordMCPAuditInput{
			Body: RecordMCPAuditBody{
				ActorUserID:    "u",
				GrantID:        "g",
				OperationClass: "read",
				OperationID:    "getTransactions",
				Result:         "success",
			},
		}

		_, err := handler.RecordAuditEntry(ctx, input)
		assert.Error(t, err)
	})
}
