package operations

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock TransferSuggestionService
// =============================================================================

type mockTransferSuggestionService struct {
	getPendingResult   []*domain.TransferSuggestion
	getPendingErr      error
	acceptResult       *domain.TransferSuggestion
	acceptErr          error
	rejectResult       *domain.TransferSuggestion
	rejectErr          error
	confirmMatchResult *domain.TransferSuggestion
	confirmMatchErr    error
}

func (m *mockTransferSuggestionService) GetPendingSuggestions(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error) {
	return m.getPendingResult, m.getPendingErr
}

func (m *mockTransferSuggestionService) AcceptSuggestion(ctx context.Context, userID, suggestionID string, notes *string) (*domain.TransferSuggestion, error) {
	return m.acceptResult, m.acceptErr
}

func (m *mockTransferSuggestionService) RejectSuggestion(ctx context.Context, userID, suggestionID string, notes *string) (*domain.TransferSuggestion, error) {
	return m.rejectResult, m.rejectErr
}

func (m *mockTransferSuggestionService) ConfirmTransferSuggestionMatch(ctx context.Context, userID, suggestionID string, actualAmount float64) (*domain.TransferSuggestion, error) {
	return m.confirmMatchResult, m.confirmMatchErr
}

// =============================================================================
// Test Helpers
// =============================================================================

