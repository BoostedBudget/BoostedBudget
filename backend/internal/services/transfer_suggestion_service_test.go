package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Repositories for TransferSuggestionService Tests
// =============================================================================

// mockTransferSuggestionRepo is a mock implementation of TransferSuggestionRepository.
type mockTransferSuggestionRepo struct {
	mock.Mock
}

func (m *mockTransferSuggestionRepo) Create(ctx context.Context, suggestion *domain.TransferSuggestion) error {
	args := m.Called(ctx, suggestion)
	return args.Error(0)
}

func (m *mockTransferSuggestionRepo) GetByID(ctx context.Context, id string) (*domain.TransferSuggestion, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.TransferSuggestion), args.Error(1)
}

func (m *mockTransferSuggestionRepo) GetByUserID(ctx context.Context, userID string, status *domain.SuggestionStatus, limit, offset int) ([]*domain.TransferSuggestion, error) {
	args := m.Called(ctx, userID, status, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.TransferSuggestion), args.Error(1)
}

func (m *mockTransferSuggestionRepo) GetPendingByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.TransferSuggestion), args.Error(1)
}

func (m *mockTransferSuggestionRepo) UpdateStatus(ctx context.Context, id string, status domain.SuggestionStatus, userNotes *string) error {
	args := m.Called(ctx, id, status, userNotes)
	return args.Error(0)
}

func (m *mockTransferSuggestionRepo) Update(ctx context.Context, suggestion *domain.TransferSuggestion) error {
	args := m.Called(ctx, suggestion)
	return args.Error(0)
}

func (m *mockTransferSuggestionRepo) ExpirePending(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return int64(args.Int(0)), args.Error(1)
}

func (m *mockTransferSuggestionRepo) CountPendingByUserID(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *mockTransferSuggestionRepo) GetByRuleID(ctx context.Context, ruleID string, limit, offset int) ([]*domain.TransferSuggestion, error) {
	args := m.Called(ctx, ruleID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.TransferSuggestion), args.Error(1)
}

func (m *mockTransferSuggestionRepo) Complete(ctx context.Context, id string, matchedTransactionID *string, actualAmount float64, completionSource string, completedAt time.Time) error {
	args := m.Called(ctx, id, matchedTransactionID, actualAmount, completionSource, completedAt)
	return args.Error(0)
}

func (m *mockTransferSuggestionRepo) GetActiveByUserID(ctx context.Context, userID string) ([]*domain.TransferSuggestion, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.TransferSuggestion), args.Error(1)
}

func (m *mockTransferSuggestionRepo) CompleteRetainingReservation(ctx context.Context, id string, actualAmount float64, completionSource string, completedAt time.Time, expectedTxnID *string) error {
	args := m.Called(ctx, id, actualAmount, completionSource, completedAt, expectedTxnID)
	return args.Error(0)
}

func (m *mockTransferSuggestionRepo) ClearMatchedTransactionID(ctx context.Context, id string, expectedTxnID *string) error {
	args := m.Called(ctx, id, expectedTxnID)
	return args.Error(0)
}

func (m *mockTransferSuggestionRepo) GetByUserIDStatuses(ctx context.Context, userID string, statuses []domain.SuggestionStatus, limit, offset int) ([]*domain.TransferSuggestion, int, error) {
	args := m.Called(ctx, userID, statuses, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Int(1), args.Error(2)
	}
	return args.Get(0).([]*domain.TransferSuggestion), args.Int(1), args.Error(2)
}

func (m *mockTransferSuggestionRepo) CancelAcceptedSuggestion(ctx context.Context, id string, userNotes *string) error {
	args := m.Called(ctx, id, userNotes)
	return args.Error(0)
}

// =============================================================================
// Test Helpers
// =============================================================================

