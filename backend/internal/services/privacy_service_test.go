package services

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/testsupport/mockassert"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

func (m *MockPrivacyRepository) GetByUserIDForUpdate(ctx context.Context, tx pgx.Tx, userID string) (*domain.PrivacySettings, error) {
	args := m.Called(ctx, tx, userID)
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

func (m *MockPrivacyRepository) UpdateTx(ctx context.Context, tx pgx.Tx, settings *domain.PrivacySettings) error {
	args := m.Called(ctx, tx, settings)
	return args.Error(0)
}

// fakePersonPropertyUnsetter records EmitPersonPropertyUnset calls.
type fakePersonPropertyUnsetter struct {
	calls []personPropertyUnsetCall
}

type personPropertyUnsetCall struct {
	distinctID string
	keys       []string
}

func (f *fakePersonPropertyUnsetter) EmitPersonPropertyUnset(distinctID string, keys []string) {
	f.calls = append(f.calls, personPropertyUnsetCall{distinctID: distinctID, keys: keys})
}

// fakeAuditMetadataRepo records LogInTxWithMetadata calls.
type fakeAuditMetadataRepo struct {
	calls []auditMetadataCall
}

type auditMetadataCall struct {
	action       string
	resourceType string
	resourceID   string
	meta         repository.AuditLogMetadata
}

func (f *fakeAuditMetadataRepo) Log(ctx context.Context, action, resourceType, resourceID string) error {
	return nil
}

func (f *fakeAuditMetadataRepo) LogInTx(ctx context.Context, tx pgx.Tx, action, resourceType, resourceID string) error {
	return nil
}

func (f *fakeAuditMetadataRepo) LogInTxWithMetadata(ctx context.Context, tx pgx.Tx, action, resourceType, resourceID string, meta repository.AuditLogMetadata) error {
	f.calls = append(f.calls, auditMetadataCall{action: action, resourceType: resourceType, resourceID: resourceID, meta: meta})
	return nil
}

// stubRequestAudit returns a RequestAuditSource reporting fixed metadata.
func stubRequestAudit(ip netip.Addr, ua string, requestID uuid.UUID) RequestAuditSource {
	return func(context.Context) (netip.Addr, string, uuid.UUID) {
		return ip, ua, requestID
	}
}

// privacySettingsRow builds the 14-column pgxmock row for a privacy_settings SELECT.
func privacySettingsRow(settingsID, userID string, necessary, analytics, interaction bool, consentAt time.Time, marketing, updates, alerts, sharing, aggregate, identified bool, createdAt, updatedAt time.Time) *pgxmock.Rows {
	return pgxmock.NewRows([]string{
		"id", "user_id", "gdpr_necessary_consent", "gdpr_analytics_consent",
		"gdpr_interaction_capture_consent",
		"gdpr_consent_timestamp",
		"marketing_emails_enabled", "product_updates_enabled",
		"security_alerts_enabled", "data_sharing_enabled",
		"include_in_aggregate_stats",
		"identified_diagnostics_enabled",
		"created_at", "updated_at",
	}).AddRow(
		settingsID, userID, necessary, analytics,
		interaction,
		consentAt,
		marketing, updates,
		alerts, sharing,
		aggregate,
		identified,
		createdAt, updatedAt,
	)
}

// TestGetPrivacySettings_Existing tests retrieving existing privacy settings
func TestGetPrivacySettings_Existing(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	service := NewPrivacyService(mockRepo, nil, nil, nil, nil)
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
	service := NewPrivacyService(mockRepo, nil, nil, nil, nil)
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
	service := NewPrivacyService(mockRepo, nil, nil, nil, nil)
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
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()
	service := NewPrivacyService(mockRepo, &fakeAuditMetadataRepo{}, pool, &fakePersonPropertyUnsetter{}, nil)
	ctx := context.Background()

	userID := uuid.New().String()
	existingSettings := domain.NewPrivacySettings(userID)
	existingSettings.ID = uuid.New().String()

	updateRequest := &domain.UpdatePrivacySettingsRequest{
		GDPRAnalyticsConsent:   boolPtr(true),
		MarketingEmailsEnabled: boolPtr(true),
	}

	// Mock expectations
	pool.ExpectBegin()
	pool.ExpectCommit()
	mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(existingSettings, nil)
	mockRepo.On("UpdateTx", ctx, mock.Anything, mock.MatchedBy(func(s *domain.PrivacySettings) bool {
		return s.UserID == userID &&
			s.GDPRAnalyticsConsent == true &&
			s.MarketingEmailsEnabled == true
	})).Return(nil)

	// Execute
	settings, err := service.UpdatePrivacySettings(ctx, userID, "user_clerk_subject", updateRequest)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, settings)
	assert.True(t, settings.GDPRAnalyticsConsent)
	assert.True(t, settings.MarketingEmailsEnabled)
	mockRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestUpdatePrivacySettings_IncludeInAggregateStats tests that IncludeInAggregateStats
// is correctly passed through from the request to the repository UpdateTx call.
// Issue #972: GAP AC nit — k-anonymity consent field must flow through the service layer.
func TestUpdatePrivacySettings_IncludeInAggregateStats(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()
	service := NewPrivacyService(mockRepo, &fakeAuditMetadataRepo{}, pool, &fakePersonPropertyUnsetter{}, nil)
	ctx := context.Background()

	userID := uuid.New().String()
	existingSettings := domain.NewPrivacySettings(userID)
	existingSettings.ID = uuid.New().String()
	existingSettings.IncludeInAggregateStats = true // default is true (opt-out model)

	updateRequest := &domain.UpdatePrivacySettingsRequest{
		IncludeInAggregateStats: boolPtr(false), // user opts out
	}

	// Mock expectations — verify IncludeInAggregateStats is forwarded to UpdateTx
	pool.ExpectBegin()
	pool.ExpectCommit()
	mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(existingSettings, nil)
	mockRepo.On("UpdateTx", ctx, mock.Anything, mock.MatchedBy(func(s *domain.PrivacySettings) bool {
		return s.UserID == userID &&
			s.IncludeInAggregateStats == false // must be the updated value
	})).Return(nil)

	// Execute
	settings, err := service.UpdatePrivacySettings(ctx, userID, "user_clerk_subject", updateRequest)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, settings)
	assert.False(t, settings.IncludeInAggregateStats)
	mockRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestUpdatePrivacySettings_ValidationFailure tests GDPR validation enforcement
func TestUpdatePrivacySettings_ValidationFailure(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	service := NewPrivacyService(mockRepo, nil, nil, nil, nil)
	ctx := context.Background()

	userID := uuid.New().String()

	// Attempt to disable necessary consent (GDPR violation)
	invalidRequest := &domain.UpdatePrivacySettingsRequest{
		GDPRNecessaryConsent: boolPtr(false), // INVALID - cannot disable
	}

	// Execute
	settings, err := service.UpdatePrivacySettings(ctx, userID, "user_clerk_subject", invalidRequest)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "necessary_consent") // GDPR validation error
	assert.Nil(t, settings)
	// UpdateTx should NOT be called due to validation failure
	mockassert.NoCallTo(t, &mockRepo.Mock, "UpdateTx")
}

