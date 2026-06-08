package operations

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/huma/inputs"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock TransferSuggestionService
// =============================================================================

type mockTransferSuggestionService struct {
	getPendingResult    []*domain.TransferSuggestion
	getPendingErr       error
	acceptResult        *domain.TransferSuggestion
	acceptErr           error
	rejectResult        *domain.TransferSuggestion
	rejectErr           error
	confirmMatchResult  *domain.TransferSuggestion
	confirmMatchErr     error
	declineMatchResult  *domain.TransferSuggestion
	declineMatchErr     error
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

func (m *mockTransferSuggestionService) DeclineTransferSuggestionMatch(ctx context.Context, userID, suggestionID string) (*domain.TransferSuggestion, error) {
	return m.declineMatchResult, m.declineMatchErr
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
// Mock Analytics for handler tests
// =============================================================================

// mockHandlerAnalytics is a minimal mock for phclient.Analytics used in handler tests.
type mockHandlerAnalytics struct {
	mock.Mock
}

func (m *mockHandlerAnalytics) Capture(distinctID, event string, properties map[string]interface{}) {
	m.Called(distinctID, event, properties)
}

func (m *mockHandlerAnalytics) Identify(distinctID string, properties map[string]interface{}) {
	m.Called(distinctID, properties)
}

func (m *mockHandlerAnalytics) Close() {
	m.Called()
}

// Ensure mockHandlerAnalytics satisfies the interface at compile time.
var _ phclient.Analytics = (*mockHandlerAnalytics)(nil)

// =============================================================================
// ConfirmMatch Analytics Tests (Issue #972)
// =============================================================================

func TestConfirmMatch_FiresEventTransferSuggestionValidated(t *testing.T) {
	userID := "user-123"
	suggestionID := "s-456"
	actualAmount := 150.00

	completed := newTestSuggestionItem(suggestionID, userID)
	completed.Status = domain.SuggestionStatusCompleted
	completed.ActualAmount = &actualAmount

	svc := &mockTransferSuggestionService{
		confirmMatchResult: completed,
	}

	analytics := new(mockHandlerAnalytics)
	handler := NewTransferSuggestionHandler(svc)
	handler.SetAnalytics(analytics)

	analytics.On("Capture", userID, phclient.EventTransferSuggestionValidated, mock.Anything).Return()

	input := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 suggestionID,
		Body:               ConfirmMatchBody{ActualAmount: actualAmount},
	}

	output, err := handler.ConfirmMatch(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	analytics.AssertCalled(t, "Capture", userID, phclient.EventTransferSuggestionValidated, mock.Anything)
}

func TestConfirmMatch_NoAnalyticsFiredOnError(t *testing.T) {
	svc := &mockTransferSuggestionService{
		confirmMatchErr: domain.ErrTransferSuggestionNotFound,
	}

	analytics := new(mockHandlerAnalytics)
	handler := NewTransferSuggestionHandler(svc)
	handler.SetAnalytics(analytics)

	input := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-123"},
		ID:                 "nonexistent",
		Body:               ConfirmMatchBody{ActualAmount: 100.00},
	}

	output, err := handler.ConfirmMatch(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	analytics.AssertNotCalled(t, "Capture", mock.Anything, mock.Anything, mock.Anything)
}

func TestConfirmMatch_NilAnalytics_NoNilPanic(t *testing.T) {
	userID := "user-123"
	suggestionID := "s-456"
	actualAmount := 150.00

	completed := newTestSuggestionItem(suggestionID, userID)
	completed.Status = domain.SuggestionStatusCompleted
	completed.ActualAmount = &actualAmount

	svc := &mockTransferSuggestionService{
		confirmMatchResult: completed,
	}

	// No SetAnalytics — analytics nil; must not panic
	handler := NewTransferSuggestionHandler(svc)

	input := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 suggestionID,
		Body:               ConfirmMatchBody{ActualAmount: actualAmount},
	}

	output, err := handler.ConfirmMatch(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
}

// =============================================================================
// GAP AH MEDIUM#6: maskReservation — matched_transaction_id scoping tests
// =============================================================================

// TestMaskReservation_NonCompletedSuggestionsOmitMatchedTransactionID verifies that
// pending and accepted suggestions have matched_transaction_id cleared in API responses.
func TestMaskReservation_NonCompletedSuggestionsOmitMatchedTransactionID(t *testing.T) {
	txnID := "txn-reserved-123"

	tests := []struct {
		name   string
		status domain.SuggestionStatus
		wantID *string // nil = should be masked; &txnID = should be present
	}{
		{name: "pending", status: domain.SuggestionStatusPending, wantID: nil},
		{name: "accepted", status: domain.SuggestionStatusAccepted, wantID: nil},
		{name: "rejected", status: domain.SuggestionStatusRejected, wantID: nil},
		{name: "expired", status: domain.SuggestionStatusExpired, wantID: nil},
		{name: "completed", status: domain.SuggestionStatusCompleted, wantID: &txnID},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestSuggestionItem("s-mask-"+tc.name, "user-mask")
			s.Status = tc.status
			s.MatchedTransactionID = &txnID

			result := maskReservation(s)

			if tc.wantID == nil {
				assert.Nil(t, result.MatchedTransactionID,
					"status=%s: matched_transaction_id should be nil in API response", tc.name)
			} else {
				require.NotNil(t, result.MatchedTransactionID,
					"status=%s: matched_transaction_id should be present in API response", tc.name)
				assert.Equal(t, *tc.wantID, *result.MatchedTransactionID)
			}
		})
	}
}

