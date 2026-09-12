//go:build integration

package jobs

// Issue #1630 Task B1 — RED integration test against real Postgres.
//
// custom_category_service.go's UpdateCategory can change ParentID/ParentPFCPrimary with ZERO
// query against budget_categories. This test drives the FULL pipeline this task built:
//
//  1. Seeds a budget with a parent custom category and a child custom category, both budgeted as
//     TOP-LEVEL rows (the child has no parent_id yet), with deliberately STALE spent_amount
//     values seeded directly — standing in for "whatever a prior sync happened to leave".
//  2. Reparents the child under the parent via services.CustomCategoryService.UpdateCategory
//     (real Postgres, real repositories) — a LEGITIMATE reparent: both sides are
//     'category_only', which cleanly contains, so the escaping-pair guard must let it through.
//  3. Asserts the stale spent_amount values are UNCHANGED immediately after the reparent — proving
//     the write itself still does not touch budget_categories (by design; the resync is a
//     separate, asynchronously-enqueued step).
//  4. Runs BudgetHierarchyResyncWorker.Work directly (simulating the job UpdateCategory enqueued)
//     and asserts the budget's totals are now CORRECT: the parent's spend rolls up the
//     transaction filed under the (now-nested) child, the child's own spend is its own
//     transaction only, and Budget.TotalSpent() (top-level rows only) reports the real combined
//     total exactly once — not the stale placeholder, and not a double count.
//
// A companion test below drives the OTHER half: reparenting into an ESCAPING shape (a
// non-containing parent) must be REFUSED before anything is persisted.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/queue/jobs/... -tags integration -p 1 \
//	  -run TestBudgetHierarchyResync_1630_B1 -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
)

// alwaysSucceedsJobQueue is a minimal queue.JobQueue stub for this test: UpdateCategory's
// reparent path now fails CLOSED (Issue #1630 review fix) when no job queue is wired, but this
// test's own point is to exercise the LEGITIMATE reparent + a directly-run resync worker, not the
// enqueue mechanics themselves — Enqueue always succeeds; the other three methods are unused by
// that path and panic if ever called, so a future caller that needs them fails loudly rather than
// silently no-op'ing.
type alwaysSucceedsJobQueue struct{}

func (alwaysSucceedsJobQueue) Enqueue(ctx context.Context, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	return &queue.JobResult{ID: 1, Kind: job.Kind()}, nil
}

func (alwaysSucceedsJobQueue) EnqueueTx(ctx context.Context, tx pgx.Tx, job queue.Job, opts ...queue.EnqueueOption) (*queue.JobResult, error) {
	panic("alwaysSucceedsJobQueue.EnqueueTx unused by this test")
}

func (alwaysSucceedsJobQueue) EnqueueMany(ctx context.Context, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	panic("alwaysSucceedsJobQueue.EnqueueMany unused by this test")
}

func (alwaysSucceedsJobQueue) EnqueueManyTx(ctx context.Context, tx pgx.Tx, jobs []queue.Job, opts ...queue.EnqueueOption) ([]*queue.JobResult, error) {
	panic("alwaysSucceedsJobQueue.EnqueueManyTx unused by this test")
}

