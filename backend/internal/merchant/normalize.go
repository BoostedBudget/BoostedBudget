// Package merchant provides merchant name normalization and override utilities
// for Issue #957: deterministic cleaning of garbled Plaid merchant names.
package merchant

import (
	"regexp"
	"strings"
)

var (
	// Card-reader prefixes (e.g. "SQ *", "TST *", "PAYPAL *").
	// Matched case-insensitively at the start of the name.
	rePrefix = regexp.MustCompile(`(?i)^(SQ|TST|SP|PP|PAYPAL)\s*\*\s*`)

	// Trailing store/location codes, e.g. "#1234", "STORE 042", "99821".
	// Applied repeatedly until no more matches.
	reSuffix = regexp.MustCompile(`(?i)(\s+(#\d+|STORE\s+\d+|\d+))+\s*$`)

	// Collapse runs of whitespace (tabs, multiple spaces) to a single space.
	reWhitespace = regexp.MustCompile(`\s+`)
)

// Normalize cleans a raw Plaid merchant name into a human-readable, deterministic
// display form. It:
//   - strips card-reader prefixes (SQ *, TST *, SP *, PP *, PAYPAL *)
//   - strips trailing store/location codes (#1234, STORE 042, trailing digit runs)
//   - collapses whitespace
//   - title-cases the result
//
// The raw merchant_name is never modified on the transaction; this function is
// used only to compute merchants.normalized_name when a merchant row is created.
func Normalize(raw string) string {
	s := raw

	// 1. Trim leading/trailing whitespace first (must precede prefix regex)
	s = strings.TrimSpace(s)

	// 2. Strip card-reader prefix
	s = rePrefix.ReplaceAllString(s, "")

	// 3. Strip trailing store/location codes (loop for multiple, e.g. "STORE 042 #99")
	for {
		stripped := reSuffix.ReplaceAllString(s, "")
		if stripped == s {
			break
		}
		s = stripped
	}

	// 4. Collapse internal whitespace and trim again
	s = reWhitespace.ReplaceAllString(strings.TrimSpace(s), " ")
	s = strings.TrimSpace(s)

	// 4. Title-case
	s = titleCase(s)

	return s
}

// titleCase applies English title-casing to words that begin with a letter:
// first rune uppercased, remainder lowercased. Words that begin with a digit
// (e.g. "12B") are left unchanged — their mixed case is intentional.
// No smart article/conjunction handling; keeps implementation deterministic.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	words := strings.Fields(s)
	for i, w := range words {
		if w == "" {
			continue
		}
		r := []rune(w)
		// Only title-case words that begin with a Unicode letter.
		if !isLetter(r[0]) {
			continue
		}
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
		for j := 1; j < len(r); j++ {
			r[j] = []rune(strings.ToLower(string(r[j])))[0]
		}
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// isLetter reports whether r is a Unicode letter.
func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r > 127 && strings.ToLower(string(r)) != strings.ToUpper(string(r)))
}
