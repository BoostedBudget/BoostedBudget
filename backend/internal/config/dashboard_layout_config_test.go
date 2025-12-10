package config

import (
	"os"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDashboardLayoutConfig(t *testing.T) {
	// Reset singleton for testing
	ResetDashboardLayoutConfigSingleton()
	os.Unsetenv("DASHBOARD_LAYOUT_CONFIG")

	config, err := LoadDashboardLayoutConfig()
	require.NoError(t, err)
	assert.NotNil(t, config)

	// Verify layout version is set
	assert.Greater(t, config.LayoutVersion, 0)

	// Verify widgets are loaded
	assert.Greater(t, len(config.Widgets), 0)
}

func TestLoadDashboardLayoutConfig_Singleton(t *testing.T) {
	// Reset singleton for testing
	ResetDashboardLayoutConfigSingleton()
	os.Unsetenv("DASHBOARD_LAYOUT_CONFIG")

	// First call
	config1, err := LoadDashboardLayoutConfig()
	require.NoError(t, err)

	// Second call should return same instance
	config2, err := LoadDashboardLayoutConfig()
	require.NoError(t, err)

	// Same pointer address
	assert.Equal(t, config1, config2)
}

func TestDashboardLayoutConfig_GetDefaultLayout(t *testing.T) {
	// Reset singleton for testing
	ResetDashboardLayoutConfigSingleton()
	os.Unsetenv("DASHBOARD_LAYOUT_CONFIG")

	config, err := LoadDashboardLayoutConfig()
	require.NoError(t, err)

	layout := config.GetDefaultLayout()
	require.NotNil(t, layout)

	// Verify layout is a domain.DashboardLayout
	assert.Equal(t, config.LayoutVersion, layout.Version)
	assert.Equal(t, len(config.Widgets), len(layout.Widgets))

	// Verify widgets are converted correctly
	for i, widget := range layout.Widgets {
		assert.Equal(t, config.Widgets[i].ID, widget.ID)
		assert.Equal(t, domain.WidgetType(config.Widgets[i].Type), widget.Type)
		assert.Equal(t, config.Widgets[i].X, widget.X)
		assert.Equal(t, config.Widgets[i].Y, widget.Y)
		assert.Equal(t, config.Widgets[i].W, widget.W)
		assert.Equal(t, config.Widgets[i].H, widget.H)
	}
}

func TestDashboardLayoutConfig_GetLayoutVersion(t *testing.T) {
	// Reset singleton for testing
	ResetDashboardLayoutConfigSingleton()
	os.Unsetenv("DASHBOARD_LAYOUT_CONFIG")

	config, err := LoadDashboardLayoutConfig()
	require.NoError(t, err)

	version := config.GetLayoutVersion()
	assert.Greater(t, version, 0)
	assert.Equal(t, config.LayoutVersion, version)
}

func TestDashboardLayoutConfig_Validation(t *testing.T) {
	// Reset singleton for testing
	ResetDashboardLayoutConfigSingleton()
	os.Unsetenv("DASHBOARD_LAYOUT_CONFIG")

	config, err := LoadDashboardLayoutConfig()
	require.NoError(t, err)

	// Verify each widget has valid configuration
	for _, widget := range config.Widgets {
		t.Run(widget.ID, func(t *testing.T) {
			assert.NotEmpty(t, widget.ID, "widget id should not be empty")
			assert.NotEmpty(t, widget.Type, "widget type should not be empty")
			assert.GreaterOrEqual(t, widget.X, 0, "widget x should be >= 0")
			assert.LessOrEqual(t, widget.X, 11, "widget x should be <= 11")
			assert.GreaterOrEqual(t, widget.Y, 0, "widget y should be >= 0")
			assert.Greater(t, widget.W, 0, "widget w should be > 0")
			assert.LessOrEqual(t, widget.W, 12, "widget w should be <= 12")
			assert.Greater(t, widget.H, 0, "widget h should be > 0")
			assert.LessOrEqual(t, widget.X+widget.W, 12, "widget should fit in grid")
		})
	}
}

func TestDashboardLayoutConfig_InvalidPath(t *testing.T) {
	// Set invalid config path
	os.Setenv("DASHBOARD_LAYOUT_CONFIG", "/nonexistent/path/config.yaml")
	defer os.Unsetenv("DASHBOARD_LAYOUT_CONFIG")

	// Reset singleton
	ResetDashboardLayoutConfigSingleton()

	_, err := LoadDashboardLayoutConfig()
	assert.Error(t, err)
}

func TestDashboardLayoutConfig_Reload(t *testing.T) {
	// Reset environment and singleton
	os.Unsetenv("DASHBOARD_LAYOUT_CONFIG")
	ResetDashboardLayoutConfigSingleton()

	config, err := LoadDashboardLayoutConfig()
	require.NoError(t, err)

	originalVersion := config.LayoutVersion
	originalWidgetCount := len(config.Widgets)

	// Reload should succeed
	err = config.Reload()
	assert.NoError(t, err)

	// Verify still has same version and widgets (config file unchanged)
	assert.Equal(t, originalVersion, config.LayoutVersion)
	assert.Equal(t, originalWidgetCount, len(config.Widgets))
}

func TestDashboardLayoutConfig_WidgetTypesValid(t *testing.T) {
	// Reset singleton for testing
	ResetDashboardLayoutConfigSingleton()
	os.Unsetenv("DASHBOARD_LAYOUT_CONFIG")

	config, err := LoadDashboardLayoutConfig()
	require.NoError(t, err)

	// All widget types should be valid
	validTypes := map[string]bool{
		"balance-overview":  true,
		"balance-history":   true,
		"cash-flow-chart":   true,
		"cash-flow-summary": true,
		"account-cards":     true,
	}

	for _, widget := range config.Widgets {
		t.Run(widget.ID, func(t *testing.T) {
			assert.True(t, validTypes[widget.Type], "widget type '%s' should be valid", widget.Type)
		})
	}
}

func TestDashboardLayoutConfig_UniqueWidgetIDs(t *testing.T) {
	// Reset singleton for testing
	ResetDashboardLayoutConfigSingleton()
	os.Unsetenv("DASHBOARD_LAYOUT_CONFIG")

	config, err := LoadDashboardLayoutConfig()
	require.NoError(t, err)

	// All widget IDs should be unique
	seenIDs := make(map[string]bool)
	for _, widget := range config.Widgets {
		assert.False(t, seenIDs[widget.ID], "widget id '%s' should be unique", widget.ID)
		seenIDs[widget.ID] = true
	}
}

func TestIsValidWidgetType(t *testing.T) {
	tests := []struct {
		widgetType string
		valid      bool
	}{
		{"balance-overview", true},
		{"balance-history", true},
		{"cash-flow-chart", true},
		{"cash-flow-summary", true},
		{"account-cards", true},
		{"invalid-type", false},
		{"", false},
		{"BALANCE-OVERVIEW", false}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.widgetType, func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidWidgetType(tt.widgetType))
		})
	}
}

func TestDashboardLayoutConfig_MatchesExpectedLayout(t *testing.T) {
	// Reset singleton for testing
	ResetDashboardLayoutConfigSingleton()
	os.Unsetenv("DASHBOARD_LAYOUT_CONFIG")

	config, err := LoadDashboardLayoutConfig()
	require.NoError(t, err)

	// Issue #449: Verify the expected default layout (version 3, 2 widgets)
	assert.Equal(t, 3, config.LayoutVersion, "layout version should be 3")
	assert.Equal(t, 2, len(config.Widgets), "should have 2 widgets")

	// Verify first widget is cash-flow-chart
	assert.Equal(t, "cash-flow-chart-1", config.Widgets[0].ID)
	assert.Equal(t, "cash-flow-chart", config.Widgets[0].Type)

	// Verify second widget is balance-history
	assert.Equal(t, "balance-history-1", config.Widgets[1].ID)
	assert.Equal(t, "balance-history", config.Widgets[1].Type)
}
