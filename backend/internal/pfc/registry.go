// Package pfc provides the canonical Plaid PFCv2 taxonomy maps and lookup helpers.
// This is a leaf package with zero project-internal imports so both domain and services
// can import it without creating a cycle.
//
// Source of truth: https://plaid.com/documents/pfc-taxonomy-all.csv
// PFCv2 released: December 2025 (adds LOAN_DISBURSEMENTS, OTHER + ~20 detailed entries)
package pfc

import (
	"sort"
	"strings"
)

// pfcPrimaryDisplayNames maps Plaid PFC primary enum values to human-readable display names.
// Refreshed to PFCv2 (December 2025): 18 primaries (PFCv1 16 + LOAN_DISBURSEMENTS + OTHER).
// Source: https://plaid.com/documents/pfc-taxonomy-all.csv
var pfcPrimaryDisplayNames = map[string]string{
	"BANK_FEES":                 "Bank Fees",
	"ENTERTAINMENT":             "Entertainment",
	"FOOD_AND_DRINK":            "Food And Drink",
	"GENERAL_MERCHANDISE":       "General Merchandise",
	"GENERAL_SERVICES":          "General Services",
	"GOVERNMENT_AND_NON_PROFIT": "Government And Non Profit",
	"HOME_IMPROVEMENT":          "Home Improvement",
	"INCOME":                    "Income",
	"LOAN_DISBURSEMENTS":        "Loan Disbursements", // PFCv2 addition
	"LOAN_PAYMENTS":             "Loan Payments",
	"MEDICAL":                   "Medical",
	"OTHER":                     "Other", // PFCv2 addition
	"PERSONAL_CARE":             "Personal Care",
	"RENT_AND_UTILITIES":        "Rent And Utilities",
	"TRANSFER_IN":               "Transfer In",
	"TRANSFER_OUT":              "Transfer Out",
	"TRANSPORTATION":            "Transportation",
	"TRAVEL":                    "Travel",
}

