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
	"capital gain reinvestment",
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

// IncomeSubtypes is the full set of subtypes that count as income in GetInvestmentIncomeSummary.
// It is WIDER than dividendSubtypes — it intentionally includes reinvested dividends/interest
// because reinvested income is still economically income. This is independent of the
// display/filter reinvest bucket (which answers a different question).
// Built from the same canonical constants to ensure consistency.
var IncomeSubtypes = func() []string {
	var s []string
	s = append(s, dividendSubtypes...)
	// reinvested dividend/interest are income; capital-gain reinvestment is not dividend income
	s = append(s, "dividend reinvestment")
	s = append(s, "interest reinvestment")
	s = append(s, interestSubtypes...)
	return s
}()

// subtypeSet converts a slice to a lookup map for O(1) membership tests.
func subtypeSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

var (
	dividendSubtypeSet = subtypeSet(dividendSubtypes)
	reinvestSubtypeSet = subtypeSet(reinvestSubtypes)
	feeTaxSubtypeSet   = subtypeSet(feeTaxSubtypes)
)

// NormalizeTradeType maps raw Plaid type/subtype to a display trade type string.
// Used for display and labelling only. For SQL filtering use TradeTypeToSQLPredicate.
func NormalizeTradeType(typ string, subtype *string) string {
	sub := ""
	if subtype != nil {
		sub = *subtype
	}
	switch typ {
	case "buy":
		return TradeTypeBuy
	case "sell":
		return TradeTypeSell
	case "fee":
		return TradeTypeFee
	case "cash":
		if dividendSubtypeSet[sub] {
			return TradeTypeDividend
		}
		if reinvestSubtypeSet[sub] {
			return TradeTypeReinvest
		}
		if feeTaxSubtypeSet[sub] {
			return TradeTypeFee
		}
		return TradeTypeCash
	}
	return TradeTypeOther
}

// TradeTypeToSQLPredicate converts a slice of display trade-type keys into a
// SQL WHERE clause fragment and argument list that can be ANDed into a query on
// investment_transactions. The predicate is applied inside the UNION investment branch
// and the dedicated /investment-transactions endpoint — always as a SQL WHERE, never
// post-fetch, so that LIMIT n+1 keyset pagination is not corrupted.
//
// tableAlias qualifies the type/subtype column references (e.g. "it") to avoid
// the "column reference is ambiguous" error when the query JOINs accounts, which
// also has type/subtype columns. Pass an empty string only when there is no JOIN
// that would introduce ambiguity.
//
// The buckets are mutually exclusive over the Plaid v23 subtype enum so that a row
// can match at most one bucket when multiple types are requested (OR semantics in the
// returned clause).
//
// Returns ("", nil) when activityTypes is empty (no filter needed).
func TradeTypeToSQLPredicate(activityTypes []string, tableAlias string) (string, []any) {
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

	var parts []string
	var args []any

	for _, at := range activityTypes {
		switch at {
		case TradeTypeBuy:
			parts = append(parts, fmt.Sprintf("%s = 'buy'", col("type")))
		case TradeTypeSell:
			parts = append(parts, fmt.Sprintf("%s = 'sell'", col("type")))
		case TradeTypeDividend:
			placeholders := sqlInPlaceholders(len(args)+1, len(dividendSubtypes))
			parts = append(parts, fmt.Sprintf("%s IN (%s)", col("subtype"), placeholders))
			for _, s := range dividendSubtypes {
				args = append(args, s)
			}
		case TradeTypeReinvest:
			placeholders := sqlInPlaceholders(len(args)+1, len(reinvestSubtypes))
			parts = append(parts, fmt.Sprintf("%s IN (%s)", col("subtype"), placeholders))
			for _, s := range reinvestSubtypes {
				args = append(args, s)
			}
		case TradeTypeFee:
			// type='fee' OR subtype IN feeTaxSubtypes
			feePlaceholders := sqlInPlaceholders(len(args)+1, len(feeTaxSubtypes))
			parts = append(parts, fmt.Sprintf("(%s = 'fee' OR %s IN (%s))", col("type"), col("subtype"), feePlaceholders))
			for _, s := range feeTaxSubtypes {
				args = append(args, s)
			}
		case TradeTypeCash:
			// type='cash' AND subtype NOT IN (dividendSubtypes ∪ reinvestSubtypes ∪ feeTaxSubtypes)
			excludedSubtypes := append(append(append([]string{}, dividendSubtypes...), reinvestSubtypes...), feeTaxSubtypes...)
			placeholders := sqlInPlaceholders(len(args)+1, len(excludedSubtypes))
			parts = append(parts, fmt.Sprintf("(%s = 'cash' AND (%s IS NULL OR %s NOT IN (%s)))",
				col("type"), col("subtype"), col("subtype"), placeholders))
			for _, s := range excludedSubtypes {
				args = append(args, s)
			}
		case TradeTypeOther:
			// Everything that is not buy/sell/cash/fee (including transfer, which Plaid emits
			// for cash movements and which NormalizeTradeType maps to TradeTypeOther for display).
			parts = append(parts, fmt.Sprintf("(%s NOT IN ('buy', 'sell', 'cash', 'fee'))", col("type")))
		}
	}

	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, " OR "), args
}

// sqlInPlaceholders generates "$n, $n+1, ..." for n positional args starting at start.
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
