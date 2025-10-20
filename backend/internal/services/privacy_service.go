package services

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// PrivacyService defines the interface for privacy settings business logic
type PrivacyService interface {
	// GetPrivacySettings retrieves privacy settings for a user
	// Creates default GDPR-compliant settings if none exist (first-time user scenario)
	GetPrivacySettings(ctx context.Context, userID string) (*domain.PrivacySettings, error)

	// UpdatePrivacySettings validates and updates privacy settings
	// Enforces GDPR compliance rules (e.g., necessary consent cannot be disabled)
	// Returns updated settings on success
	UpdatePrivacySettings(ctx context.Context, userID string, req *domain.UpdatePrivacySettingsRequest) (*domain.PrivacySettings, error)
}

// privacyService implements PrivacyService
type privacyService struct {
	repo repository.PrivacyRepository
}

// NewPrivacyService creates a new PrivacyService instance
func NewPrivacyService(repo repository.PrivacyRepository) PrivacyService {
	return &privacyService{repo: repo}
}

// GetPrivacySettings retrieves privacy settings or creates defaults if they don't exist
// This follows the pattern of auto-creating user-specific resources on first access
func (s *privacyService) GetPrivacySettings(ctx context.Context, userID string) (*domain.PrivacySettings, error) {
	// Try to get existing privacy settings
	settings, err := s.repo.GetByUserID(ctx, userID)

	if err == repository.ErrPrivacySettingsNotFound {
		// First-time access - create default GDPR-compliant settings
		newSettings := domain.NewPrivacySettings(userID)
		if err := s.repo.Create(ctx, newSettings); err != nil {
			return nil, err
		}
		return newSettings, nil
	}

	if err != nil {
		return nil, err
	}

	return settings, nil
}

// UpdatePrivacySettings validates and updates privacy settings with GDPR compliance
func (s *privacyService) UpdatePrivacySettings(ctx context.Context, userID string, req *domain.UpdatePrivacySettingsRequest) (*domain.PrivacySettings, error) {
	// Validate request (GDPR compliance checks)
	// This ensures necessary consent cannot be disabled
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get existing privacy settings
	// Note: We don't auto-create here - user must have existing settings to update
	settings, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Apply the updates to the settings
	// This method updates consent_timestamp and updated_at automatically
	req.ApplyTo(settings)

	// Persist the updated settings to the database
	if err := s.repo.Update(ctx, settings); err != nil {
		return nil, err
	}

	// TODO: Audit log the privacy settings change for GDPR compliance
	// Should log: userID, timestamp, changed fields (old → new values), IP address, user agent
	// Example: s.auditLog.LogPrivacyChange(ctx, userID, changedFields)

	return settings, nil
}