// pfcDetailedByPrimary maps each PFC primary enum to its ordered list of detailed enums.
// Hand-transcribed from Plaid PFCv2 CSV (December 2025).
// Source: https://plaid.com/documents/pfc-taxonomy-all.csv
// IMPORTANT: Keep in sync with TestAllDetailed_SnapshotEnumSet in registry_test.go.
var pfcDetailedByPrimary = map[string][]string{
	"BANK_FEES": {
		"BANK_FEES_ATM_FEES",
		"BANK_FEES_CASH_ADVANCE",        // PFCv2 addition
		"BANK_FEES_FOREIGN_TRANSACTION_FEES",
		"BANK_FEES_INSUFFICIENT_FUNDS",
		"BANK_FEES_INTEREST_CHARGE",
		"BANK_FEES_LATE_FEES",           // PFCv2 addition
		"BANK_FEES_OVERDRAFT_FEES",
		"BANK_FEES_OTHER_BANK_FEES",
	},
	"ENTERTAINMENT": {
		"ENTERTAINMENT_CASINOS_AND_GAMBLING",
		"ENTERTAINMENT_MUSIC_AND_AUDIO",
		"ENTERTAINMENT_OTHER_ENTERTAINMENT",
		"ENTERTAINMENT_SPORTING_EVENTS_AMUSEMENT_PARKS_AND_MUSEUMS",
		"ENTERTAINMENT_TV_AND_MOVIES",
		"ENTERTAINMENT_VIDEO_GAMES",
	},
	"FOOD_AND_DRINK": {
		"FOOD_AND_DRINK_BEER_WINE_AND_LIQUOR",
		"FOOD_AND_DRINK_COFFEE",
		"FOOD_AND_DRINK_FAST_FOOD",
		"FOOD_AND_DRINK_GROCERIES",
		"FOOD_AND_DRINK_OTHER_FOOD_AND_DRINK",
		"FOOD_AND_DRINK_RESTAURANT",
		"FOOD_AND_DRINK_VENDING_MACHINES",
	},
	"GENERAL_MERCHANDISE": {
		"GENERAL_MERCHANDISE_BOOKSTORES_AND_NEWSSTANDS",
		"GENERAL_MERCHANDISE_CLOTHING_AND_ACCESSORIES",
		"GENERAL_MERCHANDISE_CONVENIENCE_STORES",
		"GENERAL_MERCHANDISE_DEPARTMENT_STORES",
		"GENERAL_MERCHANDISE_DISCOUNT_STORES",
		"GENERAL_MERCHANDISE_ELECTRONICS",
		"GENERAL_MERCHANDISE_GIFTS_AND_NOVELTIES",
		"GENERAL_MERCHANDISE_OFFICE_SUPPLIES",
		"GENERAL_MERCHANDISE_ONLINE_MARKETPLACES",
		"GENERAL_MERCHANDISE_OTHER_GENERAL_MERCHANDISE",
		"GENERAL_MERCHANDISE_PET_SUPPLIES",
		"GENERAL_MERCHANDISE_SPORTING_GOODS",
		"GENERAL_MERCHANDISE_SUPERSTORES",
		"GENERAL_MERCHANDISE_TOBACCO_AND_VAPE",
	},
	"GENERAL_SERVICES": {
		"GENERAL_SERVICES_ACCOUNTING_AND_FINANCIAL_PLANNING",
		"GENERAL_SERVICES_AUTOMOTIVE",
		"GENERAL_SERVICES_CHILDCARE",
		"GENERAL_SERVICES_CONSULTING_AND_LEGAL",
		"GENERAL_SERVICES_EDUCATION",
		"GENERAL_SERVICES_INSURANCE",
		"GENERAL_SERVICES_OTHER_GENERAL_SERVICES",
		"GENERAL_SERVICES_POSTAGE_AND_SHIPPING",
		"GENERAL_SERVICES_STORAGE",
	},
	"GOVERNMENT_AND_NON_PROFIT": {
		"GOVERNMENT_AND_NON_PROFIT_DONATIONS",
		"GOVERNMENT_AND_NON_PROFIT_GOVERNMENT_DEPARTMENTS_AND_AGENCIES",
		"GOVERNMENT_AND_NON_PROFIT_OTHER_GOVERNMENT_AND_NON_PROFIT",
		"GOVERNMENT_AND_NON_PROFIT_TAX_PAYMENT",
	},
	"HOME_IMPROVEMENT": {
		"HOME_IMPROVEMENT_FURNITURE",
		"HOME_IMPROVEMENT_HARDWARE",
		"HOME_IMPROVEMENT_OTHER_HOME_IMPROVEMENT",
		"HOME_IMPROVEMENT_REPAIR_AND_MAINTENANCE",
		"HOME_IMPROVEMENT_SECURITY",
	},
	"INCOME": {
		"INCOME_CHILD_SUPPORT",       // PFCv2 addition
		"INCOME_CONTRACTOR",          // PFCv2 addition
		"INCOME_DIVIDENDS",
		"INCOME_GIG_ECONOMY",         // PFCv2 addition
		"INCOME_INTEREST_EARNED",
		"INCOME_LONG_TERM_DISABILITY", // PFCv2 addition
		"INCOME_MILITARY",            // PFCv2 addition
		"INCOME_OTHER_INCOME",
		"INCOME_RENTAL",              // PFCv2 addition
		"INCOME_RETIREMENT_PENSION",
		"INCOME_TAX_REFUND",
		"INCOME_UNEMPLOYMENT",
		"INCOME_WAGES",
	},
	// LOAN_DISBURSEMENTS is a PFCv2-new primary with 7 children.
	"LOAN_DISBURSEMENTS": {
		"LOAN_DISBURSEMENTS_AUTO",
		"LOAN_DISBURSEMENTS_COMMERCIAL",
		"LOAN_DISBURSEMENTS_EQUITY_LOAN",
		"LOAN_DISBURSEMENTS_MEDICAL",
		"LOAN_DISBURSEMENTS_OTHER_LOAN_DISBURSEMENTS",
		"LOAN_DISBURSEMENTS_PERSONAL",
		"LOAN_DISBURSEMENTS_STUDENT_LOAN",
	},
	"LOAN_PAYMENTS": {
		"LOAN_PAYMENTS_CAR_PAYMENT",
		"LOAN_PAYMENTS_CREDIT_CARD_PAYMENT",
		"LOAN_PAYMENTS_MORTGAGE_PAYMENT",
		"LOAN_PAYMENTS_OTHER_PAYMENT",
		"LOAN_PAYMENTS_PERSONAL_LOAN_PAYMENT",
		"LOAN_PAYMENTS_STUDENT_LOAN_PAYMENT",
	},
	"MEDICAL": {
		"MEDICAL_DENTAL_CARE",
		"MEDICAL_EYE_CARE",
		"MEDICAL_NURSING_CARE",
		"MEDICAL_OTHER_MEDICAL",
		"MEDICAL_PHARMACIES_AND_SUPPLEMENTS",
		"MEDICAL_PRIMARY_CARE",
		"MEDICAL_VETERINARY_SERVICES",
	},
	// OTHER is a PFCv2-new primary with one catch-all child.
	"OTHER": {
		"OTHER_OTHER",
	},
	"PERSONAL_CARE": {
		"PERSONAL_CARE_GYMS_AND_FITNESS_CENTERS",
		"PERSONAL_CARE_HAIR_AND_BEAUTY",
		"PERSONAL_CARE_LAUNDRY_AND_DRY_CLEANING",
		"PERSONAL_CARE_OTHER_PERSONAL_CARE",
	},
	"RENT_AND_UTILITIES": {
		"RENT_AND_UTILITIES_GAS_AND_ELECTRICITY",
		"RENT_AND_UTILITIES_INTERNET_AND_CABLE",
		"RENT_AND_UTILITIES_OTHER_UTILITIES",
		"RENT_AND_UTILITIES_RENT",
		"RENT_AND_UTILITIES_SEWAGE_AND_WASTE_MANAGEMENT",
		"RENT_AND_UTILITIES_TELEPHONE",
		"RENT_AND_UTILITIES_WATER",
	},
	"TRANSFER_IN": {
		"TRANSFER_IN_ACCOUNT_TRANSFER",
		"TRANSFER_IN_CASH_ADVANCES_AND_LOANS",
		"TRANSFER_IN_DEPOSIT",
		"TRANSFER_IN_FROM_APPS",             // PFCv2 addition
		"TRANSFER_IN_INVESTMENT_AND_RETIREMENT_FUNDS",
		"TRANSFER_IN_OTHER_TRANSFER_IN",
		"TRANSFER_IN_SAVINGS",
		"TRANSFER_IN_WIRE",                  // PFCv2 addition
	},
	"TRANSFER_OUT": {
		"TRANSFER_OUT_ACCOUNT_TRANSFER",
		"TRANSFER_OUT_CRYPTO",               // PFCv2 addition
		"TRANSFER_OUT_FROM_APPS",            // PFCv2 addition
		"TRANSFER_OUT_INVESTMENT_AND_RETIREMENT_FUNDS",
		"TRANSFER_OUT_OTHER_TRANSFER_OUT",
		"TRANSFER_OUT_SAVINGS",
		"TRANSFER_OUT_WIRE",                 // PFCv2 addition
		"TRANSFER_OUT_WITHDRAWAL",
	},
	"TRANSPORTATION": {
		"TRANSPORTATION_BIKES_AND_SCOOTERS",
		"TRANSPORTATION_GAS",
		"TRANSPORTATION_OTHER_TRANSPORTATION",
		"TRANSPORTATION_PARKING",
		"TRANSPORTATION_PUBLIC_TRANSIT",
		"TRANSPORTATION_TAXIS_AND_RIDE_SHARES",
		"TRANSPORTATION_TOLLS",
	},
	"TRAVEL": {
		"TRAVEL_FLIGHTS",     // PFCv2 addition
		"TRAVEL_LODGING",
		"TRAVEL_OTHER_TRAVEL",
		"TRAVEL_RENTAL_CARS",
	},
}

