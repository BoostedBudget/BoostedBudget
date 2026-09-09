package services

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/observability"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// RunRuleQuarantineCensus is the fix for #1625 Task 18: it makes the quarantine inventory able to
// count RULES, not sync passes.
//
// ruleApplicationService.reportQuarantineCandidates (the existing shadow-mode reporter) runs once
// per sync pass, for one user's enabled rules, and INCREMENTS a counter. That structurally cannot
// answer "how many distinct rules are quarantined": the same rule is re-observed and re-counted
// every single sync for as long as it stays broken, so "one rule seen 1000 times" and "1000
// rules" are indistinguishable on that counter alone.
//
// This function instead visits EVERY rule in the table EXACTLY ONCE
// (CategorizationRuleRepository.ListAllForQuarantineCensus), classifies each with the SAME
// classifier the read-model and the shadow-mode reporter both use (quarantineReason, a thin alias
// for rules.QuarantineReasonFor — see that function's doc comment on why there must only ever be
// ONE implementation), and sets observability.SetRuleQuarantinePopulation to the resulting
// distinct counts. It is meant to be invoked periodically (e.g. from a scheduled job) — each call
// is a fresh, complete census, so calling it more or less often only changes how current the gauge
// is, never how it is computed.
//
// Rule ids and user ids never leave this function: only the closed-vocabulary reason and a count
// reach the metric, exactly as internal/observability/rule_quarantine_metrics.go requires.
func RunRuleQuarantineCensus(ctx context.Context, ruleRepo repository.CategorizationRuleRepository) error {
	rules, err := ruleRepo.ListAllForQuarantineCensus(ctx)
	if err != nil {
		return err
	}

	counts := make(map[observability.QuarantineReason]int, len(observability.AllQuarantineReasons()))
	for _, rule := range rules {
		reason := quarantineReason(rule)
		if reason == "" {
			continue // not quarantined — does not belong in any reason's population
		}
		counts[reason]++
	}

	observability.SetRuleQuarantinePopulation(counts)
	return nil
}
