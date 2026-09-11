package domain

import "strings"

// Currency in the investment domain (#1631 M8a).
//
// ─────────────────────────────────────────────────────────────────────────────────────────────
// THE DECISION, WRITTEN DOWN: mixed currencies are DETECTED AND DECLARED, never converted.
//
// There is no FX rate source anywhere in this repository — no rate table, no rate provider, no
// rate column, and Plaid's investments endpoints do not supply one. Converting would therefore
// mean inventing a rate, and a total computed at an invented rate is indistinguishable, to the
// user and to us, from a correct one. So:
//
//   - Where a figure is MEANINGLESS across currencies and is per-row, it is REFUSED (nil).
//     That is the day change: quantity * (institution_price - close_price) subtracts a
//     position priced by the institution from a close price Plaid documents as being in USD for
//     a foreign-currency security. Refusing costs one cell in one row.
//
//   - Where a figure is a whole-portfolio AGGREGATE, it is still returned but DECLARED as a
//     currency-blind sum, via a currency_code that is null and an is_mixed_currency flag that is
//     true. Refusing here would blank the entire investments page for a legitimately
//     multi-currency portfolio, which is a bigger lie by omission than a labelled approximation
//     — and the label is what stops the number being SILENTLY wrong, which is the actual defect.
//
// The asymmetry is deliberate: a per-row unit error has a cheap, honest fallback (show nothing);
// a portfolio total does not.
// ─────────────────────────────────────────────────────────────────────────────────────────────
//
// UNKNOWN IS NOT A CURRENCY. Every investment row written before this change has currency_code
// NULL, because the Plaid mappers never read the provider's currency fields. If NULL counted as a
// distinct currency then every existing portfolio would report itself mixed, and every existing
// day change would vanish, on deploy day, with no change to the underlying data. So an absent or
// blank code never conflicts with anything: it is skipped, and a set containing one known code
// plus any number of unknowns resolves to that known code.

// NormalizeCurrencyCode upper-cases and trims a provider currency code, returning "" for an
// absent, blank, or whitespace-only value.
//
// Normalisation happens before EVERY comparison. Plaid does not guarantee the case of these
// tokens, and a raw string comparison that reads "usd" and "USD" as two currencies would suppress
// figures for holdings that have no currency problem whatsoever — the same class of
// false-positive-safety bug that treating NULL as a currency would cause.
func NormalizeCurrencyCode(code *string) string {
	if code == nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(*code))
}

// CurrenciesConflict reports whether two currency codes are both KNOWN and different — the only
// condition under which arithmetic between the two values they describe is a unit error.
//
// An unknown code on either side is not a conflict; see the "UNKNOWN IS NOT A CURRENCY" note
// above for why that is a correctness requirement and not leniency.
func CurrenciesConflict(a, b *string) bool {
	na, nb := NormalizeCurrencyCode(a), NormalizeCurrencyCode(b)
	if na == "" || nb == "" {
		return false
	}
	return na != nb
}

// HasCurrencyMismatch reports whether this holding is priced in a different currency from the
// security whose close price the day change would subtract.
//
// Returns false when there is no joined security: a holding with no security row has no second
// currency to disagree with, and the day change is already nil for want of a close price.
func (h *Holding) HasCurrencyMismatch() bool {
	if h.Security == nil {
		return false
	}
	return CurrenciesConflict(h.CurrencyCode, h.Security.CurrencyCode)
}

// ResolveHoldingsCurrency reports the single currency a set of holdings is priced in, and whether
// the set is mixed.
//
// Returns (nil, false) when nothing in the set states a currency — "we do not know" rather than
// "there is a problem". Returns (nil, true) when two or more distinct currencies appear, and
// deliberately names NEITHER of them: naming one would invite a caller to label the
// currency-blind sum with it, which is the precise way a mixed total becomes a confidently wrong
// one.
func ResolveHoldingsCurrency(holdings []Holding) (*string, bool) {
	codes := make([]string, 0, len(holdings))
	for i := range holdings {
		codes = append(codes, NormalizeCurrencyCode(holdings[i].CurrencyCode))
	}
	return ResolveCurrencyCodes(codes)
}

// ResolveCurrencyCodes reports the single currency a set of already-collected codes describes,
// and whether that set is mixed. The array form of ResolveHoldingsCurrency, for repository
// aggregates that collect the distinct codes in SQL rather than returning row-level values.
//
// Blank entries are ignored on the same "unknown is not a currency" rule; the SQL side filters
// NULLs out before aggregating, so a blank should not normally reach here.
func ResolveCurrencyCodes(codes []string) (*string, bool) {
	seen := ""
	for _, raw := range codes {
		code := strings.ToUpper(strings.TrimSpace(raw))
		if code == "" {
			continue
		}
		if seen == "" {
			seen = code
			continue
		}
		if seen != code {
			return nil, true
		}
	}
	if seen == "" {
		return nil, false
	}
	return &seen, false
}
