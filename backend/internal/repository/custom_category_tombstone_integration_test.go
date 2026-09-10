//go:build integration

package repository

// Issue #1569 Task 9 — the real-database half of the custom-category tombstone.
//
// pgxmock asserts query STRINGS and never executes them, and everything that decides whether this
// task is correct is semantics: whether a historical transaction still carries its override AND
// its `reviewed` state after a delete, whether `deleted_at IS NULL` genuinely hides the category
// from the pickers that used to show it while its name still resolves on those transactions,
// what the FK graph does now that nothing DELETEs the row, and whether erasure still reaches it.
// None of that is observable against a mock, so it is all here.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type categoryTombstoneFixture struct {
	tx     pgx.Tx
	userID string

	accountID     string
	linkedItemID  string
	userAccountID string

	catRepo    CustomCategoryRepository
	ruleRepo   CategorizationRuleRepository
	budgetRepo BudgetRepository
	auditRepo  AuditLogRepository

	// "Coffee" — the category under test. It is a PARENT (childID nests under it), it is the
	// override on two transactions (one live, one tombstoned by Task 7's provider-removal path),
	// the target of two rules, and a line in a budget whose rollover history names it.
	coffeeID string
	childID  string
	teaID    string

	liveTxnID      string
	tombstonedTxID string

	categoryOnlyRuleID string
	tagBearingRuleID   string
	ruleExecID         string

	budgetID     string
	budgetCatID  string
	rolloverID   string
	otherBudgetC string
}

func seedCategoryTombstoneFixture(t *testing.T) *categoryTombstoneFixture {
	t.Helper()
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	f := &categoryTombstoneFixture{
		tx:                 tx,
		userID:             uuid.New().String(),
		accountID:          uuid.New().String(),
		linkedItemID:       uuid.New().String(),
		userAccountID:      uuid.New().String(),
		coffeeID:           uuid.New().String(),
		childID:            uuid.New().String(),
		teaID:              uuid.New().String(),
		liveTxnID:          uuid.New().String(),
		tombstonedTxID:     uuid.New().String(),
		categoryOnlyRuleID: uuid.New().String(),
		tagBearingRuleID:   uuid.New().String(),
		ruleExecID:         uuid.New().String(),
		budgetID:           uuid.New().String(),
		budgetCatID:        uuid.New().String(),
		rolloverID:         uuid.New().String(),
		otherBudgetC:       uuid.New().String(),
	}
	f.catRepo = NewCustomCategoryRepository(tx)
	f.ruleRepo = NewCategorizationRuleRepository(tx)
	f.budgetRepo = NewBudgetRepository(tx)
	f.auditRepo = NewAuditLogRepository(tx)

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, f.userID, "test|"+f.userID, "cat-tomb-1569-"+f.userID+"@example.com")

	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, 'ins-cat-tomb-1569', 'depository')
	`, f.userAccountID, f.userID)

	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, f.linkedItemID, f.userID, "cat-tomb-item-"+f.linkedItemID)

	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, $5, 'Cat Tombstone Checking', 'depository', 'active')
	`, f.accountID, f.linkedItemID, f.userID, f.userAccountID, "plaid-acc-cat-"+f.accountID)

	mustExec("custom_category Coffee", `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Coffee', 'FOOD_AND_DRINK')
	`, f.coffeeID, f.userID)

	mustExec("custom_category Espresso (child of Coffee)", `
		INSERT INTO custom_categories (id, user_id, name, parent_id)
		VALUES ($1, $2, 'Espresso', $3)
	`, f.childID, f.userID, f.coffeeID)

	mustExec("custom_category Tea", `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Tea', 'FOOD_AND_DRINK')
	`, f.teaID, f.userID)

	// The live transaction: filed under Coffee BY HAND, and marked reviewed BY HAND. Both of those
	// are what the old delete destroyed.
	mustExec("live transaction", `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id,
			source, external_transaction_id, external_transaction_id_blind_idx,
			amount, date, name, pending, pfc_primary,
			category_override_kind, custom_category_override_id, category_source,
			category_override_date, effective_category_name, reviewed, reviewed_at,
			created_at, updated_at)
		VALUES ($1, $2, $3, $4,
			'plaid', $5, $6,
			4.50, CURRENT_DATE - 10, 'Blue Bottle', false, 'GENERAL_MERCHANDISE',
			'custom', $7, 'manual',
			NOW() - INTERVAL '9 days', 'Coffee', true, NOW() - INTERVAL '9 days',
			NOW(), NOW())
	`, f.liveTxnID, f.userID, f.accountID, f.linkedItemID, "ext-live-"+f.liveTxnID, "ext-live-"+f.liveTxnID, f.coffeeID)

	// A transaction Task 7 tombstoned (the provider reversed it). It STILL references Coffee via
	// the ON DELETE RESTRICT foreign key — this row is the one that makes the RESTRICT question
	// real rather than theoretical.
	mustExec("tombstoned transaction", `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id,
			source, external_transaction_id, external_transaction_id_blind_idx,
			amount, date, name, pending, pfc_primary,
			category_override_kind, custom_category_override_id, category_source,
			category_override_date, effective_category_name, reviewed, reviewed_at,
			deleted_at, deleted_reason, deleted_by,
			created_at, updated_at)
		VALUES ($1, $2, $3, $4,
			'plaid', $5, $6,
			3.25, CURRENT_DATE - 20, 'Blue Bottle (reversed)', false, 'GENERAL_MERCHANDISE',
			'custom', $7, 'manual',
			NOW() - INTERVAL '19 days', 'Coffee', true, NOW() - INTERVAL '19 days',
			NOW(), 'provider_removed_posted', 'plaid_sync',
			NOW(), NOW())
	`, f.tombstonedTxID, f.userID, f.accountID, f.linkedItemID, "ext-tomb-"+f.tombstonedTxID, "ext-tomb-"+f.tombstonedTxID, f.coffeeID)

	// Two rules targeting Coffee: one whose ONLY action was the category (the old step 2 DELETEd
	// it), one that also applies tags (the old step 3 BLANKED its target and left it firing).
	mustExec("category-only rule", `
		INSERT INTO categorization_rules
			(id, user_id, name, enabled, priority, conditions, target_kind, target_custom_category_id, target_tags)
		VALUES ($1, $2, 'File Blue Bottle as Coffee', true, 10,
			'[{"field":"merchant_name","operator":"contains","value":"Blue Bottle"}]'::jsonb,
			'custom', $3, '[]'::jsonb)
	`, f.categoryOnlyRuleID, f.userID, f.coffeeID)

	mustExec("tag-bearing rule", `
		INSERT INTO categorization_rules
			(id, user_id, name, enabled, priority, conditions, target_kind, target_custom_category_id, target_tags)
		VALUES ($1, $2, 'Coffee and tag it treat', true, 5,
			'[{"field":"name","operator":"contains","value":"Bottle"}]'::jsonb,
			'custom', $3, '["treat"]'::jsonb)
	`, f.tagBearingRuleID, f.userID, f.coffeeID)

	// The automation audit trail hanging off the category-only rule. rule_execution_history.rule_id
	// -> categorization_rules(id) is ON DELETE CASCADE (migration 000020:159), which is why
	// "delete the rule" is not an available ruling for steps 2/3.
	mustExec("rule_execution_history", `
		INSERT INTO rule_execution_history (id, rule_id, transaction_id, action_type, result, user_id)
		VALUES ($1, $2, $3, 'category', 'applied', $4)
	`, f.ruleExecID, f.categoryOnlyRuleID, f.liveTxnID, f.userID)

	mustExec("budget", `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, series_id)
		VALUES ($1, $2, 'August Budget', 'monthly', CURRENT_DATE - 30, 3000, $1)
	`, f.budgetID, f.userID)

	mustExec("budget_category for Coffee", `
		INSERT INTO budget_categories (id, budget_id, category_kind, custom_category_id, allocated_amount, spent_amount)
		VALUES ($1, $2, 'custom', $3, 100, 40)
	`, f.budgetCatID, f.budgetID, f.coffeeID)

	mustExec("unrelated budget_category", `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount)
		VALUES ($1, $2, 'pfc', 'TRANSPORTATION', 200, 10)
	`, f.otherBudgetC, f.budgetID)

	// rollover_history names the Coffee budget category as its source. Task 8 (migration 000158)
	// made budget_categories soft-deletable precisely so this row survives; a hard CASCADE from
	// custom_categories would have destroyed it again.
	mustExec("rollover_history", `
		INSERT INTO rollover_history
			(id, user_id, budget_id, source_budget_category_id, amount, rollover_type,
			 budget_period_start, budget_period_end, status)
		VALUES ($1, $2, $3, $4, 60, 'full', NOW() - INTERVAL '30 days', NOW(), 'applied')
	`, f.rolloverID, f.userID, f.budgetID, f.budgetCatID)

	return f
}

