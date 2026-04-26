package services

import (
	"encoding/json"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// pfcPrimaryDisplayNames maps Plaid PFC primary enum values to human-readable display names.
// These 16 values are the complete set as of Plaid PFC v2.
var pfcPrimaryDisplayNames = map[string]string{
	"INCOME":                    "Income",
	"TRANSFER_IN":               "Transfer In",
	"TRANSFER_OUT":              "Transfer Out",
	"LOAN_PAYMENTS":             "Loan Payments",
	"BANK_FEES":                 "Bank Fees",
	"ENTERTAINMENT":             "Entertainment",
	"FOOD_AND_DRINK":            "Food And Drink",
	"GENERAL_MERCHANDISE":       "General Merchandise",
	"HOME_IMPROVEMENT":          "Home Improvement",
	"MEDICAL":                   "Medical",
	"PERSONAL_CARE":             "Personal Care",
	"GENERAL_SERVICES":          "General Services",
	"GOVERNMENT_AND_NON_PROFIT": "Government And Non Profit",
	"TRANSPORTATION":            "Transportation",
	"TRAVEL":                    "Travel",
	"RENT_AND_UTILITIES":        "Rent And Utilities",
}

// PFCPrimaryDisplayName returns the human-readable display name for a Plaid PFC primary enum.
// Falls back to title-casing the enum for unknown values.
func PFCPrimaryDisplayName(primary string) string {
	if display, ok := pfcPrimaryDisplayNames[primary]; ok {
		return display
	}
	return pfcEnumToTitleCase(primary)
}

// PFCDetailedDisplayName derives a short display name for a Plaid PFC detailed enum by
// stripping the primary prefix. e.g. ("FOOD_AND_DRINK", "FOOD_AND_DRINK_FAST_FOOD") → "Fast Food".
func PFCDetailedDisplayName(primary, detailed string) string {
	suffix := detailed
	if strings.HasPrefix(detailed, primary+"_") {
		suffix = detailed[len(primary)+1:]
	}
	return pfcEnumToTitleCase(suffix)
}

// PFCToCategoryArray builds the []string stored in transactions.category when Plaid returns
// no legacy category but does return personal_finance_category. Returns nil when primary is
// empty so callers can treat partial PFC as absent.
func PFCToCategoryArray(primary, detailed string) []string {
	if primary == "" {
		return nil
	}
	result := []string{PFCPrimaryDisplayName(primary)}
	if detailed != "" {
		result = append(result, PFCDetailedDisplayName(primary, detailed))
	}
	return result
}

// ParsePersonalFinanceCategoryJSON parses the raw PFC JSON string stored in the DB and returns
// a structured representation with human-readable display names. Returns nil when pfcJSON is nil
// or the primary field is empty.
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

// pfcEnumToTitleCase converts SCREAMING_SNAKE_CASE to "Title Case With Spaces".
// e.g. "FAST_FOOD" → "Fast Food"
func pfcEnumToTitleCase(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, " ")
}
