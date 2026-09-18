package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

	// GetByUserIDForUpdate retrieves privacy settings for a specific user and locks the row
	// (SELECT ... FOR UPDATE) for the remainder of the caller's transaction, so a read-modify-write
	// of consent state cannot interleave with a concurrent writer.
	GetByUserIDForUpdate(ctx context.Context, tx pgx.Tx, userID string) (*domain.PrivacySettings, error)

	// Create creates new privacy settings for a user
	Create(ctx context.Context, settings *domain.PrivacySettings) error

	// CreateTx creates new privacy settings for a user within a transaction
	// Generates UUID if not already set, and sets timestamps
	CreateTx(ctx context.Context, tx pgx.Tx, settings *domain.PrivacySettings) error

	// Update updates existing privacy settings for a user
	Update(ctx context.Context, settings *domain.PrivacySettings) error

	// UpdateTx updates existing privacy settings for a user within a transaction
	UpdateTx(ctx context.Context, tx pgx.Tx, settings *domain.PrivacySettings) error
}

// privacyRepository implements PrivacyRepository using PostgreSQL
type privacyRepository struct {
	db DB
}

// NewPrivacyRepository creates a new PrivacyRepository instance
func NewPrivacyRepository(db DB) PrivacyRepository {
	return &privacyRepository{db: db}
}

// GetByUserID retrieves privacy settings for a specific user
func (r *privacyRepository) GetByUserID(ctx context.Context, userID string) (*domain.PrivacySettings, error) {
	query := `
		SELECT id, user_id, gdpr_necessary_consent, gdpr_analytics_consent,
		       gdpr_interaction_capture_consent,
		       gdpr_consent_timestamp,
		       marketing_emails_enabled, product_updates_enabled,
		       security_alerts_enabled, data_sharing_enabled,
		       include_in_aggregate_stats,
		       identified_diagnostics_enabled,
		       created_at, updated_at
		FROM privacy_settings
		WHERE user_id = $1
	`

	return scanPrivacySettingsRow(r.db.QueryRow(ctx, query, userID))
}

// GetByUserIDForUpdate is GetByUserID inside the caller's transaction, with the row locked by
// SELECT ... FOR UPDATE. The lock is held until the caller's transaction ends.
func (r *privacyRepository) GetByUserIDForUpdate(ctx context.Context, tx pgx.Tx, userID string) (*domain.PrivacySettings, error) {
	query := `
		SELECT id, user_id, gdpr_necessary_consent, gdpr_analytics_consent,
		       gdpr_interaction_capture_consent,
		       gdpr_consent_timestamp,
		       marketing_emails_enabled, product_updates_enabled,
		       security_alerts_enabled, data_sharing_enabled,
		       include_in_aggregate_stats,
		       identified_diagnostics_enabled,
		       created_at, updated_at
		FROM privacy_settings
		WHERE user_id = $1
		FOR UPDATE
	`

	return scanPrivacySettingsRow(tx.QueryRow(ctx, query, userID))
}

