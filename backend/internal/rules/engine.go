package rules

import (
	"os"
	"sort"
	"strconv"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

// RuleResult contains the outcome of evaluating rules against a transaction.
type RuleResult struct {
	// Category action is set by the first matching rule with target category fields (first-match-wins).
	TargetKind             *string
	TargetPFCPrimary       *string
	TargetPFCDetailed      *string
	TargetCustomCategoryID *string
	// CategoryRuleID is the ID of the rule that set the category.
	CategoryRuleID string

	// Tags are accumulated from all matching rules (all-match).
	Tags []string
	// TagRuleIDs are the IDs of rules that contributed tags.
	TagRuleIDs []string

	// TagSources tracks which rule contributed each tag.
	TagSources []domain.TagSource

	// MatchedRuleIDs are all rules that matched the transaction.
	MatchedRuleIDs []string

	// FailedRuleIDs are rules that could not be evaluated at all. #1625 A8: an evaluator error is
	// LOCAL to its rule — it skips that rule and no other. Before this, EvaluateRules returned on
	// the first error, so one un-evaluatable condition silently suppressed every other rule the
	// user had for that transaction.
	//
	// Reported rather than swallowed: quarantine classification and the operator both need to know
	// which rule failed, and "it just stopped matching" is the failure mode this ticket exists to
	// eliminate.
	FailedRuleIDs []string
}

// RuleEngine evaluates categorization rules against transactions.
type RuleEngine struct {
	registry *ConditionRegistry
}

// NewRuleEngine creates a new rule engine with the default condition registry.
func NewRuleEngine() *RuleEngine {
	return &RuleEngine{
		registry: NewConditionRegistry(),
	}
}

// NewRuleEngineWithRegistry creates a rule engine with a custom registry.
func NewRuleEngineWithRegistry(registry *ConditionRegistry) *RuleEngine {
	return &RuleEngine{
		registry: registry,
	}
}

// EvaluateRules evaluates all rules against a transaction and returns the result.
//
// Category assignment follows "first-match-wins" semantics:
// - Rules are sorted by priority (highest first)
// - First matching rule with a category action sets the category outcome
//
// Tag assignment follows "all-match" semantics:
// - All matching rules contribute their tags
// - Tag behavior (add/replace) is respected
func (e *RuleEngine) EvaluateRules(tx *domain.Transaction, rules []*domain.CategorizationRule) (*RuleResult, error) {
	result := &RuleResult{
		Tags:           make([]string, 0),
		TagRuleIDs:     make([]string, 0),
		TagSources:     make([]domain.TagSource, 0),
		MatchedRuleIDs: make([]string, 0),
		FailedRuleIDs:  make([]string, 0),
	}

	if len(rules) == 0 {
		return result, nil
	}

	// Sort rules by priority (highest first).
	// Tie-breaking (Codex review #957): equal-priority rules are ordered by
	// CreatedAt descending (newer rule wins) then by ID for exact-timestamp ties,
	// making the result deterministic regardless of the caller's slice order.
	sortedRules := make([]*domain.CategorizationRule, len(rules))
	copy(sortedRules, rules)
	sort.Slice(sortedRules, func(i, j int) bool {
		a, b := sortedRules[i], sortedRules[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.ID > b.ID
	})

	// Track tags to avoid duplicates
	tagSet := make(map[string]bool)

	for _, rule := range sortedRules {
		// Skip disabled rules
		if !rule.Enabled {
			continue
		}

		// Check if rule matches. An error is local to THIS rule (#1625 A8): skip it, record it,
		// and carry on with the rest. Returning here made one broken condition disable every other
		// rule for the transaction, with nothing anywhere saying so.
		matches, err := e.RuleMatches(tx, rule)
		if err != nil {
			result.FailedRuleIDs = append(result.FailedRuleIDs, rule.ID)
			continue
		}

		if !matches {
			continue
		}

		// Record matched rule
		result.MatchedRuleIDs = append(result.MatchedRuleIDs, rule.ID)

		// Handle category action (first-match-wins)
		if result.TargetKind == nil && rule.HasCategoryAction() {
			result.TargetKind = rule.TargetKind
			result.TargetPFCPrimary = rule.TargetPFCPrimary
			result.TargetPFCDetailed = rule.TargetPFCDetailed
			result.TargetCustomCategoryID = rule.TargetCustomCategoryID
			result.CategoryRuleID = rule.ID
		}

		// Handle tags (all-match)
		if rule.HasTagAction() {
			// Handle tag behavior
			if rule.TagBehavior == "replace" {
				// Clear existing tags
				result.Tags = make([]string, 0)
				result.TagRuleIDs = make([]string, 0)
				result.TagSources = make([]domain.TagSource, 0)
				tagSet = make(map[string]bool)
			}

			// Add new tags (avoid duplicates)
			for _, tag := range rule.TargetTags {
				if !tagSet[tag] {
					tagSet[tag] = true
					result.Tags = append(result.Tags, tag)
					ruleID := rule.ID
					result.TagSources = append(result.TagSources, domain.TagSource{
						Tag:    tag,
						Source: "rule",
						RuleID: &ruleID,
					})
				}
			}

			// Record which rule contributed tags
			if len(rule.TargetTags) > 0 {
				result.TagRuleIDs = append(result.TagRuleIDs, rule.ID)
			}
		}
	}

	return result, nil
}

// enforceQuarantine gates every ENFORCED quarantine reason (#1625 Task 2).
//
// Default ON. Set RULE_QUARANTINE_ENFORCE to any false-y value (false, 0, f, FALSE, False) to
// restore the pre-#1625 behaviour of running every rule regardless of how broken it is.
// Package-level rather than threaded through config because the engine is constructed in several
// places and a flag nobody can find is not a usable switch. Documented in .env.example beside the
// other kill switches, for the same reason.
//
// ⚠ #1625 final review I2: this was `os.Getenv(...) != "false"` while its own comment claimed
// "the same kill-switch convention as CACHE_ENABLED". It was not: CACHE_ENABLED goes through
// config.getEnvBool, i.e. strconv.ParseBool, which accepts 0/f/F/FALSE/False as well. So an
// operator reaching for this switch DURING AN INCIDENT — the only time it is ever used — could
// type `=0` and silently keep enforcement ON, with nothing to tell them. It now uses the same
// ParseBool rule, and an UNPARSEABLE value fails SAFE (enforcement stays on) rather than being
// read as "off".
//
// ⚠ It was RULE_QUARANTINE_ENFORCE_ZERO_CONDITIONS when zero-conditions was the only enforced
// reason. Renamed rather than kept as an alias because the old name would now be a lie about its
// own scope, and it has never shipped outside this branch. Pinned by
// TestQuarantineEnforcementKillSwitchCoversEveryEnforcedReason, which drives every reason with
// the switch off, and by TestQuarantineEnforcementReadsTheEnvVarNotJustTheGoVar, which drives the
// ENV VAR itself — a test that only assigns the Go variable proves nothing about the parsing, and
// that is exactly the gap this defect lived in.
//
// It gates ENFORCEMENT only, never COERCION. Coercion repairs a value the engine would otherwise
// read wrongly; turning it off would restore a defect rather than a behaviour.
var enforceQuarantine = quarantineEnforcementFromEnv()

// quarantineEnforceEnvVar is the switch's name in ONE place, so the code, the test that drives it
// and any future documentation generator cannot disagree about it.
const quarantineEnforceEnvVar = "RULE_QUARANTINE_ENFORCE"

// QuarantineEnforceEnvVar re-exports that name for guards in other packages. The published doc
// tags on domain.CategorizationRule promise the reader this switch exists; the test that checks
// they still name it lives in internal/services, beside the read model whose value the switch
// changes, so it needs the name from here rather than a second copy of the literal.
const QuarantineEnforceEnvVar = quarantineEnforceEnvVar

// quarantineEnforcementFromEnv mirrors config.getEnvBool(key, true): unset or unparseable means
// the default (ON), anything ParseBool understands means what it says.
func quarantineEnforcementFromEnv() bool {
	if value := os.Getenv(quarantineEnforceEnvVar); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return true
}

// QuarantineEnforced reports whether the engine is currently refusing quarantined rules.
//
// ⚠ #1625 final review I3: this exists because enforcement had THREE sources of truth. RuleMatches
// gated on the kill switch; categorization_rule_service.annotateQuarantineStatus and
// ruleApplicationService.reportQuarantineCandidates did not. With the switch off, the card said
// "Not running" while the engine went on recategorising the user's ledger — the divergence
// RuleMatches' own comment calls impossible. Every read model that reports "is this rule running"
// must go through here, not through observability.QuarantineReasonStopsMatching alone: that
// predicate answers "does this REASON stop a rule", which is only half the question while a kill
// switch exists.
func QuarantineEnforced() bool { return enforceQuarantine }

// RuleMatches checks if a single rule matches a transaction.
//
// #1625 Task 2: enforcement lives HERE and nowhere else — one site, which also covers the two
// Apply paths and the preview path that share it. Which reasons stop a rule is not decided here
// either: it is read from observability.QuarantineReasonStopsMatching. That predicate is only
// HALF the answer, though, and #1625 second fix wave H2/M3 corrected this comment for implying it
// was the whole one: the read model sets `quarantined` from
// services.quarantineStopsMatching = QuarantineEnforced() && QuarantineReasonStopsMatching(...),
// i.e. the same predicate AND the same kill switch this line gates on, and the card derives its
// sentence from that field rather than from the reason. Those two expressions being literally the
// same is what makes "the card says Not running" and "the rule matches nothing" impossible to
// disagree — an earlier revision had them diverge, and the user was told a rule was off while it
// quietly recategorised their ledger.
//
// The unenforced reason, invalid_condition_value_shape, is REPAIRED instead: see
// domain.CoercedValue. It is the COMMON case in the legacy corpus — #346 validated cond.Type and
// nothing about the value, so wrong-typed values have persisted through the public API for the
// whole life of the feature — which is why switching those rules off would switch off rules
// people actually wrote.
//
// ⚠ #1625 third fix wave F2: this said it was "the ONLY reason reachable through the ordinary
// API", and that was retracted in the second fix wave on domain.CoercedValue without being
// retracted here. The same absent validation also admitted values coercion CANNOT repair, which
// QuarantineReasonFor classifies as the ENFORCED uncoercible_condition_value — so at least two
// reasons are API-reachable, and no comment on this branch should assert a sole one again.
//
// A quarantined rule is NOT hidden from reads: it still lists, with its reason. Filtering it out
// of GetEnabledByUserID instead would make it vanish from the UI, which is the silent failure
// this ticket exists to remove.
func (e *RuleEngine) RuleMatches(tx *domain.Transaction, rule *domain.CategorizationRule) (bool, error) {
	if !rule.Enabled {
		return false, nil
	}

	if enforceQuarantine && observability.QuarantineReasonStopsMatching(QuarantineReasonFor(rule)) {
		return false, nil
	}

	if len(rule.Conditions) == 0 {
		// Only reachable with the kill switch off: a rule with no conditions matched EVERY
		// transaction before #1625, commented "No conditions means always match (edge case)".
		return true, nil
	}

	switch rule.ConditionLogic {
	case "AND":
		return e.evaluateAND(tx, rule.Conditions)
	case "OR":
		return e.evaluateOR(tx, rule.Conditions)
	default:
		// Default to AND logic
		return e.evaluateAND(tx, rule.Conditions)
	}
}

// evaluateAND returns true if ALL conditions match.
func (e *RuleEngine) evaluateAND(tx *domain.Transaction, conditions []domain.RuleCondition) (bool, error) {
	for _, condition := range conditions {
		matches, err := e.registry.Evaluate(tx, condition)
		if err != nil {
			return false, err
		}
		if !matches {
			return false, nil
		}
	}
	return true, nil
}

// evaluateOR returns true if ANY condition matches.
func (e *RuleEngine) evaluateOR(tx *domain.Transaction, conditions []domain.RuleCondition) (bool, error) {
	for _, condition := range conditions {
		matches, err := e.registry.Evaluate(tx, condition)
		if err != nil {
			return false, err
		}
		if matches {
			return true, nil
		}
	}
	return false, nil
}

// SetQuarantineEnforcementForTest flips the kill switch and returns a restore function.
//
// Exported (rather than left to package-internal assignment) because #1625 final review I3's
// divergence was BETWEEN packages: the read model lives in services and the switch lives here, so
// the test that proves they agree has to be able to move the switch from outside. It is a test
// seam and nothing in production calls it — enforcement itself is decided once, at init, from
// quarantineEnforcementFromEnv.
func SetQuarantineEnforcementForTest(enforcing bool) func() {
	original := enforceQuarantine
	enforceQuarantine = enforcing
	return func() { enforceQuarantine = original }
}
