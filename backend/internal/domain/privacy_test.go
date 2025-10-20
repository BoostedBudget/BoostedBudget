package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// TestPrivacySettings_JSONMarshaling tests that PrivacySettings struct
// correctly marshals and unmarshals to/from JSON matching the OpenAPI spec
func TestPrivacySettings_JSONMarshaling(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	original := &PrivacySettings{
		ID:                       "550e8400-e29b-41d4-a716-446655440000",
		UserID:                   "650e8400-e29b-41d4-a716-446655440001",
		GDPRNecessaryConsent:     true,
		GDPRAnalyticsConsent:     true,
		GDPRAdvertisingConsent:   false,
		GDPRConsentTimestamp:     now,
		MarketingEmailsEnabled:   false,
		ProductUpdatesEnabled:    true,
		SecurityAlertsEnabled:    true,
		DataSharingEnabled:       false,
		CreatedAt:                now,
		UpdatedAt:                now,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal PrivacySettings: %v", err)
	}

	// Unmarshal back
	var unmarshaled PrivacySettings
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal PrivacySettings: %v", err)
	}

	// Verify all fields match
	if unmarshaled.ID != original.ID {
		t.Errorf("ID mismatch: got %v, want %v", unmarshaled.ID, original.ID)
	}
	if unmarshaled.UserID != original.UserID {
		t.Errorf("UserID mismatch: got %v, want %v", unmarshaled.UserID, original.UserID)
	}
	if unmarshaled.GDPRNecessaryConsent != original.GDPRNecessaryConsent {
		t.Errorf("GDPRNecessaryConsent mismatch: got %v, want %v", unmarshaled.GDPRNecessaryConsent, original.GDPRNecessaryConsent)
	}
	if unmarshaled.GDPRAnalyticsConsent != original.GDPRAnalyticsConsent {
		t.Errorf("GDPRAnalyticsConsent mismatch: got %v, want %v", unmarshaled.GDPRAnalyticsConsent, original.GDPRAnalyticsConsent)
	}
}

// TestNewPrivacySettings tests that NewPrivacySettings creates
// a privacy settings object with correct GDPR-compliant defaults
func TestNewPrivacySettings(t *testing.T) {
	userID := "650e8400-e29b-41d4-a716-446655440001"

	settings := NewPrivacySettings(userID)

	if settings == nil {
		t.Fatal("NewPrivacySettings returned nil")
	}

	if settings.UserID != userID {
		t.Errorf("UserID mismatch: got %v, want %v", settings.UserID, userID)
	}

	// GDPR compliance: necessary consent must always be true
	if settings.GDPRNecessaryConsent != true {
		t.Error("GDPRNecessaryConsent must default to true")
	}

	// Analytics and advertising should default to false (opt-in only)
	if settings.GDPRAnalyticsConsent != false {
		t.Error("GDPRAnalyticsConsent should default to false (opt-in)")
	}
	if settings.GDPRAdvertisingConsent != false {
		t.Error("GDPRAdvertisingConsent should default to false (opt-in)")
	}

	// Email preferences: marketing off by default, updates/security on
	if settings.MarketingEmailsEnabled != false {
		t.Error("MarketingEmailsEnabled should default to false (opt-in)")
	}
	if settings.ProductUpdatesEnabled != true {
		t.Error("ProductUpdatesEnabled should default to true")
	}
	if settings.SecurityAlertsEnabled != true {
		t.Error("SecurityAlertsEnabled should default to true")
	}

	// Data sharing off by default
	if settings.DataSharingEnabled != false {
		t.Error("DataSharingEnabled should default to false (opt-in)")
	}

	// Timestamps should be set
	if settings.GDPRConsentTimestamp.IsZero() {
		t.Error("GDPRConsentTimestamp should be set")
	}
	if settings.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if settings.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

// TestUpdatePrivacySettingsRequest_Validate tests that validation
// ensures GDPR necessary consent cannot be disabled
func TestUpdatePrivacySettingsRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		request *UpdatePrivacySettingsRequest
		wantErr bool
	}{
		{
			name: "valid update - analytics consent",
			request: &UpdatePrivacySettingsRequest{
				GDPRAnalyticsConsent: boolPtr(true),
			},
			wantErr: false,
		},
		{
			name: "valid update - all optional fields",
			request: &UpdatePrivacySettingsRequest{
				GDPRAnalyticsConsent:   boolPtr(true),
				GDPRAdvertisingConsent: boolPtr(false),
				MarketingEmailsEnabled: boolPtr(true),
				ProductUpdatesEnabled:  boolPtr(false),
				SecurityAlertsEnabled:  boolPtr(true),
				DataSharingEnabled:     boolPtr(false),
			},
			wantErr: false,
		},
		{
			name: "invalid - attempt to disable necessary consent",
			request: &UpdatePrivacySettingsRequest{
				GDPRNecessaryConsent: boolPtr(false),
			},
			wantErr: true,
		},
		{
			name:    "valid - empty update (no changes)",
			request: &UpdatePrivacySettingsRequest{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestUpdatePrivacySettingsRequest_ApplyTo tests that applying an update
// correctly modifies the privacy settings and updates the consent timestamp
func TestUpdatePrivacySettingsRequest_ApplyTo(t *testing.T) {
	originalSettings := NewPrivacySettings("user123")
	originalTimestamp := originalSettings.GDPRConsentTimestamp

	// Wait a tiny bit to ensure timestamp changes
	time.Sleep(10 * time.Millisecond)

	update := &UpdatePrivacySettingsRequest{
		GDPRAnalyticsConsent:   boolPtr(true),
		GDPRAdvertisingConsent: boolPtr(true),
		MarketingEmailsEnabled: boolPtr(true),
	}

	update.ApplyTo(originalSettings)

	// Verify changes applied
	if !originalSettings.GDPRAnalyticsConsent {
		t.Error("GDPRAnalyticsConsent should be updated to true")
	}
	if !originalSettings.GDPRAdvertisingConsent {
		t.Error("GDPRAdvertisingConsent should be updated to true")
	}
	if !originalSettings.MarketingEmailsEnabled {
		t.Error("MarketingEmailsEnabled should be updated to true")
	}

	// Verify fields not in update remain unchanged
	if !originalSettings.ProductUpdatesEnabled {
		t.Error("ProductUpdatesEnabled should remain unchanged")
	}

	// Verify consent timestamp was updated
	if !originalSettings.GDPRConsentTimestamp.After(originalTimestamp) {
		t.Error("GDPRConsentTimestamp should be updated when settings change")
	}

	// Verify UpdatedAt was updated
	if !originalSettings.UpdatedAt.After(originalSettings.CreatedAt) {
		t.Error("UpdatedAt should be updated")
	}
}

// Helper function to create bool pointers for tests
func boolPtr(b bool) *bool {
	return &b
}