// TestGetTransferSuggestions_MasksReservation proves that pending suggestions returned
// by GetTransferSuggestions have matched_transaction_id omitted (GAP AH MEDIUM#6).
func TestGetTransferSuggestions_MasksReservation(t *testing.T) {
	userID := "user-mask-get"
	txnID := "txn-reserved-for-get"

	s1 := newTestSuggestionItem("s-mask-1", userID)
	s1.MatchedTransactionID = &txnID // reserved pending suggestion

	svc := &mockTransferSuggestionService{
		getPendingResult: []*domain.TransferSuggestion{s1},
	}

	handler := NewTransferSuggestionHandler(svc)
	output, err := handler.GetTransferSuggestions(context.Background(), &GetTransferSuggestionsInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
	})

	require.NoError(t, err)
	require.Len(t, output.Body, 1)
	assert.Nil(t, output.Body[0].MatchedTransactionID,
		"pending suggestion in list response must not expose matched_transaction_id")
}

// TestAcceptTransferSuggestion_MasksReservation proves that an accepted suggestion
// returned by AcceptTransferSuggestion has matched_transaction_id omitted (GAP AH MEDIUM#6).
func TestAcceptTransferSuggestion_MasksReservation(t *testing.T) {
	userID := "user-mask-accept"
	suggestionID := "s-mask-accept"
	txnID := "txn-reserved-for-accept"

	accepted := newTestSuggestionItem(suggestionID, userID)
	accepted.Status = domain.SuggestionStatusAccepted
	accepted.MatchedTransactionID = &txnID

	svc := &mockTransferSuggestionService{acceptResult: accepted}
	handler := NewTransferSuggestionHandler(svc)

	output, err := handler.AcceptTransferSuggestion(context.Background(), &AcceptTransferSuggestionInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 suggestionID,
	})

	require.NoError(t, err)
	assert.Nil(t, output.Body.Suggestion.MatchedTransactionID,
		"accepted suggestion response must not expose matched_transaction_id")
}

// TestRejectTransferSuggestion_MasksReservation proves that a rejected suggestion
// returned by RejectTransferSuggestion has matched_transaction_id omitted (GAP AH MEDIUM#6).
func TestRejectTransferSuggestion_MasksReservation(t *testing.T) {
	userID := "user-mask-reject"
	suggestionID := "s-mask-reject"
	txnID := "txn-reserved-for-reject"

	rejected := newTestSuggestionItem(suggestionID, userID)
	rejected.Status = domain.SuggestionStatusRejected
	rejected.MatchedTransactionID = &txnID

	svc := &mockTransferSuggestionService{rejectResult: rejected}
	handler := NewTransferSuggestionHandler(svc)

	output, err := handler.RejectTransferSuggestion(context.Background(), &RejectTransferSuggestionInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 suggestionID,
	})

	require.NoError(t, err)
	assert.Nil(t, output.Body.Suggestion.MatchedTransactionID,
		"rejected suggestion response must not expose matched_transaction_id")
}

// TestConfirmMatch_CompletedSuggestionExposesMatchedTransactionID verifies that a
// completed suggestion returned by ConfirmMatch DOES expose matched_transaction_id
// (for auditability — the completed row is the only one allowed to surface it).
func TestConfirmMatch_CompletedSuggestionExposesMatchedTransactionID(t *testing.T) {
	userID := "user-mask-confirm"
	suggestionID := "s-mask-confirm"
	txnID := "txn-matched-for-confirm"
	actualAmount := 120.00

	completed := newTestSuggestionItem(suggestionID, userID)
	completed.Status = domain.SuggestionStatusCompleted
	completed.ActualAmount = &actualAmount
	completed.MatchedTransactionID = &txnID

	svc := &mockTransferSuggestionService{confirmMatchResult: completed}
	handler := NewTransferSuggestionHandler(svc)

	output, err := handler.ConfirmMatch(context.Background(), &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 suggestionID,
		Body:               ConfirmMatchBody{ActualAmount: actualAmount},
	})

	require.NoError(t, err)
	require.NotNil(t, output.Body.Suggestion.MatchedTransactionID,
		"completed suggestion must expose matched_transaction_id for auditability")
	assert.Equal(t, txnID, *output.Body.Suggestion.MatchedTransactionID)
}

// =============================================================================
// GAP AI: DeclineTransferSuggestionMatch handler tests
// =============================================================================

func TestDeclineMatch_Success(t *testing.T) {
	userID := "user-ai"
	suggestionID := "suggestion-ai"

	pending := newTestSuggestionItem(suggestionID, userID)
	pending.MatchedTransactionID = nil // cleared by decline

	svc := &mockTransferSuggestionService{
		declineMatchResult: pending,
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &DeclineTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 suggestionID,
	}

	output, err := handler.DeclineMatch(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Transfer match declined", output.Body.Message)
	assert.Equal(t, suggestionID, output.Body.Suggestion.ID)
}

func TestDeclineMatch_OwnershipEnforced(t *testing.T) {
	svc := &mockTransferSuggestionService{
		declineMatchErr: domain.ErrTransferSuggestionNotFound,
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &DeclineTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-ai"},
		ID:                 "other-users-suggestion",
	}

	output, err := handler.DeclineMatch(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeclineMatch_AlreadyActioned(t *testing.T) {
	svc := &mockTransferSuggestionService{
		declineMatchErr: domain.ErrTransferSuggestionAlreadyActioned,
	}

	handler := NewTransferSuggestionHandler(svc)

	input := &DeclineTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: "user-ai"},
		ID:                 "completed-suggestion",
	}

	output, err := handler.DeclineMatch(context.Background(), input)

	assert.Nil(t, output)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already")
}

// =============================================================================
// Route Registration Test
// =============================================================================

// =============================================================================
// GAP AL: DeclineMatch MCP transactional-audit path tests
// =============================================================================

// TestDeclineMatch_MCPTransactionalPath_BeginError proves that when IsMCPOrigin==true
// and db+mcpAuditRepo are non-nil, the handler enters the MCP transactional branch
// and surfaces db.Begin() errors instead of falling through to the non-MCP service call.
func TestDeclineMatch_MCPTransactionalPath_BeginError(t *testing.T) {
	const secret = "mcp-secret-decline"
	const userID = "user-mcp-decline"
	const suggID = "sugg-mcp-decline"

	svc := &mockTransferSuggestionService{} // never called in MCP path

	dbMock := new(mockOperationsDB)
	beginErr := errors.New("db unavailable for decline")
	dbMock.On("Begin", mock.Anything).Return(nil, beginErr)

	handler := NewTransferSuggestionHandler(svc, TransferSuggestionHandlerDeps{
		DB:              dbMock,
		MCPAuditRepo:    new(mockOperationsMCPAuditRepo),
		MCPWorkerSecret: secret,
	})

	input := &DeclineTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: secret},
		ID:                 suggID,
	}

	output, err := handler.DeclineMatch(context.Background(), input)

	assert.Nil(t, output, "must not return a response when db.Begin() fails in the MCP path")
	require.Error(t, err, "handler must propagate the db.Begin error from the MCP transactional path")
	dbMock.AssertNumberOfCalls(t, "Begin", 1)
}

