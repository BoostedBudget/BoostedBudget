package services

import (
	"encoding/json"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
)

// ─── Shim layer ───────────────────────────────────────────────────────────────
// The canonical PFC taxonomy now lives in backend/internal/pfc/registry.go.
// The functions below are one-line wrappers that delegate to pfc.* so that
// existing call sites (budget_service.go, analytics_service.go,
// transaction_mapper.go, plaid_sync_provider.go, …) continue to compile
// without touching every file in this PR.
//
// shim only — do not add logic here; edit pfc/registry.go instead.

// PFCPrimaryDisplayName returns the human-readable display name for a Plaid PFC primary enum.
// Falls back to title-casing the enum for unknown values.
//
// shim only — do not add logic here; edit pfc/registry.go instead.
func PFCPrimaryDisplayName(primary string) string {
	return pfc.PrimaryDisplayName(primary)
}

// PFCDetailedDisplayName derives a short display name for a Plaid PFC detailed enum by
// stripping the primary prefix. e.g. ("FOOD_AND_DRINK", "FOOD_AND_DRINK_FAST_FOOD") → "Fast Food".
//
// shim only — do not add logic here; edit pfc/registry.go instead.
func PFCDetailedDisplayName(primary, detailed string) string {
	return pfc.DetailedDisplayName(primary, detailed)
}

// PFCToCategoryArray builds the []string stored in transactions.category when Plaid returns
// no legacy category but does return personal_finance_category. Returns nil when primary is
// empty so callers can treat partial PFC as absent.
//
// shim only — do not add logic here; edit pfc/registry.go instead.
func PFCToCategoryArray(primary, detailed string) []string {
	return pfc.PFCToCategoryArray(primary, detailed)
}

// AllPFCPrimaries returns all 18 PFCv2 primary categories sorted alphabetically by display name.
//
// shim only — do not add logic here; edit pfc/registry.go instead.
func AllPFCPrimaries() []pfc.Primary {
	return pfc.AllPrimaries()
}

// AllPFCDetailed returns all PFCv2 detailed categories sorted by primary display then display.
//
// shim only — do not add logic here; edit pfc/registry.go instead.
func AllPFCDetailed() []pfc.Detailed {
	return pfc.AllDetailed()
}

// ParsePersonalFinanceCategoryJSON parses the raw PFC JSON string stored in the DB and returns
// a structured representation with human-readable display names. Returns nil when pfcJSON is nil
// or the primary field is empty.
//
// This helper stays in services (not pfc/) because it depends on domain.ParsedPersonalFinanceCategoryData,
// and pfc/ must remain a leaf package with zero project-internal imports to avoid import cycles.
func ParsePersonalFinanceCategoryJSON(pfcJSON *string) *domain.ParsedPersonalFinanceCategoryData {
	if pfcJSON == nil {
		return nil
	}
	var raw struct {
		Primary         string `json:"primary"`
		Detailed        string `json:"detailed"`
		ConfidenceLevel string `json:"confidence_level"`
	}
	if err := json.Unmarshal([]byte(*pfcJSON), &raw); err != nil {
		return nil
	}
	if raw.Primary == "" {
		return nil
	}
	result := &domain.ParsedPersonalFinanceCategoryData{
		Primary:         raw.Primary,
		PrimaryDisplay:  PFCPrimaryDisplayName(raw.Primary),
		ConfidenceLevel: raw.ConfidenceLevel,
	}
	if raw.Detailed != "" {
		result.Detailed = raw.Detailed
		result.DetailedDisplay = PFCDetailedDisplayName(raw.Primary, raw.Detailed)
	}
	return result
}
