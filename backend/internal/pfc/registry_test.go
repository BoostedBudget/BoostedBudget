package pfc_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/pfc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAllPrimaries_Returns18_Sorted locks the PFCv2 count: 18, not 16.
func TestAllPrimaries_Returns18_Sorted(t *testing.T) {
	primaries := pfc.AllPrimaries()
	assert.Len(t, primaries, 18, "PFCv2 has 18 primary categories (16 PFCv1 + LOAN_DISBURSEMENTS + OTHER)")

	// Verify sorted by display name
	for i := 1; i < len(primaries); i++ {
		assert.True(t, primaries[i-1].Display <= primaries[i].Display,
			"AllPrimaries should be sorted by display name, got %q before %q",
			primaries[i-1].Display, primaries[i].Display)
	}
}

// TestAllPrimaries_IncludesPFCv2Additions asserts both new PFCv2 primaries are present.
func TestAllPrimaries_IncludesPFCv2Additions(t *testing.T) {
	primaries := pfc.AllPrimaries()

	enumSet := make(map[string]string)
	for _, p := range primaries {
		enumSet[p.Enum] = p.Display
	}

	assert.Equal(t, "Loan Disbursements", enumSet["LOAN_DISBURSEMENTS"],
		"PFCv2 addition LOAN_DISBURSEMENTS should map to 'Loan Disbursements'")
	assert.Equal(t, "Other", enumSet["OTHER"],
		"PFCv2 addition OTHER should map to 'Other'")
}

// TestAllPrimaries_DisplayNamesMatchRegistry verifies every Primary has a non-empty enum and display.
func TestAllPrimaries_DisplayNamesMatchRegistry(t *testing.T) {
	for _, p := range pfc.AllPrimaries() {
		assert.NotEmpty(t, p.Enum, "Primary enum must not be empty")
		assert.NotEmpty(t, p.Display, "Primary display must not be empty")
	}
}

// TestAllPrimaries_EnumKeysMatchRegistry verifies PrimaryDisplayName lookup agrees with AllPrimaries.
func TestAllPrimaries_EnumKeysMatchRegistry(t *testing.T) {
	for _, p := range pfc.AllPrimaries() {
		got := pfc.PrimaryDisplayName(p.Enum)
		assert.Equal(t, p.Display, got,
			"PrimaryDisplayName(%q) should return %q", p.Enum, p.Display)
	}
}