// TestDeclineMatch_MCPTransactionalPath_NoIdempotency_UsesTxBackedService proves that
// when IdempotencyRepo is nil, the no-idempotency branch is entered: Begin() succeeds,
// a transaction-backed service is created, and the handler returns the error from the tx service.
func TestDeclineMatch_MCPTransactionalPath_NoIdempotency_UsesTxBackedService(t *testing.T) {
	const secret = "mcp-secret-decline-noidem"
	const userID = "user-decline-noidem"
	const suggID = "sugg-decline-noidem"

	svc := &mockTransferSuggestionService{} // never called in MCP path

	txMock := new(mockPgxTx)
	// QueryRow is called by the tx-backed repo's GetByID — return ErrNoRows so the
	// service call fails cleanly without requiring a full result scan.
	txMock.On("QueryRow").Return(&errScanRow{err: pgx.ErrNoRows})
	txMock.On("Rollback", mock.Anything).Return(nil)

	dbMock := new(mockOperationsDB)
	dbMock.On("Begin", mock.Anything).Return(txMock, nil) // Begin SUCCEEDS

	// No IdempotencyRepo — exercises the no-idempotency branch.
	handler := NewTransferSuggestionHandler(svc, TransferSuggestionHandlerDeps{
		DB:              dbMock,
		MCPAuditRepo:    new(mockOperationsMCPAuditRepo),
		MCPWorkerSecret: secret,
		// IdempotencyRepo intentionally nil
	})

	input := &DeclineTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: secret},
		ID:                 suggID,
	}

	output, err := handler.DeclineMatch(context.Background(), input)

	// The handler must error because the tx-backed GetByID returns ErrNoRows.
	assert.Nil(t, output, "no output when tx-backed service call fails")
	require.Error(t, err, "handler must propagate error from the tx-backed service (no-idempotency MCP decline path)")
	dbMock.AssertNumberOfCalls(t, "Begin", 1)
	txMock.AssertCalled(t, "Rollback", mock.Anything)
	txMock.AssertCalled(t, "QueryRow")
}

// TestDeclineMatch_MCPTransactionalPath_IdempotencyCacheHit_NoAnalytics proves GAP AL
// (idempotency cache-hit replay) and GAP AS (no double-fire analytics on replay):
// when GetOrCreateInTx returns cacheHit=true, the handler returns without firing analytics.
func TestDeclineMatch_MCPTransactionalPath_IdempotencyCacheHit_NoAnalytics(t *testing.T) {
	const secret = "mcp-secret-decline-hit"
	const userID = "user-decline-hit"
	const suggID = "sugg-decline-hit"

	svc := &mockTransferSuggestionService{} // never called

	analytics := new(mockHandlerAnalytics)
	// Analytics must NOT be called on a cache-hit replay (GAP AS).

	txMock := new(mockPgxTx)
	txMock.On("Rollback", mock.Anything).Return(nil)

	dbMock := new(mockOperationsDB)
	dbMock.On("Begin", mock.Anything).Return(txMock, nil)

	declined := newTestSuggestionItem(suggID, userID)
	declined.MatchedTransactionID = nil

	idempotencyMock := new(mockOperationsIdempotencyRepo)
	// Return cacheHit=true — simulates an MCP replay with the same idempotency key.
	idempotencyMock.On("GetOrCreateInTx", mock.Anything).
		Run(func(args mock.Arguments) {
			result := args.Get(0).(**domain.TransferSuggestion)
			*result = declined
		}).
		Return(true, nil)

	handler := NewTransferSuggestionHandler(svc, TransferSuggestionHandlerDeps{
		DB:              dbMock,
		MCPAuditRepo:    new(mockOperationsMCPAuditRepo),
		MCPWorkerSecret: secret,
		IdempotencyRepo: idempotencyMock,
	})
	handler.SetAnalytics(analytics)

	input := &DeclineTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: "idem-decline-hit-1"},
		MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: secret},
		ID:                 suggID,
	}

	output, err := handler.DeclineMatch(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Transfer match declined", output.Body.Message)

	// GAP AS: analytics must NOT fire on a cache-hit replay.
	analytics.AssertNotCalled(t, "Capture", userID, phclient.EventTransferSuggestionValidated, mock.Anything)
}

// =============================================================================
// GAP AN: DeclineMatch analytics tests
// =============================================================================

