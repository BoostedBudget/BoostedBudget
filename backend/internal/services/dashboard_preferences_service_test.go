package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// createTestLayoutConfig creates a mock config for testing
// Issue #474: Service now requires layoutConfig dependency
func createTestLayoutConfig() *config.DashboardLayoutConfig {
	// Load actual config to ensure tests use real version
	cfg, err := config.LoadDashboardLayoutConfig()
	if err != nil {
		// Fallback to nil - tests should work with actual config
		return nil
	}
	return cfg
}

// mockDashboardPreferencesRepository is a mock implementation for testing
type mockDashboardPreferencesRepository struct {
	getByUserIDFn func(ctx context.Context, userID string) (*domain.DashboardPreferences, error)
	upsertFn      func(ctx context.Context, prefs *domain.DashboardPreferences) error
	deleteFn      func(ctx context.Context, userID string) error
}

func (m *mockDashboardPreferencesRepository) GetByUserID(ctx context.Context, userID string) (*domain.DashboardPreferences, error) {
	if m.getByUserIDFn != nil {
		return m.getByUserIDFn(ctx, userID)
	}
	return nil, repository.ErrDashboardPreferencesNotFound
}

func (m *mockDashboardPreferencesRepository) Upsert(ctx context.Context, prefs *domain.DashboardPreferences) error {
	if m.upsertFn != nil {
		return m.upsertFn(ctx, prefs)
	}
	prefs.UpdatedAt = time.Now()
	return nil
}

func (m *mockDashboardPreferencesRepository) Delete(ctx context.Context, userID string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, userID)
	}
	return nil
}

func TestDashboardPreferencesService_GetPreferences(t *testing.T) {
	ctx := context.Background()

	t.Run("returns saved preferences", func(t *testing.T) {
		savedLayout := domain.DashboardLayout{
			Widgets: []domain.WidgetConfig{
				{ID: "w1", Type: domain.WidgetTypeBalanceOverview, X: 0, Y: 0, W: 6, H: 3},
				{ID: "w2", Type: domain.WidgetTypeCashFlowChart, X: 6, Y: 0, W: 6, H: 3},
			},
			Version: 1,
		}
		layoutJSON, _ := json.Marshal(savedLayout)
		testTime := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)

		mockRepo := &mockDashboardPreferencesRepository{
			getByUserIDFn: func(ctx context.Context, userID string) (*domain.DashboardPreferences, error) {
				return &domain.DashboardPreferences{
					ID:        "pref-123",
					UserID:    userID,
					Layout:    layoutJSON,
					Version:   1,
					UpdatedAt: testTime,
				}, nil
			},
		}

		// Issue #474: Pass layoutConfig to service
		layoutConfig := createTestLayoutConfig()
		service := NewDashboardPreferencesService(mockRepo, layoutConfig)
		result, err := service.GetPreferences(ctx, "user-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Layout.Widgets) != 2 {
			t.Errorf("expected 2 widgets, got %d", len(result.Layout.Widgets))
		}
		if result.Layout.Widgets[0].ID != "w1" {
			t.Errorf("expected widget ID 'w1', got '%s'", result.Layout.Widgets[0].ID)
		}
		if !result.UpdatedAt.Equal(testTime) {
			t.Errorf("expected updatedAt %v, got %v", testTime, result.UpdatedAt)
		}
	})

	t.Run("returns default layout for new user", func(t *testing.T) {
		mockRepo := &mockDashboardPreferencesRepository{
			getByUserIDFn: func(ctx context.Context, userID string) (*domain.DashboardPreferences, error) {
				return nil, repository.ErrDashboardPreferencesNotFound
			},
		}

		// Issue #474: Pass layoutConfig to service
		layoutConfig := createTestLayoutConfig()
		service := NewDashboardPreferencesService(mockRepo, layoutConfig)
		result, err := service.GetPreferences(ctx, "new-user")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Issue #474: Now uses YAML config, which has 2 widgets (cash-flow + balance-history)
		expectedWidgetCount := 2
		if layoutConfig != nil {
			expectedWidgetCount = len(layoutConfig.GetDefaultLayout().Widgets)
		}
		if len(result.Layout.Widgets) != expectedWidgetCount {
			t.Errorf("expected %d widgets (from config), got %d", expectedWidgetCount, len(result.Layout.Widgets))
		}
		if result.UpdatedAt != (time.Time{}) {
			t.Errorf("expected zero UpdatedAt for default layout, got %v", result.UpdatedAt)
		}
	})

	t.Run("returns error on repository failure", func(t *testing.T) {
		mockRepo := &mockDashboardPreferencesRepository{
			getByUserIDFn: func(ctx context.Context, userID string) (*domain.DashboardPreferences, error) {
				return nil, errors.New("database error")
			},
		}

		// Issue #474: Pass layoutConfig to service
		layoutConfig := createTestLayoutConfig()
		service := NewDashboardPreferencesService(mockRepo, layoutConfig)
		_, err := service.GetPreferences(ctx, "user-123")

		if err == nil {
			t.Error("expected error from repository")
		}
	})
}

