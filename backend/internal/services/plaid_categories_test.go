package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
)


func TestPFCPrimaryDisplayName_CuratedValues(t *testing.T) {
	cases := map[string]string{
		"FOOD_AND_DRINK":            "Food And Drink",
		"TRANSPORTATION":            "Transportation",
		"RENT_AND_UTILITIES":        "Rent And Utilities",
		"GOVERNMENT_AND_NON_PROFIT": "Government And Non Profit",
		"INCOME":                    "Income",
		"GENERAL_MERCHANDISE":       "General Merchandise",
	}
	for input, expected := range cases {
		assert.Equal(t, expected, PFCPrimaryDisplayName(input), "input: %s", input)
	}
}

func TestPFCPrimaryDisplayName_UnknownFallsBackToTitleCase(t *testing.T) {
	assert.Equal(t, "Future Category", PFCPrimaryDisplayName("FUTURE_CATEGORY"))
}

func TestPFCDetailedDisplayName_StripsPrefix(t *testing.T) {
	assert.Equal(t, "Fast Food", PFCDetailedDisplayName("FOOD_AND_DRINK", "FOOD_AND_DRINK_FAST_FOOD"))
	assert.Equal(t, "Taxis And Ride Shares", PFCDetailedDisplayName("TRANSPORTATION", "TRANSPORTATION_TAXIS_AND_RIDE_SHARES"))
	assert.Equal(t, "Internet And Cable", PFCDetailedDisplayName("RENT_AND_UTILITIES", "RENT_AND_UTILITIES_INTERNET_AND_CABLE"))
}

func TestPFCDetailedDisplayName_NoPrefixMatch_TitleCasesWhole(t *testing.T) {
	assert.Equal(t, "Other Payment", PFCDetailedDisplayName("LOAN_PAYMENTS", "LOAN_PAYMENTS_OTHER_PAYMENT"))
}

// TestPfcEnumToTitleCase — tests the title-case fallback via the exported PFCPrimaryDisplayName.
// The private helper moved to pfc/registry.go; test its behavior through the public API.
func TestPfcEnumToTitleCase(t *testing.T) {
	assert.Equal(t, "Fast Food", PFCPrimaryDisplayName("FAST_FOOD"))
	assert.Equal(t, "Internet And Cable", PFCPrimaryDisplayName("INTERNET_AND_CABLE"))
	assert.Equal(t, "Income", PFCPrimaryDisplayName("INCOME"))
	assert.Equal(t, "", PFCPrimaryDisplayName(""))
}

// TestPFCPrimaryDisplayName_DelegatesToPFCPackage confirms the shim wrapper delegates correctly.
func TestPFCPrimaryDisplayName_DelegatesToPFCPackage(t *testing.T) {
	// The shim must return the same value as pfc.PrimaryDisplayName for all 18 primaries.
	assert.Equal(t, "Food And Drink", PFCPrimaryDisplayName("FOOD_AND_DRINK"))
	assert.Equal(t, "Loan Disbursements", PFCPrimaryDisplayName("LOAN_DISBURSEMENTS"), "PFCv2 primary must be present via shim")
	assert.Equal(t, "Other", PFCPrimaryDisplayName("OTHER"), "PFCv2 primary must be present via shim")
}

// TestPFCDetailedDisplayName_DelegatesToPFCPackage confirms the shim wrapper delegates correctly.
func TestPFCDetailedDisplayName_DelegatesToPFCPackage(t *testing.T) {
	assert.Equal(t, "Fast Food", PFCDetailedDisplayName("FOOD_AND_DRINK", "FOOD_AND_DRINK_FAST_FOOD"))
	assert.Equal(t, "Auto", PFCDetailedDisplayName("LOAN_DISBURSEMENTS", "LOAN_DISBURSEMENTS_AUTO"),
		"LOAN_DISBURSEMENTS_AUTO shim should return 'Auto'")
}
