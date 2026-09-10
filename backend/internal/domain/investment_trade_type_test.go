package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizeTradeType_ProductionDistribution is the test that would have caught #1631. Every
// row is a real (type, subtype, security-kind) combination measured in the production database,
// with the row count it carries, so a regression names the blast radius rather than just failing.
//
// Before the fix, 594 of 1,192 rows (49.8%) were mislabelled: the reinvestments and dividends read
// "Cash", transfers read "Other", and interest read "Cash".
func TestNormalizeTradeType_ProductionDistribution(t *testing.T) {
	const (
		realSecurity = "real, not cash-equivalent"
		cashEquiv    = "cash-equivalent"
		noSecurity   = "no security"
	)
	tests := []struct {
		rows     int
		typ      string
		subtype  *string
		security string
		want     string
	}{
		// The #1631 core: this brokerage reports a dividend reinvestment as a PAIR of cash rows
		// against the ETF, and never emits the reinvestment subtype.
		{155, "cash", strPtr("withdrawal"), realSecurity, TradeTypeReinvest},
		{211, "cash", strPtr("deposit"), realSecurity, TradeTypeDividend},
		// The same subtypes against a cash-equivalent are genuine cash movements — bill payments
		// and paycheque deposits on a brokerage cash-management account. 275 + 124 rows.
		{275, "cash", strPtr("withdrawal"), cashEquiv, TradeTypeCash},
		{124, "cash", strPtr("deposit"), cashEquiv, TradeTypeCash},
		{9, "cash", strPtr("withdrawal"), noSecurity, TradeTypeCash},
		// Newly bucketed by #1631.
		{179, "transfer", strPtr("transfer"), realSecurity, TradeTypeTransfer},
		{47, "cash", strPtr("interest"), realSecurity, TradeTypeInterest},
		{2, "cash", strPtr("contribution"), cashEquiv, TradeTypeTransfer},
		// Already correct before #1631 — pinned so the new rules cannot regress them.
		{117, "buy", strPtr("buy"), realSecurity, TradeTypeBuy},
		{68, "cash", strPtr("dividend"), realSecurity, TradeTypeDividend},
		{5, "sell", strPtr("sell"), realSecurity, TradeTypeSell},
		{6, "fee", strPtr("interest"), cashEquiv, TradeTypeFee},
		{2, "cash", strPtr("account fee"), cashEquiv, TradeTypeFee},
		// A stock split is a corporate action, not a movement of value.
		{1, "transfer", strPtr("split"), realSecurity, TradeTypeOther},
	}
	for _, tt := range tests {
		name := tt.typ + "/" + derefOr(tt.subtype, "<nil>") + " @ " + tt.security
		t.Run(name, func(t *testing.T) {
			attached := tt.security != noSecurity
			isCashEq := tt.security == cashEquiv
			got := NormalizeTradeType(tt.typ, tt.subtype, attached, &isCashEq)
			assert.Equal(t, tt.want, got, "%d production rows depend on this", tt.rows)
		})
	}
}

// TestNormalizeTradeType_RuleOrderProtectsTheBankSweep pins the one case where ORDER, not the rules
// themselves, is what keeps the classification honest. MSPBNA — the Morgan Stanley bank sweep that
// produces the cash/interest rows — is is_cash_equivalent=FALSE and type='equity', so it satisfies
// the structural condition. Only the interest rule being consulted BEFORE the structural rule stops
// its interest being reported as a dividend.
func TestNormalizeTradeType_RuleOrderProtectsTheBankSweep(t *testing.T) {
	got := NormalizeTradeType("cash", strPtr("interest"), true /*attached*/, boolPtr(false) /*known not cash-equivalent*/)
	assert.Equal(t, TradeTypeInterest, got,
		"a documented subtype must win over the structural fallback, or a bank sweep's interest "+
			"becomes a dividend")
}

// TestNormalizeTradeType_NormalisesCasingAndWhitespace covers the variance that silently fell
// through to "cash" before #1631: the subtype sets are lower-case literals and nothing trimmed or
// down-cased the incoming value.
func TestNormalizeTradeType_NormalisesCasingAndWhitespace(t *testing.T) {
	for _, raw := range []string{"Dividend", "DIVIDEND", "  dividend  ", "dIvIdEnD"} {
		t.Run(raw, func(t *testing.T) {
			assert.Equal(t, TradeTypeDividend, NormalizeTradeType("cash", strPtr(raw), true, boolPtr(false)))
		})
	}
}