func (f *categoryTombstoneFixture) count(t *testing.T, ctx context.Context, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, f.tx.QueryRow(ctx, sql, args...).Scan(&n))
	return n
}

// ---------------------------------------------------------------------------------------------
// 1. THE INVERTED EXPECTATION. A test used to pin `reviewed = false` after a category delete —
//    it encoded the bug as intended behaviour. This is the same scenario with the expectation
//    flipped, plus the four override columns and category_source it also used to destroy.
// ---------------------------------------------------------------------------------------------

func TestCategoryTombstone_DeleteKeepsHistoryAndReviewedState_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	for _, probe := range []struct {
		what string
		id   string
	}{
		{"the live transaction", f.liveTxnID},
		{"the provider-reversed transaction", f.tombstonedTxID},
	} {
		var kind, source, effectiveName *string
		var overrideID *string
		var reviewed bool
		var reviewedAt *time.Time
		require.NoError(t, f.tx.QueryRow(ctx, `
			SELECT category_override_kind, custom_category_override_id, category_source,
			       effective_category_name, reviewed, reviewed_at
			FROM transactions WHERE id = $1
		`, probe.id).Scan(&kind, &overrideID, &source, &effectiveName, &reviewed, &reviewedAt))

		require.NotNil(t, kind, "%s lost its override kind", probe.what)
		assert.Equal(t, "custom", *kind, "%s was refiled", probe.what)
		require.NotNil(t, overrideID, "%s lost its category assignment", probe.what)
		assert.Equal(t, f.coffeeID, *overrideID,
			"%s no longer points at the category the user filed it under", probe.what)
		require.NotNil(t, source)
		assert.Equal(t, "manual", *source,
			"%s was relabelled as Plaid's guess — the user categorised it by hand", probe.what)
		require.NotNil(t, effectiveName)
		assert.Equal(t, "Coffee", *effectiveName,
			"%s lost the category NAME it was filed under", probe.what)

		// The heart of the inversion. This assertion read `assert.False` before #1569.
		assert.True(t, reviewed,
			"%s was un-reviewed by a category delete. Review state is work the user did by hand, "+
				"one transaction at a time; deleting one row of configuration must not throw it away.",
			probe.what)
		assert.NotNil(t, reviewedAt,
			"%s kept `reviewed` but lost `reviewed_at` — the two must move together", probe.what)
	}
}

