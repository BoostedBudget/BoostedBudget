package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupPrivacyMockDB creates a mock database and sqlmock instance for privacy repository testing
func setupPrivacyMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, PrivacyRepository) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	repo := NewPrivacyRepository(db)
	return db, mock, repo
}

// TestGetPrivacySettingsByUserID_Success tests successful retrieval of privacy settings
func TestGetPrivacySettingsByUserID_Success(t *testing.T) {
	db, mock, repo := setupPrivacyMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	settingsID := uuid.New().String()
	now := time.Now()

	// Expected privacy settings data
	rows := sqlmock.NewRows([]string{
		"id", "user_id", "gdpr_necessary_consent", "gdpr_analytics_consent",
		"gdpr_advertising_consent", "gdpr_consent_timestamp",
		"marketing_emails_enabled", "product_updates_enabled",
		"security_alerts_enabled", "data_sharing_enabled",
		"created_at", "updated_at",
	}).AddRow(
		settingsID, userID, true, false,
		false, now,
		false, true,
		true, false,
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, gdpr_necessary_consent, gdpr_analytics_consent,
		       gdpr_advertising_consent, gdpr_consent_timestamp,
		       marketing_emails_enabled, product_updates_enabled,
		       security_alerts_enabled, data_sharing_enabled,
		       created_at, updated_at
		FROM privacy_settings
		WHERE user_id = $1
	`)).WithArgs(userID).WillReturnRows(rows)

	// Execute
	settings, err := repo.GetByUserID(ctx, userID)

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, settings)
	assert.Equal(t, settingsID, settings.ID)
	assert.Equal(t, userID, settings.UserID)
	assert.True(t, settings.GDPRNecessaryConsent)
	assert.False(t, settings.GDPRAnalyticsConsent)
	assert.False(t, settings.GDPRAdvertisingConsent)
	assert.False(t, settings.MarketingEmailsEnabled)
	assert.True(t, settings.ProductUpdatesEnabled)
	assert.True(t, settings.SecurityAlertsEnabled)
	assert.False(t, settings.DataSharingEnabled)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetPrivacySettingsByUserID_NotFound tests when privacy settings are not found
func TestGetPrivacySettingsByUserID_NotFound(t *testing.T) {
	db, mock, repo := setupPrivacyMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, gdpr_necessary_consent, gdpr_analytics_consent,
		       gdpr_advertising_consent, gdpr_consent_timestamp,
		       marketing_emails_enabled, product_updates_enabled,
		       security_alerts_enabled, data_sharing_enabled,
		       created_at, updated_at
		FROM privacy_settings
		WHERE user_id = $1
	`)).WithArgs(userID).WillReturnError(sql.ErrNoRows)

	// Execute
	settings, err := repo.GetByUserID(ctx, userID)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrPrivacySettingsNotFound, err)
	assert.Nil(t, settings)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreatePrivacySettings_Success tests successful creation of privacy settings
func TestCreatePrivacySettings_Success(t *testing.T) {
	db, mock, repo := setupPrivacyMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	settings := domain.NewPrivacySettings(userID)

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO privacy_settings (
			id, user_id, gdpr_necessary_consent, gdpr_analytics_consent,
			gdpr_advertising_consent, gdpr_consent_timestamp,
			marketing_emails_enabled, product_updates_enabled,
			security_alerts_enabled, data_sharing_enabled,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6,
			$7, $8,
			$9, $10,
			$11, $12
		)
	`)).WithArgs(
		sqlmock.AnyArg(), // ID (generated UUID)
		userID,
		true,  // necessary consent
		false, // analytics consent
		false, // advertising consent
		sqlmock.AnyArg(), // consent timestamp
		false, // marketing emails
		true,  // product updates
		true,  // security alerts
		false, // data sharing
		sqlmock.AnyArg(), // created_at
		sqlmock.AnyArg(), // updated_at
	).WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute
	err := repo.Create(ctx, settings)

	// Assert
	assert.NoError(t, err)
	assert.NotEmpty(t, settings.ID) // UUID should be generated
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdatePrivacySettings_Success tests successful update of privacy settings
func TestUpdatePrivacySettings_Success(t *testing.T) {
	db, mock, repo := setupPrivacyMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	settingsID := uuid.New().String()
	now := time.Now()

	settings := &domain.PrivacySettings{
		ID:                     settingsID,
		UserID:                 userID,
		GDPRNecessaryConsent:   true,
		GDPRAnalyticsConsent:   true, // Updated
		GDPRAdvertisingConsent: true, // Updated
		GDPRConsentTimestamp:   now,
		MarketingEmailsEnabled: true, // Updated
		ProductUpdatesEnabled:  true,
		SecurityAlertsEnabled:  true,
		DataSharingEnabled:     false,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE privacy_settings
		SET gdpr_necessary_consent = $1,
		    gdpr_analytics_consent = $2,
		    gdpr_advertising_consent = $3,
		    gdpr_consent_timestamp = $4,
		    marketing_emails_enabled = $5,
		    product_updates_enabled = $6,
		    security_alerts_enabled = $7,
		    data_sharing_enabled = $8,
		    updated_at = $9
		WHERE user_id = $10
	`)).WithArgs(
		true,  // necessary
		true,  // analytics
		true,  // advertising
		sqlmock.AnyArg(), // consent timestamp
		true,  // marketing
		true,  // product updates
		true,  // security
		false, // data sharing
		sqlmock.AnyArg(), // updated_at
		userID,
	).WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute
	err := repo.Update(ctx, settings)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdatePrivacySettings_NotFound tests update when privacy settings don't exist
func TestUpdatePrivacySettings_NotFound(t *testing.T) {
	db, mock, repo := setupPrivacyMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	settings := domain.NewPrivacySettings(userID)

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE privacy_settings
		SET gdpr_necessary_consent = $1,
		    gdpr_analytics_consent = $2,
		    gdpr_advertising_consent = $3,
		    gdpr_consent_timestamp = $4,
		    marketing_emails_enabled = $5,
		    product_updates_enabled = $6,
		    security_alerts_enabled = $7,
		    data_sharing_enabled = $8,
		    updated_at = $9
		WHERE user_id = $10
	`)).WithArgs(
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		sqlmock.AnyArg(),
		userID,
	).WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected

	// Execute
	err := repo.Update(ctx, settings)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrPrivacySettingsNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdatePrivacySettings_UpdatesTimestamp tests that update modifies gdpr_consent_timestamp
func TestUpdatePrivacySettings_UpdatesTimestamp(t *testing.T) {
	db, mock, repo := setupPrivacyMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	settings := domain.NewPrivacySettings(userID)
	originalTimestamp := settings.GDPRConsentTimestamp

	// Wait to ensure timestamp changes
	time.Sleep(10 * time.Millisecond)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE privacy_settings`)).
		WithArgs(
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			userID,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute
	err := repo.Update(ctx, settings)

	// Assert
	assert.NoError(t, err)
	// The Update method should update the UpdatedAt field
	assert.True(t, settings.UpdatedAt.After(originalTimestamp))
	assert.NoError(t, mock.ExpectationsWereMet())
}