// TestDeclineMatch_FiresEventTransferSuggestionValidated proves that the non-MCP
// decline path fires EventTransferSuggestionValidated with action="declined".
func TestDeclineMatch_FiresEventTransferSuggestionValidated(t *testing.T) {
	userID := "user-decline-analytics"
	suggestionID := "sugg-decline-analytics"

	declined := newTestSuggestionItem(suggestionID, userID)
	declined.MatchedTransactionID = nil

	svc := &mockTransferSuggestionService{
		declineMatchResult: declined,
	}

	analytics := new(mockHandlerAnalytics)
	analytics.On("Capture", userID, phclient.EventTransferSuggestionValidated, mock.MatchedBy(func(props map[string]interface{}) bool {
		return props["action"] == "declined" && props["suggestion_id"] == suggestionID
	})).Return()

	handler := NewTransferSuggestionHandler(svc)
	handler.SetAnalytics(analytics)

	input := &DeclineTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 suggestionID,
	}

	output, err := handler.DeclineMatch(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	analytics.AssertCalled(t, "Capture", userID, phclient.EventTransferSuggestionValidated, mock.Anything)
}

// TestDeclineMatch_NilAnalytics_NoNilPanic proves that nil analytics does not panic.
func TestDeclineMatch_NilAnalytics_NoNilPanic(t *testing.T) {
	userID := "user-decline-nil-analytics"
	suggestionID := "sugg-decline-nil-analytics"

	declined := newTestSuggestionItem(suggestionID, userID)

	svc := &mockTransferSuggestionService{
		declineMatchResult: declined,
	}

	// Analytics intentionally not set (nil by default).
	handler := NewTransferSuggestionHandler(svc)

	input := &DeclineTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		ID:                 suggestionID,
	}

	// Must not panic when analytics is nil.
	output, err := handler.DeclineMatch(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "Transfer match declined", output.Body.Message)
}

// TestConfirmMatch_MCPCacheHit_DoesNotFireAnalytics proves GAP AS for ConfirmMatch:
// when GetOrCreateInTx returns cacheHit=true, analytics is not re-fired.
func TestConfirmMatch_MCPCacheHit_DoesNotFireAnalytics(t *testing.T) {
	const secret = "mcp-secret-confirm-hit"
	const userID = "user-confirm-hit"
	const suggID = "sugg-confirm-hit"

	svc := &mockTransferSuggestionService{} // never called

	analytics := new(mockHandlerAnalytics)
	// Analytics must NOT be called on a cache-hit replay.

	txMock := new(mockPgxTx)
	txMock.On("Rollback", mock.Anything).Return(nil)

	dbMock := new(mockOperationsDB)
	dbMock.On("Begin", mock.Anything).Return(txMock, nil)

	actualAmount := 100.00
	completed := newTestSuggestionItem(suggID, userID)
	completed.Status = domain.SuggestionStatusCompleted
	completed.ActualAmount = &actualAmount

	idempotencyMock := new(mockOperationsIdempotencyRepo)
	idempotencyMock.On("GetOrCreateInTx", mock.Anything).
		Run(func(args mock.Arguments) {
			result := args.Get(0).(**domain.TransferSuggestion)
			*result = completed
		}).
		Return(true, nil)

	handler := NewTransferSuggestionHandler(svc, TransferSuggestionHandlerDeps{
		DB:              dbMock,
		MCPAuditRepo:    new(mockOperationsMCPAuditRepo),
		MCPWorkerSecret: secret,
		IdempotencyRepo: idempotencyMock,
	})
	handler.SetAnalytics(analytics)

	input := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: "idem-confirm-hit-1"},
		MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: secret},
		ID:                 suggID,
		Body:               ConfirmMatchBody{ActualAmount: actualAmount},
	}

	output, err := handler.ConfirmMatch(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, output)

	// GAP AS: analytics must NOT fire on a cache-hit replay.
	analytics.AssertNotCalled(t, "Capture", userID, phclient.EventTransferSuggestionValidated, mock.Anything)
}

func TestTransferSuggestionRouteRegistration(t *testing.T) {
	svc := &mockTransferSuggestionService{}
	_, api := humatest.New(t)
	handler := NewTransferSuggestionHandler(svc)
	RegisterTransferSuggestionRoutes(api, handler)

	// Without auth, should return 401.
	resp := api.Get("/api/v1/transfer-suggestions")
	assert.Equal(t, http.StatusUnauthorized, resp.Code)
}

// =============================================================================
// C10: MCP transactional confirm path (transfer_suggestion.go:192-235)
//
// The existing TestConfirmTransferSuggestionMatch_MCPGuardFallthrough only covers
// the db==nil fallthrough. These tests exercise the actual MCP transactional path
// (IsMCPOrigin==true AND db!=nil AND mcpAuditRepo!=nil), proving the guard fires and
// the handler enters the transactional code branch.
// =============================================================================

// mockOperationsDB is a minimal repository.DB mock for unit-testing the MCP
// transactional path. Only Begin() needs behaviour in C10; others panic if called
// (they are not expected in these tests).
type mockOperationsDB struct {
	mock.Mock
}

func (m *mockOperationsDB) Begin(ctx context.Context) (pgx.Tx, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(pgx.Tx), args.Error(1)
}

func (m *mockOperationsDB) Exec(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
	panic("mockOperationsDB.Exec: not used in MCP path tests")
}

func (m *mockOperationsDB) Query(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
	panic("mockOperationsDB.Query: not used in MCP path tests")
}

func (m *mockOperationsDB) QueryRow(_ context.Context, _ string, _ ...interface{}) pgx.Row {
	panic("mockOperationsDB.QueryRow: not used in MCP path tests")
}

func (m *mockOperationsDB) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	panic("mockOperationsDB.SendBatch: not used in MCP path tests")
}

var _ repository.DB = (*mockOperationsDB)(nil)

// mockOperationsMCPAuditRepo is a minimal repository.MCPAuditRepository mock.
type mockOperationsMCPAuditRepo struct{ mock.Mock }

func (m *mockOperationsMCPAuditRepo) Record(_ context.Context, _ *domain.MCPAuditEntry) error {
	return nil
}

