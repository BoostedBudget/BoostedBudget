package merchant_test

import (
	"testing"

	"github.com/maxnussbaum/BoostedBudget/internal/merchant"
	"github.com/stretchr/testify/assert"
)

// TestNormalize verifies the deterministic normalization of garbled Plaid merchant names.
// Cases mirror the examples in Issue #957 plus common prefix/suffix patterns.
func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// Issue #957 canonical examples
		{name: "whole_foods_garbled", input: "WHOLEFDSMRKT #10234", want: "Wholefdsmrkt"},
		{name: "square_coffee", input: "SQ *COFFEE SHOP 12B", want: "Coffee Shop 12B"},
		// Store code stripping
		{name: "strip_hash_code", input: "TARGET #1234", want: "Target"},
		{name: "strip_store_number", input: "WALMART STORE 042", want: "Walmart"},
		{name: "strip_trailing_digits", input: "STARBUCKS 99821", want: "Starbucks"},
		// Card-reader prefix stripping
		{name: "sq_prefix", input: "SQ *BLUE BOTTLE COFFEE", want: "Blue Bottle Coffee"},
		{name: "tst_prefix", input: "TST *FANCY RESTAURANT", want: "Fancy Restaurant"},
		{name: "sp_prefix", input: "SP *LOCAL BAKERY", want: "Local Bakery"},
		{name: "pp_prefix", input: "PP *ETSY SELLER", want: "Etsy Seller"},
		{name: "paypal_prefix", input: "PAYPAL *EBAY PURCHASE", want: "Ebay Purchase"},
		// Title-casing
		{name: "all_caps", input: "AMAZON", want: "Amazon"},
		{name: "already_title", input: "Whole Foods", want: "Whole Foods"},
		{name: "mixed_case", input: "whole FOODS market", want: "Whole Foods Market"},
		// Whitespace normalization
		{name: "extra_spaces", input: "  STARBUCKS   COFFEE  ", want: "Starbucks Coffee"},
		{name: "tab_in_name", input: "TARGET\tSTORE", want: "Target Store"},
		// Preserve multi-word names after stripping
		{name: "multi_word_after_strip", input: "SQ *BLUE BOTTLE COFFEE #001", want: "Blue Bottle Coffee"},
		// Edge: empty and whitespace-only inputs
		{name: "empty_string", input: "", want: ""},
		{name: "whitespace_only_spaces", input: "   ", want: ""},
		{name: "whitespace_only_tab", input: "\t", want: ""},
		// Edge: bare prefix (nothing after the asterisk) — falls back to trimmed raw;
		// blank display is worse than showing the original garbled name.
		{name: "just_sq_star", input: "SQ *", want: "SQ *"},
		{name: "just_tst_star", input: "TST *", want: "TST *"},
		// Edge: no transformation needed
		{name: "clean_name", input: "Whole Foods Market", want: "Whole Foods Market"},
		// Edge: multiple trailing codes
		{name: "multiple_trailing", input: "COSTCO STORE 0042 #99", want: "Costco"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := merchant.Normalize(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}
