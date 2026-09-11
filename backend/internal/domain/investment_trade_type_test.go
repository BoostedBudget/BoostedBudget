package domain

import (
	"fmt"
	"strconv"
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
		// amount and quantity matter only to the two structural rows; every other row here is
		// classified by its subtype literal and is indifferent to them (#1631 CX3).
		amount   float64
		quantity *float64
		want     string
	}{
		// The #1631 core: this brokerage reports a dividend reinvestment as a PAIR of cash rows
		// against the ETF, and never emits the reinvestment subtype.
		// Both legs carry the corroboration the structural rule requires: the withdrawal DEBITS
		// cash and delivers shares, the deposit CREDITS cash and delivers none.
		{155, "cash", strPtr("withdrawal"), realSecurity, 100, f64(2.5), TradeTypeReinvest},
		{211, "cash", strPtr("deposit"), realSecurity, -100, f64(0), TradeTypeDividend},
		// The same subtypes against a cash-equivalent are genuine cash movements — bill payments
		// and paycheque deposits on a brokerage cash-management account. 275 + 124 rows.
		{275, "cash", strPtr("withdrawal"), cashEquiv, 0, nil, TradeTypeCash},
		{124, "cash", strPtr("deposit"), cashEquiv, 0, nil, TradeTypeCash},
		{9, "cash", strPtr("withdrawal"), noSecurity, 0, nil, TradeTypeCash},
		// Newly bucketed by #1631.
		{179, "transfer", strPtr("transfer"), realSecurity, 0, nil, TradeTypeTransfer},
		{47, "cash", strPtr("interest"), realSecurity, 0, nil, TradeTypeInterest},
		{2, "cash", strPtr("contribution"), cashEquiv, 0, nil, TradeTypeTransfer},
		// Already correct before #1631 — pinned so the new rules cannot regress them.
		{117, "buy", strPtr("buy"), realSecurity, 0, nil, TradeTypeBuy},
		{68, "cash", strPtr("dividend"), realSecurity, 0, nil, TradeTypeDividend},
		{5, "sell", strPtr("sell"), realSecurity, 0, nil, TradeTypeSell},
		{6, "fee", strPtr("interest"), cashEquiv, 0, nil, TradeTypeFee},
		{2, "cash", strPtr("account fee"), cashEquiv, 0, nil, TradeTypeFee},
		// A stock split is a corporate action, not a movement of value.
		{1, "transfer", strPtr("split"), realSecurity, 0, nil, TradeTypeOther},
	}
	for _, tt := range tests {
		name := tt.typ + "/" + derefOr(tt.subtype, "<nil>") + " @ " + tt.security
		t.Run(name, func(t *testing.T) {
			attached := tt.security != noSecurity
			isCashEq := tt.security == cashEquiv
			got := NormalizeTradeType(TradeTypeRow{
				Type: tt.typ, Subtype: tt.subtype, Amount: tt.amount, Quantity: tt.quantity,
				SecurityAttached: attached, SecurityIsCashEquivalent: &isCashEq,
			})
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
	// Shaped exactly like the structural dividend leg -- a credit delivering no shares -- so ONLY
	// the interest rule being consulted first keeps it out of the dividend bucket.
	got := NormalizeTradeType(TradeTypeRow{
		Type: "cash", Subtype: strPtr("interest"), Amount: -12.34, Quantity: f64(0),
		SecurityAttached: true, SecurityIsCashEquivalent: boolPtr(false),
	})
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
			assert.Equal(t, TradeTypeDividend, NormalizeTradeType(TradeTypeRow{
				Type: "cash", Subtype: strPtr(raw),
				SecurityAttached: true, SecurityIsCashEquivalent: boolPtr(false),
			}))
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
			// Shaped as the evidenced dividend leg so the security facts are the ONLY variable.
			assert.Equal(t, tt.want, NormalizeTradeType(TradeTypeRow{
				Type: "cash", Subtype: strPtr("deposit"), Amount: -100, Quantity: f64(0),
				SecurityAttached: tt.attached, SecurityIsCashEquivalent: tt.isCash,
			}))
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
					got := NormalizeTradeType(TradeTypeRow{
						Type: typ, Subtype: sub, Amount: -100, Quantity: f64(0),
						SecurityAttached: attached, SecurityIsCashEquivalent: isCash,
					})
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
			wantContains: NormalizedSubtypeSQL("it.subtype") + " IN",
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
	assert.Equal(t, TradeTypeTransfer, NormalizeTradeType(TradeTypeRow{Type: "transfer", Subtype: strPtr("transfer"), SecurityAttached: true, SecurityIsCashEquivalent: boolPtr(false)}),
		"a portfolio transfer is a transfer, not 'other'")
	assert.Equal(t, TradeTypeOther, NormalizeTradeType(TradeTypeRow{Type: "transfer", Subtype: strPtr("split"), SecurityAttached: true, SecurityIsCashEquivalent: boolPtr(false)}),
		"a stock split is a corporate action, not a movement of value")
	assert.Equal(t, TradeTypeOther, NormalizeTradeType(TradeTypeRow{Type: "cancel", SecurityAttached: true, SecurityIsCashEquivalent: boolPtr(false)}),
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
				"raw it.subtype comparison found — must go through NormalizedSubtypeSQL")
			assert.Contains(t, clause, NormalizedSubtypeSQL("it.subtype"),
				"subtype must be compared through the normalised, NULL-safe expression")
		})
	}
}