// ---------------------------------------------------------------------------------------------
// 2. Hidden from every picker, list and count — but the name still resolves on history.
// ---------------------------------------------------------------------------------------------

func TestCategoryTombstone_HiddenFromPickersButNameStillResolves_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	before, err := f.catRepo.GetByUserID(ctx, f.userID)
	require.NoError(t, err)
	require.Len(t, before, 3, "fixture must seed three categories or the assertions below prove nothing")

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	after, err := f.catRepo.GetByUserID(ctx, f.userID)
	require.NoError(t, err)
	for _, c := range after {
		assert.NotEqual(t, f.coffeeID, c.ID, "a tombstoned category is still in the picker")
	}
	assert.Len(t, after, 2)

	_, err = f.catRepo.GetByID(ctx, f.coffeeID)
	assert.ErrorIs(t, err, domain.ErrCategoryNotFound,
		"GetByID is the write-time ownership gate; it must not hand back a tombstoned category")

	_, err = f.catRepo.GetByName(ctx, f.userID, "Coffee")
	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)

	n, err := f.catRepo.CountByUserID(ctx, f.userID)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "a tombstoned category must not consume a tier slot")

	// The other half of the product decision: the name still resolves for the transactions that
	// reference it. GetNamesByIDs is the enrichment path the transaction and rule lists use.
	names, err := f.catRepo.GetNamesByIDs(ctx, f.userID, []string{f.coffeeID})
	require.NoError(t, err)
	assert.Equal(t, "Coffee", names[f.coffeeID],
		"a past transaction filed under this category must not render as a bare UUID")

	// ...while the LIVE-only variant, which is what write-time validation uses, refuses it.
	live, err := f.catRepo.GetLiveNamesByIDs(ctx, f.userID, []string{f.coffeeID, f.teaID})
	require.NoError(t, err)
	assert.NotContains(t, live, f.coffeeID,
		"a tombstoned category must not be attachable to a new budget line or rule")
	assert.Equal(t, "Tea", live[f.teaID])
}

// ---------------------------------------------------------------------------------------------
// 3. THE RULING on steps 2 and 3: rules are DISABLED, with their target left exactly as the user
//    configured it. Pinned so a future tidy-up that deletes or blanks them fails loudly.
// ---------------------------------------------------------------------------------------------

func TestCategoryTombstone_RulesAreDisabledNotDeletedOrBlanked_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	enabledBefore, err := f.ruleRepo.GetEnabledByUserID(ctx, f.userID)
	require.NoError(t, err)
	require.Len(t, enabledBefore, 2, "fixture must seed two ENABLED rules or this test proves nothing")

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	for _, ruleID := range []string{f.categoryOnlyRuleID, f.tagBearingRuleID} {
		var enabled bool
		var kind, target *string
		require.NoError(t, f.tx.QueryRow(ctx, `
			SELECT enabled, target_kind, target_custom_category_id
			FROM categorization_rules WHERE id = $1
		`, ruleID).Scan(&enabled, &kind, &target),
			"rule %s was DELETED. It must not be: rule_execution_history.rule_id is ON DELETE "+
				"CASCADE (migration 000020), so deleting the rule destroys the automation audit "+
				"trail — exactly the history #1569 exists to keep.", ruleID)

		assert.False(t, enabled,
			"rule %s is still enabled while its target category is tombstoned", ruleID)
		require.NotNil(t, kind,
			"rule %s had its target BLANKED. A blanked rule keeps firing with a DIFFERENT effect "+
				"than the user configured, which is worse than either deleting or disabling it.", ruleID)
		assert.Equal(t, "custom", *kind)
		require.NotNil(t, target, "rule %s lost the category it pointed at", ruleID)
		assert.Equal(t, f.coffeeID, *target,
			"rule %s must keep naming the category so the user can see what it did and re-point it", ruleID)
	}

	// The automation trail survives because the rule survives.
	assert.Equal(t, 1,
		f.count(t, ctx, `SELECT COUNT(*) FROM rule_execution_history WHERE id = $1`, f.ruleExecID),
		"the rule execution history was cascaded away")

	// And nothing the engine loads can fire any more.
	enabledAfter, err := f.ruleRepo.GetEnabledByUserID(ctx, f.userID)
	require.NoError(t, err)
	assert.Empty(t, enabledAfter,
		"the rule engine reads WHERE enabled = true; a rule pointing at a tombstoned category "+
			"must not be in that set")
}

// ---------------------------------------------------------------------------------------------
// 4. The ON DELETE RESTRICT from transactions.custom_category_override_id is still satisfiable.
//    This is the one place Task 9 could have broken Task 7: the old step 1 cleared the override on
//    EVERY transaction, tombstoned ones included, which is what kept the subsequent hard DELETE
//    legal. Stopping the revert is only safe because nothing hard-DELETEs the category any more.
// ---------------------------------------------------------------------------------------------