// scanPrivacySettingsRow is the single Scan behind both privacy reads, so the column list cannot
// drift between the plain read and the locked read.
func scanPrivacySettingsRow(row pgx.Row) (*domain.PrivacySettings, error) {
	var settings domain.PrivacySettings
	err := row.Scan(
		&settings.ID,
		&settings.UserID,
		&settings.GDPRNecessaryConsent,
		&settings.GDPRAnalyticsConsent,
		&settings.GDPRInteractionCaptureConsent,
		&settings.GDPRConsentTimestamp,
		&settings.MarketingEmailsEnabled,
		&settings.ProductUpdatesEnabled,
		&settings.SecurityAlertsEnabled,
		&settings.DataSharingEnabled,
		&settings.IncludeInAggregateStats,
		&settings.IdentifiedDiagnosticsEnabled,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
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
			gdpr_interaction_capture_consent,
			gdpr_consent_timestamp,
			marketing_emails_enabled, product_updates_enabled,
			security_alerts_enabled, data_sharing_enabled,
			include_in_aggregate_stats,
			identified_diagnostics_enabled,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			$5,
			$6,
			$7, $8,
			$9, $10,
			$11,
			$12,
			$13, $14
		)
	`

	_, err := r.db.Exec(ctx, query,
		settings.ID,
		settings.UserID,
		settings.GDPRNecessaryConsent,
		settings.GDPRAnalyticsConsent,
		settings.GDPRInteractionCaptureConsent,
		settings.GDPRConsentTimestamp,
		settings.MarketingEmailsEnabled,
		settings.ProductUpdatesEnabled,
		settings.SecurityAlertsEnabled,
		settings.DataSharingEnabled,
		settings.IncludeInAggregateStats,
		settings.IdentifiedDiagnosticsEnabled,
		settings.CreatedAt,
		settings.UpdatedAt,
	)

	if err != nil {
		return err
	}

	return nil
}

// CreateTx creates new privacy settings for a user within a transaction
// Generates UUID if not already set, and sets timestamps
func (r *privacyRepository) CreateTx(ctx context.Context, tx pgx.Tx, settings *domain.PrivacySettings) error {
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
			gdpr_interaction_capture_consent,
			gdpr_consent_timestamp,
			marketing_emails_enabled, product_updates_enabled,
			security_alerts_enabled, data_sharing_enabled,
			include_in_aggregate_stats,
			identified_diagnostics_enabled,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			$5,
			$6,
			$7, $8,
			$9, $10,
			$11,
			$12,
			$13, $14
		)
	`

	_, err := tx.Exec(ctx, query,
		settings.ID,
		settings.UserID,
		settings.GDPRNecessaryConsent,
		settings.GDPRAnalyticsConsent,
		settings.GDPRInteractionCaptureConsent,
		settings.GDPRConsentTimestamp,
		settings.MarketingEmailsEnabled,
		settings.ProductUpdatesEnabled,
		settings.SecurityAlertsEnabled,
		settings.DataSharingEnabled,
		settings.IncludeInAggregateStats,
		settings.IdentifiedDiagnosticsEnabled,
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
	return updatePrivacySettings(ctx, r.db, settings)
}

// UpdateTx updates existing privacy settings for a user within a transaction,
// mirroring Update's statement and timestamp behaviour. It is the write half of the locked
// read-modify-write pair opened by GetByUserIDForUpdate.
func (r *privacyRepository) UpdateTx(ctx context.Context, tx pgx.Tx, settings *domain.PrivacySettings) error {
	return updatePrivacySettings(ctx, tx, settings)
}

// updatePrivacySettings is the single UPDATE behind Update and UpdateTx, so the two cannot drift
// apart on column list, placeholder numbering, or the not-found refusal.
func updatePrivacySettings(ctx context.Context, q Querier, settings *domain.PrivacySettings) error {
	settings.UpdatedAt = time.Now()

	query := `
		UPDATE privacy_settings
		SET gdpr_necessary_consent = $1,
		    gdpr_analytics_consent = $2,
		    gdpr_interaction_capture_consent = $3,
		    gdpr_consent_timestamp = $4,
		    marketing_emails_enabled = $5,
		    product_updates_enabled = $6,
		    security_alerts_enabled = $7,
		    data_sharing_enabled = $8,
		    include_in_aggregate_stats = $9,
		    identified_diagnostics_enabled = $10,
		    updated_at = $11
		WHERE user_id = $12
	`

	ct, err := q.Exec(ctx, query,
		settings.GDPRNecessaryConsent,
		settings.GDPRAnalyticsConsent,
		settings.GDPRInteractionCaptureConsent,
		settings.GDPRConsentTimestamp,
		settings.MarketingEmailsEnabled,
		settings.ProductUpdatesEnabled,
		settings.SecurityAlertsEnabled,
		settings.DataSharingEnabled,
		settings.IncludeInAggregateStats,
		settings.IdentifiedDiagnosticsEnabled,
		settings.UpdatedAt,
		settings.UserID,
	)

	if err != nil {
		return err
	}

	// Check if any rows were affected
	if ct.RowsAffected() == 0 {
		return ErrPrivacySettingsNotFound
	}

	return nil
}
