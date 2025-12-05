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
		t.Error("DefaultDashboardLayout() returned nil")
	}

	if len(layout.Widgets) != 5 {
		t.Errorf("DefaultDashboardLayout() widgets count = %v, expected 5", len(layout.Widgets))
	}

	if layout.Version != 1 {
		t.Errorf("DefaultDashboardLayout() version = %v, expected 1", layout.Version)
	}

	// Verify the layout is valid
	err := layout.Validate()
	if err != nil {
		t.Errorf("DefaultDashboardLayout() produced invalid layout: %v", err)
	}

	// Check expected widget types
	expectedTypes := map[WidgetType]bool{
		WidgetTypeBalanceOverview: false,
		WidgetTypeBalanceHistory:  false,
		WidgetTypeCashFlowSummary: false,
		WidgetTypeCashFlowChart:   false,
		WidgetTypeAccountCards:    false,
	}

	for _, widget := range layout.Widgets {
		expectedTypes[widget.Type] = true
	}

	for widgetType, found := range expectedTypes {
		if !found {
			t.Errorf("DefaultDashboardLayout() missing widget type: %v", widgetType)
		}
	}
}

func TestValidWidgetTypes(t *testing.T) {
	types := ValidWidgetTypes()

	expectedTypes := []WidgetType{
		WidgetTypeBalanceOverview,
		WidgetTypeBalanceHistory,
		WidgetTypeCashFlowSummary,
		WidgetTypeCashFlowChart,
		WidgetTypeAccountCards,
	}

	for _, wt := range expectedTypes {
		if !types[wt] {
			t.Errorf("ValidWidgetTypes() missing type: %v", wt)
		}
	}

	if len(types) != len(expectedTypes) {
		t.Errorf("ValidWidgetTypes() count = %v, expected %v", len(types), len(expectedTypes))
	}
}
