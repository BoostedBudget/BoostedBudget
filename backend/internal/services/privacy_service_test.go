package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockPrivacyRepository is a mock implementation of PrivacyRepository
type MockPrivacyRepository struct {
	mock.Mock
}

func (m *MockPrivacyRepository) GetByUserID(ctx context.Context, userID string) (*domain.PrivacySettings, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PrivacySettings), args.Error(1)
}

func (m *MockPrivacyRepository) Create(ctx context.Context, settings *domain.PrivacySettings) error {
	args := m.Called(ctx, settings)
	return args.Error(0)
}

func (m *MockPrivacyRepository) CreateTx(ctx context.Context, tx pgx.Tx, settings *domain.PrivacySettings) error {
	args := m.Called(ctx, tx, settings)
	return args.Error(0)
}

func (m *MockPrivacyRepository) Update(ctx context.Context, settings *domain.PrivacySettings) error {
	args := m.Called(ctx, settings)
	return args.Error(0)
}

// TestGetPrivacySettings_Existing tests retrieving existing privacy settings
func TestGetPrivacySettings_Existing(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	service := NewPrivacyService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	existingSettings := &domain.PrivacySettings{
		ID:                     uuid.New().String(),
		UserID:                 userID,
		GDPRNecessaryConsent:   true,
		GDPRAnalyticsConsent:   true,
		GDPRConsentTimestamp:   time.Now(),
		MarketingEmailsEnabled: false,
		ProductUpdatesEnabled:  true,
		SecurityAlertsEnabled:  true,
		DataSharingEnabled:     false,
		CreatedAt:              time.Now(),
		UpdatedAt:              time.Now(),
	}

	// Mock expectations
	mockRepo.On("GetByUserID", ctx, userID).Return(existingSettings, nil)

	// Execute
	settings, err := service.GetPrivacySettings(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, settings)
	assert.Equal(t, userID, settings.UserID)
	assert.True(t, settings.GDPRNecessaryConsent)
	assert.True(t, settings.GDPRAnalyticsConsent)
	mockRepo.AssertExpectations(t)
}

// TestGetPrivacySettings_NotFound_CreatesDefaults tests creating default settings when none exist
func TestGetPrivacySettings_NotFound_CreatesDefaults(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	service := NewPrivacyService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()

	// Mock expectations
	mockRepo.On("GetByUserID", ctx, userID).Return(nil, repository.ErrPrivacySettingsNotFound)
	mockRepo.On("Create", ctx, mock.MatchedBy(func(s *domain.PrivacySettings) bool {
		return s.UserID == userID &&
			s.GDPRNecessaryConsent == true && // Must be true by default
			s.GDPRAnalyticsConsent == false && // Opt-in (default false)
			s.MarketingEmailsEnabled == false && // Opt-in
			s.ProductUpdatesEnabled == true && // Opt-out (default true)
			s.SecurityAlertsEnabled == true // Opt-out (default true)
	})).Return(nil)

	// Execute
	settings, err := service.GetPrivacySettings(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, settings)
	assert.Equal(t, userID, settings.UserID)
	assert.True(t, settings.GDPRNecessaryConsent)  // GDPR compliance check
	assert.False(t, settings.GDPRAnalyticsConsent) // Opt-in model
	mockRepo.AssertExpectations(t)
	mockRepo.AssertCalled(t, "Create", ctx, mock.Anything)
}

// TestGetPrivacySettings_CreateError tests error handling when creating defaults fails
func TestGetPrivacySettings_CreateError(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	service := NewPrivacyService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	createError := errors.New("database error")

	// Mock expectations
	mockRepo.On("GetByUserID", ctx, userID).Return(nil, repository.ErrPrivacySettingsNotFound)
	mockRepo.On("Create", ctx, mock.Anything).Return(createError)

	// Execute
	settings, err := service.GetPrivacySettings(ctx, userID)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, createError, err)
	assert.Nil(t, settings)
	mockRepo.AssertExpectations(t)
}

// TestUpdatePrivacySettings_Success tests successful update of privacy settings
func TestUpdatePrivacySettings_Success(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	service := NewPrivacyService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	existingSettings := domain.NewPrivacySettings(userID)
	existingSettings.ID = uuid.New().String()

	updateRequest := &domain.UpdatePrivacySettingsRequest{
		GDPRAnalyticsConsent:   boolPtr(true),
		MarketingEmailsEnabled: boolPtr(true),
	}

	// Mock expectations
	mockRepo.On("GetByUserID", ctx, userID).Return(existingSettings, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(s *domain.PrivacySettings) bool {
		return s.UserID == userID &&
			s.GDPRAnalyticsConsent == true &&
			s.MarketingEmailsEnabled == true
	})).Return(nil)

	// Execute
	settings, err := service.UpdatePrivacySettings(ctx, userID, updateRequest)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, settings)
	assert.True(t, settings.GDPRAnalyticsConsent)
	assert.True(t, settings.MarketingEmailsEnabled)
	mockRepo.AssertExpectations(t)
}