func createTestTransferSuggestion(userID string) *domain.TransferSuggestion {
	fromAcct := "account-1"
	toAcct := "account-2"
	reason := "Savings goal contribution"
	return &domain.TransferSuggestion{
		ID:            "suggestion-123",
		UserID:        userID,
		FromAccountID: &fromAcct,
		ToAccountID:   &toAcct,
		Amount:        100.00,
		CurrencyCode:  "USD",
		Reason:        &reason,
		Source:        domain.SuggestionSourceRule,
		Status:        domain.SuggestionStatusPending,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
}

// =============================================================================
// AcceptSuggestion Tests
// =============================================================================

func TestTransferSuggestionService_AcceptSuggestion_Success(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"
	notes := "Accepted for savings goal"

	suggestion := createTestTransferSuggestion(userID)

	// Get suggestion
	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)

	// Update status
	suggestionRepo.On("UpdateStatus", ctx, suggestionID, domain.SuggestionStatusAccepted, &notes).Return(nil)

	result, err := svc.AcceptSuggestion(ctx, userID, suggestionID, &notes)

	require.NoError(t, err)
	assert.NotNil(t, result)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_AcceptSuggestion_NotFound(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "nonexistent"

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(nil, domain.ErrTransferSuggestionNotFound)

	result, err := svc.AcceptSuggestion(ctx, userID, suggestionID, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, domain.ErrTransferSuggestionNotFound, err)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_AcceptSuggestion_WrongUser(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"

	// Suggestion belongs to different user
	suggestion := createTestTransferSuggestion("other-user")

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)

	result, err := svc.AcceptSuggestion(ctx, userID, suggestionID, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, domain.ErrTransferSuggestionNotFound, err) // Return not found for security
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_AcceptSuggestion_AlreadyProcessed(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.Status = domain.SuggestionStatusAccepted // Already accepted

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)

	result, err := svc.AcceptSuggestion(ctx, userID, suggestionID, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

// AcceptSuggestion also blocks completed and expired — these are new terminal states
// introduced by #972 and their guard coverage is verified below.
func TestTransferSuggestionService_AcceptSuggestion_AlreadyCompleted(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	suggestion := createTestTransferSuggestion("user-123")
	suggestion.Status = domain.SuggestionStatusCompleted

	suggestionRepo.On("GetByID", ctx, "s-1").Return(suggestion, nil)

	result, err := svc.AcceptSuggestion(ctx, "user-123", "s-1", nil)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_AcceptSuggestion_AlreadyExpired(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	suggestion := createTestTransferSuggestion("user-123")
	suggestion.Status = domain.SuggestionStatusExpired

	suggestionRepo.On("GetByID", ctx, "s-2").Return(suggestion, nil)

	result, err := svc.AcceptSuggestion(ctx, "user-123", "s-2", nil)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// RejectSuggestion Tests
// =============================================================================

func TestTransferSuggestionService_RejectSuggestion_Success(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"
	notes := "Not needed"

	suggestion := createTestTransferSuggestion(userID)

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("UpdateStatus", ctx, suggestionID, domain.SuggestionStatusRejected, &notes).Return(nil)

	result, err := svc.RejectSuggestion(ctx, userID, suggestionID, &notes)

	require.NoError(t, err)
	assert.NotNil(t, result)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_RejectSuggestion_NotFound(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "nonexistent"

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(nil, domain.ErrTransferSuggestionNotFound)

	result, err := svc.RejectSuggestion(ctx, userID, suggestionID, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, domain.ErrTransferSuggestionNotFound, err)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// GetPendingSuggestions Tests
// =============================================================================

func TestTransferSuggestionService_GetPendingSuggestions_Success(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"

	suggestions := []*domain.TransferSuggestion{
		createTestTransferSuggestion(userID),
		createTestTransferSuggestion(userID),
	}

	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return(suggestions, nil)

	result, err := svc.GetPendingSuggestions(ctx, userID)

	require.NoError(t, err)
	assert.Len(t, result, 2)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_GetPendingSuggestions_Empty(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"

	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return([]*domain.TransferSuggestion{}, nil)

	result, err := svc.GetPendingSuggestions(ctx, userID)

	require.NoError(t, err)
	assert.Empty(t, result)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_GetPendingSuggestions_Error(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"

	suggestionRepo.On("GetPendingByUserID", ctx, userID).Return(nil, errors.New("database error"))

	result, err := svc.GetPendingSuggestions(ctx, userID)

	assert.Error(t, err)
	assert.Nil(t, result)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// Mock Analytics for TransferSuggestionService Tests
// =============================================================================

// mockGDPRAnalytics is a minimal mock for phclient.GDPRAnalytics.
type mockGDPRAnalytics struct {
	mock.Mock
}

func (m *mockGDPRAnalytics) CaptureIfConsented(ctx context.Context, userID, event string, properties map[string]interface{}) {
	m.Called(ctx, userID, event, properties)
}

// Ensure mockGDPRAnalytics satisfies the interface at compile time.
var _ phclient.GDPRAnalytics = (*mockGDPRAnalytics)(nil)

// =============================================================================
// AlreadyActioned Tests
// =============================================================================

func TestTransferSuggestionService_AcceptSuggestion_AlreadyActioned(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"

	suggestion := createTestTransferSuggestion(userID)

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("UpdateStatus", ctx, suggestionID, domain.SuggestionStatusAccepted, (*string)(nil)).
		Return(domain.ErrTransferSuggestionAlreadyActioned)

	result, err := svc.AcceptSuggestion(ctx, userID, suggestionID, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_RejectSuggestion_AlreadyActioned(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"

	suggestion := createTestTransferSuggestion(userID)

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("UpdateStatus", ctx, suggestionID, domain.SuggestionStatusRejected, (*string)(nil)).
		Return(domain.ErrTransferSuggestionAlreadyActioned)

	result, err := svc.RejectSuggestion(ctx, userID, suggestionID, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// Analytics Tests
// =============================================================================

func TestTransferSuggestionService_AcceptSuggestion_FiresAnalytics(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	analytics := new(mockGDPRAnalytics)

	svc := NewTransferSuggestionService(suggestionRepo)
	svc.SetGDPRAnalytics(analytics)

	userID := "user-123"
	suggestionID := "suggestion-123"

	suggestion := createTestTransferSuggestion(userID)

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("UpdateStatus", ctx, suggestionID, domain.SuggestionStatusAccepted, (*string)(nil)).Return(nil)
	analytics.On("CaptureIfConsented", mock.Anything, userID, phclient.TransferSuggestionAccepted, mock.AnythingOfType("map[string]interface {}")).Return()

	result, err := svc.AcceptSuggestion(ctx, userID, suggestionID, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	analytics.AssertExpectations(t)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_RejectSuggestion_FiresAnalytics(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	analytics := new(mockGDPRAnalytics)

	svc := NewTransferSuggestionService(suggestionRepo)
	svc.SetGDPRAnalytics(analytics)

	userID := "user-123"
	suggestionID := "suggestion-123"

	suggestion := createTestTransferSuggestion(userID)

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("UpdateStatus", ctx, suggestionID, domain.SuggestionStatusRejected, (*string)(nil)).Return(nil)
	analytics.On("CaptureIfConsented", mock.Anything, userID, phclient.TransferSuggestionRejected, mock.AnythingOfType("map[string]interface {}")).Return()

	result, err := svc.RejectSuggestion(ctx, userID, suggestionID, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	analytics.AssertExpectations(t)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_AcceptSuggestion_NoAnalyticsWhenNil(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	// No analytics set — should not panic.
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"

	suggestion := createTestTransferSuggestion(userID)

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("UpdateStatus", ctx, suggestionID, domain.SuggestionStatusAccepted, (*string)(nil)).Return(nil)

	result, err := svc.AcceptSuggestion(ctx, userID, suggestionID, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_RejectSuggestion_NoAnalyticsWhenNil(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	// No analytics set — should not panic.
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"

	suggestion := createTestTransferSuggestion(userID)

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("UpdateStatus", ctx, suggestionID, domain.SuggestionStatusRejected, (*string)(nil)).Return(nil)

	result, err := svc.RejectSuggestion(ctx, userID, suggestionID, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// ConfirmTransferSuggestionMatch Tests (Issue #972: C3)
// =============================================================================

func TestTransferSuggestionService_ConfirmMatch_PendingSuccess(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"
	actualAmount := 150.00
	reservedTxnID := "txn-pending-success"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.Status = domain.SuggestionStatusPending
	// GAP AU: fuzzy confirm requires a non-nil reservation (ReserveMatchedTransaction sets this)
	suggestion.MatchedTransactionID = &reservedTxnID

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// GAP AK: now uses CompleteRetainingReservation (no matched_transaction_id argument)
	// GAP AR: passes the observed MatchedTransactionID
	suggestionRepo.On("CompleteRetainingReservation", ctx, suggestionID, actualAmount, "user_confirmed", mock.AnythingOfType("time.Time"),
		mock.MatchedBy(func(s *string) bool { return s != nil && *s == reservedTxnID }),
	).Return(nil)

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, userID, suggestionID, actualAmount)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, domain.SuggestionStatusCompleted, result.Status)
	assert.NotNil(t, result.ActualAmount)
	assert.Equal(t, actualAmount, *result.ActualAmount)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_ConfirmMatch_AcceptedSuccess(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"
	actualAmount := 200.00
	reservedTxnID := "txn-accepted-success"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.Status = domain.SuggestionStatusAccepted
	// GAP AU: fuzzy confirm requires a non-nil reservation
	suggestion.MatchedTransactionID = &reservedTxnID

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// GAP AK: now uses CompleteRetainingReservation
	// GAP AR: passes the observed MatchedTransactionID
	suggestionRepo.On("CompleteRetainingReservation", ctx, suggestionID, actualAmount, "user_confirmed", mock.AnythingOfType("time.Time"),
		mock.MatchedBy(func(s *string) bool { return s != nil && *s == reservedTxnID }),
	).Return(nil)

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, userID, suggestionID, actualAmount)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, domain.SuggestionStatusCompleted, result.Status)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_ConfirmMatch_NotFound(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	suggestionRepo.On("GetByID", ctx, "nonexistent").Return(nil, domain.ErrTransferSuggestionNotFound)

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, "user-123", "nonexistent", 100.00)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, domain.ErrTransferSuggestionNotFound, err)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_ConfirmMatch_WrongUser(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"

	// Suggestion belongs to different user
	suggestion := createTestTransferSuggestion("other-user")
	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, userID, suggestionID, 100.00)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, domain.ErrTransferSuggestionNotFound, err) // security: don't reveal existence
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_ConfirmMatch_AlreadyCompleted(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.Status = domain.SuggestionStatusCompleted

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, userID, suggestionID, 100.00)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_ConfirmMatch_AlreadyRejected(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.Status = domain.SuggestionStatusRejected

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, userID, suggestionID, 100.00)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_ConfirmMatch_AlreadyExpired(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.Status = domain.SuggestionStatusExpired

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, userID, suggestionID, 100.00)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_ConfirmMatch_RepoCompleteError(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	suggestionID := "suggestion-123"
	reservedTxnID := "txn-repo-error"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.Status = domain.SuggestionStatusPending
	// GAP AU: fuzzy confirm requires a non-nil reservation to reach the repo call
	suggestion.MatchedTransactionID = &reservedTxnID

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// GAP AK: now uses CompleteRetainingReservation
	// GAP AR: passes the observed MatchedTransactionID
	suggestionRepo.On("CompleteRetainingReservation", ctx, suggestionID, 100.00, "user_confirmed", mock.AnythingOfType("time.Time"),
		mock.MatchedBy(func(s *string) bool { return s != nil && *s == reservedTxnID }),
	).Return(errors.New("db error"))

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, userID, suggestionID, 100.00)

	assert.Error(t, err)
	assert.Nil(t, result)
	suggestionRepo.AssertExpectations(t)
}

// TestTransferSuggestionService_ConfirmMatch_RetainsReservedMatchedTransactionID proves
// GAP AK fix: when a suggestion has a reserved matched_transaction_id, ConfirmTransferSuggestionMatch
// now calls CompleteRetainingReservation (which omits matched_transaction_id from the SET clause)
// so the DB retains whatever is currently there. This avoids the race where a stale-read id
// overwrites a reclaimed reservation after ClearStaleReservations ran between the read and write.
func TestTransferSuggestionService_ConfirmMatch_RetainsReservedMatchedTransactionID(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-retain"
	suggestionID := "suggestion-retain"
	actualAmount := 175.00
	reservedTxnID := "txn-reserved-by-correlation"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.ID = suggestionID
	suggestion.Status = domain.SuggestionStatusPending
	// Simulate a reservation set by CorrelateForUser fuzzy branch
	suggestion.MatchedTransactionID = &reservedTxnID

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// GAP AK: CompleteRetainingReservation is called instead of Complete — no matched_transaction_id arg.
	// The DB retains the reservation; the partial unique index continues protecting the inflow.
	// GAP AR: the service passes the observed MatchedTransactionID so the WHERE clause enforces that the
	// reservation has not been reclaimed (IS NOT DISTINCT FROM predicate).
	suggestionRepo.On("CompleteRetainingReservation", ctx, suggestionID,
		actualAmount, "user_confirmed", mock.AnythingOfType("time.Time"),
		mock.MatchedBy(func(s *string) bool { return s != nil && *s == reservedTxnID }),
	).Return(nil)

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, userID, suggestionID, actualAmount)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, domain.SuggestionStatusCompleted, result.Status)
	assert.Equal(t, actualAmount, *result.ActualAmount)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// GAP AK: ConfirmTransferSuggestionMatch uses CompleteRetainingReservation (not Complete)
// =============================================================================

// TestTransferSuggestionService_ConfirmMatch_UsesRetainReservation proves GAP AK fix:
// ConfirmTransferSuggestionMatch must call CompleteRetainingReservation (which omits
// matched_transaction_id from the SET clause) rather than Complete (which overwrites it).
// This avoids the race where a stale-read id clobbers a reclaimed reservation.
// GAP AU: a non-nil reservation is required to reach the repo; the nil guard fires first
// if MatchedTransactionID is nil.
func TestTransferSuggestionService_ConfirmMatch_UsesRetainReservation(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-ak"
	suggestionID := "suggestion-ak"
	actualAmount := 250.00
	reservedTxnID := "txn-ak-retain"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.ID = suggestionID
	suggestion.Status = domain.SuggestionStatusPending
	// GAP AU: must have a non-nil reservation; fuzzy flow always sets this via ReserveMatchedTransaction
	suggestion.MatchedTransactionID = &reservedTxnID

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// GAP AK: CompleteRetainingReservation must be called (not Complete)
	// GAP AR: passes the observed MatchedTransactionID
	suggestionRepo.On("CompleteRetainingReservation", ctx, suggestionID, actualAmount, "user_confirmed", mock.AnythingOfType("time.Time"),
		mock.MatchedBy(func(s *string) bool { return s != nil && *s == reservedTxnID }),
	).Return(nil)

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, userID, suggestionID, actualAmount)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, domain.SuggestionStatusCompleted, result.Status)
	assert.Equal(t, actualAmount, *result.ActualAmount)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// GAP AI: DeclineTransferSuggestionMatch tests
// =============================================================================

// TestDeclineTransferSuggestionMatch_ClearsReservation verifies that declining
// a pending suggestion with a reservation sets matched_transaction_id = NULL.
func TestDeclineTransferSuggestionMatch_ClearsReservation(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-ai"
	suggestionID := "suggestion-ai"
	reservedTxnID := "txn-reserved"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.ID = suggestionID
	suggestion.Status = domain.SuggestionStatusPending
	suggestion.MatchedTransactionID = &reservedTxnID

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// GAP AV: passes observed MatchedTransactionID to the repo.
	suggestionRepo.On("ClearMatchedTransactionID", ctx, suggestionID,
		mock.MatchedBy(func(s *string) bool { return s != nil && *s == reservedTxnID }),
	).Return(nil)

	result, err := svc.DeclineTransferSuggestionMatch(ctx, userID, suggestionID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.MatchedTransactionID, "reservation should be cleared")
	suggestionRepo.AssertExpectations(t)
}

// TestDeclineTransferSuggestionMatch_OwnershipEnforced verifies that a user cannot
// decline another user's suggestion (returns not-found for security).
func TestDeclineTransferSuggestionMatch_OwnershipEnforced(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	suggestion := createTestTransferSuggestion("other-user")

	suggestionRepo.On("GetByID", ctx, "suggestion-ai").Return(suggestion, nil)

	result, err := svc.DeclineTransferSuggestionMatch(ctx, "user-ai", "suggestion-ai")

	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionNotFound)
	suggestionRepo.AssertExpectations(t)
}

// TestDeclineTransferSuggestionMatch_TerminalStatusReturnsActioned verifies that
// a completed suggestion cannot have its reservation cleared.
func TestDeclineTransferSuggestionMatch_TerminalStatusReturnsActioned(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-ai"
	suggestion := createTestTransferSuggestion(userID)
	suggestion.Status = domain.SuggestionStatusCompleted

	suggestionRepo.On("GetByID", ctx, suggestion.ID).Return(suggestion, nil)

	result, err := svc.DeclineTransferSuggestionMatch(ctx, userID, suggestion.ID)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

// TestDeclineTransferSuggestionMatch_AcceptedSuggestion verifies that an accepted
// suggestion (non-terminal) can still have its reservation cleared via decline.
func TestDeclineTransferSuggestionMatch_AcceptedSuggestion(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-ai"
	suggestionID := "suggestion-accepted"
	reservedTxnID := "txn-accepted"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.ID = suggestionID
	suggestion.Status = domain.SuggestionStatusAccepted
	suggestion.MatchedTransactionID = &reservedTxnID

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// GAP AV: passes observed MatchedTransactionID to the repo.
	suggestionRepo.On("ClearMatchedTransactionID", ctx, suggestionID,
		mock.MatchedBy(func(s *string) bool { return s != nil && *s == reservedTxnID }),
	).Return(nil)

	result, err := svc.DeclineTransferSuggestionMatch(ctx, userID, suggestionID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.MatchedTransactionID)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// GAP AQ: ClearMatchedTransactionID propagates errors (TOCTOU false-success fix)
// =============================================================================

// TestDeclineTransferSuggestionMatch_ClearPropagatesAlreadyActioned proves GAP AQ fix:
// when ClearMatchedTransactionID returns ErrTransferSuggestionAlreadyActioned (because the
// suggestion went terminal between the service's GetByID read and the repo's UPDATE write),
// DeclineTransferSuggestionMatch propagates that error rather than falsely returning success.
// This simulates the TOCTOU window where the DB UPDATE matched 0 rows.
func TestDeclineTransferSuggestionMatch_ClearPropagatesAlreadyActioned(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-aq"
	suggestionID := "suggestion-aq"
	reservedTxnID := "txn-aq"

	// Service reads the suggestion as pending (before the race window closes)
	suggestion := createTestTransferSuggestion(userID)
	suggestion.ID = suggestionID
	suggestion.Status = domain.SuggestionStatusPending
	suggestion.MatchedTransactionID = &reservedTxnID

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// Simulate TOCTOU: the suggestion went terminal between the service's GetByID and the
	// repo's UPDATE execution — the repo now returns AlreadyActioned (0 rows affected).
	// GAP AV: service passes the observed MatchedTransactionID.
	suggestionRepo.On("ClearMatchedTransactionID", ctx, suggestionID,
		mock.MatchedBy(func(s *string) bool { return s != nil && *s == reservedTxnID }),
	).Return(domain.ErrTransferSuggestionAlreadyActioned)

	result, err := svc.DeclineTransferSuggestionMatch(ctx, userID, suggestionID)

	// GAP AQ: must return the error, not nil (no false success)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

// TestDeclineTransferSuggestionMatch_ClearPropagatesNotFound verifies that a not-found
// error from ClearMatchedTransactionID is also propagated correctly.
func TestDeclineTransferSuggestionMatch_ClearPropagatesNotFound(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-aq2"
	suggestionID := "suggestion-aq2"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.ID = suggestionID
	suggestion.Status = domain.SuggestionStatusPending

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("ClearMatchedTransactionID", ctx, suggestionID, (*string)(nil)).Return(domain.ErrTransferSuggestionNotFound)

	result, err := svc.DeclineTransferSuggestionMatch(ctx, userID, suggestionID)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionNotFound)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// GAP AV: DeclineTransferSuggestionMatch passes observed reservation to repo
// =============================================================================

// TestDeclineTransferSuggestionMatch_PassesObservedReservationToRepo proves GAP AV fix:
// DeclineTransferSuggestionMatch must pass the observed MatchedTransactionID to
// ClearMatchedTransactionID so the IS NOT DISTINCT FROM predicate can detect if a
// newer reservation was set between the service's GetByID read and the repo write.
// A stale decline carrying T must not release a newer reservation T2 on the same suggestion.
func TestDeclineTransferSuggestionMatch_PassesObservedReservationToRepo(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-av"
	suggestionID := "suggestion-av"
	reservedTxnID := "txn-123"

	suggestion := createTestTransferSuggestion(userID)
	suggestion.ID = suggestionID
	suggestion.Status = domain.SuggestionStatusPending
	suggestion.MatchedTransactionID = &reservedTxnID

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// GAP AV: must pass the observed MatchedTransactionID (not nil) to the repo.
	suggestionRepo.On("ClearMatchedTransactionID", ctx, suggestionID,
		mock.MatchedBy(func(s *string) bool { return s != nil && *s == reservedTxnID }),
	).Return(nil)

	result, err := svc.DeclineTransferSuggestionMatch(ctx, userID, suggestionID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.MatchedTransactionID, "reservation should be cleared in service result")
	suggestionRepo.AssertExpectations(t)
}

// TestDeclineTransferSuggestionMatch_WrongReservation_ReturnsAlreadyActioned proves that
// when repo.ClearMatchedTransactionID returns ErrTransferSuggestionAlreadyActioned (because
// the IS NOT DISTINCT FROM predicate found a different reservation in the DB — a newer
// sync claimed T2 after the stale clear), DeclineTransferSuggestionMatch propagates that
// error cleanly. The stale decline must not silently succeed.
func TestDeclineTransferSuggestionMatch_WrongReservation_ReturnsAlreadyActioned(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-av2"
	suggestionID := "suggestion-av2"
	staleTxnID := "txn-stale"

	// Service observes stale reservation T at read time.
	suggestion := createTestTransferSuggestion(userID)
	suggestion.ID = suggestionID
	suggestion.Status = domain.SuggestionStatusPending
	suggestion.MatchedTransactionID = &staleTxnID

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// Simulate TOCTOU: DB now holds T2 (a newer reservation), not T.
	// IS NOT DISTINCT FROM predicate → 0 rows → repo returns AlreadyActioned.
	suggestionRepo.On("ClearMatchedTransactionID", ctx, suggestionID,
		mock.MatchedBy(func(s *string) bool { return s != nil && *s == staleTxnID }),
	).Return(domain.ErrTransferSuggestionAlreadyActioned)

	result, err := svc.DeclineTransferSuggestionMatch(ctx, userID, suggestionID)

	// GAP AV: must propagate AlreadyActioned (stale decline must not silently succeed)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// GAP AR: ConfirmTransferSuggestionMatch TOCTOU guard (IS NOT DISTINCT FROM)
// =============================================================================

// TestConfirmTransferSuggestionMatch_TOCTOUReservationReclaimed proves GAP AR fix:
// when CompleteRetainingReservation returns ErrTransferSuggestionAlreadyActioned (because the
// inflow reservation was reclaimed by another suggestion between the service's GetByID read
// and the repo's UPDATE write), ConfirmTransferSuggestionMatch surfaces that error rather than
// silently double-counting the transfer in the aggregate stat.
func TestConfirmTransferSuggestionMatch_TOCTOUReservationReclaimed(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-ar"
	suggestionID := "suggestion-ar"
	reservedTxnID := "txn-ar"
	actualAmount := 200.00

	// Service reads the suggestion as pending with reservation T
	suggestion := createTestTransferSuggestion(userID)
	suggestion.ID = suggestionID
	suggestion.Status = domain.SuggestionStatusPending
	suggestion.MatchedTransactionID = &reservedTxnID

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// Simulate TOCTOU: ClearStaleReservations ran and another suggestion claimed T —
	// IS NOT DISTINCT FROM predicate returns 0 rows → repo returns AlreadyActioned.
	suggestionRepo.On("CompleteRetainingReservation", ctx, suggestionID,
		actualAmount, "user_confirmed", mock.AnythingOfType("time.Time"),
		mock.MatchedBy(func(s *string) bool { return s != nil && *s == reservedTxnID }),
	).Return(domain.ErrTransferSuggestionAlreadyActioned)

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, userID, suggestionID, actualAmount)

	// GAP AR: must return the error so the confirm is rejected (not double-counted)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

// TestConfirmTransferSuggestionMatch_PassesObservedReservationToRepo proves that
// ConfirmTransferSuggestionMatch correctly passes the observed MatchedTransactionID to
// CompleteRetainingReservation so the IS NOT DISTINCT FROM guard can detect reclamation.
// When MatchedTransactionID is nil (no reservation), nil is passed — but GAP AU now
// rejects at the service layer before reaching the repo, so this test verifies the
// rejection behaviour (not the old pass-through behaviour).
func TestConfirmTransferSuggestionMatch_PassesObservedNilReservationToRepo(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-ar2"
	suggestionID := "suggestion-ar2"
	actualAmount := 100.00

	suggestion := createTestTransferSuggestion(userID)
	suggestion.ID = suggestionID
	suggestion.Status = domain.SuggestionStatusPending
	suggestion.MatchedTransactionID = nil

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// GAP AU: nil reservation is now rejected at the service layer — repo is never called.

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, userID, suggestionID, actualAmount)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// GAP AU: ConfirmTransferSuggestionMatch nil-reservation guard (stale-cleared prompt)
// =============================================================================

// TestConfirmTransferSuggestionMatch_StaleReservation_RejectsWithAlreadyActioned proves
// GAP AU fix: when MatchedTransactionID is nil at confirm time (the daily
// ClearStaleReservations worker has already nulled it), ConfirmTransferSuggestionMatch must
// reject the confirm with ErrTransferSuggestionAlreadyActioned — before even reaching
// CompleteRetainingReservation — so the headline "users moved $X" stat cannot be
// double-counted by a stale fuzzy prompt.
//
// No legitimate confirm-with-nil path exists in the fuzzy flow: ReserveMatchedTransaction
// always sets a non-nil reservation before the fuzzy prompt fires. A nil reservation at
// confirm time means the prompt is stale.
func TestConfirmTransferSuggestionMatch_StaleReservation_RejectsWithAlreadyActioned(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-au"
	suggestionID := "suggestion-au"
	actualAmount := 300.00

	// Accepted suggestion whose reservation was nulled by ClearStaleReservations
	suggestion := createTestTransferSuggestion(userID)
	suggestion.ID = suggestionID
	suggestion.Status = domain.SuggestionStatusAccepted
	suggestion.MatchedTransactionID = nil // stale-cleared by daily worker

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	// CompleteRetainingReservation must NOT be called — the nil guard fires first.

	result, err := svc.ConfirmTransferSuggestionMatch(ctx, userID, suggestionID, actualAmount)

	// GAP AU: must reject to prevent double-count
	assert.Nil(t, result)
	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// GetSuggestionsByStatus Tests (#1267 Boosts page)
// =============================================================================

func TestTransferSuggestionService_GetSuggestionsByStatus_DefaultsToPending(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-123"
	pending := createTestTransferSuggestion(userID)
	pending.Status = domain.SuggestionStatusPending

	// nil status slice → pending only semantics: delegates to GetByUserIDStatuses with nil
	suggestionRepo.On("GetByUserIDStatuses", ctx, userID,
		[]domain.SuggestionStatus(nil), 20, 0).
		Return([]*domain.TransferSuggestion{pending}, 1, nil)

	items, total, err := svc.GetSuggestionsByStatus(ctx, userID, nil, 20, 0)

	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, items, 1)
	assert.Equal(t, domain.SuggestionStatusPending, items[0].Status)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_GetSuggestionsByStatus_FiltersAndPaginates(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-456"
	statuses := []domain.SuggestionStatus{
		domain.SuggestionStatusCompleted,
		domain.SuggestionStatusRejected,
	}
	completed := createTestTransferSuggestion(userID)
	completed.ID = "sugg-completed"
	completed.Status = domain.SuggestionStatusCompleted

	suggestionRepo.On("GetByUserIDStatuses", ctx, userID, statuses, 10, 5).
		Return([]*domain.TransferSuggestion{completed}, 15, nil)

	items, total, err := svc.GetSuggestionsByStatus(ctx, userID, statuses, 10, 5)

	require.NoError(t, err)
	assert.Equal(t, 15, total)
	require.Len(t, items, 1)
	assert.Equal(t, "sugg-completed", items[0].ID)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_GetSuggestionsByStatus_PropagatesRepoError(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)

	svc := NewTransferSuggestionService(suggestionRepo)

	userID := "user-789"
	statuses := []domain.SuggestionStatus{domain.SuggestionStatusAccepted}
	repoErr := errors.New("db connection error")

	suggestionRepo.On("GetByUserIDStatuses", ctx, userID, statuses, 20, 0).
		Return(nil, 0, repoErr)

	items, total, err := svc.GetSuggestionsByStatus(ctx, userID, statuses, 20, 0)

	assert.ErrorIs(t, err, repoErr)
	assert.Nil(t, items)
	assert.Equal(t, 0, total)
	suggestionRepo.AssertExpectations(t)
}

// =============================================================================
// CancelAcceptedSuggestion Tests
// =============================================================================

// TestTransferSuggestionService_CancelAccepted_Success proves that cancelling an
// accepted suggestion transitions it to rejected and returns the updated suggestion.
func TestTransferSuggestionService_CancelAccepted_Success(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"
	suggestionID := "sugg-abc"
	now := time.Now()

	suggestion := &domain.TransferSuggestion{
		ID:        suggestionID,
		UserID:    userID,
		Amount:    200.00,
		Status:    domain.SuggestionStatusAccepted,
		CreatedAt: now,
		UpdatedAt: now,
	}

	suggestionRepo := &mockTransferSuggestionRepo{}
	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("CancelAcceptedSuggestion", ctx, suggestionID, (*string)(nil)).Return(nil)

	svc := NewTransferSuggestionService(suggestionRepo)
	result, err := svc.CancelAcceptedSuggestion(ctx, userID, suggestionID, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, domain.SuggestionStatusRejected, result.Status)
	suggestionRepo.AssertExpectations(t)
}

// TestTransferSuggestionService_CancelAccepted_WithNotes proves notes are preserved.
func TestTransferSuggestionService_CancelAccepted_WithNotes(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"
	suggestionID := "sugg-abc"
	notes := "Changed my mind"
	now := time.Now()

	suggestion := &domain.TransferSuggestion{
		ID:        suggestionID,
		UserID:    userID,
		Amount:    200.00,
		Status:    domain.SuggestionStatusAccepted,
		CreatedAt: now,
		UpdatedAt: now,
	}

	suggestionRepo := &mockTransferSuggestionRepo{}
	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("CancelAcceptedSuggestion", ctx, suggestionID, &notes).Return(nil)

	svc := NewTransferSuggestionService(suggestionRepo)
	result, err := svc.CancelAcceptedSuggestion(ctx, userID, suggestionID, &notes)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, domain.SuggestionStatusRejected, result.Status)
	assert.Equal(t, &notes, result.UserNotes)
	suggestionRepo.AssertExpectations(t)
}

// TestTransferSuggestionService_CancelAccepted_WrongUser proves ownership is enforced.
func TestTransferSuggestionService_CancelAccepted_WrongUser(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"
	otherUserID := "user-xyz"
	suggestionID := "sugg-abc"

	suggestion := &domain.TransferSuggestion{
		ID:     suggestionID,
		UserID: otherUserID, // different user
		Status: domain.SuggestionStatusAccepted,
	}

	suggestionRepo := &mockTransferSuggestionRepo{}
	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)

	svc := NewTransferSuggestionService(suggestionRepo)
	result, err := svc.CancelAcceptedSuggestion(ctx, userID, suggestionID, nil)

	assert.ErrorIs(t, err, domain.ErrTransferSuggestionNotFound)
	assert.Nil(t, result)
	suggestionRepo.AssertExpectations(t)
}

// TestTransferSuggestionService_CancelAccepted_AlreadyCompleted returns AlreadyActioned.
func TestTransferSuggestionService_CancelAccepted_AlreadyCompleted(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"
	suggestionID := "sugg-abc"

	suggestion := &domain.TransferSuggestion{
		ID:     suggestionID,
		UserID: userID,
		Status: domain.SuggestionStatusCompleted,
	}

	suggestionRepo := &mockTransferSuggestionRepo{}
	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)

	svc := NewTransferSuggestionService(suggestionRepo)
	result, err := svc.CancelAcceptedSuggestion(ctx, userID, suggestionID, nil)

	assert.ErrorIs(t, err, domain.ErrTransferSuggestionAlreadyActioned)
	assert.Nil(t, result)
	suggestionRepo.AssertExpectations(t)
}

// TestTransferSuggestionService_CancelAccepted_PendingAlsoTransitions proves pending
// suggestions (rare edge case) can also be cancelled via this path.
func TestTransferSuggestionService_CancelAccepted_PendingAlsoTransitions(t *testing.T) {
	ctx := context.Background()
	userID := "user-abc"
	suggestionID := "sugg-pending"
	now := time.Now()

	suggestion := &domain.TransferSuggestion{
		ID:        suggestionID,
		UserID:    userID,
		Amount:    50.00,
		Status:    domain.SuggestionStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	suggestionRepo := &mockTransferSuggestionRepo{}
	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("CancelAcceptedSuggestion", ctx, suggestionID, (*string)(nil)).Return(nil)

	svc := NewTransferSuggestionService(suggestionRepo)
	result, err := svc.CancelAcceptedSuggestion(ctx, userID, suggestionID, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, domain.SuggestionStatusRejected, result.Status)
	suggestionRepo.AssertExpectations(t)
}