func TestBudgetHierarchyResync_1630_B1_CorrectsStaleTotalsAfterLegitimateReparent(t *testing.T) {
	pool := connectJobsTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-b1-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-claim1630-b1-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-claim1630-b1', 'Checking', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	// Parent and child custom categories — BOTH top-level to start (child.parent_id is NULL).
	parentCatID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name) VALUES ($1, $2, 'Dining')
	`, parentCatID, userID)
	require.NoError(t, err, "seed parent custom category")

	childCatID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name) VALUES ($1, $2, 'Fast Food')
	`, childCatID, userID)
	require.NoError(t, err, "seed child custom category")

	// A monthly budget anchored TODAY, so NarrowBudgetPeriodDates's own-stored-period window
	// covers the transactions seeded below.
	budgetID := uuid.New().String()
	today := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active, series_id)
		VALUES ($1, $2, 'Monthly Budget', 'monthly', $3, 1000.00, true, $1)
	`, budgetID, userID, today.AddDate(0, -1, 0).Format("2006-01-02"))
	require.NoError(t, err, "seed budget")

	// Both budget lines seeded with obviously-wrong spent_amount, standing in for "whatever a
	// prior sync happened to leave" before this resync runs.
	parentBCID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, custom_category_id, allocated_amount, spent_amount, match_logic)
		VALUES ($1, $2, 'custom', $3, 500.00, 0, 'category_only')
	`, parentBCID, budgetID, parentCatID)
	require.NoError(t, err, "seed parent budget category")

	childBCID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, custom_category_id, allocated_amount, spent_amount, match_logic)
		VALUES ($1, $2, 'custom', $3, 100.00, 999999, 'category_only')
	`, childBCID, budgetID, childCatID)
	require.NoError(t, err, "seed child budget category with a deliberately wrong spent_amount")

	// Two real transactions this period: $30 filed directly under the parent, $50 under the
	// child. amount > 0 is an outflow/expense under this schema.
	txnDate := today
	insertTxn := func(customCatID string, amount float64) {
		_, err := tx.Exec(ctx, `
			INSERT INTO transactions
				(id, user_id, account_id, linked_item_id, source, amount, date, name, pending,
				 category_override_kind, custom_category_override_id)
			VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 'txn', false, 'custom', $7)
		`, uuid.New().String(), userID, accountID, linkedItemID, amount, txnDate, customCatID)
		require.NoError(t, err, "seed transaction")
	}
	insertTxn(parentCatID, 30.00)
	insertTxn(childCatID, 50.00)

	enc := newJobsTestFieldEncryptor(t)
	txnRepo := repository.NewTransactionRepository(tx, enc)
	customCategoryRepo := repository.NewCustomCategoryRepository(tx)
	budgetRepo := repository.NewBudgetRepository(tx)
	spentCalc := services.NewBudgetCategoryMatchingService(txnRepo)

	customCategoryService := services.NewCustomCategoryService(customCategoryRepo)
	customCategoryService.SetBudgetCategoryRepository(budgetRepo)
	// Issue #1630 review fix: the resync enqueue is now fail-closed, so a legitimate reparent
	// needs a job queue wired even though this test runs the worker directly afterward rather
	// than relying on the enqueued job.
	customCategoryService.SetJobQueue(alwaysSucceedsJobQueue{})

	// Step 1: the LEGITIMATE reparent — both sides 'category_only', cleanly contained.
	updated, err := customCategoryService.UpdateCategory(ctx, userID, childCatID, &domain.UpdateCustomCategoryRequest{
		ParentID: &parentCatID,
	})
	require.NoError(t, err, "a category_only/category_only reparent must not be refused")
	require.NotNil(t, updated)
	require.NotNil(t, updated.ParentID)
	assert.Equal(t, parentCatID, *updated.ParentID)

	// Step 2: prove the write alone does NOT touch budget_categories — the stale values must
	// still be exactly what was seeded.
	preResyncCats, err := budgetRepo.GetCategoriesByBudgetID(ctx, budgetID)
	require.NoError(t, err)
	require.Len(t, preResyncCats, 2)
	for _, c := range preResyncCats {
		switch c.ID {
		case parentBCID:
			assert.Equal(t, 0.0, c.SpentAmount, "reparent alone must not touch the parent's stale spent_amount")
		case childBCID:
			assert.Equal(t, 999999.0, c.SpentAmount, "reparent alone must not touch the child's stale spent_amount")
		}
	}

	// Step 3: run the resync job directly (simulating what UpdateCategory enqueued).
	worker := NewBudgetHierarchyResyncWorker(BudgetHierarchyResyncWorkerDeps{
		CustomCategoryRepo: customCategoryRepo,
		BudgetRepo:         budgetRepo,
		SpentCalc:          spentCalc,
	})
	job := &river.Job[BudgetHierarchyResyncArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Kind: "budget_hierarchy_resync", Queue: "default", Attempt: 1},
		Args: BudgetHierarchyResyncArgs{
			UserID:           userID,
			CustomCategoryID: childCatID,
			// OldParentCustomCategoryID/OldParentPFCPrimary both nil — the child was top-level
			// before this reparent.
		},
	}
	require.NoError(t, worker.Work(ctx, job))

	// Step 4: the budget's totals are now CORRECT.
	postResyncCats, err := budgetRepo.GetCategoriesByBudgetID(ctx, budgetID)
	require.NoError(t, err)
	require.Len(t, postResyncCats, 2)

	var parentSpent, childSpent float64
	var foundParent, foundChild bool
	for _, c := range postResyncCats {
		switch c.ID {
		case parentBCID:
			parentSpent = c.SpentAmount
			foundParent = true
		case childBCID:
			childSpent = c.SpentAmount
			foundChild = true
		}
	}
	require.True(t, foundParent && foundChild)

	assert.Equal(t, 80.00, parentSpent,
		"the parent's hierarchical roll-up must now include the child's $50 transaction on top "+
			"of its own $30, since the child is now nested under it (parent_id set by the reparent)")
	assert.Equal(t, 50.00, childSpent,
		"the child's own spend is unaffected — its own $50 transaction only")

	budget := &domain.Budget{Categories: postResyncCats}
	assert.Equal(t, 80.00, budget.TotalSpent(),
		"TotalSpent sums TOP-LEVEL rows only — the child is now a clean sub-allocation of the "+
			"parent (both category_only) and must be excluded, so the real $80 combined spend is "+
			"reported exactly once, not $999999+ (the pre-resync stale value) and not $130 "+
			"(80 parent-rollup + 50 child, which would double-count the child's $50)")
}

func TestBudgetHierarchyResync_1630_B1_RefusesEscapingReparent(t *testing.T) {
	pool := connectJobsTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-b1-escape-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	parentCatID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name) VALUES ($1, $2, 'Dining')
	`, parentCatID, userID)
	require.NoError(t, err, "seed parent custom category")

	childCatID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name) VALUES ($1, $2, 'Fast Food')
	`, childCatID, userID)
	require.NoError(t, err, "seed child custom category")

	budgetID := uuid.New().String()
	today := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active, series_id)
		VALUES ($1, $2, 'Monthly Budget', 'monthly', $3, 1000.00, true, $1)
	`, budgetID, userID, today.Format("2006-01-02"))
	require.NoError(t, err, "seed budget")

	// Parent's OWN budget line has match_logic 'and' — NOT a containing shape
	// (isContainingMatchLogic requires category_only/or). Needs a non-empty tags array to pass
	// domain.BudgetCategory.Validate for 'and' — irrelevant here since this test calls the
	// service directly, but seeded for realism.
	parentBCID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, custom_category_id, allocated_amount, spent_amount, match_logic, tags)
		VALUES ($1, $2, 'custom', $3, 500.00, 0, 'and', '["big-ticket"]'::jsonb)
	`, parentBCID, budgetID, parentCatID)
	require.NoError(t, err, "seed parent budget category with a non-containing match_logic")

	childBCID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, custom_category_id, allocated_amount, spent_amount, match_logic)
		VALUES ($1, $2, 'custom', $3, 100.00, 0, 'category_only')
	`, childBCID, budgetID, childCatID)
	require.NoError(t, err, "seed child budget category")

	customCategoryRepo := repository.NewCustomCategoryRepository(tx)
	budgetRepo := repository.NewBudgetRepository(tx)

	customCategoryService := services.NewCustomCategoryService(customCategoryRepo)
	customCategoryService.SetBudgetCategoryRepository(budgetRepo)

	_, err = customCategoryService.UpdateCategory(ctx, userID, childCatID, &domain.UpdateCustomCategoryRequest{
		ParentID: &parentCatID,
	})
	require.ErrorIs(t, err, domain.ErrEscapedNestedChild,
		"nesting a category_only child under an 'and' parent's budgeted row escapes clean "+
			"containment and must be refused BEFORE anything is persisted")

	// Confirm nothing was persisted: the child is still top-level in custom_categories.
	reloaded, err := customCategoryRepo.GetByID(ctx, childCatID)
	require.NoError(t, err)
	assert.Nil(t, reloaded.ParentID, "the refused reparent must not have been written")
}

