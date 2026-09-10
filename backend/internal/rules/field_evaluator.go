package rules

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/matching"
)

// ─── Field-based evaluation (#1625) ─────────────────────────────────────────────────────────
//
// Conditions dispatch on Field + Operator rather than on a fused type name. The fourteen legacy
// fused types normalize into the same pairs (domain.Normalize), so a persisted rule and a new
// triple take the identical code path — there is exactly one implementation of "merchant
// contains", not two that can drift.
//
// ⚠ domain.FieldAmount is FROZEN on |amount|. Every persisted amount rule was written against
// that meaning, and TestAmountField_StillUsesAbsoluteValue_LegacyGuard fails if it changes.
// Direction lives in the separate FieldSignedAmount / FieldDirection fields, which no legacy row
// can normalize to.

// evaluateField evaluates a normalized condition. Returns (false, nil) for an unknown field.
//
// ⚠ The ORIGINAL justification no longer holds and this comment used to still assert it:
// EvaluateRules aborted a transaction's whole rule set on an error until #1625 A8 made errors
// rule-local. Fail-quiet is kept for a different reason — under OR logic an error skips the entire
// rule, where a non-match lets the rule's other conditions decide.
//
// ⚠ #1625 final review I1: this comment used to cite TestQuarantineEnforcementMatchesTheAdvertised
// Set as pinning that "an unnormalizable condition under OR still lets a valid sibling match,
// which is why such a rule is reported as still running rather than as quarantined". That test
// asserts the OPPOSITE, and has since Task 2: unnormalizable_condition is ENFORCED, so the test
// requires the rule NOT to match under either logic. The behaviour the citation described was
// real BEFORE enforcement moved into RuleMatches, and it is precisely why enforcement had to move
// there — a valid sibling carrying a rule the user did not write. Fail-quiet here is now a
// defence-in-depth default for the reasons above, not the mechanism that decides whether a
// quarantined rule runs.
func evaluateField(tx *domain.Transaction, c domain.RuleCondition) (bool, error) {
	op := domain.RuleConditionOperator(c.Operator)

	switch c.Field {
	case domain.FieldMerchant:
		return matchText(textOf(tx.MerchantName), op, c.Value), nil
	case domain.FieldEffectiveMerchant:
		// Falls back to the raw name when nothing enriched the transaction. Enrichment is
		// best-effort (one extra query, never fatal), so an un-enriched transaction must still
		// behave sensibly rather than silently disabling the rule.
		effective := textOf(tx.EffectiveMerchantName)
		if effective == "" {
			effective = textOf(tx.MerchantName)
		}
		return matchText(effective, op, c.Value), nil
	case domain.FieldName:
		// tx.Name is NOT NULL and always populated — it is the raw description, and the only
		// field that carries anything for a Zelle transfer, whose merchant_name is null.
		return matchText(tx.Name, op, c.Value), nil
	case domain.FieldMerchantID:
		// An opaque Plaid id: exact, case-SENSITIVE, never substring.
		return matchExactCaseSensitive(textOf(tx.MerchantID), op, c.Value), nil
	case domain.FieldAccountID:
		return matchAccountID(tx, op, c.Value), nil
	case domain.FieldAccountType:
		if tx.Account == nil {
			return false, nil
		}
		return matchText(tx.Account.Type, op, c.Value), nil
	case domain.FieldPaymentChannel:
		return matchText(textOf(tx.PaymentChannel), op, c.Value), nil
	case domain.FieldAmount:
		return matchNumber(math.Abs(tx.Amount), op, c.Value)
	case domain.FieldSignedAmount:
		return matchNumber(tx.Amount, op, c.Value)
	case domain.FieldDirection:
		return matchDirection(tx.Amount, op, c.Value), nil
	case domain.FieldDate:
		return matchDate(tx.Date, op, c.Value)
	case domain.FieldPFCPrimary, domain.FieldPFCDetailed:
		return matchPFC(tx, c.Field, op, c.Value), nil
	}
	return false, nil
}