func TestCategoryTombstone_RestrictStillSatisfiable_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	require.Equal(t, 1,
		f.count(t, ctx,
			`SELECT COUNT(*) FROM transactions WHERE id = $1 AND deleted_at IS NOT NULL AND custom_category_override_id = $2`,
			f.tombstonedTxID, f.coffeeID),
		"the fixture must hold a TOMBSTONED transaction still referencing the category, or this "+
			"test cannot observe the interaction it exists for")

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo),
		"the delete must not trip fk_transactions_custom_category_override")

	// Demonstrate that the RESTRICT is real, and that the tombstone is what avoids it: a hard
	// DELETE of the same row, with the same referencing transactions in place, still fails.
	_, err := f.tx.Exec(ctx, `SAVEPOINT restrict_probe`)
	require.NoError(t, err)
	_, err = f.tx.Exec(ctx, `DELETE FROM custom_categories WHERE id = $1`, f.coffeeID)
	require.Error(t, err,
		"if a hard DELETE now succeeds, the RESTRICT has been weakened and this test is vacuous")
	assert.Contains(t, err.Error(), "fk_transactions_custom_category_override")
	_, err = f.tx.Exec(ctx, `ROLLBACK TO SAVEPOINT restrict_probe`)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------------------------
// 5. GDPR erasure still destroys a tombstoned category — through the production cascade.
// ---------------------------------------------------------------------------------------------

func TestCategoryTombstone_GDPRErasureStillDestroysIt_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))
	require.Equal(t, 3,
		f.count(t, ctx, `SELECT COUNT(*) FROM custom_categories WHERE user_id = $1`, f.userID),
		"all three category rows must still be present BEFORE erasure, or the assertions below "+
			"would pass against rows that were never there")

	profileRepo := NewProfileRepository(f.tx, newTestFieldEncryptor(t))

	// DeleteUserCascade's C2 guard refuses while a linked_items row still owes Plaid-side cleanup.
	// This is the state the deletion worker reaches before it calls the cascade, not a bypass.
	_, err := f.tx.Exec(ctx, `UPDATE linked_items SET plaid_removed_at = NOW() WHERE user_id = $1`, f.userID)
	require.NoError(t, err)

	var requestedAt time.Time
	require.NoError(t, f.tx.QueryRow(ctx, `
		UPDATE users
		SET deletion_requested_at = NOW() - INTERVAL '31 days',
		    deletion_scheduled_at = NOW() - INTERVAL '1 day'
		WHERE id = $1
		RETURNING deletion_requested_at
	`, f.userID).Scan(&requestedAt))

	deleted, affectedOwners, err := profileRepo.DeleteUserCascade(ctx, f.userID, &requestedAt)
	require.NoError(t, err, "erasure must not be blocked by anything this task added")
	require.True(t, deleted,
		"the cascade reported a legitimate skip rather than running — every assertion below would "+
			"then be vacuous")
	assert.Empty(t, affectedOwners, "no other user's rows are involved in this fixture")

	assert.Zero(t, f.count(t, ctx, `SELECT COUNT(*) FROM custom_categories WHERE user_id = $1`, f.userID),
		"a tombstoned category survived erasure — a tombstone must never become a way to keep "+
			"data after a right-to-be-forgotten request")
	assert.Zero(t, f.count(t, ctx, `SELECT COUNT(*) FROM transactions WHERE user_id = $1`, f.userID))
	assert.Zero(t, f.count(t, ctx, `SELECT COUNT(*) FROM categorization_rules WHERE user_id = $1`, f.userID))
}

// ---------------------------------------------------------------------------------------------
// 6. THE RULING on step 4: children are reparented to top-level, not tombstoned with their parent.
// ---------------------------------------------------------------------------------------------

func TestCategoryTombstone_ChildrenReparentedToTopLevel_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	child, err := f.catRepo.GetByID(ctx, f.childID)
	require.NoError(t, err,
		"the child was tombstoned along with its parent. It must not be: a child category is LIVE "+
			"configuration the user did not ask to delete, and tombstoning it destroys more than "+
			"the reparenting this replaces.")
	assert.Nil(t, child.ParentID,
		"the child still points at an invisible parent — every tree read would render an "+
			"unreachable node")

	assert.Zero(t, f.count(t, ctx,
		`SELECT COUNT(*) FROM custom_categories WHERE id = $1 AND deleted_at IS NOT NULL`, f.childID))
}

