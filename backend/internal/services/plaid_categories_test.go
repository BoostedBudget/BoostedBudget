package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPFCToCategoryArray_PrimaryAndDetailed(t *testing.T) {
	result := PFCToCategoryArray("FOOD_AND_DRINK", "FOOD_AND_DRINK_FAST_FOOD")
	assert.Equal(t, []string{"Food And Drink", "Fast Food"}, result)
}

func TestPFCToCategoryArray_PrimaryOnly(t *testing.T) {
	result := PFCToCategoryArray("TRANSPORTATION", "")
	assert.Equal(t, []string{"Transportation"}, result)
}

func TestPFCToCategoryArray_EmptyPrimary_ReturnsNil(t *testing.T) {
	result := PFCToCategoryArray("", "FOOD_AND_DRINK_FAST_FOOD")
	assert.Nil(t, result)
}

func TestPFCToCategoryArray_BothEmpty_ReturnsNil(t *testing.T) {
	result := PFCToCategoryArray("", "")
	assert.Nil(t, result)
}

func TestPFCToCategoryArray_UnknownPrimary_TitleCaseFallback(t *testing.T) {
	result := PFCToCategoryArray("SOME_NEW_CATEGORY", "SOME_NEW_CATEGORY_DETAIL")
	assert.Equal(t, "Some New Category", result[0])
	assert.Equal(t, "Detail", result[1])
}

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

func TestPfcEnumToTitleCase(t *testing.T) {
	assert.Equal(t, "Fast Food", pfcEnumToTitleCase("FAST_FOOD"))
	assert.Equal(t, "Internet And Cable", pfcEnumToTitleCase("INTERNET_AND_CABLE"))
	assert.Equal(t, "Income", pfcEnumToTitleCase("INCOME"))
	assert.Equal(t, "", pfcEnumToTitleCase(""))
}