// TestSubtypeTrimming_GoAndSQLAgreeOnWhatPaddingIs is the CX5 regression. strings.TrimSpace trims
// every Unicode space; single-argument btrim trims ASCII spaces only. A tab- or NBSP-padded
// subtype therefore normalised on the Go side and did not on the SQL side, so the row DISPLAYED as
// Dividend while the SQL dividend predicate missed it and the cash complement claimed it -- the one
// failure the display/filter partition exists to prevent.
//
// The fix is agreement by construction: one cutset constant, with the SQL literal derived from it,
// so neither side can define "padding" on its own.
func TestSubtypeTrimming_GoAndSQLAgreeOnWhatPaddingIs(t *testing.T) {
	for _, pad := range []string{" ", "\t", "\n", "\r", "\v", "\f", "\u0085", "\u00a0"} {
		t.Run(strconv.QuoteToASCII(pad), func(t *testing.T) {
			raw := pad + "Dividend" + pad
			assert.Equal(t, TradeTypeDividend,
				NormalizeTradeType(TradeTypeRow{Type: "cash", Subtype: strPtr(raw), SecurityAttached: true, SecurityIsCashEquivalent: boolPtr(false)}),
				"Go must treat this rune as padding")
			assert.Contains(t, sqlSubtypeTrimCutsetLiteral(), fmt.Sprintf(`\u%04x`, []rune(pad)[0]),
				"the SQL btrim cutset must carry the same rune, or the partition desyncs")
		})
	}
}

// TestSQLTrimCutsetLiteralCoversExactlyTheGoCutset keeps the two halves of the CX5 fix welded
// together: adding a rune to subtypeTrimCutset must move the rendered SQL literal with it, and the
// literal must not bless a rune the Go side leaves in place.
func TestSQLTrimCutsetLiteralCoversExactlyTheGoCutset(t *testing.T) {
	literal := sqlSubtypeTrimCutsetLiteral()
	require.True(t, strings.HasPrefix(literal, "E'"), "must be an E-string so the escapes are honoured")
	require.True(t, strings.HasSuffix(literal, "'"))

	var want strings.Builder
	for _, r := range subtypeTrimCutset {
		fmt.Fprintf(&want, `\u%04x`, r)
	}
	assert.Equal(t, "E'"+want.String()+"'", literal)

	// Every rune the SQL blesses is one the Go side actually trims.
	for _, r := range subtypeTrimCutset {
		s := string(r) + "dividend"
		assert.Equal(t, "dividend", normalizeSubtype(&s))
	}
}

