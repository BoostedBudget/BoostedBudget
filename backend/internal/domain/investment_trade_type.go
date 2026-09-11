package domain

import (
	"fmt"
	"strings"
)

// Trade type display values returned by NormalizeTradeType.
const (
	TradeTypeBuy      = "buy"
	TradeTypeSell     = "sell"
	TradeTypeDividend = "dividend"
	TradeTypeReinvest = "reinvest"
	TradeTypeFee      = "fee"
	TradeTypeCash     = "cash"
	TradeTypeOther    = "other"
	// #1631: added because 226 production rows were landing on "other" (transfers) and "cash"
	// (interest) purely for want of a bucket. Interest also made the chip disagree with
	// IncomeSubtypes, which has always counted it as income.
	TradeTypeTransfer = "transfer"
	TradeTypeInterest = "interest"
)

// Canonical Plaid v23 subtype sets.
// These are the ONLY authoritative definitions — all filtering and income aggregation
// must reference these slices to ensure mutual exclusivity across buckets.
// Source: github.com/plaid/plaid-go/v23/plaid InvestmentTransactionSubtype enum.

// dividendSubtypes are cash-type transactions that represent dividend income.
var dividendSubtypes = []string{
	"dividend",
	"qualified dividend",
	"non-qualified dividend",
}

// reinvestSubtypes are cash-type transactions that represent reinvested income.
// Economically these are also income — see IncomeSubtypes.
var reinvestSubtypes = []string{
	"dividend reinvestment",
	"interest reinvestment",
	"long-term capital gain reinvestment",
	"short-term capital gain reinvestment",
}

// feeTaxSubtypes are cash-type transactions representing fees and taxes.
// Enumerated explicitly from the Plaid v23 enum — no LIKE '%fee%' patterns.
var feeTaxSubtypes = []string{
	"account fee",
	"fund fee",
	"legal fee",
	"management fee",
	"miscellaneous fee",
	"transfer fee",
	"trust fee",
	"margin expense",
	"tax",
	"tax withheld",
	"non-resident tax",
}

// interestSubtypes are cash-type transactions representing interest income.
var interestSubtypes = []string{
	"interest",
	"interest receivable",
}

// capitalGainSubtypes are capital-gain DISTRIBUTIONS — income in exactly the way a dividend is,
// and unhandled before #1631, so every one fell through to "cash". Distinct from the
// *reinvestment* variants above, which are the same income immediately spent on shares.
// "return of principal" is deliberately absent: it is a return of capital, not income.
var capitalGainSubtypes = []string{
	"long-term capital gain",
	"short-term capital gain",
	"unqualified gain",
}

// transferSubtypes are cash movements that shift money between accounts rather than earning or
// spending it. Plaid types them "cash", so without this set they read as generic Cash.
//
// "stock distribution" (#1631) is here for a different reason than its cash siblings: it delivers
// SHARES, not money, so it is not "cash" in any economic sense — but it is not income either. Cash
// and Dividend are both wrong: Dividend would have IncomeSummary count a share delivery as money
// received (IncomeSubtypes deliberately excludes this whole set, dividend and stock-distribution
// alike), and Cash would hide it behind the same generic label as an unclassified cash movement.
// Transfer is the honest bucket — like "distribution" beside it, it is value moving INTO the
// account without being earned there — so it is added to this set rather than to
// dividendSubtypes/capitalGainSubtypes. It rides the same shared slice as "distribution" into
// NormalizeTradeType's cash case and TradeTypeToSQLPredicate's transfer/cash buckets, so both
// halves of the display/filter partition move together automatically.
var transferSubtypes = []string{
	"contribution",
	"distribution",
	"stock distribution",
}

// corporateActionSubtypes carve specific rows OUT of the "transfer" bucket. Plaid types a stock
// split as transfer/split ("activity that modifies a position, but not through buy/sell activity"),
// but a split is a corporate action, not a movement of value — labelling it "Transfer" would be
// wrong. Only "split" is carved out; the remaining Plaid corporate actions (merger, spin off, ...)
// do not appear in any production data yet and are left to the transfer bucket deliberately rather
// than speculatively.
var corporateActionSubtypes = []string{
	"split",
}