// TestAllDetailed_SnapshotEnumSet is an exact-set snapshot test.
// Same-count taxonomy mutations (a renamed enum at the same total count) fail loudly.
// The expected slice is hand-maintained alongside the registry.
func TestAllDetailed_SnapshotEnumSet(t *testing.T) {
	allDetailed := pfc.AllDetailed()
	actual := make([]string, len(allDetailed))
	for i, d := range allDetailed {
		actual[i] = d.Enum
	}
	sort.Strings(actual)

	expected := []string{
		// BANK_FEES (8)
		"BANK_FEES_ATM_FEES",
		"BANK_FEES_CASH_ADVANCE",
		"BANK_FEES_FOREIGN_TRANSACTION_FEES",
		"BANK_FEES_INSUFFICIENT_FUNDS",
		"BANK_FEES_INTEREST_CHARGE",
		"BANK_FEES_LATE_FEES",
		"BANK_FEES_OVERDRAFT_FEES",
		"BANK_FEES_OTHER_BANK_FEES",
		// ENTERTAINMENT (6)
		"ENTERTAINMENT_CASINOS_AND_GAMBLING",
		"ENTERTAINMENT_MUSIC_AND_AUDIO",
		"ENTERTAINMENT_OTHER_ENTERTAINMENT",
		"ENTERTAINMENT_SPORTING_EVENTS_AMUSEMENT_PARKS_AND_MUSEUMS",
		"ENTERTAINMENT_TV_AND_MOVIES",
		"ENTERTAINMENT_VIDEO_GAMES",
		// FOOD_AND_DRINK (7)
		"FOOD_AND_DRINK_BEER_WINE_AND_LIQUOR",
		"FOOD_AND_DRINK_COFFEE",
		"FOOD_AND_DRINK_FAST_FOOD",
		"FOOD_AND_DRINK_GROCERIES",
		"FOOD_AND_DRINK_OTHER_FOOD_AND_DRINK",
		"FOOD_AND_DRINK_RESTAURANT",
		"FOOD_AND_DRINK_VENDING_MACHINES",
		// GENERAL_MERCHANDISE (14)
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
		// GENERAL_SERVICES (9)
		"GENERAL_SERVICES_ACCOUNTING_AND_FINANCIAL_PLANNING",
		"GENERAL_SERVICES_AUTOMOTIVE",
		"GENERAL_SERVICES_CHILDCARE",
		"GENERAL_SERVICES_CONSULTING_AND_LEGAL",
		"GENERAL_SERVICES_EDUCATION",
		"GENERAL_SERVICES_INSURANCE",
		"GENERAL_SERVICES_OTHER_GENERAL_SERVICES",
		"GENERAL_SERVICES_POSTAGE_AND_SHIPPING",
		"GENERAL_SERVICES_STORAGE",
		// GOVERNMENT_AND_NON_PROFIT (4)
		"GOVERNMENT_AND_NON_PROFIT_DONATIONS",
		"GOVERNMENT_AND_NON_PROFIT_GOVERNMENT_DEPARTMENTS_AND_AGENCIES",
		"GOVERNMENT_AND_NON_PROFIT_OTHER_GOVERNMENT_AND_NON_PROFIT",
		"GOVERNMENT_AND_NON_PROFIT_TAX_PAYMENT",
		// HOME_IMPROVEMENT (5)
		"HOME_IMPROVEMENT_FURNITURE",
		"HOME_IMPROVEMENT_HARDWARE",
		"HOME_IMPROVEMENT_OTHER_HOME_IMPROVEMENT",
		"HOME_IMPROVEMENT_REPAIR_AND_MAINTENANCE",
		"HOME_IMPROVEMENT_SECURITY",
		// INCOME (13)
		"INCOME_CHILD_SUPPORT",
		"INCOME_CONTRACTOR",
		"INCOME_DIVIDENDS",
		"INCOME_GIG_ECONOMY",
		"INCOME_INTEREST_EARNED",
		"INCOME_LONG_TERM_DISABILITY",
		"INCOME_MILITARY",
		"INCOME_OTHER_INCOME",
		"INCOME_RENTAL",
		"INCOME_RETIREMENT_PENSION",
		"INCOME_TAX_REFUND",
		"INCOME_UNEMPLOYMENT",
		"INCOME_WAGES",
		// LOAN_DISBURSEMENTS (7) — new PFCv2 primary
		"LOAN_DISBURSEMENTS_AUTO",
		"LOAN_DISBURSEMENTS_COMMERCIAL",
		"LOAN_DISBURSEMENTS_EQUITY_LOAN",
		"LOAN_DISBURSEMENTS_MEDICAL",
		"LOAN_DISBURSEMENTS_OTHER_LOAN_DISBURSEMENTS",
		"LOAN_DISBURSEMENTS_PERSONAL",
		"LOAN_DISBURSEMENTS_STUDENT_LOAN",
		// LOAN_PAYMENTS (6)
		"LOAN_PAYMENTS_CAR_PAYMENT",
		"LOAN_PAYMENTS_CREDIT_CARD_PAYMENT",
		"LOAN_PAYMENTS_MORTGAGE_PAYMENT",
		"LOAN_PAYMENTS_OTHER_PAYMENT",
		"LOAN_PAYMENTS_PERSONAL_LOAN_PAYMENT",
		"LOAN_PAYMENTS_STUDENT_LOAN_PAYMENT",
		// MEDICAL (7)
		"MEDICAL_DENTAL_CARE",
		"MEDICAL_EYE_CARE",
		"MEDICAL_NURSING_CARE",
		"MEDICAL_OTHER_MEDICAL",
		"MEDICAL_PHARMACIES_AND_SUPPLEMENTS",
		"MEDICAL_PRIMARY_CARE",
		"MEDICAL_VETERINARY_SERVICES",
		// OTHER (1) — new PFCv2 primary
		"OTHER_OTHER",
		// PERSONAL_CARE (4)
		"PERSONAL_CARE_GYMS_AND_FITNESS_CENTERS",
		"PERSONAL_CARE_HAIR_AND_BEAUTY",
		"PERSONAL_CARE_LAUNDRY_AND_DRY_CLEANING",
		"PERSONAL_CARE_OTHER_PERSONAL_CARE",
		// RENT_AND_UTILITIES (7)
		"RENT_AND_UTILITIES_GAS_AND_ELECTRICITY",
		"RENT_AND_UTILITIES_INTERNET_AND_CABLE",
		"RENT_AND_UTILITIES_OTHER_UTILITIES",
		"RENT_AND_UTILITIES_RENT",
		"RENT_AND_UTILITIES_SEWAGE_AND_WASTE_MANAGEMENT",
		"RENT_AND_UTILITIES_TELEPHONE",
		"RENT_AND_UTILITIES_WATER",
		// TRANSFER_IN (8)
		"TRANSFER_IN_ACCOUNT_TRANSFER",
		"TRANSFER_IN_CASH_ADVANCES_AND_LOANS",
		"TRANSFER_IN_DEPOSIT",
		"TRANSFER_IN_FROM_APPS",
		"TRANSFER_IN_INVESTMENT_AND_RETIREMENT_FUNDS",
		"TRANSFER_IN_OTHER_TRANSFER_IN",
		"TRANSFER_IN_SAVINGS",
		"TRANSFER_IN_WIRE",
		// TRANSFER_OUT (8)
		"TRANSFER_OUT_ACCOUNT_TRANSFER",
		"TRANSFER_OUT_CRYPTO",
		"TRANSFER_OUT_FROM_APPS",
		"TRANSFER_OUT_INVESTMENT_AND_RETIREMENT_FUNDS",
		"TRANSFER_OUT_OTHER_TRANSFER_OUT",
		"TRANSFER_OUT_SAVINGS",
		"TRANSFER_OUT_WIRE",
		"TRANSFER_OUT_WITHDRAWAL",
		// TRANSPORTATION (7)
		"TRANSPORTATION_BIKES_AND_SCOOTERS",
		"TRANSPORTATION_GAS",
		"TRANSPORTATION_OTHER_TRANSPORTATION",
		"TRANSPORTATION_PARKING",
		"TRANSPORTATION_PUBLIC_TRANSIT",
		"TRANSPORTATION_TAXIS_AND_RIDE_SHARES",
		"TRANSPORTATION_TOLLS",
		// TRAVEL (4)
		"TRAVEL_FLIGHTS",
		"TRAVEL_LODGING",
		"TRAVEL_OTHER_TRAVEL",
		"TRAVEL_RENTAL_CARS",
	}
	sort.Strings(expected)

	assert.Equal(t, expected, actual, "Detailed enum snapshot mismatch — Plaid taxonomy may have changed. Update registry_test.go and pfc/registry.go together.")
	assert.Len(t, actual, len(expected), "Total detailed count should match snapshot (currently %d)", len(expected))
}

