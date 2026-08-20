package domain

import (
	"encoding/json"
	"testing"
)

func TestWidgetConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		widget  WidgetConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid widget",
			widget: WidgetConfig{
				ID:   "balance-overview-1",
				Type: WidgetTypeBalanceOverview,
				X:    0,
				Y:    0,
				W:    4,
				H:    2,
			},
			wantErr: false,
		},
		{
			name: "missing id",
			widget: WidgetConfig{
				Type: WidgetTypeBalanceOverview,
				X:    0,
				Y:    0,
				W:    4,
				H:    2,
			},
			wantErr: true,
			errMsg:  "widget id is required",
		},
		{
			name: "missing type",
			widget: WidgetConfig{
				ID: "balance-overview-1",
				X:  0,
				Y:  0,
				W:  4,
				H:  2,
			},
			wantErr: true,
			errMsg:  "widget type is required",
		},
		{
			name: "invalid type",
			widget: WidgetConfig{
				ID:   "custom-1",
				Type: "invalid-type",
				X:    0,
				Y:    0,
				W:    4,
				H:    2,
			},
			wantErr: true,
			errMsg:  "invalid widget type",
		},
		{
			name: "negative x position",
			widget: WidgetConfig{
				ID:   "balance-overview-1",
				Type: WidgetTypeBalanceOverview,
				X:    -1,
				Y:    0,
				W:    4,
				H:    2,
			},
			wantErr: true,
			errMsg:  "x position must be between 0 and 11",
		},
		{
			name: "x position too large",
			widget: WidgetConfig{
				ID:   "balance-overview-1",
				Type: WidgetTypeBalanceOverview,
				X:    12,
				Y:    0,
				W:    4,
				H:    2,
			},
			wantErr: true,
			errMsg:  "x position must be between 0 and 11",
		},
		{
			name: "negative y position",
			widget: WidgetConfig{
				ID:   "balance-overview-1",
				Type: WidgetTypeBalanceOverview,
				X:    0,
				Y:    -1,
				W:    4,
				H:    2,
			},
			wantErr: true,
			errMsg:  "y position must be >= 0",
		},
		{
			name: "zero width",
			widget: WidgetConfig{
				ID:   "balance-overview-1",
				Type: WidgetTypeBalanceOverview,
				X:    0,
				Y:    0,
				W:    0,
				H:    2,
			},
			wantErr: true,
			errMsg:  "width must be between 1 and 12",
		},
		{
			name: "width too large",
			widget: WidgetConfig{
				ID:   "balance-overview-1",
				Type: WidgetTypeBalanceOverview,
				X:    0,
				Y:    0,
				W:    13,
				H:    2,
			},
			wantErr: true,
			errMsg:  "width must be between 1 and 12",
		},
		{
			name: "zero height",
			widget: WidgetConfig{
				ID:   "balance-overview-1",
				Type: WidgetTypeBalanceOverview,
				X:    0,
				Y:    0,
				W:    4,
				H:    0,
			},
			wantErr: true,
			errMsg:  "height must be >= 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.widget.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("WidgetConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && err.Error() != tt.errMsg {
				t.Errorf("WidgetConfig.Validate() error = %v, wantErrMsg %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestDashboardLayout_Validate(t *testing.T) {
	tests := []struct {
		name    string
		layout  DashboardLayout
		wantErr bool
	}{
		{
			name: "valid layout with widgets",
			layout: DashboardLayout{
				Widgets: []WidgetConfig{
					{ID: "w1", Type: WidgetTypeBalanceOverview, X: 0, Y: 0, W: 4, H: 2},
					{ID: "w2", Type: WidgetTypeCashFlowSummary, X: 4, Y: 0, W: 8, H: 2},
				},
				Version: 1,
			},
			wantErr: false,
		},
		{
			name: "empty layout is valid",
			layout: DashboardLayout{
				Widgets: []WidgetConfig{},
				Version: 1,
			},
			wantErr: false,
		},
		{
			name: "invalid widget in layout",
			layout: DashboardLayout{
				Widgets: []WidgetConfig{
					{ID: "", Type: WidgetTypeBalanceOverview, X: 0, Y: 0, W: 4, H: 2},
				},
				Version: 1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.layout.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("DashboardLayout.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDashboardPreferences_Validate(t *testing.T) {
	validLayout := DashboardLayout{
		Widgets: []WidgetConfig{
			{ID: "w1", Type: WidgetTypeBalanceOverview, X: 0, Y: 0, W: 4, H: 2},
		},
		Version: 1,
	}
	validLayoutJSON, _ := json.Marshal(validLayout)

	tests := []struct {
		name    string
		prefs   DashboardPreferences
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid preferences",
			prefs: DashboardPreferences{
				UserID: "user-123",
				Layout: validLayoutJSON,
			},
			wantErr: false,
		},
		{
			name: "missing user_id",
			prefs: DashboardPreferences{
				Layout: validLayoutJSON,
			},
			wantErr: true,
			errMsg:  "user_id is required",
		},
		{
			name: "missing layout",
			prefs: DashboardPreferences{
				UserID: "user-123",
			},
			wantErr: true,
			errMsg:  "layout is required",
		},
		{
			name: "invalid layout JSON",
			prefs: DashboardPreferences{
				UserID: "user-123",
				Layout: []byte("not json"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.prefs.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("DashboardPreferences.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" && err != nil && err.Error() != tt.errMsg {
				t.Errorf("DashboardPreferences.Validate() error = %v, wantErrMsg %v", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestDashboardPreferences_GetSetLayout(t *testing.T) {
	layout := &DashboardLayout{
		Widgets: []WidgetConfig{
			{ID: "w1", Type: WidgetTypeBalanceOverview, X: 0, Y: 0, W: 4, H: 2},
		},
		Version: 1,
	}

	prefs := &DashboardPreferences{
		UserID: "user-123",
	}

	// Test SetLayout
	err := prefs.SetLayout(layout)
	if err != nil {
		t.Errorf("SetLayout() error = %v", err)
	}

	// Test GetLayout
	retrieved, err := prefs.GetLayout()
	if err != nil {
		t.Errorf("GetLayout() error = %v", err)
	}

	if len(retrieved.Widgets) != 1 {
		t.Errorf("GetLayout() widgets count = %v, expected 1", len(retrieved.Widgets))
	}

	if retrieved.Widgets[0].ID != "w1" {
		t.Errorf("GetLayout() widget ID = %v, expected w1", retrieved.Widgets[0].ID)
	}

	if prefs.Version != 1 {
		t.Errorf("SetLayout() should set Version, got %v, expected 1", prefs.Version)
	}
}

func TestDefaultDashboardLayout(t *testing.T) {
	layout := DefaultDashboardLayout()

	if layout == nil {
		t.Fatal("DefaultDashboardLayout() returned nil")
	}

	// Issue #474: Version 4 with 2 widgets (synced with config/default_dashboard_layout.yaml)
	if len(layout.Widgets) != 2 {
		t.Errorf("DefaultDashboardLayout() widgets count = %v, expected 2", len(layout.Widgets))
	}

	if layout.Version != 4 {
		t.Errorf("DefaultDashboardLayout() version = %v, expected 4", layout.Version)
	}

	// Verify the layout is valid
	err := layout.Validate()
	if err != nil {
		t.Errorf("DefaultDashboardLayout() produced invalid layout: %v", err)
	}

	// Verify the first widget is cash-flow-chart at full width
	if len(layout.Widgets) > 0 {
		widget := layout.Widgets[0]
		if widget.Type != WidgetTypeCashFlowChart {
			t.Errorf("DefaultDashboardLayout() expected cash-flow-chart widget, got %v", widget.Type)
		}
		if widget.W != 12 {
			t.Errorf("DefaultDashboardLayout() widget width = %v, expected 12 (full width)", widget.W)
		}
	}

	// Issue #474: Verify second widget is balance-history
	if len(layout.Widgets) > 1 {
		widget := layout.Widgets[1]
		if widget.Type != WidgetTypeBalanceHistory {
			t.Errorf("DefaultDashboardLayout() expected balance-history widget as second widget, got %v", widget.Type)
		}
	}
}

func TestValidWidgetTypes(t *testing.T) {
	types := ValidWidgetTypes()
	allWidgetTypes := AllWidgetTypes()

	// ValidWidgetTypes should contain all types from AllWidgetTypes
	if len(types) != len(allWidgetTypes) {
		t.Errorf("ValidWidgetTypes() count = %v, expected %v", len(types), len(allWidgetTypes))
	}

	for _, info := range allWidgetTypes {
		if !types[info.Type] {
			t.Errorf("ValidWidgetTypes() missing type: %v", info.Type)
		}
	}
}

// Issue #1297: Investment widget types

func TestInvestmentWidgetTypes_Present(t *testing.T) {
	allTypes := AllWidgetTypes()
	validTypes := ValidWidgetTypes()

	investmentTypes := []WidgetType{
		WidgetTypePortfolioSummary,
		WidgetTypePortfolioHoldings,
		WidgetTypeAssetAllocation,
	}

	for _, wt := range investmentTypes {
		t.Run(string(wt), func(t *testing.T) {
			// Must be in ValidWidgetTypes
			if !validTypes[wt] {
				t.Errorf("ValidWidgetTypes() missing investment type: %v", wt)
			}

			// Must be in AllWidgetTypes with investments category
			var found *WidgetTypeInfo
			for i := range allTypes {
				if allTypes[i].Type == wt {
					found = &allTypes[i]
					break
				}
			}
			if found == nil {
				t.Errorf("AllWidgetTypes() missing investment type: %v", wt)
				return
			}
			if found.Category != WidgetCategoryInvestments {
				t.Errorf("AllWidgetTypes() type %v category = %v, want %v", wt, found.Category, WidgetCategoryInvestments)
			}
		})
	}
}

func TestInvestmentWidgetTypes_Sizes(t *testing.T) {
	tests := []struct {
		wt        WidgetType
		defaultW  int
		defaultH  int
		minW      int
		minH      int
	}{
		{WidgetTypePortfolioSummary, 6, 3, 4, 2},
		{WidgetTypePortfolioHoldings, 6, 4, 4, 3},
		{WidgetTypeAssetAllocation, 4, 4, 3, 3},
	}

	for _, tt := range tests {
		t.Run(string(tt.wt), func(t *testing.T) {
			info := GetWidgetTypeInfo(tt.wt)
			if info == nil {
				t.Fatalf("GetWidgetTypeInfo(%v) returned nil", tt.wt)
			}
			if info.DefaultWidth != tt.defaultW {
				t.Errorf("DefaultWidth = %v, want %v", info.DefaultWidth, tt.defaultW)
			}
			if info.DefaultHeight != tt.defaultH {
				t.Errorf("DefaultHeight = %v, want %v", info.DefaultHeight, tt.defaultH)
			}
			if info.MinWidth != tt.minW {
				t.Errorf("MinWidth = %v, want %v", info.MinWidth, tt.minW)
			}
			if info.MinHeight != tt.minH {
				t.Errorf("MinHeight = %v, want %v", info.MinHeight, tt.minH)
			}
		})
	}
}

func TestInvestmentWidgetTypes_ValidInLayout(t *testing.T) {
	// New types must pass WidgetConfig.Validate() so layout save doesn't reject them
	widgets := []WidgetConfig{
		{ID: "portfolio-summary-1", Type: WidgetTypePortfolioSummary, X: 0, Y: 0, W: 6, H: 3},
		{ID: "portfolio-holdings-1", Type: WidgetTypePortfolioHoldings, X: 6, Y: 0, W: 6, H: 4},
		{ID: "asset-allocation-1", Type: WidgetTypeAssetAllocation, X: 0, Y: 4, W: 4, H: 4},
	}
	for _, w := range widgets {
		if err := w.Validate(); err != nil {
			t.Errorf("WidgetConfig{Type: %v}.Validate() = %v, want nil", w.Type, err)
		}
	}
}

// Issue #1297: WidgetConfig.Settings — opaque JSONB blob, must round-trip through Get/SetLayout

func TestWidgetConfig_Settings_RoundTrip(t *testing.T) {
	settings := map[string]any{
		"topN":   float64(10), // JSON numbers unmarshal as float64
		"sortBy": "value",
	}
	layout := &DashboardLayout{
		Widgets: []WidgetConfig{
			{
				ID:       "portfolio-holdings-1",
				Type:     WidgetTypePortfolioHoldings,
				X:        0,
				Y:        0,
				W:        6,
				H:        4,
				Settings: settings,
			},
		},
		Version: 4,
	}

	prefs := &DashboardPreferences{UserID: "user-1"}
	if err := prefs.SetLayout(layout); err != nil {
		t.Fatalf("SetLayout() error = %v", err)
	}

	got, err := prefs.GetLayout()
	if err != nil {
		t.Fatalf("GetLayout() error = %v", err)
	}
	if len(got.Widgets) != 1 {
		t.Fatalf("GetLayout() widgets count = %v, want 1", len(got.Widgets))
	}

	gotSettings := got.Widgets[0].Settings
	if gotSettings == nil {
		t.Fatal("GetLayout() widget Settings is nil after round-trip")
	}
	if gotSettings["topN"] != float64(10) {
		t.Errorf("settings topN = %v, want 10", gotSettings["topN"])
	}
	if gotSettings["sortBy"] != "value" {
		t.Errorf("settings sortBy = %v, want value", gotSettings["sortBy"])
	}
}

func TestWidgetConfig_Settings_NilPreserved(t *testing.T) {
	// A widget with no settings must round-trip with nil Settings (back-compat for old rows)
	layout := &DashboardLayout{
		Widgets: []WidgetConfig{
			{ID: "balance-overview-1", Type: WidgetTypeBalanceOverview, X: 0, Y: 0, W: 4, H: 2},
		},
		Version: 4,
	}
	prefs := &DashboardPreferences{UserID: "user-1"}
	if err := prefs.SetLayout(layout); err != nil {
		t.Fatalf("SetLayout() error = %v", err)
	}
	got, err := prefs.GetLayout()
	if err != nil {
		t.Fatalf("GetLayout() error = %v", err)
	}
	if got.Widgets[0].Settings != nil {
		t.Errorf("expected nil Settings for widget with no settings, got %v", got.Widgets[0].Settings)
	}
}

func TestWidgetConfig_Settings_ScalarAndArrayDoNotPanic(t *testing.T) {
	// Old or malformed rows might have non-object settings; Validate must not panic.
	// We test this by unmarshalling raw JSON directly (simulating a DB read).
	rawLayouts := []string{
		// null settings
		`{"widgets":[{"i":"w1","type":"balance-overview","x":0,"y":0,"w":4,"h":2,"settings":null}],"version":4}`,
		// scalar settings (string) — not valid but must not panic
		`{"widgets":[{"i":"w1","type":"balance-overview","x":0,"y":0,"w":4,"h":2,"settings":"bad"}],"version":4}`,
		// array settings — not valid but must not panic
		`{"widgets":[{"i":"w1","type":"balance-overview","x":0,"y":0,"w":4,"h":2,"settings":[1,2,3]}],"version":4}`,
		// missing settings field entirely
		`{"widgets":[{"i":"w1","type":"balance-overview","x":0,"y":0,"w":4,"h":2}],"version":4}`,
	}

	for _, raw := range rawLayouts {
		prefs := &DashboardPreferences{
			UserID: "user-1",
			Layout: []byte(raw),
		}
		// Must not panic; error is acceptable for malformed inputs
		got, _ := prefs.GetLayout()
		if got != nil {
			// If parsing succeeded, Validate should not panic
			_ = got.Validate()
		}
	}
}