// TestUpdatePrivacySettings_IncludeInAggregateStats tests that IncludeInAggregateStats
// is correctly passed through from the request to the repository Update call.
// Issue #972: GAP AC nit — k-anonymity consent field must flow through the service layer.
func TestUpdatePrivacySettings_IncludeInAggregateStats(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	service := NewPrivacyService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	existingSettings := domain.NewPrivacySettings(userID)
	existingSettings.ID = uuid.New().String()
	existingSettings.IncludeInAggregateStats = true // default is true (opt-out model)

	updateRequest := &domain.UpdatePrivacySettingsRequest{
		IncludeInAggregateStats: boolPtr(false), // user opts out
	}

	// Mock expectations — verify IncludeInAggregateStats is forwarded to Update
	mockRepo.On("GetByUserID", ctx, userID).Return(existingSettings, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(s *domain.PrivacySettings) bool {
		return s.UserID == userID &&
			s.IncludeInAggregateStats == false // must be the updated value
	})).Return(nil)

	// Execute
	settings, err := service.UpdatePrivacySettings(ctx, userID, updateRequest)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, settings)
	assert.False(t, settings.IncludeInAggregateStats)
	mockRepo.AssertExpectations(t)
}

// TestUpdatePrivacySettings_ValidationFailure tests GDPR validation enforcement
func TestUpdatePrivacySettings_ValidationFailure(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	service := NewPrivacyService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()

	// Attempt to disable necessary consent (GDPR violation)
	invalidRequest := &domain.UpdatePrivacySettingsRequest{
		GDPRNecessaryConsent: boolPtr(false), // INVALID - cannot disable
	}

	// Execute
	settings, err := service.UpdatePrivacySettings(ctx, userID, invalidRequest)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "necessary_consent") // GDPR validation error
	assert.Nil(t, settings)
	// Update should NOT be called due to validation failure
	mockRepo.AssertNotCalled(t, "Update")
}

// TestUpdatePrivacySettings_NotFound tests updating non-existent privacy settings
func TestUpdatePrivacySettings_NotFound(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	service := NewPrivacyService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	updateRequest := &domain.UpdatePrivacySettingsRequest{
		GDPRAnalyticsConsent: boolPtr(true),
	}

	// Mock expectations
	mockRepo.On("GetByUserID", ctx, userID).Return(nil, repository.ErrPrivacySettingsNotFound)

	// Execute
	settings, err := service.UpdatePrivacySettings(ctx, userID, updateRequest)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, repository.ErrPrivacySettingsNotFound, err)
	assert.Nil(t, settings)
	// Should not attempt Update if settings don't exist
	mockRepo.AssertNotCalled(t, "Update")
	mockRepo.AssertExpectations(t)
}

// TestUpdatePrivacySettings_UpdatesTimestamp tests that timestamps are updated
func TestUpdatePrivacySettings_UpdatesTimestamp(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	service := NewPrivacyService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	existingSettings := domain.NewPrivacySettings(userID)
	existingSettings.ID = uuid.New().String()
	originalTimestamp := existingSettings.GDPRConsentTimestamp

	// Wait to ensure timestamp changes
	time.Sleep(10 * time.Millisecond)

	updateRequest := &domain.UpdatePrivacySettingsRequest{
		GDPRAnalyticsConsent: boolPtr(true),
	}

	// Mock expectations
	mockRepo.On("GetByUserID", ctx, userID).Return(existingSettings, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(s *domain.PrivacySettings) bool {
		// Verify timestamp was updated
		return s.GDPRConsentTimestamp.After(originalTimestamp)
	})).Return(nil)

	// Execute
	settings, err := service.UpdatePrivacySettings(ctx, userID, updateRequest)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, settings)
	assert.True(t, settings.GDPRConsentTimestamp.After(originalTimestamp))
	mockRepo.AssertExpectations(t)
}

// TestUpdatePrivacySettings_PartialUpdate tests updating only specific fields
func TestUpdatePrivacySettings_PartialUpdate(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	service := NewPrivacyService(mockRepo)
	ctx := context.Background()

	userID := uuid.New().String()
	existingSettings := domain.NewPrivacySettings(userID)
	existingSettings.ID = uuid.New().String()
	existingSettings.GDPRAnalyticsConsent = false
	existingSettings.MarketingEmailsEnabled = false

	// Only update analytics consent, leave marketing emails unchanged
	partialRequest := &domain.UpdatePrivacySettingsRequest{
		GDPRAnalyticsConsent: boolPtr(true),
		// MarketingEmailsEnabled is NOT set (should remain false)
	}

	// Mock expectations
	mockRepo.On("GetByUserID", ctx, userID).Return(existingSettings, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(s *domain.PrivacySettings) bool {
		return s.GDPRAnalyticsConsent == true && // Updated
			s.MarketingEmailsEnabled == false // Unchanged
	})).Return(nil)

	// Execute
	settings, err := service.UpdatePrivacySettings(ctx, userID, partialRequest)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, settings)
	assert.True(t, settings.GDPRAnalyticsConsent)    // Changed
	assert.False(t, settings.MarketingEmailsEnabled) // Unchanged
	mockRepo.AssertExpectations(t)
}

// Helper function for creating bool pointers in tests
func boolPtr(b bool) *bool {
	return &b
}
