package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"gopkg.in/yaml.v3"
)

// DashboardLayoutConfig holds the default dashboard layout configuration
// Loaded from config/default_dashboard_layout.yaml (centralized config directory)
// Issue #449: Centralize default dashboard layout configuration
type DashboardLayoutConfig struct {
	LayoutVersion int            `yaml:"layout_version"`
	Widgets       []WidgetYAML   `yaml:"widgets"`
	mu            sync.RWMutex   `yaml:"-"` // Protects concurrent access
}

// WidgetYAML represents a widget configuration in YAML format
// Uses snake_case field names for YAML, converts to domain.WidgetConfig
type WidgetYAML struct {
	ID   string `yaml:"id"`
	Type string `yaml:"type"`
	X    int    `yaml:"x"`
	Y    int    `yaml:"y"`
	W    int    `yaml:"w"`
	H    int    `yaml:"h"`
	MinW *int   `yaml:"minW,omitempty"`
	MinH *int   `yaml:"minH,omitempty"`
	MaxW *int   `yaml:"maxW,omitempty"`
	MaxH *int   `yaml:"maxH,omitempty"`
}

// Global dashboard layout config instance (singleton)
var (
	globalDashboardLayoutConfig *DashboardLayoutConfig
	dashboardLayoutConfigOnce   sync.Once
	dashboardLayoutConfigError  error
)

// LoadDashboardLayoutConfig loads the default dashboard layout configuration from YAML file
// Returns cached instance on subsequent calls (singleton pattern)
func LoadDashboardLayoutConfig() (*DashboardLayoutConfig, error) {
	dashboardLayoutConfigOnce.Do(func() {
		globalDashboardLayoutConfig, dashboardLayoutConfigError = loadDashboardLayoutFromFile()
	})
	return globalDashboardLayoutConfig, dashboardLayoutConfigError
}

// loadDashboardLayoutFromFile reads and parses the dashboard layout YAML file
func loadDashboardLayoutFromFile() (*DashboardLayoutConfig, error) {
	// Get config file path from environment or use default
	configPath := getEnv("DASHBOARD_LAYOUT_CONFIG", "config/default_dashboard_layout.yaml")

	// Make path absolute if it's relative
	if !filepath.IsAbs(configPath) {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}

		// Try current directory first
		if _, err := os.Stat(filepath.Join(wd, configPath)); err == nil {
			configPath = filepath.Join(wd, configPath)
		} else {
			// Try going up directories to find project root
			dir := wd
			found := false
			for i := 0; i < 5; i++ { // Try up to 5 levels up
				testPath := filepath.Join(dir, configPath)
				if _, err := os.Stat(testPath); err == nil {
					configPath = testPath
					found = true
					break
				}
				dir = filepath.Dir(dir)
			}
			if !found {
				return nil, fmt.Errorf("config file not found at %s (searched from %s)", configPath, wd)
			}
		}
	}

	// Read the YAML file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read dashboard layout config file %s: %w", configPath, err)
	}

	// Parse YAML
	var config DashboardLayoutConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse dashboard layout config YAML: %w", err)
	}

	// Validate configuration
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("dashboard layout config validation failed: %w", err)
	}

	return &config, nil
}

// validate checks that the dashboard layout configuration is valid
func (c *DashboardLayoutConfig) validate() error {
	if c.LayoutVersion <= 0 {
		return fmt.Errorf("layout_version must be a positive integer, got %d", c.LayoutVersion)
	}

	if len(c.Widgets) == 0 {
		return fmt.Errorf("at least one widget must be defined")
	}

	// Track widget IDs to check for duplicates
	seenIDs := make(map[string]bool)

	for i, widget := range c.Widgets {
		if widget.ID == "" {
			return fmt.Errorf("widget at index %d has empty id", i)
		}

		if seenIDs[widget.ID] {
			return fmt.Errorf("duplicate widget id: %s", widget.ID)
		}
		seenIDs[widget.ID] = true

		if widget.Type == "" {
			return fmt.Errorf("widget '%s' has empty type", widget.ID)
		}

		// Validate widget type is a known type
		if !isValidWidgetType(widget.Type) {
			return fmt.Errorf("widget '%s' has invalid type: %s", widget.ID, widget.Type)
		}

		// Validate positions and sizes
		if widget.X < 0 || widget.X > 11 {
			return fmt.Errorf("widget '%s' has invalid x position: %d (must be 0-11)", widget.ID, widget.X)
		}

		if widget.Y < 0 {
			return fmt.Errorf("widget '%s' has invalid y position: %d (must be >= 0)", widget.ID, widget.Y)
		}

		if widget.W < 1 || widget.W > 12 {
			return fmt.Errorf("widget '%s' has invalid width: %d (must be 1-12)", widget.ID, widget.W)
		}

		if widget.H < 1 {
			return fmt.Errorf("widget '%s' has invalid height: %d (must be >= 1)", widget.ID, widget.H)
		}

		// Validate widget fits in grid
		if widget.X+widget.W > 12 {
			return fmt.Errorf("widget '%s' extends beyond grid (x=%d, w=%d)", widget.ID, widget.X, widget.W)
		}

		// Validate min constraints if present
		if widget.MinW != nil && *widget.MinW < 1 {
			return fmt.Errorf("widget '%s' has invalid minW: %d (must be >= 1)", widget.ID, *widget.MinW)
		}

		if widget.MinH != nil && *widget.MinH < 1 {
			return fmt.Errorf("widget '%s' has invalid minH: %d (must be >= 1)", widget.ID, *widget.MinH)
		}
	}

	return nil
}

// isValidWidgetType checks if the widget type is a known registered type
// Uses domain.ValidWidgetTypes() as single source of truth
func isValidWidgetType(widgetType string) bool {
	return domain.ValidWidgetTypes()[domain.WidgetType(widgetType)]
}

// GetDefaultLayout returns the default dashboard layout as a domain object
// Thread-safe with read lock
func (c *DashboardLayoutConfig) GetDefaultLayout() *domain.DashboardLayout {
	c.mu.RLock()
	defer c.mu.RUnlock()

	widgets := make([]domain.WidgetConfig, len(c.Widgets))
	for i, w := range c.Widgets {
		widgets[i] = domain.WidgetConfig{
			ID:   w.ID,
			Type: domain.WidgetType(w.Type),
			X:    w.X,
			Y:    w.Y,
			W:    w.W,
			H:    w.H,
			MinW: w.MinW,
			MinH: w.MinH,
			MaxW: w.MaxW,
			MaxH: w.MaxH,
		}
	}

	return &domain.DashboardLayout{
		Widgets: widgets,
		Version: c.LayoutVersion,
	}
}

// GetLayoutVersion returns the layout version
// Thread-safe with read lock
func (c *DashboardLayoutConfig) GetLayoutVersion() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.LayoutVersion
}

// Reload reloads the dashboard layout configuration from disk
// Useful for configuration hot-reloading without restart
// Thread-safe with write lock
func (c *DashboardLayoutConfig) Reload() error {
	newConfig, err := loadDashboardLayoutFromFile()
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.LayoutVersion = newConfig.LayoutVersion
	c.Widgets = newConfig.Widgets

	return nil
}

// ResetDashboardLayoutConfigSingleton resets the singleton for testing purposes
// Should only be used in tests
func ResetDashboardLayoutConfigSingleton() {
	dashboardLayoutConfigOnce = sync.Once{}
	globalDashboardLayoutConfig = nil
	dashboardLayoutConfigError = nil
}