// TestNormalizeTradeType_StructuralRuleNeedsBothFacts proves that "no security" and "a security
// whose is_cash_equivalent flag is NULL" must not collapse into one nil. The caller passes two
// booleans precisely so they cannot; an absent security and a cash-equivalent one both land on
// Cash, while only an affirmatively non-cash-equivalent security triggers the structural rule.
func TestNormalizeTradeType_StructuralRuleNeedsBothFacts(t *testing.T) {
	tests := []struct {
		name     string
		attached bool
		isCash   *bool
		want     string
	}{
		{"security known NOT cash-equivalent → structural", true, boolPtr(false), TradeTypeDividend},
		{"security known cash-equivalent → plain cash", true, boolPtr(true), TradeTypeCash},
		{"no security at all → plain cash", false, nil, TradeTypeCash},
		// The conservative case, and the reason the flag is a pointer: Plaid ships
		// is_cash_equivalent as a NullableBool, so an institution can omit it. An absent flag is
		// not evidence of anything, so it must NOT reclassify the row — inventing a "Dividend"
		// label out of silence is worse than leaving today's "Cash".
		{"security with an UNKNOWN flag → plain cash", true, nil, TradeTypeCash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeTradeType("cash", strPtr("deposit"), tt.attached, tt.isCash))
		})
	}
}

// TestNormalizeTradeType_EveryOutputIsAKnownBucket guards the display contract the frontend union
// and ACTIVITY_TYPE_CONFIG depend on: an unknown key renders the "Other" fallback chip, so a new
// bucket added here without adding it there is a silent UI regression.
func TestNormalizeTradeType_EveryOutputIsAKnownBucket(t *testing.T) {
	known := map[string]bool{
		TradeTypeBuy: true, TradeTypeSell: true, TradeTypeDividend: true, TradeTypeReinvest: true,
		TradeTypeInterest: true, TradeTypeFee: true, TradeTypeTransfer: true, TradeTypeCash: true,
		TradeTypeOther: true,
	}
	var subtypes []*string
	subtypes = append(subtypes, nil)
	for _, set := range [][]string{
		dividendSubtypes, reinvestSubtypes, feeTaxSubtypes, interestSubtypes,
		capitalGainSubtypes, transferSubtypes, corporateActionSubtypes,
		{"deposit", "withdrawal", "buy", "sell", "trade", "adjustment", "unexpected-new-plaid-value"},
	} {
		for _, v := range set {
			s := v
			subtypes = append(subtypes, &s)
		}
	}
	for _, typ := range []string{"buy", "sell", "cash", "fee", "transfer", "cancel", ""} {
		for _, sub := range subtypes {
			for _, attached := range []bool{true, false} {
				for _, isCash := range []*bool{boolPtr(true), boolPtr(false), nil} {
					got := NormalizeTradeType(typ, sub, attached, isCash)
					require.True(t, known[got],
						"NormalizeTradeType(%q, %v, attached=%v, isCash=%v) returned unknown bucket %q",
						typ, derefOr(sub, "<nil>"), attached, isCash, got)
				}
			}
		}
	}
}

// TestSubtypeSetsAreDisjoint keeps the Go classification a partition at the source: if two sets
// shared a literal, which bucket won would depend on the order of the switch rather than on intent.
func TestSubtypeSetsAreDisjoint(t *testing.T) {
	sets := map[string][]string{
		"dividend":        dividendSubtypes,
		"reinvest":        reinvestSubtypes,
		"feeTax":          feeTaxSubtypes,
		"interest":        interestSubtypes,
		"capitalGain":     capitalGainSubtypes,
		"transfer":        transferSubtypes,
		"corporateAction": corporateActionSubtypes,
	}
	seen := map[string]string{}
	for name, set := range sets {
		for _, v := range set {
			if prev, dup := seen[v]; dup {
				t.Errorf("subtype %q appears in both %q and %q — the buckets are not disjoint", v, prev, name)
			}
			seen[v] = name
		}
	}
}

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
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
			clause, _ := TradeTypeToSQLPredicate(tt.activityTypes, "", "sec")
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
			// #1631: compared through the NULL-safe normalised expression, not the raw column.
			wantContains: "COALESCE(lower(btrim(it.subtype)), '') IN",
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
			clause, _ := TradeTypeToSQLPredicate(tt.activityTypes, tt.tableAlias, "sec")
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
	// #1631: capital-gain DISTRIBUTIONS are income in exactly the way a dividend is, and were
	// absent from every set, so each one fell through to "cash".
	assert.Contains(t, IncomeSubtypes, "long-term capital gain")
	assert.Contains(t, IncomeSubtypes, "short-term capital gain")
	assert.NotContains(t, IncomeSubtypes, "return of principal",
		"a return of capital is not income")
}

