package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	// ErrPrivacySettingsNotFound is returned when privacy settings are not found for a user
	ErrPrivacySettingsNotFound = errors.New("privacy settings not found")
)

// PrivacyRepository defines the interface for privacy settings data access
type PrivacyRepository interface {
	// GetByUserID retrieves privacy settings for a specific user
	GetByUserID(ctx context.Context, userID string) (*domain.PrivacySettings, error)

	// Create creates new privacy settings for a user
	Create(ctx context.Context, settings *domain.PrivacySettings) error

	// Update updates existing privacy settings for a user
	Update(ctx context.Context, settings *domain.PrivacySettings) error
}

// privacyRepository implements PrivacyRepository using PostgreSQL
type privacyRepository struct {
	db *sql.DB
}

// NewPrivacyRepository creates a new PrivacyRepository instance
func NewPrivacyRepository(db *sql.DB) PrivacyRepository {
	return &privacyRepository{db: db}
}

// GetByUserID retrieves privacy settings for a specific user
func (r *privacyRepository) GetByUserID(ctx context.Context, userID string) (*domain.PrivacySettings, error) {
	query := `
		SELECT id, user_id, gdpr_necessary_consent, gdpr_analytics_consent,
		       gdpr_advertising_consent, gdpr_consent_timestamp,
		       marketing_emails_enabled, product_updates_enabled,
		       security_alerts_enabled, data_sharing_enabled,
		       created_at, updated_at
		FROM privacy_settings
		WHERE user_id = $1
	`

	var settings domain.PrivacySettings
	err := r.db.QueryRowContext(ctx, query, userID).Scan(
		&settings.ID,
		&settings.UserID,
		&settings.GDPRNecessaryConsent,
		&settings.GDPRAnalyticsConsent,
		&settings.GDPRAdvertisingConsent,
		&settings.GDPRConsentTimestamp,
		&settings.MarketingEmailsEnabled,
		&settings.ProductUpdatesEnabled,
		&settings.SecurityAlertsEnabled,
		&settings.DataSharingEnabled,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrPrivacySettingsNotFound
	}
	if err != nil {
		return nil, err
	}

	return &settings, nil
}

// Create creates new privacy settings for a user
// Generates UUID if not already set, and sets timestamps
func (r *privacyRepository) Create(ctx context.Context, settings *domain.PrivacySettings) error {
	// Generate UUID if not already set
	if settings.ID == "" {
		settings.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	settings.CreatedAt = now
	settings.UpdatedAt = now

	query := `
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
	`

	_, err := r.db.ExecContext(ctx, query,
		settings.ID,
		settings.UserID,
		settings.GDPRNecessaryConsent,
		settings.GDPRAnalyticsConsent,
		settings.GDPRAdvertisingConsent,
		settings.GDPRConsentTimestamp,
		settings.MarketingEmailsEnabled,
		settings.ProductUpdatesEnabled,
		settings.SecurityAlertsEnabled,
		settings.DataSharingEnabled,
		settings.CreatedAt,
		settings.UpdatedAt,
	)

	if err != nil {
		return err
	}

	return nil
}

// Update updates existing privacy settings for a user
// Updates the updated_at timestamp automatically
func (r *privacyRepository) Update(ctx context.Context, settings *domain.PrivacySettings) error {
	// Update timestamp
	settings.UpdatedAt = time.Now()

	query := `
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
	`

	result, err := r.db.ExecContext(ctx, query,
		settings.GDPRNecessaryConsent,
		settings.GDPRAnalyticsConsent,
		settings.GDPRAdvertisingConsent,
		settings.GDPRConsentTimestamp,
		settings.MarketingEmailsEnabled,
		settings.ProductUpdatesEnabled,
		settings.SecurityAlertsEnabled,
		settings.DataSharingEnabled,
		settings.UpdatedAt,
		settings.UserID,
	)

	if err != nil {
		return err
	}

	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrPrivacySettingsNotFound
	}

	return nil
}
