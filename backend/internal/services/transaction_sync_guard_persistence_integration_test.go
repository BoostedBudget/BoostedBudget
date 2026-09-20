//go:build integration

package services

// Issue #1684, Defect 2 — the persisted-state regression suite for the `modified` path.
//
// WHY A SEPARATE FILE, AND WHY INTEGRATION-TAGGED. Defect 2 shipped because every assertion that
// touched the modified path asserted a CALL, never a ROW. `TestProcessModifiedTransactions_Success`
// (transaction_sync_service_test.go:1117) hands `UpdateFromSync` a `mock.MatchedBy` argument and
// stops there: a branch that applied rules in memory and then wrote through a statement with
// nowhere to put the category tuple would satisfy it. The claims below are about what Postgres
// holds after the write — the category tuple, `reviewed_at`, the side-effect gates — and pgxmock
// never sends a statement to a planner, so none of them is observable against a mock.
//
// ⚠ THE FIVE SUBTESTS COMPILE AND RUN ONLY UNDER `-tags integration`. An untagged
// `go test ./internal/services/...` excludes this file entirely (Go drops `//go:build integration`
// files without the tag), so an untagged copy would silently assert nothing about persisted state.
// The parent test is named exactly `TestProcessModifiedTransactions_GuardAndPersistence` so a
// `-run` filter cannot pass vacuously.
//
// The fixtures are the committed `ruleHistoryFixture` (rule_execution_history_integration_test.go)
// driven through the real `processModifiedTransactions`, exactly as T5's
// transaction_sync_modified_upsert_rules_integration_test.go and T6's
// transaction_sync_rule_persistence_integration_test.go do. Nothing here is hand-INSERTed into the
// state under test.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// TestProcessModifiedTransactions_GuardAndPersistence pins the whole contract of the `modified`
// path against real Postgres:
//
//	(a) the UPSERT branch applies rules and the insert-gated side effects fire for the row it
//	    actually inserted;
//	(b) the UPDATE branch persists the rule output through the T6 write (UpdateFromSync);
//	(c) a CategorySourceManual row is SKIPPED — the T6 guard-state copy is what makes the guard real;
//	(d) a Reviewed row is SKIPPED INCLUDING its reviewed_at;
//	(e) a rule whose output changes nothing is not rewritten BY THE RULE, while provider fields are
//	    still rewritten.
func TestProcessModifiedTransactions_GuardAndPersistence(t *testing.T) {
	// (a) ---------------------------------------------------------------------------------------
	//
	// The upsert branch (`existingTxn == nil`) stores the edit as a fresh INSERT. T5 made it apply
	// rules first; the observable proof is both the PERSISTED category tuple AND the insert-gated
	// side effects — EmitBatchSideEffects only acts for ids in CreateBatch's insertedIDs, so a
	// history row pointing at the persisted id is what proves it was called with the real inserted
	// id rather than an empty map or a synthetic one.
	t.Run("upsert_branch_applies_rules_and_emits_insert_gated_side_effects", func(t *testing.T) {
		f := seedRuleHistoryFixture(t)
		ctx := context.Background()
		svc := f.newModifiedPathService(t)
		enc := ruleHistoryEncryptor(t)

		// A genuinely new external id: GetByExternalID filters `deleted_at IS NULL`, and this id has
		// no row at all (live or tombstoned), so the write is a real INSERT and the insertedIDs set
		// contains it.
		externalID := "plaid-guard-upsert-" + uuid.New().String()
		payload := f.modifiedPayload(externalID, "Blue Bottle Coffee")

		require.NoError(t, svc.processModifiedTransactions(ctx, []plaid.Transaction{payload},
			&domain.LinkedItem{ID: f.linkedItemID, UserID: f.userID}, f.accountMap(), nil))

		var (
			id                                              string
			kind, pfcPrimary, categorySource, effectiveName *string
			reviewed                                        bool
			reviewedAt                                      *time.Time
		)
		require.NoError(t, f.tx.QueryRow(ctx, `
			SELECT id, category_override_kind, pfc_primary_override, category_source,
			       effective_category_name, reviewed, reviewed_at
			FROM transactions
			WHERE user_id = $1
			  AND source = 'plaid'
			  AND external_transaction_id_blind_idx = $2
			  AND deleted_at IS NULL
		`, f.userID, enc.BlindIndex(externalID)).Scan(
			&id, &kind, &pfcPrimary, &categorySource, &effectiveName, &reviewed, &reviewedAt))

		require.NotNil(t, kind,
			"the upsert branch stored the edit WITHOUT rule-categorizing it — the category is NULL")
		assert.Equal(t, "pfc", *kind)
		require.NotNil(t, pfcPrimary, "the rule's target category must be PERSISTED, not only in memory")
		assert.Equal(t, "FOOD_AND_DRINK", *pfcPrimary)
		require.NotNil(t, categorySource)
		assert.Equal(t, string(domain.CategorySourceRule), *categorySource)
		require.NotNil(t, effectiveName)
		assert.Equal(t, "FOOD_AND_DRINK", *effectiveName,
			"the display name is derived from the rule's tuple on the INSERT")
		assert.True(t, reviewed, "a rule applying a category counts as an automated review")
		require.NotNil(t, reviewedAt)

		// The insert-gated side effect. `EmitBatchSideEffects` is gated on
		// `insertedIDs[app.TransactionID]`, so the row below exists ONLY if it was called with the
		// id CreateBatch returned for THIS row. Its `transaction_id` is the FK back to the
		// persisted row, so an id that named no row could not satisfy it.
		var historyTxID *string
		require.NoError(t, f.tx.QueryRow(ctx, `
			SELECT transaction_id FROM rule_execution_history
			WHERE user_id = $1 AND rule_id = $2
		`, f.userID, f.ruleID).Scan(&historyTxID))
		require.NotNil(t, historyTxID, "the category rule's execution must be in the automation trail")
		assert.Equal(t, id, *historyTxID,
			"the trail must point at the PERSISTED id the upsert inserted")

		var categoryMatches int64
		require.NoError(t, f.tx.QueryRow(ctx,
			`SELECT match_count FROM categorization_rules WHERE id = $1`, f.ruleID).Scan(&categoryMatches))
		assert.EqualValues(t, 1, categoryMatches,
			"a genuinely inserted row IS included in the insert-gated side effects")
	})

	// (b) ---------------------------------------------------------------------------------------
	//
	// The update branch (`existingTxn != nil`) must persist the rule's output through T6's sibling
	// writer. A plain `Update` writes provider columns only — no category tuple, no tags, no review
	// state — so the PERSISTED override below is the evidence that UpdateFromSync ran. Reverting the
	// sibling to `Update` makes this subtest fail; that is QA scenario (ii).
	t.Run("update_branch_persists_the_rule_output_through_update_from_sync", func(t *testing.T) {
		f := seedRuleHistoryFixture(t)
		ctx := context.Background()
		svc := f.newModifiedPathService(t)

		externalID := "plaid-guard-update-" + uuid.New().String()
		storedID := uuid.New().String()
		// What Plaid first delivered: no override, category_source 'plaid', unreviewed, and a RAW
		// pfc_primary the rule is about to override.
		f.seedStoredTransaction(t, &domain.Transaction{
			ID: storedID, UserID: f.userID, AccountID: f.accountID, LinkedItemID: f.linkedItemID,
			Source: domain.SourcePlaid, ExternalTransactionID: &externalID,
			Amount: 5.00, Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			Name: "Blue Bottle Coffee", MerchantName: strPtr("Blue Bottle Coffee"),
			PFCPrimary: strPtr("GENERAL_MERCHANDISE"),
		})

		payload := f.modifiedPayload(externalID, "Blue Bottle Coffee")
		require.NoError(t, svc.processModifiedTransactions(ctx, []plaid.Transaction{payload},
			&domain.LinkedItem{ID: f.linkedItemID, UserID: f.userID}, f.accountMap(), nil))

		got := readPersistedRuleState(t, ctx, f.tx, storedID)

		require.NotNil(t, got.categoryOverrideKind,
			"the rule's override must reach the persisted row — UpdateFromSync is what carries it")
		assert.Equal(t, "pfc", *got.categoryOverrideKind)
		require.NotNil(t, got.pfcPrimaryOverride)
		assert.Equal(t, "FOOD_AND_DRINK", *got.pfcPrimaryOverride)
		require.NotNil(t, got.categorySource)
		assert.Equal(t, string(domain.CategorySourceRule), *got.categorySource)
		assert.True(t, got.reviewed, "a rule applying a category counts as an automated review")
		require.NotNil(t, got.reviewedAt)

		// `Update`'s CASE reads the PRE-update row, so a writer that set the override but left the
		// display-name CASE alone would store the raw provider value here. The sibling computes it
		// from the NEW parameter values.
		require.NotNil(t, got.effectiveCategoryName)
		assert.Equal(t, "FOOD_AND_DRINK", *got.effectiveCategoryName,
			"the display name must be recomputed from the post-rule tuple, not the pre-update row")

		// Provider columns are still rewritten by the same statement — the sibling replaces
		// `Update`'s contract, it does not narrow it.
		assert.Equal(t, 7.25, got.amount, "the provider's new amount must land")
	})

	// (c) ---------------------------------------------------------------------------------------
	//
	// The manual guard, asserted on the PERSISTED row. The payload WOULD match the fixture's coffee
	// rule, so the guard — not the absence of a match — is what leaves this row alone. T6's
	// guard-state copy is what makes the guard see `CategorySourceManual`; QA scenario (i) reverts
	// that copy and this subtest fails.
	t.Run("manual_row_is_skipped_on_the_persisted_row", func(t *testing.T) {
		f := seedRuleHistoryFixture(t)
		ctx := context.Background()
		svc := f.newModifiedPathService(t)

		externalID := "plaid-guard-manual-" + uuid.New().String()
		storedID := uuid.New().String()
		kind, primary := "pfc", "PERSONAL_CARE"
		// ⚠ `Reviewed` is deliberately FALSE here: this subtest pins the `CategorySourceManual` half
		// of the guard on its own, so reverting the guard-state copy makes it red for that reason
		// rather than because `Reviewed` was left behind.
		f.seedStoredTransaction(t, &domain.Transaction{
			ID: storedID, UserID: f.userID, AccountID: f.accountID, LinkedItemID: f.linkedItemID,
			Source: domain.SourcePlaid, ExternalTransactionID: &externalID,
			Amount: 5.00, Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			Name: "Blue Bottle Coffee", MerchantName: strPtr("Blue Bottle Coffee"),
			CategoryOverrideKind: &kind, PFCPrimaryOverride: &primary,
			CategorySourceField: domain.CategorySourceManual,
		})

		before := readPersistedRuleState(t, ctx, f.tx, storedID)
		require.NotNil(t, before.categorySource, "fixture guard: the stored row really is category_source='manual'")
		require.Equal(t, string(domain.CategorySourceManual), *before.categorySource)

		payload := f.modifiedPayload(externalID, "Blue Bottle Coffee")
		require.NoError(t, svc.processModifiedTransactions(ctx, []plaid.Transaction{payload},
			&domain.LinkedItem{ID: f.linkedItemID, UserID: f.userID}, f.accountMap(), nil))

		after := readPersistedRuleState(t, ctx, f.tx, storedID)
		assertUserOwnedTupleUnchanged(t, "a manual row", before, after)

		// The modified write DOES rewrite provider columns by design; that is not a violation of
		// "skipped", it is the path's purpose.
		assert.Equal(t, 7.25, after.amount, "provider columns are rewritten BY DESIGN")

		// The guard skips BEFORE evaluation, so nothing reaches the side-effect gates either.
		var matches int64
		require.NoError(t, f.tx.QueryRow(ctx,
			`SELECT match_count FROM categorization_rules WHERE id = $1`, f.ruleID).Scan(&matches))
		assert.Zero(t, matches, "a guard-skipped row must not inflate the match count")
		assert.Zero(t, f.count(t, ctx,
			`SELECT COUNT(*) FROM rule_execution_history WHERE user_id = $1`, f.userID),
			"a guard-skipped row must not appear in the automation trail")
	})

	// (d) ---------------------------------------------------------------------------------------
	//
	// The reviewed guard, asserted on the PERSISTED row. The load-bearing half is `reviewed_at`: the
	// guard SKIPS rule application for a reviewed row, but the write still executes — so if the
	// branch does not copy ReviewedAt, the write nulls reviewed_at while leaving reviewed=true and
	// silently loses the user's review timestamp. This pins the accepted residual (a reviewed row is
	// not re-evaluated) as deliberate.
	t.Run("reviewed_row_is_skipped_including_reviewed_at_on_the_persisted_row", func(t *testing.T) {
		f := seedRuleHistoryFixture(t)
		ctx := context.Background()
		svc := f.newModifiedPathService(t)

		externalID := "plaid-guard-reviewed-" + uuid.New().String()
		storedID := uuid.New().String()
		reviewedAt := time.Now().UTC().Truncate(time.Microsecond)
		f.seedStoredTransaction(t, &domain.Transaction{
			ID: storedID, UserID: f.userID, AccountID: f.accountID, LinkedItemID: f.linkedItemID,
			Source: domain.SourcePlaid, ExternalTransactionID: &externalID,
			Amount: 5.00, Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			Name: "Blue Bottle Coffee", MerchantName: strPtr("Blue Bottle Coffee"),
			Reviewed: true, ReviewedAt: &reviewedAt,
		})

		before := readPersistedRuleState(t, ctx, f.tx, storedID)
		require.NotNil(t, before.reviewedAt, "fixture guard: the stored row really carries a review time")
		require.True(t, before.reviewed)

		payload := f.modifiedPayload(externalID, "Blue Bottle Coffee")
		require.NoError(t, svc.processModifiedTransactions(ctx, []plaid.Transaction{payload},
			&domain.LinkedItem{ID: f.linkedItemID, UserID: f.userID}, f.accountMap(), nil))

		after := readPersistedRuleState(t, ctx, f.tx, storedID)
		assertUserOwnedTupleUnchanged(t, "a reviewed row", before, after)

		require.NotNil(t, after.reviewedAt, "reviewed_at must survive a modified re-delivery")
		assert.True(t, after.reviewedAt.Equal(reviewedAt),
			"reviewed_at is the user's own timestamp; a sync must not restamp or drop it")
		assert.Nil(t, after.categoryOverrideKind,
			"a reviewed row is not re-categorized: the rule must not reach the persisted tuple")
		assert.Equal(t, 7.25, after.amount, "provider columns are rewritten BY DESIGN")
	})

	// (e) ---------------------------------------------------------------------------------------
	//
	// A row whose rule output changes nothing. "Changes nothing" is defined the way the production
	// audit trail defines it — `len(app.Executions) == 0`, populated by buildRuleExecutionRecords
	// (rule_application_service.go:483-546) — NOT as "the row is not rewritten". The modified path
	// rewrites provider fields by design; what it must not do is rewrite the RULE-OWNED tuple or
	// emit side effects for a match that produced no change.
	//
	// ⚠ The category rule is DISABLED for this subtest. A category rule always flips `reviewed`
	// false→true, and that flip IS a change (rule_application_service.go:499-501), so it can never
	// produce the no-op shape this subtest pins. Only the tag-only rule may match.
	t.Run("no_op_rule_output_leaves_the_rule_owned_tuple_alone", func(t *testing.T) {
		f := seedRuleHistoryFixture(t)
		ctx := context.Background()
		svc := f.newModifiedPathService(t)

		_, err := f.tx.Exec(ctx, `UPDATE categorization_rules SET enabled = false WHERE id = $1`, f.ruleID)
		require.NoError(t, err)

		externalID := "plaid-guard-noop-" + uuid.New().String()
		storedID := uuid.New().String()
		f.seedStoredTransaction(t, &domain.Transaction{
			ID: storedID, UserID: f.userID, AccountID: f.accountID, LinkedItemID: f.linkedItemID,
			Source: domain.SourcePlaid, ExternalTransactionID: &externalID,
			Amount: 5.00, Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			Name: "Blue Bottle Coffee", MerchantName: strPtr("Blue Bottle Coffee"),
			// Already carrying the tag rule's own tag, already attributed to THAT rule — the only
			// shape that reaches the `tagsBefore` clause in buildRuleExecutionRecords.
			Tags:       []string{"caffeine"},
			TagsSource: []domain.TagSource{{Tag: "caffeine", Source: "rule", RuleID: &f.tagRuleID}},
		})

		// PRECONDITION, through the same real rule service production uses. The definition of
		// "changes nothing" is len(Executions) == 0: the tag rule MATCHES (MatchedRuleIDs is
		// non-empty, so a BatchRuleApplication is produced) but contributes no change.
		probeSvc := NewRuleApplicationService(repository.NewCategorizationRuleRepository(f.tx))
		probeSvc.SetRuleExecutionHistoryRepository(repository.NewRuleExecutionHistoryRepository(f.tx))
		probe := domain.Transaction{
			ID:                  storedID,
			MerchantName:        strPtr("Blue Bottle Coffee"),
			Tags:                []string{"caffeine"},
			TagsSource:          []domain.TagSource{{Tag: "caffeine", Source: "rule", RuleID: &f.tagRuleID}},
			CategorySourceField: domain.CategorySourcePlaid,
		}
		probeApps, err := probeSvc.ApplyRulesToBatch(ctx, []domain.Transaction{probe}, f.userID)
		require.NoError(t, err)
		require.Len(t, probeApps, 1, "the tag rule matches — that is the point: matched, changed nothing")
		assert.Empty(t, probeApps[0].Executions,
			"len(Executions) == 0 is what 'changes nothing' means; a non-empty slice here would make "+
				"the assertions below meaningless")

		before := readPersistedRuleState(t, ctx, f.tx, storedID)
		require.Equal(t, []string{"caffeine"}, before.tags, "fixture guard: the tag is really stored")

		payload := f.modifiedPayload(externalID, "Blue Bottle Coffee")
		require.NoError(t, svc.processModifiedTransactions(ctx, []plaid.Transaction{payload},
			&domain.LinkedItem{ID: f.linkedItemID, UserID: f.userID}, f.accountMap(), nil))

		after := readPersistedRuleState(t, ctx, f.tx, storedID)

		// No category change, no review-state change, no tag change — the rule-owned tuple is
		// untouched.
		assertUserOwnedTupleUnchanged(t, "a no-op rule output", before, after)

		// …but provider fields ARE rewritten. This is the modified path's purpose; the assertion is
		// here so a future "simplification" that skips the write entirely cannot pass.
		assert.Equal(t, 7.25, after.amount, "provider fields are still rewritten as normal")

		// No side effects: the synthetic insertedIDs excludes a matched-but-unchanged row, so no
		// match-count increment and no history row.
		var tagMatches int64
		require.NoError(t, f.tx.QueryRow(ctx,
			`SELECT match_count FROM categorization_rules WHERE id = $1`, f.tagRuleID).Scan(&tagMatches))
		assert.Zero(t, tagMatches, "a matched-but-unchanged row must not inflate the match count")
		assert.Zero(t, f.count(t, ctx,
			`SELECT COUNT(*) FROM rule_execution_history WHERE user_id = $1`, f.userID),
			"nothing changed, so nothing belongs in the automation trail")
	})
}
