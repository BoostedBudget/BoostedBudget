package repository

import (
	"context"

	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupPrivacyMockDB creates a mock database and sqlmock instance for privacy repository testing
func setupPrivacyMockDB(t *testing.T) (pgxmock.PgxPoolIface, PrivacyRepository) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewPrivacyRepository(mock)
	return mock, repo
}

// TestGetPrivacySettingsByUserID_Success tests successful retrieval of privacy settings
func TestGetPrivacySettingsByUserID_Success(t *testing.T) {
	mock, repo := setupPrivacyMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	settingsID := uuid.New().String()
	now := time.Now()

	// Expected privacy settings data
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "gdpr_necessary_consent", "gdpr_analytics_consent",
		"gdpr_consent_timestamp",
		"marketing_emails_enabled", "product_updates_enabled",
		"security_alerts_enabled", "data_sharing_enabled",
		"include_in_aggregate_stats",
		"created_at", "updated_at",
	}).AddRow(
		settingsID, userID, true, false,
		now,
		false, true,
		true, false,
		true,
		now, now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, gdpr_necessary_consent, gdpr_analytics_consent,
		       gdpr_consent_timestamp,
		       marketing_emails_enabled, product_updates_enabled,
		       security_alerts_enabled, data_sharing_enabled,
		       include_in_aggregate_stats,
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
	assert.False(t, settings.MarketingEmailsEnabled)
	assert.True(t, settings.ProductUpdatesEnabled)
	assert.True(t, settings.SecurityAlertsEnabled)
	assert.False(t, settings.DataSharingEnabled)
	assert.True(t, settings.IncludeInAggregateStats)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetPrivacySettingsByUserID_NotFound tests when privacy settings are not found
func TestGetPrivacySettingsByUserID_NotFound(t *testing.T) {
	mock, repo := setupPrivacyMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, gdpr_necessary_consent, gdpr_analytics_consent,
		       gdpr_consent_timestamp,
		       marketing_emails_enabled, product_updates_enabled,
		       security_alerts_enabled, data_sharing_enabled,
		       include_in_aggregate_stats,
		       created_at, updated_at
		FROM privacy_settings
		WHERE user_id = $1
	`)).WithArgs(userID).WillReturnError(pgx.ErrNoRows)

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
	mock, repo := setupPrivacyMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	settings := domain.NewPrivacySettings(userID)

	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO privacy_settings (
			id, user_id, gdpr_necessary_consent, gdpr_analytics_consent,
			gdpr_consent_timestamp,
			marketing_emails_enabled, product_updates_enabled,
			security_alerts_enabled, data_sharing_enabled,
			include_in_aggregate_stats,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			$5,
			$6, $7,
			$8, $9,
			$10,
			$11, $12
		)
	`)).WithArgs(
		pgxmock.AnyArg(), // ID (generated UUID)
		userID,
		true,             // necessary consent
		false,            // analytics consent
		pgxmock.AnyArg(), // consent timestamp
		false,            // marketing emails
		true,             // product updates
		true,             // security alerts
		false,            // data sharing
		true,             // include_in_aggregate_stats
		pgxmock.AnyArg(), // created_at
		pgxmock.AnyArg(), // updated_at
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.Create(ctx, settings)

	// Assert
	assert.NoError(t, err)
	assert.NotEmpty(t, settings.ID) // UUID should be generated
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdatePrivacySettings_Success tests successful update of privacy settings
func TestUpdatePrivacySettings_Success(t *testing.T) {
	mock, repo := setupPrivacyMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	settingsID := uuid.New().String()
	now := time.Now()

	settings := &domain.PrivacySettings{
		ID:                     settingsID,
		UserID:                 userID,
		GDPRNecessaryConsent:   true,
		GDPRAnalyticsConsent:   true, // Updated
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
		    gdpr_consent_timestamp = $3,
		    marketing_emails_enabled = $4,
		    product_updates_enabled = $5,
		    security_alerts_enabled = $6,
		    data_sharing_enabled = $7,
		    include_in_aggregate_stats = $8,
		    updated_at = $9
		WHERE user_id = $10
	`)).WithArgs(
		true,             // necessary
		true,             // analytics
		pgxmock.AnyArg(), // consent timestamp
		true,             // marketing
		true,             // product updates
		true,             // security
		false,            // data sharing
		false,            // include_in_aggregate_stats
		pgxmock.AnyArg(), // updated_at
		userID,
	).WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.Update(ctx, settings)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdatePrivacySettings_NotFound tests update when privacy settings don't exist
func TestUpdatePrivacySettings_NotFound(t *testing.T) {
	mock, repo := setupPrivacyMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	settings := domain.NewPrivacySettings(userID)

	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE privacy_settings
		SET gdpr_necessary_consent = $1,
		    gdpr_analytics_consent = $2,
		    gdpr_consent_timestamp = $3,
		    marketing_emails_enabled = $4,
		    product_updates_enabled = $5,
		    security_alerts_enabled = $6,
		    data_sharing_enabled = $7,
		    include_in_aggregate_stats = $8,
		    updated_at = $9
		WHERE user_id = $10
	`)).WithArgs(
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		pgxmock.AnyArg(),
		userID,
	).WillReturnResult(pgxmock.NewResult("UPDATE", 0)) // 0 rows affected

	// Execute
	err := repo.Update(ctx, settings)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, ErrPrivacySettingsNotFound, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestPrivacyRepository_IncludeInAggregateStats tests that include_in_aggregate_stats
// is persisted and read back correctly via the mock repository layer.
func TestPrivacyRepository_IncludeInAggregateStats(t *testing.T) {
	t.Run("Create persists include_in_aggregate_stats=true", func(t *testing.T) {
		mock, repo := setupPrivacyMockDB(t)
		defer mock.Close()

		ctx := context.Background()
		userID := uuid.New().String()
		settings := domain.NewPrivacySettings(userID) // defaults to true

		mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO privacy_settings (
			id, user_id, gdpr_necessary_consent, gdpr_analytics_consent,
			gdpr_consent_timestamp,
			marketing_emails_enabled, product_updates_enabled,
			security_alerts_enabled, data_sharing_enabled,
			include_in_aggregate_stats,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			$5,
			$6, $7,
			$8, $9,
			$10,
			$11, $12
		)
	`)).WithArgs(
			pgxmock.AnyArg(), // ID
			userID,
			true,             // necessary
			false,            // analytics
			pgxmock.AnyArg(), // consent timestamp
			false,            // marketing
			true,             // product updates
			true,             // security
			false,            // data sharing
			true,             // include_in_aggregate_stats (default true)
			pgxmock.AnyArg(), // created_at
			pgxmock.AnyArg(), // updated_at
		).WillReturnResult(pgxmock.NewResult("INSERT", 1))

		err := repo.Create(ctx, settings)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetByUserID reads include_in_aggregate_stats correctly", func(t *testing.T) {
		mock, repo := setupPrivacyMockDB(t)
		defer mock.Close()

		ctx := context.Background()
		userID := uuid.New().String()
		settingsID := uuid.New().String()
		now := time.Now()

		rows := pgxmock.NewRows([]string{
			"id", "user_id", "gdpr_necessary_consent", "gdpr_analytics_consent",
			"gdpr_consent_timestamp",
			"marketing_emails_enabled", "product_updates_enabled",
			"security_alerts_enabled", "data_sharing_enabled",
			"include_in_aggregate_stats",
			"created_at", "updated_at",
		}).AddRow(
			settingsID, userID, true, false,
			now,
			false, true,
			true, false,
			false, // include_in_aggregate_stats explicitly false
			now, now,
		)

		mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, gdpr_necessary_consent, gdpr_analytics_consent,
		       gdpr_consent_timestamp,
		       marketing_emails_enabled, product_updates_enabled,
		       security_alerts_enabled, data_sharing_enabled,
		       include_in_aggregate_stats,
		       created_at, updated_at
		FROM privacy_settings
		WHERE user_id = $1
	`)).WithArgs(userID).WillReturnRows(rows)

		settings, err := repo.GetByUserID(ctx, userID)
		require.NoError(t, err)
		assert.False(t, settings.IncludeInAggregateStats, "should read back false from DB")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestUpdatePrivacySettings_UpdatesTimestamp tests that update modifies gdpr_consent_timestamp
func TestUpdatePrivacySettings_UpdatesTimestamp(t *testing.T) {
	mock, repo := setupPrivacyMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	settings := domain.NewPrivacySettings(userID)
	originalTimestamp := settings.GDPRConsentTimestamp

	// Wait to ensure timestamp changes
	time.Sleep(10 * time.Millisecond)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE privacy_settings`)).
		WithArgs(
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			userID,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Execute
	err := repo.Update(ctx, settings)

	// Assert
	assert.NoError(t, err)
	// The Update method should update the UpdatedAt field
	assert.True(t, settings.UpdatedAt.After(originalTimestamp))
	assert.NoError(t, mock.ExpectationsWereMet())
}
