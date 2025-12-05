package domain

import (
	"encoding/json"
	"errors"
	"time"
)

// WidgetType represents the type of widget
type WidgetType string

const (
	WidgetTypeBalanceOverview WidgetType = "balance-overview"
	WidgetTypeBalanceHistory  WidgetType = "balance-history"
	WidgetTypeCashFlowSummary WidgetType = "cash-flow-summary"
	WidgetTypeCashFlowChart   WidgetType = "cash-flow-chart"
	WidgetTypeAccountCards    WidgetType = "account-cards"
)

// ValidWidgetTypes returns a map of valid widget types
func ValidWidgetTypes() map[WidgetType]bool {
	return map[WidgetType]bool{
		WidgetTypeBalanceOverview: true,
		WidgetTypeBalanceHistory:  true,
		WidgetTypeCashFlowSummary: true,
		WidgetTypeCashFlowChart:   true,
		WidgetTypeAccountCards:    true,
	}
}

// WidgetConfig represents a single widget's configuration in the layout
// Compatible with react-grid-layout's LayoutItem structure
type WidgetConfig struct {
	ID   string     `json:"i"`    // Unique widget instance ID (react-grid-layout uses "i")
	Type WidgetType `json:"type"` // Widget type
	X    int        `json:"x"`    // Grid X position (0-11)
	Y    int        `json:"y"`    // Grid Y position
	W    int        `json:"w"`    // Width in grid units (1-12)
	H    int        `json:"h"`    // Height in grid units
	MinW *int       `json:"minW,omitempty"` // Minimum width
	MinH *int       `json:"minH,omitempty"` // Minimum height
	MaxW *int       `json:"maxW,omitempty"` // Maximum width
	MaxH *int       `json:"maxH,omitempty"` // Maximum height
}

// Validate checks that the WidgetConfig has valid values
func (w *WidgetConfig) Validate() error {
	if w.ID == "" {
		return errors.New("widget id is required")
	}
	if w.Type == "" {
		return errors.New("widget type is required")
	}
	if !ValidWidgetTypes()[w.Type] {
		return errors.New("invalid widget type")
	}
	if w.X < 0 || w.X > 11 {
		return errors.New("x position must be between 0 and 11")
	}
	if w.Y < 0 {
		return errors.New("y position must be >= 0")
	}
	if w.W < 1 || w.W > 12 {
		return errors.New("width must be between 1 and 12")
	}
	if w.H < 1 {
		return errors.New("height must be >= 1")
	}
	return nil
}

// DashboardLayout represents the user's full dashboard layout
type DashboardLayout struct {
	Widgets []WidgetConfig `json:"widgets"`
	Version int            `json:"version"` // For future migrations
}

// Validate checks that the DashboardLayout has valid values
func (l *DashboardLayout) Validate() error {
	for i, widget := range l.Widgets {
		if err := widget.Validate(); err != nil {
			return errors.New("widget " + widget.ID + " at index " + string(rune(i+'0')) + ": " + err.Error())
		}
	}
	return nil
}

// DashboardPreferences represents a user's dashboard preferences stored in the database
type DashboardPreferences struct {
	ID        string          `json:"id" db:"id"`
	UserID    string          `json:"user_id" db:"user_id"`
	Layout    json.RawMessage `json:"layout" db:"layout"` // JSONB of DashboardLayout
	Version   int             `json:"version" db:"version"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt time.Time       `json:"updated_at" db:"updated_at"`
}

// GetLayout parses the Layout JSONB into a DashboardLayout struct
func (p *DashboardPreferences) GetLayout() (*DashboardLayout, error) {
	var layout DashboardLayout
	if err := json.Unmarshal(p.Layout, &layout); err != nil {
		return nil, err
	}
	return &layout, nil
}

// SetLayout sets the Layout from a DashboardLayout struct
func (p *DashboardPreferences) SetLayout(layout *DashboardLayout) error {
	data, err := json.Marshal(layout)
	if err != nil {
		return err
	}
	p.Layout = data
	p.Version = layout.Version
	return nil
}

// Validate checks that the DashboardPreferences has all required fields
func (p *DashboardPreferences) Validate() error {
	if p.UserID == "" {
		return errors.New("user_id is required")
	}
	if p.Layout == nil {
		return errors.New("layout is required")
	}
	// Parse and validate the layout
	layout, err := p.GetLayout()
	if err != nil {
		return errors.New("invalid layout JSON: " + err.Error())
	}
	return layout.Validate()
}

// DefaultDashboardLayout returns the default widget layout for new users
func DefaultDashboardLayout() *DashboardLayout {
	minW3, minH2 := 3, 2
	minW6, minH2Alt := 6, 2
	minW4, minH3 := 4, 3

	return &DashboardLayout{
		Widgets: []WidgetConfig{
			{ID: "balance-overview-1", Type: WidgetTypeBalanceOverview, X: 0, Y: 0, W: 4, H: 2, MinW: &minW3, MinH: &minH2},
			{ID: "cash-flow-summary-1", Type: WidgetTypeCashFlowSummary, X: 4, Y: 0, W: 8, H: 2, MinW: &minW6, MinH: &minH2Alt},
			{ID: "balance-history-1", Type: WidgetTypeBalanceHistory, X: 0, Y: 2, W: 6, H: 4, MinW: &minW4, MinH: &minH3},
			{ID: "cash-flow-chart-1", Type: WidgetTypeCashFlowChart, X: 6, Y: 2, W: 6, H: 4, MinW: &minW4, MinH: &minH3},
			{ID: "account-cards-1", Type: WidgetTypeAccountCards, X: 0, Y: 6, W: 12, H: 3, MinW: &minW6, MinH: &minH2Alt},
		},
		Version: 1,
	}
}

// DashboardPreferencesRequest represents the request body for PUT /api/v1/dashboard/preferences
type DashboardPreferencesRequest struct {
	Layout DashboardLayout `json:"layout"`
}

// DashboardPreferencesResponse represents the response for GET/PUT /api/v1/dashboard/preferences
type DashboardPreferencesResponse struct {
	Layout    DashboardLayout `json:"layout"`
	UpdatedAt time.Time       `json:"updated_at"`
}