// A TOMBSTONED child's recorded parent is HISTORY, and the reparent must not rewrite it.
//
// #1569 T8 fix round 2. The reparent used to be unfiltered, waved through the write-target rule by
// an exemption whose stated reason was FABRICATED: it claimed "the parent is being deleted in this
// same transaction" and that a filter "would break the delete". DeleteWithReassignment never
// deletes the parent — step 1 TOMBSTONES it — so fk_custom_categories_parent_id (live definition:
// ON DELETE RESTRICT) is never at risk, and nothing was being protected.
//
// What the exemption actually waved through is the mutation the write rule exists to surface. The
// step-3 ruling above justifies reparenting by "every tree read would then render a node whose
// parent is invisible" — an argument about a LIVE child, which is rendered. A TOMBSTONED child is
// rendered by no tree read at all, so the justification does not reach it, and rewriting its
// recorded parent_id destroys the one fact about it worth keeping: what it was filed under when
// the user deleted it.
//
// Order matters and is the whole test: delete the CHILD first, then the parent.
func TestCategoryTombstone_TombstonedChildKeepsItsRecordedParent_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.childID, f.auditRepo))
	require.Equal(t, 1, f.count(t, ctx,
		`SELECT COUNT(*) FROM custom_categories WHERE id = $1 AND deleted_at IS NOT NULL AND parent_id = $2`,
		f.childID, f.coffeeID),
		"the fixture must hold a TOMBSTONED child still recording its parent, or this test cannot "+
			"observe the interaction it exists for")

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	var parentID *string
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT parent_id FROM custom_categories WHERE id = $1`, f.childID).Scan(&parentID))
	require.NotNil(t, parentID,
		"the reparent rewrote a TOMBSTONED child's parent_id to NULL. That row is history: it "+
			"records what the child was filed under when the user deleted it, and no tree read "+
			"renders it, so nothing is gained by clearing it and the recorded parent is lost.")
	assert.Equal(t, f.coffeeID, *parentID)
}

// The companion risk, and the reason the change above needs a second test rather than one.
//
// fk_custom_categories_parent_id is ON DELETE RESTRICT (verified against the live schema). Leaving
// a tombstoned child pointing at a tombstoned parent creates a self-referencing pair that did not
// exist before this change, and GDPR erasure deletes both in one cascade from `users`. If RESTRICT
// fired there, the retention improvement would have bought a right-to-be-forgotten failure — the
// one trade this branch may never make.
func TestCategoryTombstone_GDPRErasureDestroysASelfReferencingTombstonedPair_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.childID, f.auditRepo))
	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))
	require.Equal(t, 1, f.count(t, ctx,
		`SELECT COUNT(*) FROM custom_categories c
		 WHERE c.id = $1 AND c.deleted_at IS NOT NULL
		   AND c.parent_id IN (SELECT id FROM custom_categories WHERE id = $2 AND deleted_at IS NOT NULL)`,
		f.childID, f.coffeeID),
		"the fixture must hold a tombstoned child pointing at a TOMBSTONED parent — the exact "+
			"shape RESTRICT could refuse — or this test proves nothing")

	profileRepo := NewProfileRepository(f.tx, newTestFieldEncryptor(t))

	_, err := f.tx.Exec(ctx, `UPDATE linked_items SET plaid_removed_at = NOW() WHERE user_id = $1`, f.userID)
	require.NoError(t, err)

	var requestedAt time.Time
	require.NoError(t, f.tx.QueryRow(ctx, `
		UPDATE users
		SET deletion_requested_at = NOW() - INTERVAL '31 days',
		    deletion_scheduled_at = NOW() - INTERVAL '1 day'
		WHERE id = $1
		RETURNING deletion_requested_at
	`, f.userID).Scan(&requestedAt))

	deleted, _, err := profileRepo.DeleteUserCascade(ctx, f.userID, &requestedAt)
	require.NoError(t, err,
		"fk_custom_categories_parent_id is ON DELETE RESTRICT, and a tombstoned child pointing at "+
			"a tombstoned parent is a shape this branch newly creates. If erasure now fails, the "+
			"retained parent_id has to go — a right-to-be-forgotten failure is not a trade this "+
			"branch may make.")
	require.True(t, deleted, "the cascade reported a skip; every assertion below would be vacuous")

	assert.Zero(t, f.count(t, ctx,
		`SELECT COUNT(*) FROM custom_categories WHERE user_id = $1`, f.userID))
}

// The step-4 reparent is `WHERE parent_id = $1 AND user_id = $2`. The user_id predicate is
// defence-in-depth, not a fix: parent_id can never cross users in the first place, because
// enforce_custom_category_two_levels (migration 000078) rejects it BEFORE INSERT OR UPDATE OF
// parent_id. This test is the evidence for that claim rather than a reading of the trigger source.
func TestCategoryTombstone_ParentIdCannotCrossUsers_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	otherUser := uuid.New().String()
	_, err := f.tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, otherUser, "test|"+otherUser, "cat-tomb-other-"+otherUser+"@example.com")
	require.NoError(t, err)

	_, err = f.tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, parent_id)
		VALUES ($1, $2, 'Poached', $3)
	`, uuid.New().String(), otherUser, f.coffeeID)
	require.Error(t, err, "a cross-user parent_id must be impossible to write")
	assert.Contains(t, err.Error(), "same user")
}

// ---------------------------------------------------------------------------------------------
// 7. Budget categories are TOMBSTONED, not cascaded away — and the rollover trail survives.
// ---------------------------------------------------------------------------------------------

func TestCategoryTombstone_BudgetCategoriesTombstonedNotCascaded_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	var deletedReason, deletedBy *string
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT deleted_reason, deleted_by FROM budget_categories WHERE id = $1`, f.budgetCatID,
	).Scan(&deletedReason, &deletedBy),
		"the budget category was hard-deleted by ON DELETE CASCADE. Task 8 (migration 000158) "+
			"soft-deleted this table precisely so rollover_history survives; a cascade from here "+
			"would undo that.")
	require.NotNil(t, deletedReason)
	assert.Equal(t, domain.SoftDeleteReasonParent, *deletedReason)
	require.NotNil(t, deletedBy)
	assert.Equal(t, domain.SoftDeletedByUser, *deletedBy)

	assert.Equal(t, 1, f.count(t, ctx, `SELECT COUNT(*) FROM rollover_history WHERE id = $1`, f.rolloverID),
		"rollover_history naming this budget category as its source was destroyed")

	// It disappears from the budget the way the cascade used to make it disappear...
	cats, err := f.budgetRepo.GetCategoriesByBudgetID(ctx, f.budgetID)
	require.NoError(t, err)
	require.Len(t, cats, 1, "the tombstoned budget line must not be listed")
	assert.Equal(t, f.otherBudgetC, cats[0].ID, "the unrelated budget line must be untouched")
}

// ---------------------------------------------------------------------------------------------
// 8. The name is freed by the tombstone; a genuine duplicate still maps to ErrCategoryAlreadyExists.
// ---------------------------------------------------------------------------------------------

func TestCategoryTombstone_NameIsFreedByTombstone_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	err := f.catRepo.Create(ctx, &domain.CustomCategory{
		UserID: f.userID,
		Name:   "Coffee",
	})
	require.NoError(t, err,
		"unique_user_category_name must be scoped to live rows, or a deleted name is taken forever")

	// ...and the duplicate mapping still works off the partial index's name.
	err = f.catRepo.Create(ctx, &domain.CustomCategory{UserID: f.userID, Name: "Coffee"})
	assert.ErrorIs(t, err, domain.ErrCategoryAlreadyExists,
		"the partial UNIQUE INDEX must keep the constraint NAME mapCategoryWriteError matches on")
}

// ---------------------------------------------------------------------------------------------
// 9. The delete is audited, in the same transaction as the state change.
// ---------------------------------------------------------------------------------------------

func TestCategoryTombstone_DeleteIsAudited_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	var action, resourceType string
	var actor *string
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT action, resource_type, user_id FROM audit_logs WHERE resource_id = $1`, f.coffeeID,
	).Scan(&action, &resourceType, &actor))
	assert.Equal(t, "custom_category.deleted", action)
	assert.Equal(t, "custom_category", resourceType)
	assert.Nil(t, actor,
		"AuditLogRepository.Log carries no actor and writes NULL by design (#1506/#1507 deadlock class)")
}