func (m *mockOperationsMCPAuditRepo) RecordInTx(_ context.Context, _ pgx.Tx, _ *domain.MCPAuditEntry) error {
	return nil
}

var _ repository.MCPAuditRepository = (*mockOperationsMCPAuditRepo)(nil)

// mockOperationsIdempotencyRepo is a minimal repository.IdempotencyRepository mock.
type mockOperationsIdempotencyRepo struct{ mock.Mock }

func (m *mockOperationsIdempotencyRepo) GetOrCreate(_ context.Context, _, _, _ string, fn func() (interface{}, error), result interface{}) (bool, error) {
	return false, nil
}

func (m *mockOperationsIdempotencyRepo) GetOrCreateInTx(_ context.Context, _ pgx.Tx, _, _, _ string, fn func() (interface{}, error), result interface{}) (bool, error) {
	args := m.Called(result)
	return args.Bool(0), args.Error(1)
}

func (m *mockOperationsIdempotencyRepo) DeleteExpired(_ context.Context) (int64, error) {
	return 0, nil
}

var _ repository.IdempotencyRepository = (*mockOperationsIdempotencyRepo)(nil)

func TestConfirmMatch_MCPTransactionalPath_BeginError(t *testing.T) {
	// Proves that when IsMCPOrigin==true and db+mcpAuditRepo are non-nil,
	// the handler enters the MCP transactional branch (transfer_suggestion.go:192+)
	// and surfaces db.Begin() errors instead of falling through to the non-MCP service call.
	const secret = "mcp-secret"
	const userID = "user-mcp-tx"
	const suggID = "sugg-mcp-tx"

	// Service mock: if the fallthrough path is mistakenly taken, confirmMatch
	// would call the service and succeed — that would falsify the assertion below.
	svc := &mockTransferSuggestionService{}

	dbMock := new(mockOperationsDB)
	beginErr := errors.New("db unavailable")
	dbMock.On("Begin", mock.Anything).Return(nil, beginErr)

	handler := NewTransferSuggestionHandler(svc, TransferSuggestionHandlerDeps{
		DB:              dbMock,
		MCPAuditRepo:    new(mockOperationsMCPAuditRepo),
		MCPWorkerSecret: secret,
	})

	input := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: secret},
		ID:                 suggID,
		Body:               ConfirmMatchBody{ActualAmount: 100.00},
	}

	output, err := handler.ConfirmMatch(context.Background(), input)

	// The handler must return an error (from db.Begin) — not a successful response.
	// If the non-MCP fallthrough path were taken, the service's nil result would
	// cause a panic or the handler would return nil/no-error, which would fail here.
	assert.Nil(t, output, "must not return a response when db.Begin() fails in the MCP path")
	require.Error(t, err, "handler must propagate the db.Begin error from the MCP transactional path")

	// Begin was called exactly once, proving the MCP guard fired (not the fallthrough).
	dbMock.AssertNumberOfCalls(t, "Begin", 1)
}

// mockPgxTx is a minimal pgx.Tx mock for unit-testing the MCP no-idempotency path.
// Only Rollback and QueryRow carry behaviour; all other methods panic because
// they are not expected during the confirm-match no-idempotency code path.
type mockPgxTx struct{ mock.Mock }

func (m *mockPgxTx) Begin(_ context.Context) (pgx.Tx, error) {
	panic("mockPgxTx.Begin: not used in no-idempotency path test")
}
func (m *mockPgxTx) Commit(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
func (m *mockPgxTx) Rollback(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
func (m *mockPgxTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	panic("mockPgxTx.Exec: not used in no-idempotency path test")
}
func (m *mockPgxTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	panic("mockPgxTx.Query: not used in no-idempotency path test")
}
func (m *mockPgxTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	args := m.Called()
	return args.Get(0).(pgx.Row)
}
func (m *mockPgxTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	panic("mockPgxTx.CopyFrom: not used in no-idempotency path test")
}
func (m *mockPgxTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	panic("mockPgxTx.SendBatch: not used in no-idempotency path test")
}
func (m *mockPgxTx) LargeObjects() pgx.LargeObjects {
	panic("mockPgxTx.LargeObjects: not used in no-idempotency path test")
}
func (m *mockPgxTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	panic("mockPgxTx.Prepare: not used in no-idempotency path test")
}
func (m *mockPgxTx) Conn() *pgx.Conn {
	panic("mockPgxTx.Conn: not used in no-idempotency path test")
}

var _ pgx.Tx = (*mockPgxTx)(nil)

// errScanRow is a pgx.Row that always returns an error on Scan, simulating a
// failed database lookup (e.g. suggestion not found) in unit tests.
type errScanRow struct{ err error }

func (e *errScanRow) Scan(_ ...any) error { return e.err }

var _ pgx.Row = (*errScanRow)(nil)

// TestConfirmMatch_MCPTransactionalPath_NoIdempotency_UsesTxBackedService proves that
// when IdempotencyRepo is nil, the no-idempotency branch (transfer_suggestion.go:228-242)
// is entered: Begin() succeeds, a transaction-backed service is created (not h.service),
// and the handler returns the error from the tx-backed service call.
//
// Key distinction from TestConfirmMatch_MCPTransactionalPath_BeginError:
// - Here Begin() SUCCEEDS and returns a mock tx (proving the post-Begin code path runs).
// - The tx-backed GetByID fails (QueryRow returns ErrNoRows), so the handler errors.
// - h.service.ConfirmTransferSuggestionMatch is NOT called — the tx-backed service is used.
// - Rollback is called via defer (transaction cleanup).
func TestConfirmMatch_MCPTransactionalPath_NoIdempotency_UsesTxBackedService(t *testing.T) {
	const secret = "mcp-secret-no-idem"
	const userID = "user-mcp-no-idem"
	const suggID = "sugg-mcp-no-idem"

	// If the non-MCP fallthrough path is mistakenly taken, h.service would be called
	// and return (nil, nil) → buildOutput(nil) → handler succeeds with no error.
	// Our require.Error below would then fail, proving the fallthrough was taken.
	svc := &mockTransferSuggestionService{} // confirmMatchResult=nil, confirmMatchErr=nil

	txMock := new(mockPgxTx)
	// QueryRow is called by the tx-backed repo's GetByID — return ErrNoRows so the
	// service call fails cleanly without requiring a full result scan.
	txMock.On("QueryRow").Return(&errScanRow{err: pgx.ErrNoRows})
	// Rollback is called via defer rollbackMCPAuditTx, regardless of success/error.
	txMock.On("Rollback", mock.Anything).Return(nil)

	dbMock := new(mockOperationsDB)
	dbMock.On("Begin", mock.Anything).Return(txMock, nil) // Begin SUCCEEDS

	// No IdempotencyRepo — exercises the no-idempotency branch (transfer_suggestion.go:228+).
	handler := NewTransferSuggestionHandler(svc, TransferSuggestionHandlerDeps{
		DB:              dbMock,
		MCPAuditRepo:    new(mockOperationsMCPAuditRepo),
		MCPWorkerSecret: secret,
		// IdempotencyRepo intentionally nil
	})

	input := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: secret},
		ID:                 suggID,
		Body:               ConfirmMatchBody{ActualAmount: 50.00},
	}

	output, err := handler.ConfirmMatch(context.Background(), input)

	// The handler must error because the tx-backed GetByID returns ErrNoRows.
	// If the non-MCP fallthrough path were taken, svc returns (nil, nil) and
	// the handler would succeed (no error), causing require.Error to fail.
	assert.Nil(t, output, "no output when tx-backed service call fails")
	require.Error(t, err, "handler must propagate error from the tx-backed service (no-idempotency MCP path)")

	// Begin succeeded and Rollback was called — proves the MCP tx path was entered
	// and transaction cleanup fired via defer.
	dbMock.AssertNumberOfCalls(t, "Begin", 1)
	txMock.AssertCalled(t, "Rollback", mock.Anything)

	// QueryRow was called on the mock tx — proves the tx-backed repo/service was
	// used, not h.service (which is not a testify mock and was never called).
	txMock.AssertCalled(t, "QueryRow")
}

