package domain

// WidgetCategory represents the category of a widget for grouping in the catalog
type WidgetCategory string

const (
	WidgetCategoryOverview WidgetCategory = "overview"
	WidgetCategoryCharts   WidgetCategory = "charts"
	WidgetCategoryAccounts WidgetCategory = "accounts"
)

// WidgetTypeInfo contains metadata for a widget type
// This is the single source of truth for widget type definitions
type WidgetTypeInfo struct {
	Type          WidgetType     `json:"type"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Category      WidgetCategory `json:"category"`
	DefaultWidth  int            `json:"defaultWidth"`
	DefaultHeight int            `json:"defaultHeight"`
	MinWidth      int            `json:"minWidth"`
	MinHeight     int            `json:"minHeight"`
	MaxWidth      *int           `json:"maxWidth,omitempty"`
	MaxHeight     *int           `json:"maxHeight,omitempty"`
}

// AllWidgetTypes returns the canonical list of all widget types with their metadata
// This is the single source of truth for valid widget types
func AllWidgetTypes() []WidgetTypeInfo {
	maxW12, maxH8 := 12, 8
	maxW6, maxH6 := 6, 6

	return []WidgetTypeInfo{
		{
			Type:          WidgetTypeBalanceOverview,
			Name:          "Balance Overview",
			Description:   "Current balance summary with period comparison",
			Category:      WidgetCategoryOverview,
			DefaultWidth:  4,
			DefaultHeight: 3,
			MinWidth:      4,
			MinHeight:     3,
		},
		{
			Type:          WidgetTypeBalanceHistory,
			Name:          "Balance History",
			Description:   "Historical balance trend chart with account type breakdown",
			Category:      WidgetCategoryCharts,
			DefaultWidth:  6,
			DefaultHeight: 4,
			MinWidth:      4,
			MinHeight:     4,
			MaxWidth:      &maxW12,
			MaxHeight:     &maxH8,
		},
		{
			Type:          WidgetTypeCashFlowSummary,
			Name:          "Cash Flow Summary",
			Description:   "Income, expenses, and net cash flow with period comparison",
			Category:      WidgetCategoryOverview,
			DefaultWidth:  8,
			DefaultHeight: 3,
			MinWidth:      4,
			MinHeight:     3,
		},
		{
			Type:          WidgetTypeCashFlowChart,
			Name:          "Cash Flow Chart",
			Description:   "Bar chart showing income vs expenses with net cash flow trend",
			Category:      WidgetCategoryCharts,
			DefaultWidth:  6,
			DefaultHeight: 4,
			MinWidth:      4,
			MinHeight:     4,
		},
		{
			Type:          WidgetTypeAccountCards,
			Name:          "Account Cards",
			Description:   "Individual account balance cards",
			Category:      WidgetCategoryAccounts,
			DefaultWidth:  12,
			DefaultHeight: 3,
			MinWidth:      4,
			MinHeight:     3,
		},
		{
			Type:          WidgetTypeNetWorth,
			Name:          "Net Worth",
			Description:   "Track your total net worth with assets and liabilities breakdown. Toggle trend chart on/off.",
			Category:      WidgetCategoryOverview,
			DefaultWidth:  4,
			DefaultHeight: 4,
			MinWidth:      3,
			MinHeight:     3,
			MaxWidth:      &maxW6,
			MaxHeight:     &maxH6,
		},
	}
}

// GetWidgetTypeInfo returns the metadata for a specific widget type
// Returns nil if the widget type is not found
func GetWidgetTypeInfo(widgetType WidgetType) *WidgetTypeInfo {
	for _, info := range AllWidgetTypes() {
		if info.Type == widgetType {
			return &info
		}
	}
	return nil
}
