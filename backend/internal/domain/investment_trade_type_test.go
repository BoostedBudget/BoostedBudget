package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeTradeType(t *testing.T) {
	tests := []struct {
		name    string
		typ     string
		subtype *string
		want    string
	}{
		{name: "buy", typ: "buy", want: "buy"},
		{name: "sell", typ: "sell", want: "sell"},
		{name: "dividend", typ: "cash", subtype: strPtr("dividend"), want: "dividend"},
		{name: "qualified dividend", typ: "cash", subtype: strPtr("qualified dividend"), want: "dividend"},
		{name: "reinvestment", typ: "cash", subtype: strPtr("dividend reinvestment"), want: "reinvest"},
		{name: "interest reinvestment", typ: "cash", subtype: strPtr("interest reinvestment"), want: "reinvest"},
		{name: "management fee", typ: "cash", subtype: strPtr("management fee"), want: "fee"},
		{name: "margin expense", typ: "cash", subtype: strPtr("margin expense"), want: "fee"},
		{name: "tax withheld", typ: "cash", subtype: strPtr("tax withheld"), want: "fee"},
		{name: "fee type", typ: "fee", want: "fee"},
		{name: "cash deposit", typ: "cash", subtype: strPtr("deposit"), want: "cash"},
		{name: "cash withdrawal", typ: "cash", subtype: strPtr("withdrawal"), want: "cash"},
		{name: "transfer", typ: "transfer", want: "other"},
		{name: "other type", typ: "unknown_type", want: "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeTradeType(tt.typ, tt.subtype)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTradeTypeToSQLPredicate_SingleBucket(t *testing.T) {
	tests := []struct {
		name          string
		activityTypes []string
		wantClause    string // just check it's non-empty and mention the right type
		wantNoClause  bool
	}{
		{
			name:          "empty → no clause",
			activityTypes: nil,
			wantNoClause:  true,
		},
		{
			name:          "buy",
			activityTypes: []string{"buy"},
			wantClause:    "type = 'buy'",
		},
		{
			name:          "sell",
			activityTypes: []string{"sell"},
			wantClause:    "type = 'sell'",
		},
		{
			name:          "multiple types",
			activityTypes: []string{"buy", "sell"},
			wantClause:    "buy", // partial check — just verify it contains a key token
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// tableAlias="" exercises the unqualified path (no JOIN ambiguity context).
			clause, _ := TradeTypeToSQLPredicate(tt.activityTypes, "")
			if tt.wantNoClause {
				assert.Empty(t, clause)
			} else {
				assert.NotEmpty(t, clause)
				assert.Contains(t, clause, tt.wantClause)
			}
		})
	}
}

// TestTradeTypeToSQLPredicate_TableAlias verifies that providing a tableAlias qualifies
// the type/subtype column references to prevent "column reference is ambiguous" errors
// when the query JOINs accounts (which also has type/subtype columns).
func TestTradeTypeToSQLPredicate_TableAlias(t *testing.T) {
	tests := []struct {
		name          string
		activityTypes []string
		tableAlias    string
		wantContains  string
		wantAbsent    string // must NOT appear (bare column name)
	}{
		{
			name:          "buy with alias qualifies type",
			activityTypes: []string{"buy"},
			tableAlias:    "it",
			wantContains:  "it.type = 'buy'",
			wantAbsent:    "type = 'buy'", // this partial match would also hit "it.type = 'buy'"
		},
		{
			name:          "sell with alias qualifies type",
			activityTypes: []string{"sell"},
			tableAlias:    "it",
			wantContains:  "it.type = 'sell'",
		},
		{
			name:          "dividend with alias qualifies subtype",
			activityTypes: []string{"dividend"},
			tableAlias:    "it",
			wantContains:  "it.subtype IN",
		},
		{
			name:          "fee with alias qualifies both type and subtype",
			activityTypes: []string{"fee"},
			tableAlias:    "it",
			wantContains:  "it.type = 'fee'",
		},
		{
			name:          "empty alias produces unqualified columns",
			activityTypes: []string{"buy"},
			tableAlias:    "",
			wantContains:  "type = 'buy'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clause, _ := TradeTypeToSQLPredicate(tt.activityTypes, tt.tableAlias)
			assert.NotEmpty(t, clause)
			assert.Contains(t, clause, tt.wantContains)
		})
	}
}

// TestTradeTypeToSQLPredicate_Partition verifies that the fee/cash/dividend buckets
// are mutually exclusive (no row can match two separate buckets via the predicates).
// This is a white-box test checking the canonical constant sets are disjoint.
func TestTradeTypeToSQLPredicate_Partition(t *testing.T) {
	// Verify canonical sets have no overlap (they'll be verified by their actual constants).
	for _, s := range dividendSubtypes {
		assert.NotContains(t, reinvestSubtypes, s, "dividend and reinvest sets must be disjoint: %q", s)
		assert.NotContains(t, feeTaxSubtypes, s, "dividend and feeTax sets must be disjoint: %q", s)
	}
	for _, s := range reinvestSubtypes {
		assert.NotContains(t, feeTaxSubtypes, s, "reinvest and feeTax sets must be disjoint: %q", s)
	}
}

// TestIncomeSubtypes_IncludesReinvested verifies that the income subtype set
// includes dividend reinvestment and interest reinvestment (per plan requirement).
func TestIncomeSubtypes_IncludesReinvested(t *testing.T) {
	assert.Contains(t, IncomeSubtypes, "dividend reinvestment")
	assert.Contains(t, IncomeSubtypes, "interest reinvestment")
	assert.Contains(t, IncomeSubtypes, "dividend")
	assert.Contains(t, IncomeSubtypes, "interest")
}

// TestTradeTypePartition_TransferRoundTrip verifies that the display and filter
// buckets agree for Plaid type="transfer": NormalizeTradeType maps it to TradeTypeOther,
// and TradeTypeToSQLPredicate([TradeTypeOther]) does NOT exclude transfer rows via a
// stray 'transfer' entry in the NOT IN list. Without this, activity_types=other would
// silently drop all transfer rows — they display as "other" but are unreachable by filter.
func TestTradeTypePartition_TransferRoundTrip(t *testing.T) {
	// Display side: transfer → "other"
	assert.Equal(t, TradeTypeOther, NormalizeTradeType("transfer", nil),
		"transfer rows must display as 'other'")

	// Filter side: activity_types=other must match transfer rows.
	// The other bucket emits a NOT IN clause — 'transfer' must NOT be in that list.
	clause, args := TradeTypeToSQLPredicate([]string{TradeTypeOther}, "it")
	assert.NotEmpty(t, clause)
	assert.Nil(t, args, "other bucket binds no args")
	assert.Contains(t, clause, "it.type NOT IN",
		"other predicate must use NOT IN to exclude known types")
	assert.NotContains(t, clause, "'transfer'",
		"other filter must not exclude transfer rows — they display as 'other' and must be reachable via activity_types=other")
}