// IncomeSubtypes is the full set of subtypes that count as income in GetInvestmentIncomeSummary.
// It is WIDER than dividendSubtypes — it intentionally includes reinvested dividends/interest
// because reinvested income is still economically income. This is independent of the
// display/filter reinvest bucket (which answers a different question).
// Built from the same canonical constants to ensure consistency.
// IncomeSubtypes is the full set of subtypes that count as income in GetInvestmentIncomeSummary.
//
// ⚠ This is the SUBTYPE-LITERAL half of the income universe only. It cannot express the #1631
// structural case — a cash/deposit against a non-cash-equivalent security — because that depends on
// the joined security, not on the subtype. IncomeSummary applies both; see
// investment_analytics_repository.go. Before #1631 this set was the whole universe, which is why
// 211 dividend rows worth $6,249.43 were missing from reported portfolio income.
// ⚠ #1631 CX4: the reinvestments are taken from reinvestSubtypes WHOLESALE, never re-listed here.
// This set used to hand-list "dividend reinvestment" and "interest reinvestment" while
// reinvestSubtypes also carried the two capital-gain reinvestments, so a capital-gain reinvestment
// displayed as Reinvest and was unreachable from IncomeSummary -- with the capital-gain
// DISTRIBUTION beside it counting, which is the asymmetry that gives the game away. Resolved by
// WIDENING income rather than narrowing the display bucket: a reinvestment is income immediately
// spent on shares, which is the stated reason the other two were here in the first place.
var IncomeSubtypes = func() []string {
	var s []string
	s = append(s, dividendSubtypes...)
	s = append(s, capitalGainSubtypes...)
	s = append(s, reinvestSubtypes...)
	s = append(s, interestSubtypes...)
	return s
}()

func subtypeSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

var (
	dividendSubtypeSet        = subtypeSet(dividendSubtypes)
	reinvestSubtypeSet        = subtypeSet(reinvestSubtypes)
	feeTaxSubtypeSet          = subtypeSet(feeTaxSubtypes)
	interestSubtypeSet        = subtypeSet(interestSubtypes)
	capitalGainSubtypeSet     = subtypeSet(capitalGainSubtypes)
	transferSubtypeSet        = subtypeSet(transferSubtypes)
	corporateActionSubtypeSet = subtypeSet(corporateActionSubtypes)
)

// subtypeTrimCutset is the ONE definition of "padding" for a Plaid subtype, shared by the Go and
// SQL halves of the display/filter partition.
//
// It is an explicit cutset rather than strings.TrimSpace because TrimSpace trims every rune with
// the Unicode White_Space property, while SQL btrim takes a literal set of characters. Two
// independent definitions of whitespace is exactly how the partition desynced: a tab- or
// NBSP-padded subtype normalised on the Go side, did not on the SQL side, and the row displayed as
// Dividend while the dividend predicate missed it and the cash complement claimed it.
//
// The set is ASCII whitespace plus NEL and NBSP. That is deliberately NARROWER than TrimSpace: the
// remaining Unicode Zs separators (U+2000..U+200A, U+3000, ...) are not padding any real feed
// emits, and every rune added here has to be mirrored into SQL, so the set stays small enough to
// mirror honestly. A rune outside it is left in place by BOTH sides, which keeps the row in one
// bucket on both -- unclassified, but never displayed as one thing and filtered as another.
const subtypeTrimCutset = " \t\n\v\f\r\u0085\u00a0"

// sqlSubtypeTrimCutsetLiteral renders subtypeTrimCutset as a Postgres E-string of \uXXXX escapes.
// Deriving it from the Go constant is what makes the mirror mechanical: a rune added to the cutset
// moves both sides, and neither can drift. Escapes are used uniformly (even for a plain space) so
// the rendered SQL carries no literal control characters into query logs.
func sqlSubtypeTrimCutsetLiteral() string {
	var b strings.Builder
	b.WriteString("E'")
	for _, r := range subtypeTrimCutset {
		fmt.Fprintf(&b, `\u%04x`, r)
	}
	b.WriteString("'")
	return b.String()
}

// normalizeSubtype lower-cases and trims a possibly-absent Plaid subtype. Every set lookup goes
// through it: the literals in the sets above are lower-case, and before #1631 nothing normalised
// the incoming value, so any casing or whitespace variance silently fell through to "cash".
// The SQL side mirrors this exactly -- same cutset, same order -- via NormalizedSubtypeSQL.
func normalizeSubtype(subtype *string) string {
	if subtype == nil {
		return ""
	}
	return strings.ToLower(strings.Trim(*subtype, subtypeTrimCutset))
}