// TestTradeTypePartition_TransferAndOtherRoundTrip pins the partition around Plaid's "transfer"
// type, which #1631 moved. It used to map to TradeTypeOther, so `activity_types=other` had to reach
// transfer rows; now transfer has its own bucket and only a carved-out corporate action falls to
// "other". Both halves must move together — if the display side says "transfer" while the filter's
// other-bucket still claims those rows, one label becomes unreachable and another double-counts.
func TestTradeTypePartition_TransferAndOtherRoundTrip(t *testing.T) {
	// Display side.
	assert.Equal(t, TradeTypeTransfer, NormalizeTradeType("transfer", strPtr("transfer"), true, boolPtr(false)),
		"a portfolio transfer is a transfer, not 'other'")
	assert.Equal(t, TradeTypeOther, NormalizeTradeType("transfer", strPtr("split"), true, boolPtr(false)),
		"a stock split is a corporate action, not a movement of value")
	assert.Equal(t, TradeTypeOther, NormalizeTradeType("cancel", nil, true, boolPtr(false)),
		"an unknown Plaid type still falls to 'other'")

	// Filter side: the transfer bucket must claim transfer rows...
	transferClause, transferArgs := TradeTypeToSQLPredicate([]string{TradeTypeTransfer}, "it", "sec")
	assert.NotEmpty(t, transferClause)
	assert.Contains(t, transferClause, "it.type = 'transfer'")
	assert.Contains(t, transferArgs, "split", "the corporate-action carve-out must be bound")
	assert.Contains(t, transferArgs, "contribution", "cash-typed transfer subtypes must be bound")

	// ...and the other bucket must no longer claim them wholesale, but must still reach the
	// carved-out split.
	otherClause, otherArgs := TradeTypeToSQLPredicate([]string{TradeTypeOther}, "it", "sec")
	assert.NotEmpty(t, otherClause)
	assert.Contains(t, otherClause, "'transfer'",
		"the other bucket must name transfer — to EXCLUDE the type and re-admit the split subtype")
	assert.Contains(t, otherArgs, "split",
		"activity_types=other must still reach a transfer/split row, which displays as 'other'")
}

// TestTradeTypeToSQLPredicate_RequiresSecuritiesAliasForStructuralBuckets pins the contract change
// #1631 forced. The dividend/reinvest/cash buckets now depend on the joined security, so they
// cannot be rendered join-free the way the pre-#1631 unit tests did. Silently emitting a predicate
// that ignores cash-equivalence would desync the partition, so this panics instead.
func TestTradeTypeToSQLPredicate_RequiresSecuritiesAliasForStructuralBuckets(t *testing.T) {
	for _, bucket := range []string{TradeTypeDividend, TradeTypeReinvest, TradeTypeCash} {
		t.Run(bucket, func(t *testing.T) {
			assert.Panics(t, func() {
				TradeTypeToSQLPredicate([]string{bucket}, "it", "")
			}, "%s depends on securities.is_cash_equivalent and must refuse an empty alias", bucket)
		})
	}
	// Buckets that do not consult the security stay renderable without the join.
	for _, bucket := range []string{TradeTypeBuy, TradeTypeSell, TradeTypeFee, TradeTypeInterest, TradeTypeTransfer, TradeTypeOther} {
		t.Run(bucket+" needs no securities alias", func(t *testing.T) {
			assert.NotPanics(t, func() {
				TradeTypeToSQLPredicate([]string{bucket}, "it", "")
			})
		})
	}
}

// TestTradeTypeToSQLPredicate_SubtypeComparisonsAreNullSafe proves the SQL side cannot drop a row
// from BOTH halves of the partition. A bare `subtype NOT IN (...)` is NULL for a NULL subtype, so
// such a row would satisfy neither the cash bucket nor any other — invisible to every filter while
// still displaying a label. Every subtype comparison must therefore be COALESCEd.
func TestTradeTypeToSQLPredicate_SubtypeComparisonsAreNullSafe(t *testing.T) {
	all := []string{
		TradeTypeBuy, TradeTypeSell, TradeTypeDividend, TradeTypeReinvest,
		TradeTypeInterest, TradeTypeFee, TradeTypeTransfer, TradeTypeCash, TradeTypeOther,
	}
	for _, bucket := range all {
		t.Run(bucket, func(t *testing.T) {
			clause, _ := TradeTypeToSQLPredicate([]string{bucket}, "it", "sec")
			require.NotEmpty(t, clause)
			if !strings.Contains(clause, "it.subtype") {
				return // buckets that never look at subtype are trivially null-safe
			}
			assert.NotRegexp(t, `[^)]it\.subtype\s+(IN|NOT IN|=)`, clause,
				"raw it.subtype comparison found — must go through COALESCE(lower(btrim(...)), '')")
			assert.Contains(t, clause, "COALESCE(lower(btrim(it.subtype)), '')",
				"subtype must be compared through the normalised, NULL-safe expression")
		})
	}
}