// ---------------------------------------------------------------------------------------------
// 10. Deleting an already-tombstoned category is a no-op that reports not-found, and a second
//     delete must not write a second audit row or re-disable anything.
// ---------------------------------------------------------------------------------------------

func TestCategoryTombstone_SecondDeleteIsNotFound_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	err := f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo)
	assert.ErrorIs(t, err, domain.ErrCategoryNotFound)

	assert.Equal(t, 1, f.count(t, ctx, `SELECT COUNT(*) FROM audit_logs WHERE resource_id = $1`, f.coffeeID),
		"the second, refused delete still wrote an audit entry")
}

// ---------------------------------------------------------------------------------------------
// 11. FIX ROUND 1 (review finding M1): every `tombstone-exempt` read, exercised against a
//     tombstoned category.
//
// The guard proves a reason was WRITTEN next to each exempt reference. It cannot prove the reason
// is true. Sixteen references to custom_categories are audited; ten of them were exempt with no
// behavioural test involving a tombstoned category at all, which meant the exemption mechanism was
// documentation rather than a pin. This is one fixture and ten assertions that convert it.
//
// Each sub-test names the file:line and the enclosing function it covers. If a future edit
// "tidies up" one of those exemptions by adding `AND deleted_at IS NULL`, the corresponding
// assertion fails with the reason the exemption exists.
// ---------------------------------------------------------------------------------------------