// TradeTypeRow carries the row-local facts NormalizeTradeType classifies on. It is a struct
// rather than a parameter list because several of the facts are same-typed and adjacent
// (two booleans about the security, two numbers about the movement), and a silently transposed
// pair is exactly the kind of defect a display/filter partition cannot survive.
//
// Every field is available on investment_transactions without a join except the two security
// facts, which both production callers already LEFT JOIN for. Nothing here needs a correlated
// lookup, so TradeTypeToSQLPredicate can mirror the whole rule in a WHERE fragment.
type TradeTypeRow struct {
	// Type and Subtype are the raw Plaid values, unnormalised. Subtype is nil-able.
	Type    string
	Subtype *string

	// Amount is Plaid signed cash effect for the row: POSITIVE when cash is debited (purchases,
	// outflows) and NEGATIVE when cash is credited (sales, dividends, inflows). This is inverted
	// relative to the Transactions product, which is why no primary bucket is keyed on it. The
	// column is NOT NULL, so this is a value and not a pointer.
	Amount float64

	// Quantity is the share count the row moves, NULL for a cash-only row. It is a pointer
	// because an absent quantity is not evidence of a zero one.
	Quantity *float64

	// SecurityAttached and SecurityIsCashEquivalent describe the JOINED security: whether the row
	// references one at all, and whether Plaid flagged it a cash equivalent. They are two fields,
	// not one nullable bool, because "no security" and "a security whose is_cash_equivalent is
	// NULL" are different facts that must not arrive as the same nil.
	SecurityAttached         bool
	SecurityIsCashEquivalent *bool
}

// isStructuralDividend and isStructuralReinvest are the CORROBORATION the structural rule demands
// beyond the security facts (#1631 CX3). Without them the rule turned ANY cash/deposit or
// cash/withdrawal against an affirmatively-not-cash-equivalent security into income or a purchase,
// with no pairing, sign or share-count evidence required at all -- so a bank sweep emitting a
// generic deposit read as a Dividend. MSPBNA is the live case: is_cash_equivalent=false and
// type='equity', so the security facts alone cannot tell a sweep vehicle from an ETF.
//
// The production evidence the rule was built on carried two more facts on the row itself, and the
// rule now requires them:
//
//   - The amount sign agrees with the direction. A deposit that DEBITS cash is not money arriving,
//     and a withdrawal that CREDITS it is not money spent. Under Plaid's inverted convention that
//     is Amount < 0 for the deposit leg and Amount > 0 for the withdrawal leg.
//   - The share count matches the leg. The dividend leg delivered NO shares (quantity 0): money
//     arrived and bought nothing yet. The reinvest leg delivered shares (quantity > 0): that is
//     the purchase. A sweep deposit that delivers shares of the sweep vehicle fails the first of
//     these, which is what takes MSPBNA out of the rule.
//
// An absent Quantity fails both, deliberately and for the same reason an absent is_cash_equivalent
// does: silence is not evidence, and the conservative reading is today's Cash.
func (r TradeTypeRow) isStructuralDividend() bool {
	return r.Amount < 0 && r.Quantity != nil && *r.Quantity == 0
}

func (r TradeTypeRow) isStructuralReinvest() bool {
	return r.Amount > 0 && r.Quantity != nil && *r.Quantity > 0
}