// Primary represents a single Plaid PFC primary category with its enum key and display name.
type Primary struct {
	Enum    string `json:"enum"`
	Display string `json:"display"`
}

// Detailed represents a single Plaid PFC detailed category entry.
type Detailed struct {
	PrimaryEnum    string `json:"primary_enum"`
	PrimaryDisplay string `json:"primary_display"`
	Enum           string `json:"enum"`
	Display        string `json:"display"`
}

// AllPrimaries returns all 18 PFCv2 primary categories sorted alphabetically by display name.
func AllPrimaries() []Primary {
	result := make([]Primary, 0, len(pfcPrimaryDisplayNames))
	for enum, display := range pfcPrimaryDisplayNames {
		result = append(result, Primary{Enum: enum, Display: display})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Display < result[j].Display
	})
	return result
}

// AllDetailed returns all PFCv2 detailed categories sorted by primary display name then
// detailed display name.
func AllDetailed() []Detailed {
	var result []Detailed
	for primaryEnum, children := range pfcDetailedByPrimary {
		primaryDisplay := PrimaryDisplayName(primaryEnum)
		for _, detailedEnum := range children {
			result = append(result, Detailed{
				PrimaryEnum:    primaryEnum,
				PrimaryDisplay: primaryDisplay,
				Enum:           detailedEnum,
				Display:        DetailedDisplayName(primaryEnum, detailedEnum),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].PrimaryDisplay != result[j].PrimaryDisplay {
			return result[i].PrimaryDisplay < result[j].PrimaryDisplay
		}
		return result[i].Display < result[j].Display
	})
	return result
}

// PrimaryDisplayName returns the human-readable display name for a Plaid PFC primary enum.
// Falls back to title-casing the enum for unknown values.
func PrimaryDisplayName(primary string) string {
	if display, ok := pfcPrimaryDisplayNames[primary]; ok {
		return display
	}
	return enumToTitleCase(primary)
}

// DetailedDisplayName derives a short display name for a Plaid PFC detailed enum by
// stripping the primary prefix. e.g. ("FOOD_AND_DRINK", "FOOD_AND_DRINK_FAST_FOOD") → "Fast Food".
func DetailedDisplayName(primary, detailed string) string {
	suffix := detailed
	if strings.HasPrefix(detailed, primary+"_") {
		suffix = detailed[len(primary)+1:]
	}
	return enumToTitleCase(suffix)
}

// enumToTitleCase converts SCREAMING_SNAKE_CASE to "Title Case With Spaces".
// e.g. "FAST_FOOD" → "Fast Food"
func enumToTitleCase(s string) string {
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
