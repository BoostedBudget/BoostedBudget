// Plain-language rendering of the backend's fixed quarantine vocabulary. Keys mirror
// observability.QuarantineReason's wire values (backend/internal/observability/
// rule_quarantine_metrics.go); an unrecognised key falls back to a generic phrase rather than
// rendering a raw enum at the user. The mapping is kept in sync with the Go constants by
// AutoCategorizationRuleCard.test.tsx, which parses that file rather than restating the list.
//
// ⚠ EVERY fragment here describes ONLY THE PROBLEM. None of them says whether the rule is
// running, and none of them may (#1625 second fix wave H2/M3).
//
// The semantics this branch settled on: `quarantined` means "the server is currently preventing
// this rule from matching anything", and that is SWITCH-DEPENDENT by design. With
// RULE_QUARANTINE_ENFORCE off the backend reports quarantined=false for an enforcement-class
// reason, because the engine really is running the rule — a zero-condition rule then matches
// EVERY transaction. An earlier revision keyed the sentence to the REASON and so rendered "It is
// not being run" over a rule that was recategorising the user's entire ledger: the ticket's own
// defect, published to the user. Run status is therefore derived from `enabled` and `quarantined`
// and from nothing else, in ONE place (quarantineSentence below), and the reason contributes only
// the clause after the colon.
export const QUARANTINE_ISSUES: Record<string, string> = {
  no_conditions_matches_everything: 'it has no conditions, so it matches every transaction',
  invalid_condition_logic_defaults_to_and: 'its condition logic is not AND or OR',
  unnormalizable_condition: 'one of its conditions uses a field or operator that no longer exists',
  uncoercible_condition_value:
    'one of its conditions has a value that cannot be read as the kind of value that condition needs',
  invalid_condition_value_shape:
    'one of its conditions has a value of the wrong type, which the server reads as the one obvious value it could mean',
}

const GENERIC_QUARANTINE_ISSUE = 'it has a problem with its conditions'

// quarantineSentence is the ONE place a run status is stated.
//
// enabled=false wins over everything: RuleMatches (backend/internal/rules/engine.go) returns
// false for !rule.Enabled BEFORE quarantine is consulted at all, so a disabled rule is not
// running no matter what the other two fields say (#1625 Task 10).
//
// quarantined=true is the server saying it is refusing the rule right now.
//
// The remaining case — a problem the server is NOT currently stopping — deliberately says "may
// still be matching" rather than "is running": with enforcement off, an uncoercible value still
// makes the evaluator error and that rule contributes nothing, so a flat "is running" would be
// its own false claim.
export function quarantineSentence(enabled: boolean, quarantined: boolean, reason: string): string {
  const issue = QUARANTINE_ISSUES[reason] ?? GENERIC_QUARANTINE_ISSUE
  if (!enabled) {
    return `This rule is disabled, so it is not running. Before re-enabling: ${issue}.`
  }
  if (quarantined) {
    return `This rule is not being run by the server: ${issue}. Edit it to fix that.`
  }
  return `The server has not stopped this rule, so it may still be matching transactions: ${issue}. Edit the rule to confirm that is what you meant.`
}
