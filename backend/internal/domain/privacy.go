package domain

import (
	"errors"
	"time"
)

// PrivacySettings represents GDPR-compliant user privacy and consent preferences
// This model matches the privacy_settings table and OpenAPI PrivacySettings schema
type PrivacySettings struct {
	// Primary identifier
	ID string `json:"id" db:"id"`

	// Foreign key to users table
	UserID string `json:"user_id" db:"user_id"`

	// GDPR Cookie Consent
	// necessary_consent is always true - required for app functionality
	GDPRNecessaryConsent bool `json:"gdpr_necessary_consent" db:"gdpr_necessary_consent"`

	// Analytics cookies consent (Google Analytics, usage tracking)
	GDPRAnalyticsConsent bool `json:"gdpr_analytics_consent" db:"gdpr_analytics_consent"`

	// When user last updated their consent preferences
	GDPRConsentTimestamp time.Time `json:"gdpr_consent_timestamp" db:"gdpr_consent_timestamp"`

	// Email preferences
	MarketingEmailsEnabled bool `json:"marketing_emails_enabled" db:"marketing_emails_enabled"`
	ProductUpdatesEnabled  bool `json:"product_updates_enabled" db:"product_updates_enabled"`
	SecurityAlertsEnabled  bool `json:"security_alerts_enabled" db:"security_alerts_enabled"`

	// Data sharing preference (share anonymized data for research/improvement)
	DataSharingEnabled bool `json:"data_sharing_enabled" db:"data_sharing_enabled"`

	// Aggregate stats preference (include anonymized data in aggregate statistics)
	IncludeInAggregateStats bool `json:"include_in_aggregate_stats" db:"include_in_aggregate_stats"`

	// Timestamps
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// UpdatePrivacySettingsRequest represents the request body for updating privacy settings
// Matches the OpenAPI UpdatePrivacySettings schema
// All fields are optional (nil means no change)
type UpdatePrivacySettingsRequest struct {
	// Note: GDPRNecessaryConsent is intentionally not updatable - always true
	// Attempting to set it to false will result in validation error
	GDPRNecessaryConsent    *bool `json:"gdpr_necessary_consent,omitempty"`
	GDPRAnalyticsConsent    *bool `json:"gdpr_analytics_consent,omitempty"`
	MarketingEmailsEnabled  *bool `json:"marketing_emails_enabled,omitempty"`
	ProductUpdatesEnabled   *bool `json:"product_updates_enabled,omitempty"`
	SecurityAlertsEnabled   *bool `json:"security_alerts_enabled,omitempty"`
	DataSharingEnabled      *bool `json:"data_sharing_enabled,omitempty"`
	IncludeInAggregateStats *bool `json:"include_in_aggregate_stats,omitempty"`
}

// NewPrivacySettings creates a new PrivacySettings object with GDPR-compliant defaults
// Called when a user first registers or when privacy settings don't exist
func NewPrivacySettings(userID string) *PrivacySettings {
	now := time.Now()
	return &PrivacySettings{
		UserID: userID,
		// GDPR compliance: necessary consent always true
		GDPRNecessaryConsent: true,
		// Opt-in model: analytics defaults to false
		GDPRAnalyticsConsent: false,
		GDPRConsentTimestamp: now,
		// Email preferences: marketing opt-in, updates/security opt-out
		MarketingEmailsEnabled: false,
		ProductUpdatesEnabled:  true,
		SecurityAlertsEnabled:  true,
		// Data sharing opt-in
		DataSharingEnabled: false,
		// Aggregate stats: opt-out model (included by default)
		IncludeInAggregateStats: true,
		// Timestamps
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Validate ensures the update request is GDPR-compliant
// Returns an error if user attempts to disable necessary consent
func (req *UpdatePrivacySettingsRequest) Validate() error {
	// GDPR compliance: necessary consent cannot be disabled
	if req.GDPRNecessaryConsent != nil && !*req.GDPRNecessaryConsent {
		return errors.New("gdpr_necessary_consent cannot be disabled - required for app functionality")
	}
	return nil
}

// ApplyTo applies the update request to the given privacy settings
// Updates the consent timestamp and updated_at timestamp
// Only modifies fields that are non-nil in the request
func (req *UpdatePrivacySettingsRequest) ApplyTo(settings *PrivacySettings) {
	now := time.Now()

	// Apply each field if present in the update request
	if req.GDPRAnalyticsConsent != nil {
		settings.GDPRAnalyticsConsent = *req.GDPRAnalyticsConsent
	}
	if req.MarketingEmailsEnabled != nil {
		settings.MarketingEmailsEnabled = *req.MarketingEmailsEnabled
	}
	if req.ProductUpdatesEnabled != nil {
		settings.ProductUpdatesEnabled = *req.ProductUpdatesEnabled
	}
	if req.SecurityAlertsEnabled != nil {
		settings.SecurityAlertsEnabled = *req.SecurityAlertsEnabled
	}
	if req.DataSharingEnabled != nil {
		settings.DataSharingEnabled = *req.DataSharingEnabled
	}
	if req.IncludeInAggregateStats != nil {
		settings.IncludeInAggregateStats = *req.IncludeInAggregateStats
	}

	// Update timestamps
	settings.GDPRConsentTimestamp = now
	settings.UpdatedAt = now
}