// TestNormalizedSubtypeSQL_TrimsWithTheSharedCutset pins that the rendered expression passes the
// cutset to btrim. Single-argument btrim is the CX5 defect itself.
func TestNormalizedSubtypeSQL_TrimsWithTheSharedCutset(t *testing.T) {
	got := NormalizedSubtypeSQL("it.subtype")
	assert.Equal(t,
		"COALESCE(lower(btrim(it.subtype, "+sqlSubtypeTrimCutsetLiteral()+")), '')", got)
}

// f64 returns a pointer to a float64 literal for the quantity field.
func f64(v float64) *float64 { return &v }

// dividendLegRow is the production-evidenced shape of the structural dividend leg: a real
// security, a cash CREDIT (negative under Plaid's convention), and no shares delivered.
func dividendLegRow(sub string) TradeTypeRow {
	return TradeTypeRow{
		Type: "cash", Subtype: strPtr(sub), Amount: -100, Quantity: f64(0),
		SecurityAttached: true, SecurityIsCashEquivalent: boolPtr(false),
	}
}

// reinvestLegRow is the paired purchase leg: a cash DEBIT (positive) that delivers shares.
func reinvestLegRow(sub string) TradeTypeRow {
	return TradeTypeRow{
		Type: "cash", Subtype: strPtr(sub), Amount: 100, Quantity: f64(2.5),
		SecurityAttached: true, SecurityIsCashEquivalent: boolPtr(false),
	}
}

// TestNormalizeTradeType_StructuralRuleRequiresTheProductionShape is the CX3 regression. The rule
// admitted ANY cash/deposit or cash/withdrawal against an affirmatively-not-cash-equivalent
// security, with no corroboration at all -- so a bank sweep emitting a generic deposit became a
// Dividend. MSPBNA is the concrete case: is_cash_equivalent=false and type='equity', so the
// security facts alone do not tell it apart from an ETF.
//
// The rule now also requires the two row-local facts the production evidence actually carried:
// the amount sign agreeing with the direction, and the share count. Both are already on the row,
// so SQL mirrors them without a join.
func TestNormalizeTradeType_StructuralRuleRequiresTheProductionShape(t *testing.T) {
	tests := []struct {
		name string
		row  TradeTypeRow
		want string
	}{
		{"the evidenced dividend leg", dividendLegRow("deposit"), TradeTypeDividend},
		{"the evidenced reinvest leg", reinvestLegRow("withdrawal"), TradeTypeReinvest},

		// The MSPBNA case. A bank sweep deposit DELIVERS SHARES of the sweep vehicle: money did
		// not arrive as income, it was moved into a holding. A deposit that buys something is not
		// a dividend, whatever the security flags say.
		{"MSPBNA sweep deposit delivering shares", func() TradeTypeRow {
			r := dividendLegRow("deposit")
			r.Quantity = f64(1500)
			return r
		}(), TradeTypeCash},

		// Sign disagreement: a "deposit" that DEBITS cash is not money arriving.
		{"deposit whose amount is a debit", func() TradeTypeRow {
			r := dividendLegRow("deposit")
			r.Amount = 100
			return r
		}(), TradeTypeCash},
		{"withdrawal whose amount is a credit", func() TradeTypeRow {
			r := reinvestLegRow("withdrawal")
			r.Amount = -100
			return r
		}(), TradeTypeCash},

		// A withdrawal that acquires nothing is a cash movement out, not a reinvestment.
		{"withdrawal delivering no shares", func() TradeTypeRow {
			r := reinvestLegRow("withdrawal")
			r.Quantity = f64(0)
			return r
		}(), TradeTypeCash},

		// An absent quantity is not evidence, exactly as an absent is_cash_equivalent is not.
		{"deposit with an unknown quantity", func() TradeTypeRow {
			r := dividendLegRow("deposit")
			r.Quantity = nil
			return r
		}(), TradeTypeCash},
		{"withdrawal with an unknown quantity", func() TradeTypeRow {
			r := reinvestLegRow("withdrawal")
			r.Quantity = nil
			return r
		}(), TradeTypeCash},

		// A zero-amount row corroborates neither direction.
		{"deposit with a zero amount", func() TradeTypeRow {
			r := dividendLegRow("deposit")
			r.Amount = 0
			return r
		}(), TradeTypeCash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeTradeType(tt.row))
		})
	}
}