// TestUpdatePrivacySettings_NotFound tests updating non-existent privacy settings
func TestUpdatePrivacySettings_NotFound(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()
	service := NewPrivacyService(mockRepo, &fakeAuditMetadataRepo{}, pool, &fakePersonPropertyUnsetter{}, nil)
	ctx := context.Background()

	userID := uuid.New().String()
	updateRequest := &domain.UpdatePrivacySettingsRequest{
		GDPRAnalyticsConsent: boolPtr(true),
	}

	// Mock expectations
	pool.ExpectBegin()
	pool.ExpectRollback()
	mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(nil, repository.ErrPrivacySettingsNotFound)

	// Execute
	settings, err := service.UpdatePrivacySettings(ctx, userID, "user_clerk_subject", updateRequest)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, repository.ErrPrivacySettingsNotFound, err)
	assert.Nil(t, settings)
	// Should not attempt UpdateTx if settings don't exist
	mockassert.NoCallTo(t, &mockRepo.Mock, "UpdateTx")
	mockRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestUpdatePrivacySettings_UpdatesTimestamp tests that timestamps are updated
func TestUpdatePrivacySettings_UpdatesTimestamp(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()
	service := NewPrivacyService(mockRepo, &fakeAuditMetadataRepo{}, pool, &fakePersonPropertyUnsetter{}, nil)
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
	pool.ExpectBegin()
	pool.ExpectCommit()
	mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(existingSettings, nil)
	mockRepo.On("UpdateTx", ctx, mock.Anything, mock.MatchedBy(func(s *domain.PrivacySettings) bool {
		// Verify timestamp was updated
		return s.GDPRConsentTimestamp.After(originalTimestamp)
	})).Return(nil)

	// Execute
	settings, err := service.UpdatePrivacySettings(ctx, userID, "user_clerk_subject", updateRequest)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, settings)
	assert.True(t, settings.GDPRConsentTimestamp.After(originalTimestamp))
	mockRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestUpdatePrivacySettings_PartialUpdate tests updating only specific fields
