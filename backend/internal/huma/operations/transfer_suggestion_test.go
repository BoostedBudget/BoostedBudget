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
	getPendingResult []*domain.TransferSuggestion
	getPendingErr    error
	acceptResult     *domain.TransferSuggestion
	acceptErr        error
	rejectResult     *domain.TransferSuggestion
	rejectErr        error
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