func TestCategoryTombstone_ExemptReadsStillSeeTheHistory_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	txnRepo := NewTransactionRepository(f.tx, newTestFieldEncryptor(t))
	analyticsRepo := NewAnalyticsRepository(f.tx)

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	custom := "custom"
	start := time.Now().AddDate(0, 0, -60)
	end := time.Now().AddDate(0, 0, 1)

	effectiveName := func(txnID string) *string {
		t.Helper()
		var n *string
		require.NoError(t, f.tx.QueryRow(ctx,
			`SELECT effective_category_name FROM transactions WHERE id = $1`, txnID).Scan(&n))
		return n
	}

	// transaction_repository.go:601 — Update()'s effective_category_name recompute.
	t.Run("Update recomputes the name from a tombstoned category", func(t *testing.T) {
		txn, err := txnRepo.GetByID(ctx, f.liveTxnID)
		require.NoError(t, err)
		txn.Name = "Blue Bottle (renamed)"
		require.NoError(t, txnRepo.Update(ctx, txn))
		got := effectiveName(f.liveTxnID)
		require.NotNil(t, got, "the next Plaid refresh blanked the category name on a historical row")
		assert.Equal(t, "Coffee", *got)
	})

	// transaction_repository.go:1166 — buildWhereClause's CustomCategoryIDs expansion.
	t.Run("the transaction list filter still matches history", func(t *testing.T) {
		got, err := txnRepo.GetByUserIDWithFilters(ctx, f.userID,
			domain.TransactionFilters{CustomCategoryIDs: []string{f.coffeeID}}, 50, 0)
		require.NoError(t, err)
		require.Len(t, got, 1, "the user's own past transaction vanished from their own filter")
		assert.Equal(t, f.liveTxnID, got[0].ID)
	})

	// transaction_repository.go:2841 — the unified (banking+investment) keyset filter.
	t.Run("the unified keyset filter still matches history", func(t *testing.T) {
		items, _, err := txnRepo.GetByUserIDKeysetUnified(ctx, f.userID,
			domain.TransactionFilters{CustomCategoryIDs: []string{f.coffeeID}}, nil, 50)
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, f.liveTxnID, items[0].ID)
	})

	// transaction_repository.go:1545 — UpdateCategory's name resolution.
	t.Run("UpdateCategory resolves the name rather than writing NULL", func(t *testing.T) {
		require.NoError(t, txnRepo.UpdateCategory(ctx, f.liveTxnID, f.userID,
			&custom, nil, nil, &f.coffeeID, domain.CategorySourceManual))
		got := effectiveName(f.liveTxnID)
		require.NotNil(t, got,
			"filtering here would write a NULL name instead of refusing the write — the worse failure")
		assert.Equal(t, "Coffee", *got)
	})

	// transaction_repository.go:1614 — BulkUpdateCategory's name resolution.
	t.Run("BulkUpdateCategory resolves the name rather than writing NULL", func(t *testing.T) {
		n, err := txnRepo.BulkUpdateCategory(ctx, f.userID, []string{f.liveTxnID},
			&custom, nil, nil, &f.coffeeID, domain.CategorySourceManual)
		require.NoError(t, err)
		require.EqualValues(t, 1, n)
		got := effectiveName(f.liveTxnID)
		require.NotNil(t, got)
		assert.Equal(t, "Coffee", *got)
	})

	// transaction_repository.go:2192 — the rename-propagation job.
	t.Run("the rename job still writes a name for a tombstoned category", func(t *testing.T) {
		n, err := txnRepo.UpdateEffectiveCategoryNameByCustomCategoryID(ctx, f.userID, f.coffeeID)
		require.NoError(t, err)
		// 2, not 1: this statement carries no transactions-side tombstone filter either, so the
		// provider-reversed row is rewritten alongside the live one. That is Task 7's ruling, not
		// a slip here — a reversed transaction still renders, so its name must stay current.
		require.EqualValues(t, 2, n)
		got := effectiveName(f.liveTxnID)
		require.NotNil(t, got, "a rename job landing after the delete blanked the historical name")
		assert.Equal(t, "Coffee", *got)
	})

	// transaction_repository.go:1865 — buildCategoryClause's PFC-primary rollup branch.
	t.Run("a PFC budget line still counts spend filed under a tombstoned child", func(t *testing.T) {
		pfc := "FOOD_AND_DRINK"
		sum, err := txnRepo.GetSpentSumForBudgetCategory(ctx, f.userID, &domain.BudgetCategory{
			CategoryKind: "pfc", PFCPrimary: &pfc, MatchLogic: "category_only",
		}, start, end)
		require.NoError(t, err)
		assert.InDelta(t, 4.50, sum, 0.001,
			"deleting a child category silently shrank its parent's budgeted spend")
	})

	// transaction_repository.go:1873 — buildCategoryClause's custom branch.
	t.Run("a custom budget line still counts its own history", func(t *testing.T) {
		sum, err := txnRepo.GetSpentSumForBudgetCategory(ctx, f.userID, &domain.BudgetCategory{
			CategoryKind: "custom", CustomCategoryID: &f.coffeeID, MatchLogic: "category_only",
		}, start, end)
		require.NoError(t, err)
		assert.InDelta(t, 4.50, sum, 0.001)
	})

	// analytics_repository.go:928 — buildSpendingFilter, RollUpChildren = true.
	t.Run("analytics with rollup still reports the tombstoned category's spend", func(t *testing.T) {
		rows, err := analyticsRepo.GetSpendingByCategory(ctx, f.userID, domain.SpendingFilters{
			StartDate:         start,
			EndDate:           end,
			CustomCategoryIDs: []string{f.coffeeID},
			RollUpChildren:    true,
		})
		require.NoError(t, err)
		require.NotEmpty(t, rows,
			"a closed month quietly restated itself because a category was deleted")
	})

	// analytics_repository.go:943 — buildSpendingFilter, RollUpChildren = false.
	t.Run("analytics without rollup still reports it", func(t *testing.T) {
		rows, err := analyticsRepo.GetSpendingByCategory(ctx, f.userID, domain.SpendingFilters{
			StartDate:         start,
			EndDate:           end,
			CustomCategoryIDs: []string{f.coffeeID},
			RollUpChildren:    false,
		})
		require.NoError(t, err)
		require.NotEmpty(t, rows)
	})
}

// ---------------------------------------------------------------------------------------------
// 12. FIX ROUND 1 (review finding I1): a disabled rule cannot be ARMED again onto a tombstoned
//     target, and the engine's own read refuses one even if it somehow is.
//
// Step 2's whole justification is that a rule whose target the user can no longer see cannot keep
// acting. Before this round that held for exactly as long as nobody sent
// `PATCH /api/v1/rules/{id} {"enabled": true}`: the liveness gate lived inside
// `if req.TargetCustomCategoryID != nil`, so a body with no target field skipped it entirely, and
// ToggleRule never had a gate at all. This is the repository half — the service half is
// TestCategorizationRuleService_{UpdateRule,ToggleRule}_RefusesToArmOntoATombstonedCategory.
// ---------------------------------------------------------------------------------------------

func TestCategoryTombstone_EngineRefusesARuleReArmedOntoATombstone_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	// Re-arm BOTH rules the way a direct-SQL repair, an unguarded future writer, or the API bug
	// this test was written for would: flip `enabled` and touch nothing else.
	ct, err := f.tx.Exec(ctx,
		`UPDATE categorization_rules SET enabled = true WHERE user_id = $1 AND target_custom_category_id = $2`,
		f.userID, f.coffeeID)
	require.NoError(t, err)
	require.EqualValues(t, 2, ct.RowsAffected(),
		"both rules must be back to enabled = true in the DB, or this test proves nothing")

	enabled, err := f.ruleRepo.GetEnabledByUserID(ctx, f.userID)
	require.NoError(t, err)
	assert.Empty(t, enabled,
		"the rule engine reads GetEnabledByUserID; a rule naming a tombstoned category must not "+
			"be in that set even when its `enabled` column says true")

	// A rule with no custom target is unaffected by the new predicate.
	pfcRuleID := uuid.New().String()
	_, err = f.tx.Exec(ctx, `
		INSERT INTO categorization_rules
			(id, user_id, name, enabled, priority, conditions, target_kind, target_pfc_primary)
		VALUES ($1, $2, 'Plain PFC rule', true, 1,
			'[{"field":"name","operator":"contains","value":"x"}]'::jsonb, 'pfc', 'TRAVEL')
	`, pfcRuleID, f.userID)
	require.NoError(t, err)

	enabled, err = f.ruleRepo.GetEnabledByUserID(ctx, f.userID)
	require.NoError(t, err)
	require.Len(t, enabled, 1,
		"the tombstone predicate must not hide rules that have no custom-category target at all")
	assert.Equal(t, pfcRuleID, enabled[0].ID)
}