// NormalizeTradeType maps raw Plaid type/subtype to a display trade type string.
// Used for display and labelling only. For SQL filtering use TradeTypeToSQLPredicate.
// NormalizeTradeType maps a raw Plaid (type, subtype) pair onto the display bucket the UI shows.
//
// securityAttached and securityIsCashEquivalent describe the JOINED security: whether the row
// references one at all, and whether Plaid flagged it "a highly liquid asset that can be treated
// like cash" (CUR:USD, sweep funds). They are two parameters, not one nullable bool, because "no
// security" and "a security whose is_cash_equivalent is NULL" are different facts that must not
// arrive as the same nil.
//
// ⚠ The structural rule requires an AFFIRMATIVE false, not merely "not true". Plaid ships the flag
// as a NullableBool, so an institution can omit it; treating an absent flag as "not cash
// equivalent" would reclassify rows on the strength of a fact we do not have, inventing a
// "Dividend" label from silence. NULL therefore falls through to Cash — today's behaviour — and the
// SQL side mirrors this with `is_cash_equivalent IS FALSE`, which is likewise false for NULL.
//
// ⚠ Rule ORDER is load-bearing. Plaid's documented subtypes are consulted FIRST and the structural
// cash-equivalence rule LAST, because "dividend reinvestment" is a real Plaid subtype that other
// institutions do emit — the structural rule is an institution fallback, never the primary path.
// It is also what keeps a bank sweep honest: MSPBNA is is_cash_equivalent=false and type='equity',
// so only the earlier interest rule stops its cash/interest rows being read as dividends.
//
// The structural rule exists because the brokerage in #1631 reports a dividend reinvestment as a
// PAIR of rows — a cash/deposit credit and a cash/withdrawal debit, both against the ETF — and
// never emits the reinvestment subtype.
//
// ⚠ The pairing is the OBSERVATION that motivated the rule, not something the rule verifies.
// Classification here is row-local: no counterpart lookup, no same-day or same-security matching,
// no amount reconciliation between the two legs. A standalone cash/deposit with a real security, a
// credit amount and zero shares is labelled Dividend whether or not any withdrawal pairs with it.
// That is deliberate — classification runs at READ time over one row, and a pairing join would
// make every feed page a self-join over the same table — but it means the rule is a structural
// INFERENCE with the production distribution behind it (211 deposits at quantity 0, 155
// withdrawals at quantity > 0, 147 of the 155 pairing same-day same-security), not a recognition
// of a matched event. A brokerage that emits unpaired zero-quantity credits against real
// securities for something other than income would be misread, and nothing here would detect it.
//
// ⚠ It is NOT enough that the security is not a cash equivalent. That alone admitted any deposit or
// withdrawal at all, which is how a bank sweep's generic deposit became a Dividend. The rule also
// demands the corroboration the production rows actually carried — the amount sign agreeing with
// the direction, and the share count matching the leg — so that "money in is income, money out
// bought shares" is something the ROW says rather than something the rule assumes. See
// TradeTypeRow.isStructuralDividend.
//
// Every PRIMARY bucket is keyed on the subtype LITERALS, never the amount sign. Plaid's convention
// is inverted relative to the Transactions product ("Inflow, such as stock sales, is shown as a
// negative amount, and outflow, such as stock purchases, is positive"), so reaching for intuitive
// polarity inverts the whole classification.
//
// ⚠ The STRUCTURAL rule is the one exception, and it reads the sign deliberately (#1631 CX3): it
// has no subtype literal to key on, so the sign and the share count are the only corroboration
// available. It is not a shortcut for polarity — it REQUIRES the sign to agree with the direction
// the subtype already states, and rejects the row when it does not. SQL mirrors it exactly, because
// both facts are columns on the same row and need no join; see StructuralDividendSQL.
func NormalizeTradeType(row TradeTypeRow) string {
	sub := normalizeSubtype(row.Subtype)
	// A cash movement against a security KNOWN not to be cash-equivalent is a security-level event.
	// An unknown flag is not evidence, so it is not structural.
	structural := row.SecurityAttached &&
		row.SecurityIsCashEquivalent != nil && !*row.SecurityIsCashEquivalent

	switch row.Type {
	case "buy":
		return TradeTypeBuy
	case "sell":
		return TradeTypeSell
	case "fee":
		return TradeTypeFee
	case "transfer":
		if corporateActionSubtypeSet[sub] {
			return TradeTypeOther
		}
		return TradeTypeTransfer
	case "cash":
		switch {
		case dividendSubtypeSet[sub], capitalGainSubtypeSet[sub]:
			return TradeTypeDividend
		case reinvestSubtypeSet[sub]:
			return TradeTypeReinvest
		case feeTaxSubtypeSet[sub]:
			return TradeTypeFee
		case interestSubtypeSet[sub]:
			return TradeTypeInterest
		case transferSubtypeSet[sub]:
			return TradeTypeTransfer
		case structural && sub == "deposit" && row.isStructuralDividend():
			return TradeTypeDividend
		case structural && sub == "withdrawal" && row.isStructuralReinvest():
			return TradeTypeReinvest
		}
		return TradeTypeCash
	}
	return TradeTypeOther
}

