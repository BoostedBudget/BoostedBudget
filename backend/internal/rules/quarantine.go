package rules

import (
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// QuarantineReasonFor returns a non-empty reason when a rule cannot be evaluated as written.
// It never inspects user CONTENT — only structure — so its result is safe to log and to label a
// metric with.
//
// #1625: this is the SINGLE classifier. It labels the shadow-mode metric, fills the read model's
// `quarantine_reason`, and — since Task 2 — decides what RuleMatches refuses to run. Three
// implementations of "is this rule broken" would eventually disagree, and the disagreement would
// be invisible: the user would be told one thing and the engine would do another, which is the
// whole defect class this ticket exists to close. It lives in package rules rather than package
// services because the engine cannot import services.
//
// The rule is inspected through COPIES: Normalize mutates, and normalizing the live rule here
// would silently change what the very next evaluation matches.
func QuarantineReasonFor(rule *domain.CategorizationRule) observability.QuarantineReason {
	if rule == nil {
		return ""
	}
	if len(rule.Conditions) == 0 {
		// engine.go used to treat this as "match everything", which is a ledger-wide
		// recategorisation nobody asked for.
		return observability.QuarantineNoConditions
	}
	if rule.ConditionLogic != "AND" && rule.ConditionLogic != "OR" {
		return observability.QuarantineInvalidLogic
	}

	// A repairable value does not end the scan. A rule can hold one condition whose value is
	// merely wrong-typed and another whose value is unreadable, and the unreadable one decides:
	// reporting the softer reason would leave the rule advertised as running while the engine
	// refuses it.
	repairable := observability.QuarantineReason("")
	for i := range rule.Conditions {
		c := rule.Conditions[i] // copy: Normalize mutates, and this must not touch the live rule
		if err := c.Normalize(); err != nil {
			return observability.QuarantineUnnormalizable
		}
		if err := c.ValidateShape(); err != nil {
			if _, ok := c.CoercedValue(); !ok {
				return observability.QuarantineUncoercibleValue
			}
			repairable = observability.QuarantineInvalidValue
		}
	}
	return repairable
}