//  13. FIX ROUND 1 (review finding M3): the owning-budget scoping subquery must NOT filter
//     tombstoned budgets.
//
// The subquery answers "whose budget is this line on", and a tombstoned budget is still the same
// user's. `AND b.deleted_at IS NULL` would satisfy the read guard and pass every other test in
// this file while silently skipping a live budget line under a deleted budget — leaving it
// pointing at a category that no longer exists. Task 8 tombstones a budget's categories with it,
// so that state is not reachable through the API; it IS reachable through a direct-SQL repair or
// a partial failure, which is the case the exemption reason names. Pinned here so the "tidy-up"
// fails loudly.
func TestCategoryTombstone_BudgetLineUnderATombstonedBudgetIsStillReached_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	// Tombstone the BUDGET but deliberately leave its category line live.
	_, err := f.tx.Exec(ctx, `
		UPDATE budgets SET deleted_at = NOW(), deleted_reason = 'user_deleted', deleted_by = 'user'
		WHERE id = $1
	`, f.budgetID)
	require.NoError(t, err)
	require.Equal(t, 1, f.count(t, ctx,
		`SELECT COUNT(*) FROM budget_categories WHERE id = $1 AND deleted_at IS NULL`, f.budgetCatID),
		"the budget line must still be live going in, or this test proves nothing")

	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	assert.Equal(t, 1, f.count(t, ctx,
		`SELECT COUNT(*) FROM budget_categories WHERE id = $1 AND deleted_at IS NOT NULL`, f.budgetCatID),
		"a live budget line under a TOMBSTONED budget was skipped — the ownership subquery must "+
			"scope by owner, not by the budget's own liveness")
}

// ---------------------------------------------------------------------------------------------
// H1 (#1625 second fix wave): the disable in step 2 is a WRITE THE USER CAN SEE, so it must move
// the optimistic-concurrency token.
//
// B2 keyed Update's guard on `version` and justified it as "version moves only on user edits, so
// competing VISIBLE changes still conflict and invisible bookkeeping does not". This statement
// was false for exactly one writer: DeleteWithReassignment's `UPDATE categorization_rules SET
// enabled = false` is user-triggered (they deleted the category), it is visible (the card flips
// to "Disabled" and the engine stops firing the rule), and it left `version` alone.
//
// The lost update that produced: a user opens the rule editor, deletes the category in another
// tab, then saves a NAME change. UpdateRule's live-category recheck does not fire — the request
// supplies neither `enabled` nor `target_custom_category_id` (categorization_rule_service.go's
// two gates) — so the stale row is written back WHOLE, restoring `enabled = true` on a rule
// pointing at a tombstoned category. That is precisely the "keeps firing at something the user
// cannot see" failure the disable exists to prevent, reintroduced by the guard that was supposed
// to stop lost updates.
//
// Asserted on the REFUSAL, not merely on the final `enabled`: an assertion that only read the
// column back would also pass against a repository that silently re-ran the disable.
func TestCategoryTombstone_DisablingRulesMovesTheConcurrencyToken_Integration(t *testing.T) {
	f := seedCategoryTombstoneFixture(t)
	ctx := context.Background()

	// The user opens the rule editor. This read carries the version the eventual save will claim.
	editing, err := f.ruleRepo.GetByID(ctx, f.categoryOnlyRuleID)
	require.NoError(t, err)
	require.True(t, editing.Enabled, "the fixture rule must be enabled going in, or this test proves nothing")
	versionAtRead := editing.Version
	require.NotZero(t, versionAtRead, "a stored rule must carry a version; 0 would make the guard vacuous")

	// In another tab, they delete the category. Step 2 disables this rule.
	require.NoError(t, f.catRepo.DeleteWithReassignment(ctx, f.userID, f.coffeeID, f.auditRepo))

	// Falsifiability: the disable really did land, and it really is invisible to a guard keyed on
	// anything that did not move.
	var enabledAfterDisable bool
	var versionAfterDisable int64
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT enabled, version FROM categorization_rules WHERE id = $1`, f.categoryOnlyRuleID,
	).Scan(&enabledAfterDisable, &versionAfterDisable))
	require.False(t, enabledAfterDisable, "step 2 did not disable the rule; the rest of this test is meaningless")
	assert.Greater(t, versionAfterDisable, versionAtRead,
		"a user-visible disable must move `version`; leaving it means a stale editor write is "+
			"accepted and silently re-enables a rule pointing at a tombstoned category")

	// The save the user had in flight: a NAME change only. Neither of UpdateRule's live-category
	// gates fires for it, so the repository guard is the only thing standing between this write
	// and a resurrected rule.
	editing.Name = "Renamed while the category was being deleted"
	err = f.ruleRepo.Update(ctx, editing)
	require.Error(t, err, "the stale whole-row write must be refused")
	assert.True(t, errors.Is(err, domain.ErrRuleConcurrentlyModified),
		"the stale write must report the concurrency conflict, got: %v", err)

	var enabledFinal bool
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT enabled FROM categorization_rules WHERE id = $1`, f.categoryOnlyRuleID,
	).Scan(&enabledFinal))
	assert.False(t, enabledFinal,
		"the rule was re-enabled by a rename — it still targets a tombstoned category, which is "+
			"the exact state the disable exists to prevent")
}
