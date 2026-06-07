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
