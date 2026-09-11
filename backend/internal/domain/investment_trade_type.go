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
var IncomeSubtypes = func() []string {
	var s []string
	s = append(s, dividendSubtypes...)
	s = append(s, capitalGainSubtypes...)
	s = append(s, "dividend reinvestment")
	s = append(s, "interest reinvestment")
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

// normalizeSubtype lower-cases and trims a possibly-absent Plaid subtype. Every set lookup goes
// through it: the literals in the sets above are lower-case, and before #1631 nothing normalised
// the incoming value, so any casing or whitespace variance silently fell through to "cash".
// The SQL side mirrors this exactly with COALESCE(lower(btrim(subtype)), the empty string).
func normalizeSubtype(subtype *string) string {
	if subtype == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*subtype))
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
// never emits the reinvestment subtype. A cash movement against a non-cash-equivalent security is
// therefore a security-level event: money in is income, money out bought shares.
//
// Keyed on the subtype LITERALS, never the amount sign. Plaid's convention is inverted relative to
// the Transactions product ("Inflow, such as stock sales, is shown as a negative amount, and
// outflow, such as stock purchases, is positive"), so reaching for intuitive polarity inverts the
// whole classification — and SQL could not mirror a mixed subtype+sign rule without desyncing.
func NormalizeTradeType(typ string, subtype *string, securityAttached bool, securityIsCashEquivalent *bool) string {
	sub := normalizeSubtype(subtype)
	// A cash movement against a security KNOWN not to be cash-equivalent is a security-level event.
	// An unknown flag is not evidence, so it is not structural.
	structural := securityAttached &&
		securityIsCashEquivalent != nil && !*securityIsCashEquivalent

	switch typ {
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
		case structural && sub == "deposit":
			return TradeTypeDividend
		case structural && sub == "withdrawal":
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
// NULL-safety: every subtype comparison goes through COALESCE(lower(btrim(subtype)), the empty string), which
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

// NormalizedSubtypeSQL renders the NULL-safe, normalised subtype expression that every subtype
// comparison must go through — the SQL mirror of normalizeSubtype.
func NormalizedSubtypeSQL(column string) string {
	return fmt.Sprintf("COALESCE(lower(btrim(%s)), '')", column)
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
	structural := SecurityIsStructuralSQL(securitiesAlias)

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
				"(%s = 'cash' AND (%s IN (%s) OR (%s = 'deposit' AND %s)))",
				col("type"), sub, inList(append(append([]string{}, dividendSubtypes...), capitalGainSubtypes...)),
				sub, structural))
		case TradeTypeReinvest:
			// Subtype-literal reinvestments, plus the structural case: a cash withdrawal against a
			// real security is cash spent acquiring it.
			parts = append(parts, fmt.Sprintf(
				"(%s = 'cash' AND (%s IN (%s) OR (%s = 'withdrawal' AND %s)))",
				col("type"), sub, inList(reinvestSubtypes),
				sub, structural))
		case TradeTypeCash:
			// The exact complement: a cash row classified by none of the sets above, and not a
			// structural deposit/withdrawal.
			classified := append([]string{}, dividendSubtypes...)
			classified = append(classified, capitalGainSubtypes...)
			classified = append(classified, reinvestSubtypes...)
			classified = append(classified, feeTaxSubtypes...)
			classified = append(classified, interestSubtypes...)
			classified = append(classified, transferSubtypes...)
			parts = append(parts, fmt.Sprintf(
				"(%s = 'cash' AND %s NOT IN (%s) AND NOT (%s IN ('deposit', 'withdrawal') AND %s))",
				col("type"), sub, inList(classified), sub, structural))
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
