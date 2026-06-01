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

// mockIdentityResolver resolves a Clerk user ID to an internal user identity (#1247).
type mockIdentityResolver struct {
	userID string
	err    error
}

func (m *mockIdentityResolver) GetByClerkID(_ context.Context, _ string) (*domain.UserIdentity, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &domain.UserIdentity{UserID: m.userID}, nil
}

func TestMCPAuditHandler_RecordAuditEntry(t *testing.T) {
	ctx := context.Background()

	t.Run("records a valid read-class audit entry with correct Worker secret", func(t *testing.T) {
		repo := &mockMCPAuditRepository{}
		handler := NewMCPAuditHandler(repo, "test-worker-secret", &mockIdentityResolver{userID: "internal-uuid-1"})

		input := &RecordMCPAuditInput{
			MCPWorkerSecret: "test-worker-secret",
			Body: RecordMCPAuditBody{
				ActorUserID:           "user-123",
				GrantID:               "grant-456",
				Scopes:                []string{"email", "profile"},
				OperationClass:        "read",
				OperationID:           "getTransactions",
				ConfirmationPresented: false,
				ConfirmationConfirmed: false,
				Result:                "success",
			},
		}

		output, err := handler.RecordAuditEntry(ctx, input)
		require.NoError(t, err)
		assert.NotNil(t, output)

		require.NotNil(t, repo.recordedEntry)
		// #1247: the Worker sends the Clerk user ID ("user-123"); the handler resolves it
		// to the internal UUID before recording (actor_user_id is a UUID column).
		assert.Equal(t, "internal-uuid-1", repo.recordedEntry.ActorUserID)
		assert.Equal(t, "grant-456", repo.recordedEntry.GrantID)
		assert.Equal(t, "read", repo.recordedEntry.OperationClass)
		assert.Equal(t, "getTransactions", repo.recordedEntry.OperationID)
		assert.Equal(t, "success", repo.recordedEntry.Result)
	})

	t.Run("R7-2: rejects request with missing or wrong Worker secret (403)", func(t *testing.T) {
		repo := &mockMCPAuditRepository{}
		handler := NewMCPAuditHandler(repo, "real-secret", &mockIdentityResolver{userID: "internal-uuid-1"})

		// No secret
		_, err := handler.RecordAuditEntry(ctx, &RecordMCPAuditInput{
			Body: RecordMCPAuditBody{ActorUserID: "u", GrantID: "g", OperationClass: "read", OperationID: "op", Result: "success"},
		})
		assert.Error(t, err, "missing secret must be rejected")

		// Wrong secret
		_, err = handler.RecordAuditEntry(ctx, &RecordMCPAuditInput{
			MCPWorkerSecret: "wrong-secret",
			Body:            RecordMCPAuditBody{ActorUserID: "u", GrantID: "g", OperationClass: "read", OperationID: "op", Result: "success"},
		})
		assert.Error(t, err, "wrong secret must be rejected")
		assert.Nil(t, repo.recordedEntry, "no entry should be recorded on rejection")
	})

	t.Run("records a write-class audit entry with confirmation confirmed", func(t *testing.T) {
		repo := &mockMCPAuditRepository{}
		handler := NewMCPAuditHandler(repo, "test-worker-secret", &mockIdentityResolver{userID: "internal-uuid-1"})

		input := &RecordMCPAuditInput{
			MCPWorkerSecret: "test-worker-secret",
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
		handler := NewMCPAuditHandler(repo, "test-worker-secret", &mockIdentityResolver{userID: "internal-uuid-1"})

		input := &RecordMCPAuditInput{
			MCPWorkerSecret: "test-worker-secret",
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

	t.Run("#1247: fails and records nothing when the actor Clerk ID cannot be resolved", func(t *testing.T) {
		repo := &mockMCPAuditRepository{}
		handler := NewMCPAuditHandler(repo, "test-worker-secret", &mockIdentityResolver{err: assert.AnError})

		input := &RecordMCPAuditInput{
			MCPWorkerSecret: "test-worker-secret",
			Body: RecordMCPAuditBody{
				ActorUserID:    "user_unknown",
				GrantID:        "g",
				OperationClass: "read",
				OperationID:    "getTransactions",
				Result:         "success",
			},
		}

		_, err := handler.RecordAuditEntry(ctx, input)
		assert.Error(t, err)
		assert.Nil(t, repo.recordedEntry, "no audit row should be written when actor resolution fails")
	})
}