// =============================================================================
// GAP BB: idempotency key scoped per suggestion ID
// =============================================================================

// idempotencyKeyCaptureMock implements repository.IdempotencyRepository and captures
// the key argument passed to GetOrCreateInTx. It always returns cacheHit=true with
// a preset suggestion so the fn callback is never invoked (no need for a live tx).
type idempotencyKeyCaptureMock struct {
	capturedKey  string
	returnResult *domain.TransferSuggestion
}

func (m *idempotencyKeyCaptureMock) GetOrCreate(_ context.Context, _, _, _ string, _ func() (interface{}, error), _ interface{}) (bool, error) {
	return false, nil
}

func (m *idempotencyKeyCaptureMock) GetOrCreateInTx(_ context.Context, _ pgx.Tx, key, _, _ string, _ func() (interface{}, error), result interface{}) (bool, error) {
	m.capturedKey = key
	// Return cacheHit=true with a preset result so the fn is never called.
	if r, ok := result.(**domain.TransferSuggestion); ok {
		*r = m.returnResult
	}
	return true, nil
}

func (m *idempotencyKeyCaptureMock) DeleteExpired(_ context.Context) (int64, error) { return 0, nil }

var _ repository.IdempotencyRepository = (*idempotencyKeyCaptureMock)(nil)

// TestConfirmMatch_MCP_IdempotencyKeyIncludesSuggestionID_GAPBB verifies GAP BB:
// the idempotency key passed to GetOrCreateInTx must be scoped to the suggestion ID
// (key = input.IdempotencyKey + ":" + input.ID). Without this scoping, two MCP
// requests that share the same base key but target different suggestions collide:
// the second request returns the first suggestion's cached response and never executes.
func TestConfirmMatch_MCP_IdempotencyKeyIncludesSuggestionID_GAPBB(t *testing.T) {
	const secret = "mcp-bb-confirm-secret"
	const userID = "user-bb-confirm"
	const suggID = "sugg-bb-confirm-xyz"
	const baseKey = "base-idem-key-bb-confirm"

	confirmed := newTestSuggestionItem(suggID, userID)
	confirmed.MatchedTransactionID = nil

	svc := &mockTransferSuggestionService{} // never called (cache-hit returned by mock)

	txMock := new(mockPgxTx)
	txMock.On("Rollback", mock.Anything).Return(nil)

	dbMock := new(mockOperationsDB)
	dbMock.On("Begin", mock.Anything).Return(txMock, nil)

	keyCapturer := &idempotencyKeyCaptureMock{returnResult: confirmed}

	handler := NewTransferSuggestionHandler(svc, TransferSuggestionHandlerDeps{
		DB:              dbMock,
		MCPAuditRepo:    new(mockOperationsMCPAuditRepo),
		MCPWorkerSecret: secret,
		IdempotencyRepo: keyCapturer,
	})

	input := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: baseKey},
		MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: secret},
		ID:                 suggID,
		Body:               ConfirmMatchBody{ActualAmount: 100.00},
	}

	_, err := handler.ConfirmMatch(context.Background(), input)
	require.NoError(t, err)

	// GAP BB/BC: the key must include the suggestion ID and the canonical amount.
	// Key format: "baseKey:suggID:amount_2dp" — ensures two different (suggestion, amount)
	// pairs never collide, even when the same base Idempotency-Key header is reused.
	assert.Equal(t, baseKey+":"+suggID+":100.00", keyCapturer.capturedKey,
		"GAP BB/BC: idempotency key must be scoped to (suggestionID, amount_2dp) to prevent "+
			"cross-suggestion and different-amount cache collisions")
}