func TestDashboardPreferencesService_SavePreferences(t *testing.T) {
	ctx := context.Background()

	t.Run("saves valid layout", func(t *testing.T) {
		mockRepo := &mockDashboardPreferencesRepository{
			upsertFn: func(ctx context.Context, prefs *domain.DashboardPreferences) error {
				prefs.UpdatedAt = time.Now()
				return nil
			},
		}

		// Issue #474: Pass layoutConfig to service
		layoutConfig := createTestLayoutConfig()
		service := NewDashboardPreferencesService(mockRepo, layoutConfig)

		layout := domain.DashboardLayout{
			Widgets: []domain.WidgetConfig{
				{ID: "w1", Type: domain.WidgetTypeBalanceOverview, X: 0, Y: 0, W: 4, H: 2},
			},
			Version: 1,
		}

		result, err := service.SavePreferences(ctx, "user-123", layout)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Layout.Widgets) != 1 {
			t.Errorf("expected 1 widget, got %d", len(result.Layout.Widgets))
		}
		if result.UpdatedAt.IsZero() {
			t.Error("expected UpdatedAt to be set")
		}
	})

	t.Run("rejects invalid layout", func(t *testing.T) {
		mockRepo := &mockDashboardPreferencesRepository{}
		// Issue #474: Pass layoutConfig to service
		layoutConfig := createTestLayoutConfig()
		service := NewDashboardPreferencesService(mockRepo, layoutConfig)

		layout := domain.DashboardLayout{
			Widgets: []domain.WidgetConfig{
				{ID: "", Type: domain.WidgetTypeBalanceOverview, X: 0, Y: 0, W: 4, H: 2}, // Invalid: empty ID
			},
			Version: 1,
		}

		_, err := service.SavePreferences(ctx, "user-123", layout)

		if err == nil {
			t.Error("expected validation error")
		}
	})

	t.Run("returns error on repository failure", func(t *testing.T) {
		mockRepo := &mockDashboardPreferencesRepository{
			upsertFn: func(ctx context.Context, prefs *domain.DashboardPreferences) error {
				return errors.New("database error")
			},
		}

		// Issue #474: Pass layoutConfig to service
		layoutConfig := createTestLayoutConfig()
		service := NewDashboardPreferencesService(mockRepo, layoutConfig)

		layout := domain.DashboardLayout{
			Widgets: []domain.WidgetConfig{
				{ID: "w1", Type: domain.WidgetTypeBalanceOverview, X: 0, Y: 0, W: 4, H: 2},
			},
			Version: 1,
		}

		_, err := service.SavePreferences(ctx, "user-123", layout)

		if err == nil {
			t.Error("expected error from repository")
		}
	})
}

func TestDashboardPreferencesService_ResetToDefault(t *testing.T) {
	ctx := context.Background()

	t.Run("resets to default layout from YAML config", func(t *testing.T) {
		mockRepo := &mockDashboardPreferencesRepository{
			upsertFn: func(ctx context.Context, prefs *domain.DashboardPreferences) error {
				prefs.UpdatedAt = time.Now()
				return nil
			},
		}

		// Issue #474: Pass layoutConfig to service - this is the key fix
		layoutConfig := createTestLayoutConfig()
		service := NewDashboardPreferencesService(mockRepo, layoutConfig)
		result, err := service.ResetToDefault(ctx, "user-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Issue #474: Now uses YAML config, which has 2 widgets and version 4
		expectedLayout := layoutConfig.GetDefaultLayout()
		if len(result.Layout.Widgets) != len(expectedLayout.Widgets) {
			t.Errorf("expected %d widgets (from YAML config), got %d", len(expectedLayout.Widgets), len(result.Layout.Widgets))
		}

		// Issue #474: Verify version matches YAML config (currently 4)
		// This is the critical fix - previously returned version 2 causing infinite reset loop
		expectedVersion := expectedLayout.Version
		if result.Layout.Version != expectedVersion {
			t.Errorf("expected version %d (from YAML config), got %d", expectedVersion, result.Layout.Version)
		}
	})
}