func textOf(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// matchText applies a case-insensitive text operator. The condition value may be a scalar or an
// array; an array means "any of", which is the shape already in the database.
func matchText(subject string, op domain.RuleConditionOperator, value interface{}) bool {
	if subject == "" {
		return false
	}
	values := matching.ExtractStringValues(value)
	if len(values) == 0 {
		return false
	}
	lower := strings.ToLower(subject)

	anyOf := func(fn func(a, b string) bool) bool {
		for _, v := range values {
			if fn(lower, strings.ToLower(v)) {
				return true
			}
		}
		return false
	}

	switch op {
	case domain.RuleOpContains:
		return anyOf(strings.Contains)
	case domain.RuleOpStartsWith:
		return anyOf(strings.HasPrefix)
	case domain.RuleOpEndsWith:
		return anyOf(strings.HasSuffix)
	case domain.RuleOpEq, domain.RuleOpIn:
		return anyOf(func(a, b string) bool { return a == b })
	case domain.RuleOpNeq:
		return !anyOf(func(a, b string) bool { return a == b })
	}
	return false
}

func matchExactCaseSensitive(subject string, op domain.RuleConditionOperator, value interface{}) bool {
	if subject == "" {
		return false
	}
	values := matching.ExtractStringValues(value)
	if len(values) == 0 {
		return false
	}
	found := false
	for _, v := range values {
		if subject == v {
			found = true
			break
		}
	}
	if op == domain.RuleOpNeq {
		return !found
	}
	return found
}

// matchAccountID keeps the #1109/#1306 chain-aware behaviour verbatim: prefer the stable
// UserAccountID when set, then FALL THROUGH to the raw AccountID rather than returning early,
// because a rule whose value migration 000072 skipped still stores a raw account_id.
//
// Known limitation, carried over from the AccountIDEvaluator this replaced (#1625 removed it):
// accounts.id and user_accounts.id are both server-generated v4 UUIDs, so a rule storing account
// B's user_account_id could in principle match a transaction whose raw AccountID happens to equal
// that UUID. ~1 in 2^122 per pair, not practically exploitable. Documented by
// TestAccountIDMatching_ChainAware_KnownLimitation_UUIDCollision.
func matchAccountID(tx *domain.Transaction, op domain.RuleConditionOperator, value interface{}) bool {
	values := matching.ExtractStringValues(value)
	if len(values) == 0 {
		return false
	}
	// A transaction with no account identity at all matches NOTHING, including a negative
	// predicate. Codex review: without this, `account_id neq X` returned true for a transaction
	// with an empty AccountID and nil UserAccountID, which is both surprising and inconsistent
	// with matchText/matchExactCaseSensitive, which reject an empty subject outright.
	hasIdentity := tx.AccountID != "" || (tx.UserAccountID != nil && *tx.UserAccountID != "")
	if !hasIdentity {
		return false
	}
	matches := func(subject string) bool {
		if subject == "" {
			return false
		}
		for _, v := range values {
			if subject == v {
				return true
			}
		}
		return false
	}
	found := tx.UserAccountID != nil && *tx.UserAccountID != "" && matches(*tx.UserAccountID) || matches(tx.AccountID)
	if op == domain.RuleOpNeq {
		return !found
	}
	return found
}

func matchNumber(subject float64, op domain.RuleConditionOperator, value interface{}) (bool, error) {
	if op == domain.RuleOpBetween {
		m, ok := value.(map[string]interface{})
		if !ok {
			return false, fmt.Errorf("invalid numeric range: expected an object with min/max")
		}
		minVal, okMin := extractFloat(m["min"])
		maxVal, okMax := extractFloat(m["max"])
		if !okMin || !okMax {
			return false, fmt.Errorf("invalid numeric range: min and max must be numbers")
		}
		return subject >= minVal && subject <= maxVal, nil
	}

	threshold, ok := extractFloat(value)
	if !ok {
		return false, fmt.Errorf("invalid amount: %v", value)
	}
	const epsilon = 0.001
	switch op {
	case domain.RuleOpGt:
		return subject > threshold, nil
	case domain.RuleOpGte:
		return subject >= threshold, nil
	case domain.RuleOpLt:
		return subject < threshold, nil
	case domain.RuleOpLte:
		return subject <= threshold, nil
	case domain.RuleOpEq:
		return math.Abs(subject-threshold) < epsilon, nil
	case domain.RuleOpNeq:
		return math.Abs(subject-threshold) >= epsilon, nil
	}
	return false, nil
}

// matchDirection reads the sign of the raw amount. Positive is an outflow (Plaid PFCv2).
// A zero amount is neither, and matches neither.
func matchDirection(amount float64, op domain.RuleConditionOperator, value interface{}) bool {
	if amount == 0 {
		return false
	}
	actual := domain.DirectionInflow
	if amount > 0 {
		actual = domain.DirectionOutflow
	}
	return matchText(actual, op, value)
}

func matchDate(txDate time.Time, op domain.RuleConditionOperator, value interface{}) (bool, error) {
	const layout = "2006-01-02"

	if op == domain.RuleOpBetween {
		m, ok := value.(map[string]interface{})
		if !ok {
			return false, fmt.Errorf("invalid date range: expected an object with start/end")
		}
		startStr, hasStart := m["start"].(string)
		endStr, hasEnd := m["end"].(string)
		if !hasStart && !hasEnd {
			return false, fmt.Errorf("date range must have at least start or end")
		}
		if hasStart {
			start, err := time.Parse(layout, startStr)
			if err != nil {
				return false, fmt.Errorf("invalid start date format: %s", startStr)
			}
			if txDate.Before(start) {
				return false, nil
			}
		}
		if hasEnd {
			end, err := time.Parse(layout, endStr)
			if err != nil {
				return false, fmt.Errorf("invalid end date format: %s", endStr)
			}
			// End is inclusive: advance a day and require strictly before it.
			end = end.AddDate(0, 0, 1)
			if txDate.After(end) || txDate.Equal(end) {
				return false, nil
			}
		}
		return true, nil
	}

	s, ok := value.(string)
	if !ok {
		return false, fmt.Errorf("invalid date: expected a YYYY-MM-DD string")
	}
	bound, err := time.Parse(layout, s)
	if err != nil {
		return false, fmt.Errorf("invalid date format: %s", s)
	}
	day := txDate.Truncate(24 * time.Hour)
	switch op {
	case domain.RuleOpEq:
		return day.Equal(bound.Truncate(24 * time.Hour)), nil
	case domain.RuleOpGte:
		return !txDate.Before(bound), nil
	case domain.RuleOpLte:
		return txDate.Before(bound.AddDate(0, 0, 1)), nil
	}
	return false, nil
}

func matchPFC(tx *domain.Transaction, field domain.RuleConditionField, op domain.RuleConditionOperator, value interface{}) bool {
	effective := tx.GetEffectiveCategory()
	if effective == nil || effective.Kind != domain.EffectiveCategoryKindPFC {
		return false
	}
	var subject *string
	if field == domain.FieldPFCPrimary {
		subject = effective.PFCPrimary
	} else {
		subject = effective.PFCDetailed
	}
	if subject == nil {
		return false
	}
	return matchText(*subject, op, value)
}