// TestBudgetHierarchyResync_1630_ReviewFix_ResyncsGrandparentPFCRootReachedThroughACustomParent
// is the RED test for the #1630 adversarial-review HIGH finding: the resync worker added the old
// and new CUSTOM parent IDs to affectedCustomIDs but never resolved THOSE parents' own
// ParentPFCPrimary, so a PFC-root budget row reached only through a custom grandparent (pfc root
// X <- custom C via parent_pfc_primary <- custom G via parent_id) was silently skipped.
//
// Shape: a budget contains only PFC root X. Custom C is nested under X (parent_pfc_primary = X).
// Custom G is nested under C (parent_id = C). A transaction is filed under G. Reparenting G to
// TOP-LEVEL changes what X's roll-up includes (buildCategoryClause's grandchild UNION branch, and
// its removal here) even though neither C nor G is itself a budgeted row — X is the only budget
// row, and it must still be resynced.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/queue/jobs/... -tags integration -p 1 \
//	  -run TestBudgetHierarchyResync_1630_ReviewFix_ResyncsGrandparentPFCRootReachedThroughACustomParent -v
func TestBudgetHierarchyResync_1630_ReviewFix_ResyncsGrandparentPFCRootReachedThroughACustomParent(t *testing.T) {
	pool := connectJobsTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-grandparent-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-claim1630-grandparent-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-claim1630-grandparent', 'Checking', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	const pfcPrimary = "FOOD_AND_DRINK"

	// Custom C nested under PFC root X.
	customParentID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary) VALUES ($1, $2, 'Dining', $3)
	`, customParentID, userID, pfcPrimary)
	require.NoError(t, err, "seed custom category C nested under PFC root X")

	// Custom G nested under custom C — the grandchild whose reparent this test drives.
	grandchildID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, parent_id) VALUES ($1, $2, 'Fast Food', $3)
	`, grandchildID, userID, customParentID)
	require.NoError(t, err, "seed custom category G nested under C")

	// A monthly budget anchored TODAY, containing ONLY a PFC-root row for X — neither C nor G
	// is itself a budgeted row.
	budgetID := uuid.New().String()
	today := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO budgets (id, user_id, name, period, start_date, amount, is_active, series_id)
		VALUES ($1, $2, 'Monthly Budget', 'monthly', $3, 1000.00, true, $1)
	`, budgetID, userID, today.Format("2006-01-02"))
	require.NoError(t, err, "seed budget")

	pfcBCID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_categories (id, budget_id, category_kind, pfc_primary, allocated_amount, spent_amount, match_logic)
		VALUES ($1, $2, 'pfc', $3, 500.00, 12345.00, 'category_only')
	`, pfcBCID, budgetID, pfcPrimary)
	require.NoError(t, err, "seed PFC-root budget category with a deliberately wrong stale spent_amount")

	// One real transaction this period, filed under grandchild G — reached today via X's
	// grandchild UNION branch (buildCategoryClause).
	txnDate := today
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name, pending,
			 category_override_kind, custom_category_override_id)
		VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 'txn', false, 'custom', $7)
	`, uuid.New().String(), userID, accountID, linkedItemID, 50.00, txnDate, grandchildID)
	require.NoError(t, err, "seed transaction filed under G")

	enc := newJobsTestFieldEncryptor(t)
	txnRepo := repository.NewTransactionRepository(tx, enc)
	customCategoryRepo := repository.NewCustomCategoryRepository(tx)
	budgetRepo := repository.NewBudgetRepository(tx)
	spentCalc := services.NewBudgetCategoryMatchingService(txnRepo)

	// Reparent G to TOP-LEVEL (clear parent_id) directly, mirroring what
	// custom_category_service.UpdateCategory's write does — this test's own point is the
	// worker's affected-set resolution, not the service's escaping-pair guard.
	_, err = tx.Exec(ctx, `UPDATE custom_categories SET parent_id = NULL WHERE id = $1`, grandchildID)
	require.NoError(t, err, "reparent G to top-level")

	// Run the resync job directly, exactly as custom_category_service.UpdateCategory would have
	// enqueued it: CustomCategoryID is the reparented category (G), and
	// OldParentCustomCategoryID carries C's pre-reparent identity (captured before the write).
	worker := NewBudgetHierarchyResyncWorker(BudgetHierarchyResyncWorkerDeps{
		CustomCategoryRepo: customCategoryRepo,
		BudgetRepo:         budgetRepo,
		SpentCalc:          spentCalc,
	})
	job := &river.Job[BudgetHierarchyResyncArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Kind: "budget_hierarchy_resync", Queue: "default", Attempt: 1},
		Args: BudgetHierarchyResyncArgs{
			UserID:                    userID,
			CustomCategoryID:          grandchildID,
			OldParentCustomCategoryID: &customParentID,
		},
	}
	require.NoError(t, worker.Work(ctx, job))

	postResyncCats, err := budgetRepo.GetCategoriesByBudgetID(ctx, budgetID)
	require.NoError(t, err)
	require.Len(t, postResyncCats, 1)
	assert.Equal(t, 0.00, postResyncCats[0].SpentAmount,
		"X's PFC-root row must be resynced even though neither C nor G is itself a budgeted "+
			"row: G moved out from under X's grandchild UNION branch, so X's roll-up must drop "+
			"back to $0 rather than staying at the stale $12345 seeded above — a leftover "+
			"$12345 (or the pre-reparent $50) proves X was never flagged as affected")
}