// TestDeclineMatch_MCP_IdempotencyKeyIncludesSuggestionID_GAPBB mirrors the confirm
// test for the decline path.
func TestDeclineMatch_MCP_IdempotencyKeyIncludesSuggestionID_GAPBB(t *testing.T) {
	const secret = "mcp-bb-decline-secret"
	const userID = "user-bb-decline"
	const suggID = "sugg-bb-decline-xyz"
	const baseKey = "base-idem-key-bb-decline"

	declined := newTestSuggestionItem(suggID, userID)
	declined.MatchedTransactionID = nil

	svc := &mockTransferSuggestionService{}

	txMock := new(mockPgxTx)
	txMock.On("Rollback", mock.Anything).Return(nil)

	dbMock := new(mockOperationsDB)
	dbMock.On("Begin", mock.Anything).Return(txMock, nil)

	keyCapturer := &idempotencyKeyCaptureMock{returnResult: declined}

	handler := NewTransferSuggestionHandler(svc, TransferSuggestionHandlerDeps{
		DB:              dbMock,
		MCPAuditRepo:    new(mockOperationsMCPAuditRepo),
		MCPWorkerSecret: secret,
		IdempotencyRepo: keyCapturer,
	})

	input := &DeclineTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: baseKey},
		MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: secret},
		ID:                 suggID,
	}

	_, err := handler.DeclineMatch(context.Background(), input)
	require.NoError(t, err)

	assert.Equal(t, baseKey+":"+suggID, keyCapturer.capturedKey,
		"GAP BB: decline path must scope the idempotency key to the suggestion ID")
}

// TestConfirmMatch_MCP_IdempotencyKey_IncludesAmount_GAPBC verifies that the confirm
// scoped key encodes the canonical actual_amount so that two requests with the same
// Idempotency-Key + suggestion ID but a DIFFERENT actual_amount produce different cache
// keys (no wrong-cached-body replay).
//
// GAP BC: BB key-folding left a residual hole — same-key / same-op / same-suggestion /
// different actual_amount would return the first cached body instead of re-validating.
// Fix: append ":<amount_2dp>" to the confirm scopedKey; the decline path is unchanged
// (no mutable payload beyond the already-folded suggestion ID).
func TestConfirmMatch_MCP_IdempotencyKey_IncludesAmount_GAPBC(t *testing.T) {
	const secret = "mcp-bc-confirm-secret"
	const userID = "user-bc-confirm"
	const suggID = "sugg-bc-confirm-xyz"
	const baseKey = "base-idem-key-bc-confirm"

	confirmed := newTestSuggestionItem(suggID, userID)
	confirmed.MatchedTransactionID = nil

	svc := &mockTransferSuggestionService{}

	newHandler := func(capturer *idempotencyKeyCaptureMock) *TransferSuggestionHandler {
		txMock := new(mockPgxTx)
		txMock.On("Rollback", mock.Anything).Return(nil)
		dbMock := new(mockOperationsDB)
		dbMock.On("Begin", mock.Anything).Return(txMock, nil)
		return NewTransferSuggestionHandler(svc, TransferSuggestionHandlerDeps{
			DB:              dbMock,
			MCPAuditRepo:    new(mockOperationsMCPAuditRepo),
			MCPWorkerSecret: secret,
			IdempotencyRepo: capturer,
		})
	}

	// First confirm: actual_amount = 100.00
	capturer1 := &idempotencyKeyCaptureMock{returnResult: confirmed}
	h1 := newHandler(capturer1)
	input1 := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: baseKey},
		MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: secret},
		ID:                 suggID,
		Body:               ConfirmMatchBody{ActualAmount: 100.00},
	}
	_, err := h1.ConfirmMatch(context.Background(), input1)
	require.NoError(t, err)

	// Second confirm: same key + same suggestion, but different amount = 99.99
	capturer2 := &idempotencyKeyCaptureMock{returnResult: confirmed}
	h2 := newHandler(capturer2)
	input2 := &ConfirmTransferSuggestionMatchInput{
		AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
		IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: baseKey},
		MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: secret},
		ID:                 suggID,
		Body:               ConfirmMatchBody{ActualAmount: 99.99},
	}
	_, err = h2.ConfirmMatch(context.Background(), input2)
	require.NoError(t, err)

	// GAP BC: the two scoped keys MUST differ (different amounts → different cache entries).
	assert.NotEqual(t, capturer1.capturedKey, capturer2.capturedKey,
		"GAP BC: different actual_amount values must produce different scoped idempotency keys")

	// Same amount → same key (dedup still works).
	capturer3 := &idempotencyKeyCaptureMock{returnResult: confirmed}
	h3 := newHandler(capturer3)
	input3 := *input1 // identical copy
	_, err = h3.ConfirmMatch(context.Background(), &input3)
	require.NoError(t, err)
	assert.Equal(t, capturer1.capturedKey, capturer3.capturedKey,
		"GAP BC: same actual_amount must produce the same scoped key (dedup still works)")
}

// amountCapturingService wraps mockTransferSuggestionService and records the actualAmount
// passed to ConfirmTransferSuggestionMatch. Used by TestConfirmMatch_SubCent_... below.
type amountCapturingService struct {
	mockTransferSuggestionService
	capturedAmount float64
}

func (m *amountCapturingService) ConfirmTransferSuggestionMatch(_ context.Context, _, _ string, actualAmount float64) (*domain.TransferSuggestion, error) {
	m.capturedAmount = actualAmount
	return m.confirmMatchResult, m.confirmMatchErr
}