// TradeTypeToSQLPredicate is the SQL half of the display/filter PARTITION: it converts display
// trade-type keys into a WHERE fragment over investment_transactions, and its buckets must select
// exactly the rows NormalizeTradeType labels with the same key. Applied as a SQL WHERE, never
// post-fetch, so LIMIT n+1 keyset pagination is not corrupted.
//
// tableAlias qualifies type/subtype (e.g. "it") to avoid "column reference is ambiguous" when the
// query JOINs accounts, which also has type/subtype columns.
//
// ⚠ securitiesAlias is REQUIRED (e.g. "sec") and must name a LEFT JOINed securities relation.
// Before #1631 this function could be rendered join-free; it no longer can, because the dividend,
// reinvest and cash buckets now depend on the joined security's is_cash_equivalent flag exactly as
// NormalizeTradeType does. Both production callers already have the join
// (transaction_repository.go, investment_transaction_repository.go). Passing "" makes those three
// buckets un-renderable, so it panics rather than silently emitting a predicate that disagrees with
// the display side — a desynced partition is the one failure this function exists to prevent.
//
// NULL-safety: every subtype comparison goes through NormalizedSubtypeSQL, which
// mirrors normalizeSubtype on the Go side and removes three-valued logic entirely — a NULL subtype
// becomes the empty string and falls out of every IN and into every NOT IN, rather than making the whole predicate
// NULL and dropping the row from BOTH sides of the partition.
//
// Returns ("", nil) when activityTypes is empty (no filter needed).
// SecurityIsStructuralSQL renders "the joined security is a real holding, not a cash equivalent" —
// the condition under which a cash movement is a SECURITY-LEVEL event rather than a cash movement.
//
// It mirrors NormalizeTradeType exactly: attached AND affirmatively not cash-equivalent. `IS FALSE`
// (never `NOT ...` or `IS NOT TRUE`) is what makes a NULL flag fall through to the cash reading on
// this side too, and being two-valued it cannot drop a row out of both buckets the way a NULL-
// producing comparison would.
//
// Exported so the filter predicate and IncomeSummary share ONE definition. They classify the same
// rows for different purposes, and #1631 exists partly because two surfaces disagreed about the
// same row; a second copy of this expression is how that happens again.
func SecurityIsStructuralSQL(securitiesAlias string) string {
	return fmt.Sprintf("(%s.id IS NOT NULL AND %s.is_cash_equivalent IS FALSE)",
		securitiesAlias, securitiesAlias)
}

// StructuralDividendSQL and StructuralReinvestSQL are the SQL mirror of TradeTypeRow's
// isStructuralDividend / isStructuralReinvest, and of the subtype and security conditions that go
// with them. They render the WHOLE structural leg minus the type = 'cash' gate, which every caller
// applies itself.
//
// Exported, and shared by the filter predicate and IncomeSummary, for the same reason
// SecurityIsStructuralSQL is: three surfaces classify these rows, and #1631 exists because two of
// them disagreed about the same row. A second copy of this expression is how that happens again.
//
// Both are TWO-VALUED by construction. amount is NOT NULL in the schema, the security condition
// uses IS FALSE, and quantity is wrapped in COALESCE with a sentinel that fails the comparison --
// never a bare quantity = 0, which would evaluate to NULL for a NULL quantity and drop the row out
// of the dividend bucket AND out of the cash complement that negates it.
//
// subExpr must be the output of NormalizedSubtypeSQL so the subtype comparison is normalised the
// same way the Go side normalises it.
func StructuralDividendSQL(subExpr, amountCol, quantityCol, securitiesAlias string) string {
	return fmt.Sprintf("(%s = 'deposit' AND %s AND %s < 0 AND COALESCE(%s, -1) = 0)",
		subExpr, SecurityIsStructuralSQL(securitiesAlias), amountCol, quantityCol)
}

func StructuralReinvestSQL(subExpr, amountCol, quantityCol, securitiesAlias string) string {
	return fmt.Sprintf("(%s = 'withdrawal' AND %s AND %s > 0 AND COALESCE(%s, 0) > 0)",
		subExpr, SecurityIsStructuralSQL(securitiesAlias), amountCol, quantityCol)
}

// NormalizedSubtypeSQL renders the NULL-safe, normalised subtype expression that every subtype
// comparison must go through — the SQL mirror of normalizeSubtype. btrim is given the SHARED
// cutset: single-argument btrim trims spaces only, which is how a tab-padded subtype came to be
// displayed as one bucket and filtered into another.
func NormalizedSubtypeSQL(column string) string {
	return fmt.Sprintf("COALESCE(lower(btrim(%s, %s)), '')", column, sqlSubtypeTrimCutsetLiteral())
}