func newTestSuggestionItem(id, userID string) *domain.TransferSuggestion {
	now := time.Now()
	reason := "Rule triggered"
	return &domain.TransferSuggestion{
		ID:           id,
		UserID:       userID,
		Amount:       100.00,
		CurrencyCode: "USD",
		Reason:       &reason,
		Source:       domain.SuggestionSourceRule,
		Status:       domain.SuggestionStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// =============================================================================
// GetTransferSuggestions Tests
// =============================================================================

func TestGetTransferSuggestions_Success(t *testing.T) {
	userID := "user-123"
	svc := &mockTransferSuggestionService{
		getPendingResult: []*domain.TransferSuggestion{
			newTestSuggestionItem("s1", userID),
			newTestSuggestionItem("s2", userID),
		},
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &GetTransferSuggestionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
	}

	output, err := handler.GetTransferSuggestions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Len(t, output.Body, 2)
	assert.Equal(t, "s1", output.Body[0].ID)
}

func TestGetTransferSuggestions_EmptyList(t *testing.T) {
	svc := &mockTransferSuggestionService{
		getPendingResult: []*domain.TransferSuggestion{},
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &GetTransferSuggestionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetTransferSuggestions(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.NotNil(t, output.Body, "should return empty slice, not nil")
	assert.Len(t, output.Body, 0)
}

func TestGetTransferSuggestions_Error(t *testing.T) {
	svc := &mockTransferSuggestionService{
		getPendingErr: errors.New("database error"),
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &GetTransferSuggestionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
	}

	output, err := handler.GetTransferSuggestions(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
}

// =============================================================================
// AcceptTransferSuggestion Tests
// =============================================================================

func TestAcceptTransferSuggestion_Success(t *testing.T) {
	userID := "user-123"
	suggestionID := "s-456"

	svc := &mockTransferSuggestionService{
		acceptResult: newTestSuggestionItem(suggestionID, userID),
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &AcceptTransferSuggestionInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 suggestionID,
	}

	output, err := handler.AcceptTransferSuggestion(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Transfer suggestion accepted", output.Body.Message)
	assert.Equal(t, suggestionID, output.Body.Suggestion.ID)
}

func TestAcceptTransferSuggestion_NotFound(t *testing.T) {
	svc := &mockTransferSuggestionService{
		acceptErr: domain.ErrTransferSuggestionNotFound,
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &AcceptTransferSuggestionInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
	}

	output, err := handler.AcceptTransferSuggestion(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAcceptTransferSuggestion_AlreadyActioned(t *testing.T) {
	svc := &mockTransferSuggestionService{
		acceptErr: domain.ErrTransferSuggestionAlreadyActioned,
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &AcceptTransferSuggestionInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "s-789",
	}

	output, err := handler.AcceptTransferSuggestion(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	// Huma wraps the error as a 409 Conflict; message contains "already".
	assert.Contains(t, err.Error(), "already")
}

// =============================================================================
// RejectTransferSuggestion Tests
// =============================================================================

func TestRejectTransferSuggestion_Success(t *testing.T) {
	userID := "user-123"
	suggestionID := "s-456"

	svc := &mockTransferSuggestionService{
		rejectResult: newTestSuggestionItem(suggestionID, userID),
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &RejectTransferSuggestionInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 suggestionID,
	}

	output, err := handler.RejectTransferSuggestion(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Transfer suggestion rejected", output.Body.Message)
	assert.Equal(t, suggestionID, output.Body.Suggestion.ID)
}

func TestRejectTransferSuggestion_NotFound(t *testing.T) {
	svc := &mockTransferSuggestionService{
		rejectErr: domain.ErrTransferSuggestionNotFound,
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &RejectTransferSuggestionInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
	}

	output, err := handler.RejectTransferSuggestion(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRejectTransferSuggestion_AlreadyActioned(t *testing.T) {
	svc := &mockTransferSuggestionService{
		rejectErr: domain.ErrTransferSuggestionAlreadyActioned,
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &RejectTransferSuggestionInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "s-789",
	}

	output, err := handler.RejectTransferSuggestion(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	// Huma wraps the error as a 409 Conflict; message contains "already".
	assert.Contains(t, err.Error(), "already")
}

// =============================================================================
// C3: ConfirmTransferSuggestionMatch Tests
// =============================================================================

func TestConfirmTransferSuggestionMatch_Success(t *testing.T) {
	userID := "user-123"
	suggestionID := "s-456"
	actualAmount := 150.00

	completed := newTestSuggestionItem(suggestionID, userID)
	completed.Status = domain.SuggestionStatusCompleted
	completed.ActualAmount = &actualAmount

	svc := &mockTransferSuggestionService{
		confirmMatchResult: completed,
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 suggestionID,
		Body:               ConfirmMatchBody{ActualAmount: actualAmount},
	}

	output, err := handler.ConfirmMatch(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Transfer suggestion confirmed", output.Body.Message)
	assert.Equal(t, suggestionID, output.Body.Suggestion.ID)
	assert.Equal(t, domain.SuggestionStatusCompleted, output.Body.Suggestion.Status)
}

func TestConfirmTransferSuggestionMatch_AlreadyActioned(t *testing.T) {
	svc := &mockTransferSuggestionService{
		confirmMatchErr: domain.ErrTransferSuggestionAlreadyActioned,
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "s-789",
		Body:               ConfirmMatchBody{ActualAmount: 100.00},
	}

	output, err := handler.ConfirmMatch(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already")
}

func TestConfirmTransferSuggestionMatch_NotFound(t *testing.T) {
	svc := &mockTransferSuggestionService{
		confirmMatchErr: domain.ErrTransferSuggestionNotFound,
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
		Body:               ConfirmMatchBody{ActualAmount: 100.00},
	}

	output, err := handler.ConfirmMatch(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestConfirmTransferSuggestionMatch_MCPGuardFallthrough verifies that when
// X-MCP-Worker-Secret is set but db is nil (or mcpAuditRepo is nil), the
// handler falls through to the non-MCP path and still calls the service.
// This covers the IsMCPOrigin guard logic (lines 166+ in transfer_suggestion.go).
func TestConfirmTransferSuggestionMatch_MCPGuardFallthrough(t *testing.T) {
	userID := "user-mcp"
	suggestionID := "s-mcp-001"
	actualAmount := 200.00
	mcpWorkerSecret := "test-secret"

	completed := newTestSuggestionItem(suggestionID, userID)
	completed.Status = domain.SuggestionStatusCompleted
	completed.ActualAmount = &actualAmount

	svc := &mockTransferSuggestionService{
		confirmMatchResult: completed,
	}

	// Handler with MCPWorkerSecret but nil db/mcpAuditRepo — MCP guard evaluates false,
	// falls through to non-MCP path.
	handler := NewTransferSuggestionHandler(svc, TransferSuggestionHandlerDeps{
		MCPWorkerSecret: mcpWorkerSecret,
		// DB and MCPAuditRepo intentionally nil
	})

	input := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: mcpWorkerSecret},
		ID:                 suggestionID,
		Body:               ConfirmMatchBody{ActualAmount: actualAmount},
	}

	output, err := handler.ConfirmMatch(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Transfer suggestion confirmed", output.Body.Message)
	assert.Equal(t, domain.SuggestionStatusCompleted, output.Body.Suggestion.Status)
}

// =============================================================================
// Route Registration Test
// =============================================================================

func TestTransferSuggestionRouteRegistration(t *testing.T) {
	svc := &mockTransferSuggestionService{}
	_, api := humatest.New(t)
	handler := NewTransferSuggestionHandler(svc)
	RegisterTransferSuggestionRoutes(api, handler)

	// Without auth, should return 401.
	resp := api.Get("/api/v1/transfer-suggestions")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}