// TestStructuralSQL_CarriesEveryFactTheGoRuleRequires keeps CX3 a partition change rather than a
// display-only one: the SQL structural conditions must test the same amount sign and share count,
// and must stay two-valued so a NULL quantity cannot drop a row out of BOTH buckets.
func TestStructuralSQL_CarriesEveryFactTheGoRuleRequires(t *testing.T) {
	sub := NormalizedSubtypeSQL("it.subtype")
	div := StructuralDividendSQL(sub, "it.amount", "it.quantity", "sec")
	rei := StructuralReinvestSQL(sub, "it.amount", "it.quantity", "sec")

	assert.Contains(t, div, "'deposit'")
	assert.Contains(t, div, "it.amount < 0", "the deposit leg must be a cash credit")
	assert.Contains(t, div, "COALESCE(it.quantity", "a NULL quantity must not make the condition NULL")
	assert.Contains(t, div, SecurityIsStructuralSQL("sec"))

	assert.Contains(t, rei, "'withdrawal'")
	assert.Contains(t, rei, "it.amount > 0", "the withdrawal leg must be a cash debit")
	assert.Contains(t, rei, "COALESCE(it.quantity", "a NULL quantity must not make the condition NULL")
	assert.Contains(t, rei, SecurityIsStructuralSQL("sec"))
}

// TestTradeTypeToSQLPredicate_StructuralBucketsShareOneDefinition proves both halves of the
// partition moved: the dividend, reinvest and cash buckets must all render the SAME structural
// expressions, so tightening the rule in one place cannot leave a row claimed by two buckets or
// by none.
func TestTradeTypeToSQLPredicate_StructuralBucketsShareOneDefinition(t *testing.T) {
	sub := NormalizedSubtypeSQL("it.subtype")
	div := StructuralDividendSQL(sub, "it.amount", "it.quantity", "sec")
	rei := StructuralReinvestSQL(sub, "it.amount", "it.quantity", "sec")

	dividendClause, _ := TradeTypeToSQLPredicate([]string{TradeTypeDividend}, "it", "sec")
	assert.Contains(t, dividendClause, div)

	reinvestClause, _ := TradeTypeToSQLPredicate([]string{TradeTypeReinvest}, "it", "sec")
	assert.Contains(t, reinvestClause, rei)

	cashClause, _ := TradeTypeToSQLPredicate([]string{TradeTypeCash}, "it", "sec")
	assert.Contains(t, cashClause, div, "the cash complement must exclude exactly what dividend claims")
	assert.Contains(t, cashClause, rei, "the cash complement must exclude exactly what reinvest claims")
}

// TestIncomeSubtypes_CoversEveryReinvestment is the CX4 regression. reinvestSubtypes has always
// carried the two capital-gain reinvestments, but IncomeSubtypes hand-listed only the dividend and
// interest ones -- so a capital-gain reinvestment DISPLAYED as Reinvest and was unreachable from
// IncomeSummary, while the capital-gain DISTRIBUTION beside it counted. Two hand-maintained lists
// with an implied relationship is how that gap opened, so the relationship is now derived.
func TestIncomeSubtypes_CoversEveryReinvestment(t *testing.T) {
	for _, sub := range reinvestSubtypes {
		assert.Contains(t, IncomeSubtypes, sub,
			"a reinvestment is income immediately spent on shares -- it cannot display as Reinvest "+
				"and be absent from IncomeSummary")
	}
	// The asymmetry that made the gap visible: the distribution counts, so the reinvestment of
	// that same distribution must too.
	assert.Contains(t, IncomeSubtypes, "long-term capital gain")
	assert.Contains(t, IncomeSubtypes, "long-term capital gain reinvestment")
}
