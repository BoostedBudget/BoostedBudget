package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// DashboardPreferencesService provides dashboard layout preference management
// Issue #354: Customizable Dashboard Widget System
type DashboardPreferencesService interface {
	// GetPreferences retrieves dashboard preferences for a user
	// Returns default layout if no preferences exist
	GetPreferences(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error)

	// SavePreferences saves dashboard layout preferences for a user
	SavePreferences(ctx context.Context, userID string, layout domain.DashboardLayout) (*domain.DashboardPreferencesResponse, error)

	// ResetToDefault resets dashboard preferences to the default layout
	ResetToDefault(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error)
}

type dashboardPreferencesService struct {
	prefsRepo repository.DashboardPreferencesRepository
}

// NewDashboardPreferencesService creates a new DashboardPreferencesService instance
func NewDashboardPreferencesService(prefsRepo repository.DashboardPreferencesRepository) DashboardPreferencesService {
	return &dashboardPreferencesService{
		prefsRepo: prefsRepo,
	}
}

// GetPreferences retrieves dashboard preferences for a user
func (s *dashboardPreferencesService) GetPreferences(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error) {
	prefs, err := s.prefsRepo.GetByUserID(ctx, userID)
	if err == repository.ErrDashboardPreferencesNotFound {
		// Return default layout for new users
		defaultLayout := domain.DefaultDashboardLayout()
		return &domain.DashboardPreferencesResponse{
			Layout: *defaultLayout,
			// UpdatedAt is zero time for default layout (no saved preferences)
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get dashboard preferences: %w", err)
	}

	// Parse the stored layout
	layout, err := prefs.GetLayout()
	if err != nil {
		return nil, fmt.Errorf("failed to parse dashboard layout: %w", err)
	}

	return &domain.DashboardPreferencesResponse{
		Layout:    *layout,
		UpdatedAt: prefs.UpdatedAt,
	}, nil
}

// SavePreferences saves dashboard layout preferences for a user
func (s *dashboardPreferencesService) SavePreferences(ctx context.Context, userID string, layout domain.DashboardLayout) (*domain.DashboardPreferencesResponse, error) {
	// Validate the layout
	if err := layout.Validate(); err != nil {
		return nil, fmt.Errorf("invalid layout: %w", err)
	}

	// Convert layout to JSON
	layoutJSON, err := json.Marshal(layout)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal layout: %w", err)
	}

	prefs := &domain.DashboardPreferences{
		UserID:  userID,
		Layout:  layoutJSON,
		Version: layout.Version,
	}

	// Upsert (create or update)
	if err := s.prefsRepo.Upsert(ctx, prefs); err != nil {
		return nil, fmt.Errorf("failed to save dashboard preferences: %w", err)
	}

	return &domain.DashboardPreferencesResponse{
		Layout:    layout,
		UpdatedAt: prefs.UpdatedAt,
	}, nil
}

// ResetToDefault resets dashboard preferences to the default layout
func (s *dashboardPreferencesService) ResetToDefault(ctx context.Context, userID string) (*domain.DashboardPreferencesResponse, error) {
	defaultLayout := domain.DefaultDashboardLayout()

	// Save the default layout
	return s.SavePreferences(ctx, userID, *defaultLayout)
}