// TestAllDetailed_EveryDetailedHasKnownPrimary verifies every detailed enum's primary prefix
// matches a known curated primary.
func TestAllDetailed_EveryDetailedHasKnownPrimary(t *testing.T) {
	primarySet := make(map[string]bool)
	for _, p := range pfc.AllPrimaries() {
		primarySet[p.Enum] = true
	}

	for _, d := range pfc.AllDetailed() {
		assert.Equal(t, d.PrimaryEnum, d.PrimaryEnum, "sanity") // always true
		assert.True(t, primarySet[d.PrimaryEnum],
			"Detailed %q has unrecognized primary %q", d.Enum, d.PrimaryEnum)
		assert.True(t, strings.HasPrefix(d.Enum, d.PrimaryEnum+"_"),
			"Detailed enum %q should start with its primary %q followed by '_'", d.Enum, d.PrimaryEnum)
	}
}

// TestAllDetailed_LoanDisbursementsHasAll7Children confirms the PFCv2 new primary has all children.
func TestAllDetailed_LoanDisbursementsHasAll7Children(t *testing.T) {
	var loanDisbursements []pfc.Detailed
	for _, d := range pfc.AllDetailed() {
		if d.PrimaryEnum == "LOAN_DISBURSEMENTS" {
			loanDisbursements = append(loanDisbursements, d)
		}
	}
	require.Len(t, loanDisbursements, 7, "LOAN_DISBURSEMENTS should have exactly 7 detailed children in PFCv2")

	enumSet := make(map[string]bool)
	for _, d := range loanDisbursements {
		enumSet[d.Enum] = true
	}
	assert.True(t, enumSet["LOAN_DISBURSEMENTS_AUTO"])
	assert.True(t, enumSet["LOAN_DISBURSEMENTS_COMMERCIAL"])
	assert.True(t, enumSet["LOAN_DISBURSEMENTS_EQUITY_LOAN"])
	assert.True(t, enumSet["LOAN_DISBURSEMENTS_MEDICAL"])
	assert.True(t, enumSet["LOAN_DISBURSEMENTS_PERSONAL"])
	assert.True(t, enumSet["LOAN_DISBURSEMENTS_STUDENT_LOAN"])
	assert.True(t, enumSet["LOAN_DISBURSEMENTS_OTHER_LOAN_DISBURSEMENTS"])
}