func TestUpdatePrivacySettings_PartialUpdate(t *testing.T) {
	// Setup
	mockRepo := new(MockPrivacyRepository)
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()
	service := NewPrivacyService(mockRepo, &fakeAuditMetadataRepo{}, pool, &fakePersonPropertyUnsetter{}, nil)
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
	pool.ExpectBegin()
	pool.ExpectCommit()
	mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(existingSettings, nil)
	mockRepo.On("UpdateTx", ctx, mock.Anything, mock.MatchedBy(func(s *domain.PrivacySettings) bool {
		return s.GDPRAnalyticsConsent == true && // Updated
			s.MarketingEmailsEnabled == false // Unchanged
	})).Return(nil)

	// Execute
	settings, err := service.UpdatePrivacySettings(ctx, userID, "user_clerk_subject", partialRequest)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, settings)
	assert.True(t, settings.GDPRAnalyticsConsent)    // Changed
	assert.False(t, settings.MarketingEmailsEnabled) // Unchanged
	mockRepo.AssertExpectations(t)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestUpdatePrivacySettings_MergedStateValidation pins the three request shapes against the
// MERGED state: the cascade runs on the analytics=false delta only, and Validate refuses a merged
// state that enables interaction capture without analytics consent.
func TestUpdatePrivacySettings_MergedStateValidation(t *testing.T) {
	t.Run("shape 1: analytics=false + interaction=true in one request is rejected and not persisted", func(t *testing.T) {
		mockRepo := new(MockPrivacyRepository)
		pool, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer pool.Close()
		service := NewPrivacyService(mockRepo, &fakeAuditMetadataRepo{}, pool, &fakePersonPropertyUnsetter{}, nil)
		ctx := context.Background()

		userID := uuid.New().String()
		existing := domain.NewPrivacySettings(userID)
		existing.GDPRAnalyticsConsent = true
		existing.GDPRInteractionCaptureConsent = true

		pool.ExpectBegin()
		pool.ExpectRollback()
		mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(existing, nil)

		settings, err := service.UpdatePrivacySettings(ctx, userID, "user_clerk_subject", &domain.UpdatePrivacySettingsRequest{
			GDPRAnalyticsConsent:          boolPtr(false),
			GDPRInteractionCaptureConsent: boolPtr(true),
		})

		require.ErrorIs(t, err, domain.ErrInteractionCaptureRequiresAnalytics)
		assert.Nil(t, settings)
		mockassert.NoCallTo(t, &mockRepo.Mock, "UpdateTx")
		require.NoError(t, pool.ExpectationsWereMet())
	})

	t.Run("shape 2: analytics=false alone cascades interaction off and persists", func(t *testing.T) {
		mockRepo := new(MockPrivacyRepository)
		pool, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer pool.Close()
		service := NewPrivacyService(mockRepo, &fakeAuditMetadataRepo{}, pool, &fakePersonPropertyUnsetter{}, nil)
		ctx := context.Background()

		userID := uuid.New().String()
		existing := domain.NewPrivacySettings(userID)
		existing.GDPRAnalyticsConsent = true
		existing.GDPRInteractionCaptureConsent = true

		pool.ExpectBegin()
		pool.ExpectCommit()
		mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(existing, nil)
		mockRepo.On("UpdateTx", ctx, mock.Anything, mock.MatchedBy(func(s *domain.PrivacySettings) bool {
			return !s.GDPRAnalyticsConsent && !s.GDPRInteractionCaptureConsent
		})).Return(nil)

		settings, err := service.UpdatePrivacySettings(ctx, userID, "user_clerk_subject", &domain.UpdatePrivacySettingsRequest{
			GDPRAnalyticsConsent: boolPtr(false),
		})

		require.NoError(t, err)
		require.NotNil(t, settings)
		assert.False(t, settings.GDPRInteractionCaptureConsent)
		mockRepo.AssertExpectations(t)
		require.NoError(t, pool.ExpectationsWereMet())
	})

	t.Run("shape 3: interaction=true alone while analytics is already false is rejected", func(t *testing.T) {
		mockRepo := new(MockPrivacyRepository)
		pool, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer pool.Close()
		service := NewPrivacyService(mockRepo, &fakeAuditMetadataRepo{}, pool, &fakePersonPropertyUnsetter{}, nil)
		ctx := context.Background()

		userID := uuid.New().String()
		existing := domain.NewPrivacySettings(userID)
		existing.GDPRAnalyticsConsent = false
		existing.GDPRInteractionCaptureConsent = false

		pool.ExpectBegin()
		pool.ExpectRollback()
		mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(existing, nil)

		settings, err := service.UpdatePrivacySettings(ctx, userID, "user_clerk_subject", &domain.UpdatePrivacySettingsRequest{
			GDPRInteractionCaptureConsent: boolPtr(true),
		})

		require.ErrorIs(t, err, domain.ErrInteractionCaptureRequiresAnalytics)
		assert.Nil(t, settings)
		mockassert.NoCallTo(t, &mockRepo.Mock, "UpdateTx")
		require.NoError(t, pool.ExpectationsWereMet())
	})
}

