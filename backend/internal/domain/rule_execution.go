package domain

import (
	"encoding/json"
	"time"
)

// Issue #1569 Task 5 (#1572) — the automation audit trail.
//
// `rule_execution_history` has existed since migration 000020 and gained a GDPR erasure path plus
// point-in-time snapshot columns in 000164. Until this change it had NO production writer and NO
// reader: every row in it came from a test fixture, so the trail an auditing user would be shown
// was permanently empty. This type is the Go side of both halves.
//
// ⚠ #1572's stated blocker — "`rules.Engine` is never constructed in production" — is FALSE. The
// type is `rules.RuleEngine`, it is constructed by `rules.NewRuleEngine()` inside
// `NewRuleApplicationService` (services/rule_application_service.go) and driven on the live Plaid
// sync path from `transactionSyncService.processAddedTransactions`. The missing piece was the
// writer, not the engine.
//
// ⚠ CITED BY NAME, NOT BY LINE. The first version of this comment said ":67" and was already wrong
// in the commit that wrote it — the call is 47 lines further down — because the line was read
// before the same commit's own edits landed above it. Names do not drift.

// Rule execution action types. Pinned by the live `valid_action_type` CHECK constraint:
// action_type IN ('category','tags','both'). Any other value is rejected by Postgres.
const (
	RuleExecutionActionCategory = "category"
	RuleExecutionActionTags     = "tags"
	RuleExecutionActionBoth     = "both"
)

// Rule execution results. Pinned by the live `valid_execution_result` CHECK:
// result IN ('applied','skipped_manual').
//
// ⚠ ONLY `applied` IS EVER WRITTEN, and that is a product decision rather than an omission. The
// user's ruling for #1572 is "record only when a rule actually changes something", so a row exists
// precisely when a rule changed state. `skipped_manual` is additionally UNRECORDABLE as the match
// loop stands: the manual/reviewed skip in ApplyRulesToBatch happens BEFORE EvaluateRules is
// called, so there is no matched rule to attribute the skip to. Recording it would mean writing one
// row per enabled rule per skipped transaction, which is a different feature.
const (
	RuleExecutionResultApplied       = "applied"
	RuleExecutionResultSkippedManual = "skipped_manual"
)

// RuleExecution is one row of rule_execution_history: a record that one categorization rule changed
// one transaction, and what the rule was at the moment it did so.
type RuleExecution struct {
	ID string `json:"id"`

	// RuleID is NULL once the rule has been deleted (fk_rule_execution_history_rule_id is
	// ON DELETE SET NULL since #1569 Task 14). NULL means "the rule is gone", never "no rule" —
	// RuleName and DefinitionSnapshot are what a reader shows in its place.
	RuleID *string `json:"rule_id"`

	// TransactionID is NULL once the transaction has been hard-deleted
	// (fk_rule_execution_history_transaction_id is ON DELETE SET NULL since #1569 Task 7).
	// A provider removal does not reach this: it tombstones the transaction instead.
	TransactionID *string `json:"transaction_id"`

	// UserID is the owner, denormalised at execution time. It is the row's ONLY path to GDPR
	// erasure now that rule_id is nullable, and the key every per-user read filters on.
	UserID string `json:"user_id"`

	// RuleName is the rule's name AS IT WAS at execution time. Never updated on rename.
	RuleName *string `json:"rule_name,omitempty"`

	// DefinitionSnapshot is the rule definition AS IT WAS at execution time, so an orphaned row can
	// still answer "what did this automation do, and why" with no parent to join to. Written once,
	// at INSERT; nothing ever rewrites it.
	DefinitionSnapshot json.RawMessage `json:"definition_snapshot,omitempty"`

	ActionType string    `json:"action_type"`
	Result     string    `json:"result"`
	ExecutedAt time.Time `json:"executed_at"`
}

// RuleExecutionsResponse is the paginated account-wide automation history payload.
// Mirrors BoostExecutionsResponse.
type RuleExecutionsResponse struct {
	Executions []*RuleExecution `json:"executions"`
	Total      int              `json:"total"`
	Limit      int              `json:"limit"`
	Offset     int              `json:"offset"`
}