// TestAllDetailed_IncomeIncludesPFCv2Additions verifies the 6 new income subcategories.
func TestAllDetailed_IncomeIncludesPFCv2Additions(t *testing.T) {
	incomeSet := make(map[string]bool)
	for _, d := range pfc.AllDetailed() {
		if d.PrimaryEnum == "INCOME" {
			incomeSet[d.Enum] = true
		}
	}
	assert.True(t, incomeSet["INCOME_CHILD_SUPPORT"])
	assert.True(t, incomeSet["INCOME_CONTRACTOR"])
	assert.True(t, incomeSet["INCOME_GIG_ECONOMY"])
	assert.True(t, incomeSet["INCOME_LONG_TERM_DISABILITY"])
	assert.True(t, incomeSet["INCOME_MILITARY"])
	assert.True(t, incomeSet["INCOME_RENTAL"])
}

// TestAllDetailed_DisplayDerivedFromHelper verifies the Display field on each Detailed row
// matches what DetailedDisplayName would return.
func TestAllDetailed_DisplayDerivedFromHelper(t *testing.T) {
	for _, d := range pfc.AllDetailed() {
		expected := pfc.DetailedDisplayName(d.PrimaryEnum, d.Enum)
		assert.Equal(t, expected, d.Display,
			"Detailed row for %q should have Display=%q, got %q", d.Enum, expected, d.Display)
	}
}

// TestAllDetailed_SortedByPrimaryThenDisplay verifies the sort order.
func TestAllDetailed_SortedByPrimaryThenDisplay(t *testing.T) {
	all := pfc.AllDetailed()
	for i := 1; i < len(all); i++ {
		prev, curr := all[i-1], all[i]
		if prev.PrimaryDisplay == curr.PrimaryDisplay {
			assert.True(t, prev.Display <= curr.Display,
				"Within primary %q, %q should sort before %q",
				prev.PrimaryDisplay, prev.Display, curr.Display)
		} else {
			assert.True(t, prev.PrimaryDisplay <= curr.PrimaryDisplay,
				"%q primary should sort before %q primary",
				prev.PrimaryDisplay, curr.PrimaryDisplay)
		}
	}
}