func TradeTypeToSQLPredicate(activityTypes []string, tableAlias, securitiesAlias string) (string, []any) {
	if len(activityTypes) == 0 {
		return "", nil
	}

	// col returns "alias.column" when tableAlias is non-empty, or just "column" otherwise.
	col := func(column string) string {
		if tableAlias == "" {
			return column
		}
		return tableAlias + "." + column
	}

	// sub is the normalised subtype expression — the SQL mirror of normalizeSubtype.
	sub := NormalizedSubtypeSQL(col("subtype"))

	needsSecurities := false
	for _, at := range activityTypes {
		if at == TradeTypeDividend || at == TradeTypeReinvest || at == TradeTypeCash {
			needsSecurities = true
		}
	}
	if needsSecurities && securitiesAlias == "" {
		panic("TradeTypeToSQLPredicate: securitiesAlias is required for the dividend/reinvest/cash " +
			"buckets — see the #1631 note on this function")
	}
	structuralDividend := StructuralDividendSQL(sub, col("amount"), col("quantity"), securitiesAlias)
	structuralReinvest := StructuralReinvestSQL(sub, col("amount"), col("quantity"), securitiesAlias)

	var parts []string
	var args []any

	// inList renders "$n, $n+1, ..." for vals and appends them to args.
	inList := func(vals []string) string {
		ph := sqlInPlaceholders(len(args)+1, len(vals))
		for _, v := range vals {
			args = append(args, v)
		}
		return ph
	}

	for _, at := range activityTypes {
		switch at {
		case TradeTypeBuy:
			parts = append(parts, fmt.Sprintf("%s = 'buy'", col("type")))
		case TradeTypeSell:
			parts = append(parts, fmt.Sprintf("%s = 'sell'", col("type")))
		case TradeTypeFee:
			// type='fee' OR a cash row whose subtype is a fee/tax.
			parts = append(parts, fmt.Sprintf("(%s = 'fee' OR (%s = 'cash' AND %s IN (%s)))",
				col("type"), col("type"), sub, inList(feeTaxSubtypes)))
		case TradeTypeInterest:
			parts = append(parts, fmt.Sprintf("(%s = 'cash' AND %s IN (%s))",
				col("type"), sub, inList(interestSubtypes)))
		case TradeTypeTransfer:
			// type='transfer' minus the corporate actions, plus the cash-typed transfer subtypes.
			parts = append(parts, fmt.Sprintf(
				"((%s = 'transfer' AND %s NOT IN (%s)) OR (%s = 'cash' AND %s IN (%s)))",
				col("type"), sub, inList(corporateActionSubtypes),
				col("type"), sub, inList(transferSubtypes)))
		case TradeTypeDividend:
			// Subtype-literal dividends and capital-gain distributions, plus the structural case:
			// a cash deposit against a real security is income credited for that security.
			parts = append(parts, fmt.Sprintf(
				"(%s = 'cash' AND (%s IN (%s) OR %s))",
				col("type"), sub, inList(append(append([]string{}, dividendSubtypes...), capitalGainSubtypes...)),
				structuralDividend))
		case TradeTypeReinvest:
			// Subtype-literal reinvestments, plus the structural case: a cash withdrawal against a
			// real security is cash spent acquiring it.
			parts = append(parts, fmt.Sprintf(
				"(%s = 'cash' AND (%s IN (%s) OR %s))",
				col("type"), sub, inList(reinvestSubtypes),
				structuralReinvest))
		case TradeTypeCash:
			// The exact complement: a cash row classified by none of the sets above, and neither a
			// structural dividend nor a structural reinvestment. The two structural expressions are
			// the SAME ones the dividend and reinvest buckets render, so tightening the rule moves
			// all three buckets at once and cannot leave a row claimed twice or not at all.
			classified := append([]string{}, dividendSubtypes...)
			classified = append(classified, capitalGainSubtypes...)
			classified = append(classified, reinvestSubtypes...)
			classified = append(classified, feeTaxSubtypes...)
			classified = append(classified, interestSubtypes...)
			classified = append(classified, transferSubtypes...)
			parts = append(parts, fmt.Sprintf(
				"(%s = 'cash' AND %s NOT IN (%s) AND NOT (%s OR %s))",
				col("type"), sub, inList(classified), structuralDividend, structuralReinvest))
		case TradeTypeOther:
			// Neither a known type, nor a carved-out corporate action.
			parts = append(parts, fmt.Sprintf(
				"(%s NOT IN ('buy', 'sell', 'cash', 'fee', 'transfer') OR (%s = 'transfer' AND %s IN (%s)))",
				col("type"), col("type"), sub, inList(corporateActionSubtypes)))
		}
	}

	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, " OR "), args
}

func sqlInPlaceholders(start, count int) string {
	if count == 0 {
		return ""
	}
	parts := make([]string, count)
	for i := range count {
		parts[i] = fmt.Sprintf("$%d", start+i)
	}
	return strings.Join(parts, ", ")
}
