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
	assert.Contains(t, err.Error(), "already processed")
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

// mockAnalytics is a minimal mock for phclient.Analytics.
type mockAnalytics struct {
	mock.Mock
}

func (m *mockAnalytics) Capture(distinctID, event string, properties map[string]interface{}) {
	m.Called(distinctID, event, properties)
}

func (m *mockAnalytics) Identify(distinctID string, properties map[string]interface{}) {
	m.Called(distinctID, properties)
}

func (m *mockAnalytics) Close() {
	m.Called()
}

// Ensure mockAnalytics satisfies the interface at compile time.
var _ phclient.Analytics = (*mockAnalytics)(nil)

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
	analytics := new(mockAnalytics)

	svc := NewTransferSuggestionService(suggestionRepo)
	svc.SetAnalytics(analytics)

	userID := "user-123"
	suggestionID := "suggestion-123"

	suggestion := createTestTransferSuggestion(userID)

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("UpdateStatus", ctx, suggestionID, domain.SuggestionStatusAccepted, (*string)(nil)).Return(nil)
	analytics.On("Capture", userID, phclient.TransferSuggestionAccepted, mock.AnythingOfType("map[string]interface {}")).Return()

	result, err := svc.AcceptSuggestion(ctx, userID, suggestionID, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)
	analytics.AssertExpectations(t)
	suggestionRepo.AssertExpectations(t)
}

func TestTransferSuggestionService_RejectSuggestion_FiresAnalytics(t *testing.T) {
	ctx := context.Background()
	suggestionRepo := new(mockTransferSuggestionRepo)
	analytics := new(mockAnalytics)

	svc := NewTransferSuggestionService(suggestionRepo)
	svc.SetAnalytics(analytics)

	userID := "user-123"
	suggestionID := "suggestion-123"

	suggestion := createTestTransferSuggestion(userID)

	suggestionRepo.On("GetByID", ctx, suggestionID).Return(suggestion, nil)
	suggestionRepo.On("UpdateStatus", ctx, suggestionID, domain.SuggestionStatusRejected, (*string)(nil)).Return(nil)
	analytics.On("Capture", userID, phclient.TransferSuggestionRejected, mock.AnythingOfType("map[string]interface {}")).Return()

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