// TestUpdatePrivacySettings_UnsetEmission pins the post-commit person-property removal: it fires
// ONLY on a true -> false delta of identified diagnostics, and forwards the CLERK SUBJECT (the
// PostHog distinct id), never the internal user id.
func TestUpdatePrivacySettings_UnsetEmission(t *testing.T) {
	const clerkSubject = "user_clerk_1683_subject"

	newService := func(t *testing.T, mockRepo *MockPrivacyRepository, unsetter *fakePersonPropertyUnsetter) PrivacyService {
		t.Helper()
		pool, err := pgxmock.NewPool()
		require.NoError(t, err)
		t.Cleanup(pool.Close)
		pool.ExpectBegin()
		pool.ExpectCommit()
		return NewPrivacyService(mockRepo, &fakeAuditMetadataRepo{}, pool, unsetter, nil)
	}

	t.Run("true to false delta emits $unset with the Clerk subject", func(t *testing.T) {
		mockRepo := new(MockPrivacyRepository)
		unsetter := &fakePersonPropertyUnsetter{}
		service := newService(t, mockRepo, unsetter)
		ctx := context.Background()

		userID := uuid.New().String()
		existing := domain.NewPrivacySettings(userID) // identified diagnostics defaults true

		mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(existing, nil)
		mockRepo.On("UpdateTx", ctx, mock.Anything, mock.Anything).Return(nil)

		_, err := service.UpdatePrivacySettings(ctx, userID, clerkSubject, &domain.UpdatePrivacySettingsRequest{
			IdentifiedDiagnosticsEnabled: boolPtr(false),
		})

		require.NoError(t, err)
		require.Len(t, unsetter.calls, 1)
		assert.Equal(t, clerkSubject, unsetter.calls[0].distinctID, "must forward the Clerk subject, not the internal user id")
		assert.NotEqual(t, userID, unsetter.calls[0].distinctID)
		assert.Equal(t, []string{"name"}, unsetter.calls[0].keys)
	})

	t.Run("no delta does not emit", func(t *testing.T) {
		mockRepo := new(MockPrivacyRepository)
		unsetter := &fakePersonPropertyUnsetter{}
		service := newService(t, mockRepo, unsetter)
		ctx := context.Background()

		userID := uuid.New().String()
		existing := domain.NewPrivacySettings(userID) // identified diagnostics defaults true

		mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(existing, nil)
		mockRepo.On("UpdateTx", ctx, mock.Anything, mock.Anything).Return(nil)

		_, err := service.UpdatePrivacySettings(ctx, userID, clerkSubject, &domain.UpdatePrivacySettingsRequest{
			IdentifiedDiagnosticsEnabled: boolPtr(true),
		})

		require.NoError(t, err)
		assert.Empty(t, unsetter.calls)
	})

	t.Run("false to true does not emit", func(t *testing.T) {
		mockRepo := new(MockPrivacyRepository)
		unsetter := &fakePersonPropertyUnsetter{}
		service := newService(t, mockRepo, unsetter)
		ctx := context.Background()

		userID := uuid.New().String()
		existing := domain.NewPrivacySettings(userID)
		existing.IdentifiedDiagnosticsEnabled = false

		mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(existing, nil)
		mockRepo.On("UpdateTx", ctx, mock.Anything, mock.Anything).Return(nil)

		_, err := service.UpdatePrivacySettings(ctx, userID, clerkSubject, &domain.UpdatePrivacySettingsRequest{
			IdentifiedDiagnosticsEnabled: boolPtr(true),
		})

		require.NoError(t, err)
		assert.Empty(t, unsetter.calls)
	})

	t.Run("analytics withdrawal alone does not emit", func(t *testing.T) {
		mockRepo := new(MockPrivacyRepository)
		unsetter := &fakePersonPropertyUnsetter{}
		service := newService(t, mockRepo, unsetter)
		ctx := context.Background()

		userID := uuid.New().String()
		existing := domain.NewPrivacySettings(userID)
		existing.GDPRAnalyticsConsent = true
		existing.GDPRInteractionCaptureConsent = true

		mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(existing, nil)
		mockRepo.On("UpdateTx", ctx, mock.Anything, mock.Anything).Return(nil)

		_, err := service.UpdatePrivacySettings(ctx, userID, clerkSubject, &domain.UpdatePrivacySettingsRequest{
			GDPRAnalyticsConsent: boolPtr(false),
		})

		require.NoError(t, err)
		assert.Empty(t, unsetter.calls)
	})
}

