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
		INSERT INTO rule_execution_history (id, rule_id, transaction_id, action_type, result)
		VALUES ($1, $2, $3, 'category', 'applied')
	`, f.ruleExecID, f.categoryOnlyRuleID, f.liveTxnID)

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
