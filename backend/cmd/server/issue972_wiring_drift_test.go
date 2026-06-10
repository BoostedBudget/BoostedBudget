package main

// TestIssue972ProductionWiringNoDrift guards against the "dead-setter" bug class:
// service setters that exist, are unit-tested individually, but are never called in
// the production main.go composition — so the feature silently no-ops in production
// while all unit tests pass (each test injects deps manually).
//
// This test scans main.go as text and asserts the required Issue #972 setter calls
// and constructor wiring are present. It cannot replace integration tests but it DOES
// catch the canonical blind spot that every prior pass has missed at least one of.
//
// Invariant: every production setter for Issue #972 services must appear in main.go.
// If a setter is removed, the corresponding assertion here must be removed too.
//
// Issue #972: Savings Transfer Automation — wiring correctness guard (GAP H class).

import (
	"os"
	"strings"
	"testing"
)

func TestIssue972ProductionWiringNoDrift(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("could not read main.go: %v", err)
	}
	src := string(data)

	// Each entry: (expression that MUST appear in main.go, human-readable description).
	required := []struct {
		expr string
		desc string
	}{
		// boostExecutionService setters — the canonical dead-setter class.
		// Unit tests in boost_execution_service_test.go inject each dep manually;
		// these assertions verify the production composition also wires them.
		{
			"boostExecutionService.SetSavingsGoalRepo(",
			"boostExecutionService.SetSavingsGoalRepo must be called so executeSavingsGoal doesn't return 'not configured' (GAP H)",
		},
		{
			"boostExecutionService.SetAccountRepo(",
			"boostExecutionService.SetAccountRepo must be called for account ownership checks at execution time (GAP AE)",
		},
		{
			"boostExecutionService.SetChainResolver(",
			"boostExecutionService.SetChainResolver must be called with a real AccountChainResolver (GAP H)",
		},
		{
			"boostExecutionService.SetNotificationRepo(",
			"boostExecutionService.SetNotificationRepo must be called so suggestion notifications are created",
		},
		{
			"boostExecutionService.SetAnalytics(",
			"boostExecutionService.SetAnalytics must be called so rule execution fires analytics events",
		},

		// boostService setters — IDOR ownership validation at create/update time (GAP AE).
		{
			"boostService.SetSavingsGoalRepo(",
			"boostService.SetSavingsGoalRepo must be called to prevent savings_goal_id IDOR at rule creation",
		},
		{
			"boostService.SetAccountRepo(",
			"boostService.SetAccountRepo must be called to prevent account IDOR at rule creation",
		},

		// Transfer correlation service — wires fuzzy-match detection to transaction sync.
		{
			"transactionSyncService.SetTransferCorrelationService(",
			"SetTransferCorrelationService must be called so fuzzy-match detection runs on every Plaid sync (GAP B4)",
		},

		// budgetSyncService event processor — wires budget_period_end events to boostExecutionService.
		// Without this call budget_updated and budget_period_end events are silently dropped.
		{
			"budgetSyncService.SetEventProcessor(",
			"budgetSyncService.SetEventProcessor(boostExecutionService) must be called so budget events reach the boost engine (Issue #972 B1, Plan §B1 Bugfix wiring)",
		},

		// Budget period close check worker — expirer and timezone must be wired.
		// These are constructor fields, not setters, but the pattern is the same.
		{
			"transferSuggestionRepo, // Issue #972 GAP E",
			"TransferSuggestionExpirer must be wired into the BudgetPeriodCloseCheckWorker so ExpirePending runs daily (GAP E)",
		},
		{
			"UserTimezones:",
			"BudgetPeriodCloseCheckWorker.UserTimezones must be wired so period boundaries are computed in user timezone (Issue #972 Task 2e)",
		},
		{
			"budgetPeriodCloseCheckWorker.SetJobQueue(",
			"BudgetPeriodCloseCheckWorker.SetJobQueue must be called after River client creation so the worker can enqueue rule evaluation jobs (circular dep pattern)",
		},

		// Transfer suggestion handler — GDPR analytics and PostHog must both be wired.
		{
			"transferSuggestionSvc.SetGDPRAnalytics(",
			"transferSuggestionSvc.SetGDPRAnalytics must be called so GDPR consent events fire on confirm-match (Issue #972 C4)",
		},
		{
			"humaTransferSuggestionHandler.SetAnalytics(",
			"humaTransferSuggestionHandler.SetAnalytics must be called so PostHog fires on confirm-match success (Issue #972 C4 / Phase E)",
		},

		// Savings stats snapshot periodic job must carry UniqueOpts (B1 fix).
		// Guards against silent duplicate snapshot creation during retry storms.
		{
			"river.UniqueOpts{",
			"SavingsStatsArgs periodic job must have UniqueOpts for queue-level dedup (Issue #972 B1)",
		},
	}

	var failures []string
	for _, r := range required {
		if !strings.Contains(src, r.expr) {
			failures = append(failures, "MISSING: "+r.expr+" — "+r.desc)
		}
	}

	if len(failures) > 0 {
		t.Errorf("Issue #972 production wiring drift detected in main.go (%d missing):\n\n%s\n\n"+
			"Each missing call is a production misconfiguration invisible to unit tests.\n"+
			"Add the missing call to cmd/server/main.go or update this test if the wiring changed.",
			len(failures), strings.Join(failures, "\n"))
	}
}