// TestUpdatePrivacySettings_TransactionalOrder is the G6 ordered expectation: Begin -> locked
// SELECT -> UPDATE -> audit INSERT -> Commit, with no erasure-fence SELECT anywhere in the path.
// It runs the REAL repositories against a pgxmock pool, so the statement text and argument order
// are pinned, and the audit row must carry the RequestAudit metadata.
func TestUpdatePrivacySettings_TransactionalOrder(t *testing.T) {
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()

	repo := repository.NewPrivacyRepository(pool)
	auditRepo := repository.NewAuditLogMetadataRepository(pool)
	unsetter := &fakePersonPropertyUnsetter{}
	auditIP := netip.MustParseAddr("203.0.113.7")
	requestID := uuid.New()
	service := NewPrivacyService(repo, auditRepo, pool, unsetter, stubRequestAudit(auditIP, "privacy-test-agent/1.0", requestID))

	ctx := context.Background()
	userID := uuid.New().String()
	settingsID := uuid.New().String()
	now := time.Now()
	const clerkSubject = "user_clerk_1683_subject"

	pool.ExpectBegin()
	pool.ExpectQuery("FOR UPDATE").WithArgs(userID).WillReturnRows(
		privacySettingsRow(settingsID, userID, true, false, false, now, false, true, true, false, true, true, now, now))
	pool.ExpectExec("UPDATE privacy_settings").WithArgs(
		true,             // necessary
		false,            // analytics
		false,            // interaction capture
		pgxmock.AnyArg(), // consent timestamp
		false,            // marketing
		true,             // product updates
		true,             // security
		false,            // data sharing
		true,             // include_in_aggregate_stats
		false,            // identified diagnostics (the delta)
		pgxmock.AnyArg(), // updated_at
		userID,
	).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec("INSERT INTO audit_logs").WithArgs(
		"privacy_settings.updated",
		"privacy_settings",
		userID,
		auditIP,
		"privacy-test-agent/1.0",
		requestID,
		[]byte(nil), // changes
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	pool.ExpectCommit()

	settings, err := service.UpdatePrivacySettings(ctx, userID, clerkSubject, &domain.UpdatePrivacySettingsRequest{
		IdentifiedDiagnosticsEnabled: boolPtr(false),
	})

	require.NoError(t, err)
	require.NotNil(t, settings)
	assert.False(t, settings.IdentifiedDiagnosticsEnabled)
	require.NoError(t, pool.ExpectationsWereMet())

	// The $unset is post-commit and carries the Clerk subject.
	require.Len(t, unsetter.calls, 1)
	assert.Equal(t, clerkSubject, unsetter.calls[0].distinctID)
	assert.Equal(t, []string{"name"}, unsetter.calls[0].keys)
}

// TestUpdatePrivacySettings_AuditMetadataRecorded pins the audit binding: resource_type is
// 'privacy_settings' and resource_id is the user id, so erasure redaction reaches the row.
func TestUpdatePrivacySettings_AuditMetadataRecorded(t *testing.T) {
	mockRepo := new(MockPrivacyRepository)
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()

	auditRepo := &fakeAuditMetadataRepo{}
	service := NewPrivacyService(mockRepo, auditRepo, pool, &fakePersonPropertyUnsetter{}, nil)
	ctx := context.Background()

	userID := uuid.New().String()
	existing := domain.NewPrivacySettings(userID)

	pool.ExpectBegin()
	pool.ExpectCommit()
	mockRepo.On("GetByUserIDForUpdate", ctx, mock.Anything, userID).Return(existing, nil)
	mockRepo.On("UpdateTx", ctx, mock.Anything, mock.Anything).Return(nil)

	_, err = service.UpdatePrivacySettings(ctx, userID, "user_clerk_subject", &domain.UpdatePrivacySettingsRequest{
		GDPRAnalyticsConsent: boolPtr(true),
	})

	require.NoError(t, err)
	require.Len(t, auditRepo.calls, 1)
	assert.Equal(t, "privacy_settings.updated", auditRepo.calls[0].action)
	assert.Equal(t, "privacy_settings", auditRepo.calls[0].resourceType)
	assert.Equal(t, userID, auditRepo.calls[0].resourceID)
	require.NoError(t, pool.ExpectationsWereMet())
}

// Helper function for creating bool pointers in tests
func boolPtr(b bool) *bool {
	return &b
}