// TestConfirmMatch_SubCent_IdempotencyRounding_GAPCG verifies GAP CG:
// sub-cent actual_amount values must be rounded to 2dp at the handler level so that:
//  1. The idempotency key fold uses the rounded amount (same key for 250.751 and 250.754).
//  2. The service call (non-MCP path) receives the rounded 2dp amount, not the raw float.
//
// Without the fix: the key fold truncates at 2dp (so 250.751 and 250.754 share the same
// scoped key), but the service call uses the raw float — two clients sending 250.751 and
// 250.754 with the same base key collide on the cache key, and the fn for the first client
// persists a different actual_amount than what the second client's request implies.
func TestConfirmMatch_SubCent_IdempotencyRounding_GAPCG(t *testing.T) {
	const secret = "mcp-cg-secret"
	const userID = "user-cg-confirm"
	const suggID = "sugg-cg-confirm"
	const baseKey = "base-idem-key-cg"

	confirmed := newTestSuggestionItem(suggID, userID)
	confirmed.MatchedTransactionID = nil

	t.Run("key fold uses 2dp rounded amount", func(t *testing.T) {
		// Test via MCP path using idempotencyKeyCaptureMock (cacheHit=true → fn not called).
		// Verifies the scoped key fold at line 238-239.
		svc := &mockTransferSuggestionService{} // never called (cacheHit=true)

		newHandler := func(capturer *idempotencyKeyCaptureMock) *TransferSuggestionHandler {
			txMock := new(mockPgxTx)
			txMock.On("Rollback", mock.Anything).Return(nil)
			dbMock := new(mockOperationsDB)
			dbMock.On("Begin", mock.Anything).Return(txMock, nil)
			return NewTransferSuggestionHandler(svc, TransferSuggestionHandlerDeps{
				DB:              dbMock,
				MCPAuditRepo:    new(mockOperationsMCPAuditRepo),
				MCPWorkerSecret: secret,
				IdempotencyRepo: capturer,
			})
		}

		// 250.751 → key must contain :250.75 (not :250.751 or :250.75100...)
		capturer1 := &idempotencyKeyCaptureMock{returnResult: confirmed}
		_, err := newHandler(capturer1).ConfirmMatch(context.Background(), &ConfirmTransferSuggestionMatchInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
			IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: baseKey},
			MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: secret},
			ID:                 suggID,
			Body:               ConfirmMatchBody{ActualAmount: 250.751},
		})
		require.NoError(t, err)
		assert.Contains(t, capturer1.capturedKey, ":250.75",
			"GAP CG: scoped key must embed the 2dp-rounded amount (250.751 → :250.75)")

		// 250.754 → same 2dp fold → same key as 250.751.
		capturer2 := &idempotencyKeyCaptureMock{returnResult: confirmed}
		_, err = newHandler(capturer2).ConfirmMatch(context.Background(), &ConfirmTransferSuggestionMatchInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
			IdempotencyInput:   inputs.IdempotencyInput{IdempotencyKey: baseKey},
			MCPContextInput:    inputs.MCPContextInput{MCPWorkerSecret: secret},
			ID:                 suggID,
			Body:               ConfirmMatchBody{ActualAmount: 250.754},
		})
		require.NoError(t, err)
		assert.Equal(t, capturer1.capturedKey, capturer2.capturedKey,
			"GAP CG: 250.751 and 250.754 both round to 250.75 — must share the same scoped key")
	})

	t.Run("service call receives 2dp rounded amount (non-MCP path)", func(t *testing.T) {
		// Test via the non-MCP path (line 278) where h.service IS the injected service mock,
		// so amountCapturingService can record what actualAmount was passed.
		svc := &amountCapturingService{}
		svc.confirmMatchResult = confirmed

		// No MCPWorkerSecret dep → IsMCPOrigin returns false → non-MCP path at line 278.
		handler := NewTransferSuggestionHandler(svc)
		_, err := handler.ConfirmMatch(context.Background(), &ConfirmTransferSuggestionMatchInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
			ID:                 suggID,
			Body:               ConfirmMatchBody{ActualAmount: 250.751},
		})
		require.NoError(t, err)
		assert.InDelta(t, 250.75, svc.capturedAmount, 0.0001,
			"GAP CG: non-MCP service call must receive 2dp-rounded amount (250.751 → 250.75)")
	})

	t.Run("IEEE-754 .005 boundary behavior is documented (not a bug)", func(t *testing.T) {
		// math.Round(x*100)/100 uses binary float64 arithmetic. For most sub-cent values
		// this produces the expected 2dp result. However, 1.005 is represented as slightly
		// less than 1.005 in IEEE-754, so math.Round(1.005*100) = math.Round(100.4999...) = 100
		// and the result rounds to 1.00, not 1.01. This is acceptable for our use case:
		// real bank amounts from Plaid are always exact cents, so this branch is only
		// reachable via adversarial or buggy clients sending sub-cent values. The key fold
		// and service call both receive the same rounded value, so idempotency remains coherent.
		svc := &amountCapturingService{}
		svc.confirmMatchResult = confirmed

		handler := NewTransferSuggestionHandler(svc)
		_, err := handler.ConfirmMatch(context.Background(), &ConfirmTransferSuggestionMatchInput{
			AuthenticatedInput: inputs.AuthenticatedInput{UserID: userID},
			ID:                 suggID,
			Body:               ConfirmMatchBody{ActualAmount: 1.005},
		})
		require.NoError(t, err)
		// 1.005 * 100 = 100.4999... in IEEE-754 → rounds to 100 → 1.00 (not 1.01).
		// This test documents the actual behavior so future readers understand it is expected.
		assert.InDelta(t, 1.00, svc.capturedAmount, 0.0001,
			"IEEE-754: 1.005 rounds to 1.00 (not 1.01) — documented expected behavior for sub-cent edge case")
	})
}
