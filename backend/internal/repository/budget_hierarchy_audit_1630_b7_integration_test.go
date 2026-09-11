//go:build integration

package repository

// Issue #1630 Task B7: the audit that goes looking for budgets already holding a shape the
// write-time guards (domain.DuplicateParentIdentity, domain.EscapedNestedChild) would refuse if
// created today. See budget_hierarchy_audit.go's header comment for the full design.
//
// This test seeds an ESCAPING PAIR directly via BudgetRepository.CreateCategory — which, exactly
// like budget_spend_double_count_1630_integration_test.go's direct-SQL seed, bypasses the guard
// entirely (the guard lives in AddCategoryChecked/UpdateCategoryChecked/CreateWithCategories, not
// in CreateCategory) — alongside a THIRD, unrelated category whose stored spent_amount is
// deliberately wrong (stale), to prove the two verdicts are independent:
//
//   - the escaping pair is REPORTED (outcome reported_escaping_pair) and its match_logic is left
//     byte-for-byte untouched;
//   - the stale row's spent_amount is REPAIRED to the narrow recomputation;
//   - a second run of the SAME budget is a no-op: no second ledger row, and the corrected
//     spent_amount is not rewritten again.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/repository/... -tags integration -p 1 \
//	  -run TestAuditBudgetHierarchy_1630B7 -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// directSpentCalculator satisfies BudgetHierarchySpentCalculator by forwarding straight to
// TransactionRepository.GetSpentSumForBudgetCategory — functionally identical to
// services.NewBudgetCategoryMatchingService's forwarding CalculateCategorySpent, defined locally so
// this in-package test (package repository) never has to import services, which itself imports
// repository (services already imports repository for its narrow interfaces, e.g.
// services.BudgetSyncRepository; the reverse import from an in-package repository test would
// cycle — see budget_hierarchy_audit_1630_b7_integration_test.go's history for why
// package repository_test files elsewhere in this directory redeclare their own test helpers
// rather than sharing this package's unexported ones).
type directSpentCalculator struct {
	txRepo TransactionRepository
}

func (d directSpentCalculator) CalculateCategorySpent(ctx context.Context, userID string, category *domain.BudgetCategory, startDate, endDate time.Time) (float64, error) {
	return d.txRepo.GetSpentSumForBudgetCategory(ctx, userID, category, startDate, endDate)
}

func TestAuditBudgetHierarchy_1630B7(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-b7-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	linkedItemID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-token')
	`, linkedItemID, userID, "item-claim1630-b7-"+linkedItemID)
	require.NoError(t, err, "seed linked item")

	accountID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type)
		VALUES ($1, $2, $3, 'plaid-claim1630-b7', 'Checking', 'depository')
	`, accountID, linkedItemID, userID)
	require.NoError(t, err, "seed account")

	// The same nested-custom-under-PFC-primary shape as the HIGH-severity double-count test.
	customCatID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Splurge Rent Stuff', 'RENT_AND_UTILITIES')
	`, customCatID, userID)
	require.NoError(t, err, "seed custom category nested under RENT_AND_UTILITIES")

	periodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.January, 31, 23, 59, 59, 0, time.UTC)
	txDate := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)

	const seededSpend = 275.50
	_, err = tx.Exec(ctx, `
		INSERT INTO transactions
			(id, user_id, account_id, linked_item_id, source, amount, date, name, pending,
			 category_override_kind, custom_category_override_id)
		VALUES ($1, $2, $3, $4, 'plaid', $5, $6, 'Fancy Rent Add-on', false,
			'custom', $7)
	`, uuid.New().String(), userID, accountID, linkedItemID, seededSpend, txDate, customCatID)
	require.NoError(t, err, "seed the one transaction the escaping pair double-counts")

	enc := newTestFieldEncryptor(t)
	txRepo := NewTransactionRepository(tx, enc)
	spentCalc := directSpentCalculator{txRepo: txRepo}
	budgetRepo := NewBudgetRepository(tx)

	budget := &domain.Budget{
		UserID:    userID,
		Name:      "Claim 1630 B7 Budget",
		Period:    domain.BudgetPeriodMonthly,
		StartDate: periodStart,
		Amount:    1000,
	}
	require.NoError(t, budgetRepo.Create(ctx, budget), "seed budget")

	parentPrimary := "RENT_AND_UTILITIES"
	// The PARENT's own budget line: budgeted, category_only (containing).
	parentCategory := &domain.BudgetCategory{
		BudgetID:        budget.ID,
		CategoryKind:    "pfc",
		PFCPrimary:      &parentPrimary,
		MatchLogic:      domain.MatchLogicCategoryOnly,
		AllocatedAmount: 500,
		// Seeded ALREADY CORRECT (equal to what CalculateCategorySpent will recompute), so this
		// test's "repaired" evidence comes ONLY from the unrelated stale row below — isolating the
		// two verdicts from each other.
		SpentAmount: seededSpend,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, parentCategory), "seed parent category")

	// The CHILD's own budget line: match_logic 'or' escapes the parent's containment envelope
	// (domain.isContainedMatchLogic('or') == false). CreateCategory does NOT run
	// domain.EscapedNestedChild — that guard lives in AddCategoryChecked/UpdateCategoryChecked/
	// CreateWithCategories — so this call bypasses it exactly as the direct-SQL seed in
	// budget_spend_double_count_1630_integration_test.go bypasses the service layer.
	childCategory := &domain.BudgetCategory{
		BudgetID:         budget.ID,
		CategoryKind:     "custom",
		CustomCategoryID: &customCatID,
		MatchLogic:       domain.MatchLogicOr,
		AllocatedAmount:  100,
		SpentAmount:      seededSpend, // also already correct
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, childCategory), "seed escaping child category")

	// A THIRD, UNRELATED category with a deliberately WRONG stored spent_amount — no transactions
	// are filed under FOOD_AND_DRINK, so the narrow recomputation is 0, and the seeded 42.00 is
	// stale by construction.
	unrelatedPrimary := "FOOD_AND_DRINK"
	staleCategory := &domain.BudgetCategory{
		BudgetID:        budget.ID,
		CategoryKind:    "pfc",
		PFCPrimary:      &unrelatedPrimary,
		MatchLogic:      domain.MatchLogicCategoryOnly,
		AllocatedAmount: 200,
		SpentAmount:     42.00,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, staleCategory), "seed stale, unrelated category")

	// Reload the budget the way BudgetRepository.GetByID would (Categories populated), exactly what
	// AuditBudgetHierarchy expects its caller to hand it.
	loaded, err := budgetRepo.GetByID(ctx, budget.ID)
	require.NoError(t, err)
	require.Len(t, loaded.Categories, 3)

	// ─── RUN 1 ──────────────────────────────────────────────────────────────────────────────────
	verdict, err := budgetRepo.AuditBudgetHierarchy(ctx, loaded, periodStart, periodEnd, spentCalc)
	require.NoError(t, err)

	assert.Equal(t, domain.BudgetHierarchyAuditOutcomeReportedEscapingPair, verdict.Outcome,
		"an escaping pair must be reported, not silently repaired")
	assert.ElementsMatch(t, []string{parentCategory.ID, childCategory.ID}, verdict.FlaggedEscapingPairCategoryIDs,
		"both halves of the escaping pair must be named in the ledger's flagged_escaping_pair_category_ids")
	assert.Empty(t, verdict.FlaggedDuplicateIdentityCategoryIDs,
		"no duplicate-identity pair was seeded, so that typed column must stay empty")
	assert.Equal(t, 3, verdict.CategoriesExamined)
	assert.Equal(t, 1, verdict.CategoriesRepaired, "only the stale, unrelated row should be repaired")

	// match_logic on the escaping pair must be BYTE-FOR-BYTE UNTOUCHED — the whole point of
	// "report only, never repair" for this shape.
	rows, err := budgetRepo.GetCategoriesByBudgetID(ctx, budget.ID)
	require.NoError(t, err)
	byID := map[string]domain.BudgetCategory{}
	for _, c := range rows {
		byID[c.ID] = c
	}
	assert.Equal(t, domain.MatchLogicCategoryOnly, byID[parentCategory.ID].MatchLogic,
		"the parent's match_logic must never be rewritten by this audit")
	assert.Equal(t, domain.MatchLogicOr, byID[childCategory.ID].MatchLogic,
		"the escaping child's match_logic must never be rewritten by this audit")

	// The stale row's spent_amount IS corrected — to 0, since no transactions are filed under
	// FOOD_AND_DRINK.
	assert.InDelta(t, 0, byID[staleCategory.ID].SpentAmount, 0.001,
		"the stale, unrelated category's spent_amount must be corrected by the narrow recomputation")

	// The escaping pair's own spent_amount, seeded already-correct, must remain unchanged.
	assert.InDelta(t, seededSpend, byID[parentCategory.ID].SpentAmount, 0.001)
	assert.InDelta(t, seededSpend, byID[childCategory.ID].SpentAmount, 0.001)

	// Exactly one ledger row for this budget.
	var ledgerCount int
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*) FROM budget_hierarchy_audits WHERE budget_id = $1`, budget.ID,
	).Scan(&ledgerCount))
	assert.Equal(t, 1, ledgerCount)

	// ─── RUN 2: idempotency — a second run is a no-op ──────────────────────────────────────────
	reloaded, err := budgetRepo.GetByID(ctx, budget.ID)
	require.NoError(t, err)
	verdict2, err := budgetRepo.AuditBudgetHierarchy(ctx, reloaded, periodStart, periodEnd, spentCalc)
	require.NoError(t, err)

	// The recomputation itself is naturally idempotent (already-correct rows produce no writes);
	// the ledger's ON CONFLICT (budget_id) DO NOTHING is what makes the SECOND EXAMINATION a no-op
	// rather than a duplicate row or an error.
	assert.Equal(t, 0, verdict2.CategoriesRepaired, "the second run must find nothing left to repair")

	require.NoError(t, tx.QueryRow(ctx,
		`SELECT count(*) FROM budget_hierarchy_audits WHERE budget_id = $1`, budget.ID,
	).Scan(&ledgerCount))
	assert.Equal(t, 1, ledgerCount, "a second run must not append a second ledger row")

	rowsAfter, err := budgetRepo.GetCategoriesByBudgetID(ctx, budget.ID)
	require.NoError(t, err)
	for _, c := range rowsAfter {
		if c.ID == staleCategory.ID {
			assert.InDelta(t, 0, c.SpentAmount, 0.001, "the repaired row must not drift on a second run")
		}
	}

	// BudgetIDsWithUnexaminedHierarchy must exclude this budget now that it has a ledger row.
	unexamined, err := budgetRepo.BudgetIDsWithUnexaminedHierarchy(ctx, 1000)
	require.NoError(t, err)
	assert.NotContains(t, unexamined, budget.ID,
		"a budget already in the ledger must not be re-enumerated for examination")
}

// TestListBudgetHierarchyAuditFindings_1630B7 proves the admin-facing read decodes the ledger's
// flagged_category_ids array correctly (the ::text[] cast in listBudgetHierarchyAuditFindingsSQL)
// and that the outcome filter and the "no filter" path both return what they claim.
func TestListBudgetHierarchyAuditFindings_1630B7(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-b7-list-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	budgetRepo := NewBudgetRepository(tx)
	budget := &domain.Budget{
		UserID:    userID,
		Name:      "Claim 1630 B7 List Budget",
		Period:    domain.BudgetPeriodMonthly,
		StartDate: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Amount:    500,
	}
	require.NoError(t, budgetRepo.Create(ctx, budget))

	catID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO budget_hierarchy_audits (
			budget_id, user_id, outcome, reason, categories_examined, categories_repaired,
			flagged_escaping_pair_category_ids
		) VALUES ($1, $2, 'reported_escaping_pair', 'test seed', 2, 0, $3)
	`, budget.ID, userID, []string{catID})
	require.NoError(t, err, "seed a ledger row directly")

	// Filtered to the exact outcome: the row comes back with its flagged id decoded correctly,
	// under the typed column matching the outcome it was seeded with.
	findings, err := budgetRepo.ListBudgetHierarchyAuditFindings(ctx, domain.BudgetHierarchyAuditOutcomeReportedEscapingPair, 10)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, budget.ID, findings[0].BudgetID)
	assert.Equal(t, []string{catID}, findings[0].FlaggedEscapingPairCategoryIDs)
	assert.Empty(t, findings[0].FlaggedDuplicateIdentityCategoryIDs)

	// Filtered to a DIFFERENT outcome: nothing comes back.
	none, err := budgetRepo.ListBudgetHierarchyAuditFindings(ctx, domain.BudgetHierarchyAuditOutcomeVerified, 10)
	require.NoError(t, err)
	assert.Empty(t, none)

	// Unfiltered ("" outcome): the row is still returned.
	all, err := budgetRepo.ListBudgetHierarchyAuditFindings(ctx, "", 10)
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

// zeroSpentCalculator is a BudgetHierarchySpentCalculator stub that always recomputes to 0 — used
// by tests below that only care about the duplicate-identity/escaping-pair DETECTION, not the
// spend recomputation, and seed no transactions at all.
type zeroSpentCalculator struct{}

func (zeroSpentCalculator) CalculateCategorySpent(ctx context.Context, userID string, category *domain.BudgetCategory, startDate, endDate time.Time) (float64, error) {
	return 0, nil
}

// TestAuditBudgetHierarchy_1630Review_CombinedFindingPreservesBothTypedCategoryIDArrays is the RED
// for the review's MED finding: a budget can carry BOTH a duplicate-parent-identity pair AND an
// escaping nested pair at once. decideBudgetHierarchyAuditOutcome still picks only ONE preferred
// `outcome` for the ledger row (duplicate-identity, being the more fundamental ambiguity) — but
// before migration 000197 split flagged_category_ids into two typed columns, the escaping pair's
// category ids were stored in the SAME untyped array as the duplicate pair's, with no way for an
// operator to tell which ids belonged to which finding. This seeds both shapes on one budget and
// asserts BOTH typed arrays are independently populated, regardless of which outcome won.
func TestAuditBudgetHierarchy_1630Review_CombinedFindingPreservesBothTypedCategoryIDArrays(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "claim1630-review-combined-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	budgetRepo := NewBudgetRepository(tx)
	budget := &domain.Budget{
		UserID:    userID,
		Name:      "Claim 1630 Review Combined Finding Budget",
		Period:    domain.BudgetPeriodMonthly,
		StartDate: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Amount:    1000,
	}
	require.NoError(t, budgetRepo.Create(ctx, budget))

	// unique_budget_category_discriminated (migration 000083) backstops duplicate identity at the
	// DB level for every row created going forward — which is exactly why DuplicateParentIdentity
	// only ever matters for a budget whose duplicate PREDATES that index. To seed one in this
	// test, the index must come down first; it is restored automatically when this test's outer
	// tx rolls back (never committed), so no other test ever sees it missing.
	_, err = tx.Exec(ctx, `DROP INDEX unique_budget_category_discriminated`)
	require.NoError(t, err, "drop the unique index so a genuine (pre-index-era) duplicate can be seeded")

	// Duplicate-identity pair: two live rows, same primary, both primary-only. CreateCategory
	// bypasses AddCategoryChecked's guard, exactly like the escaping-pair fixture above.
	rentPrimary := "RENT_AND_UTILITIES"
	dupA := &domain.BudgetCategory{
		BudgetID: budget.ID, CategoryKind: "pfc", PFCPrimary: &rentPrimary,
		MatchLogic: domain.MatchLogicCategoryOnly, AllocatedAmount: 400,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, dupA), "seed duplicate-identity row A")
	dupB := &domain.BudgetCategory{
		BudgetID: budget.ID, CategoryKind: "pfc", PFCPrimary: &rentPrimary,
		MatchLogic: domain.MatchLogicCategoryOnly, AllocatedAmount: 100,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, dupB), "seed duplicate-identity row B")

	// Escaping pair, unrelated to the duplicate above: a non-containing 'and' parent with a
	// category_only child nested under it.
	foodPrimary := "FOOD_AND_DRINK"
	foodDetailed := "FOOD_AND_DRINK_FAST_FOOD"
	escParent := &domain.BudgetCategory{
		BudgetID: budget.ID, CategoryKind: "pfc", PFCPrimary: &foodPrimary,
		MatchLogic: domain.MatchLogicAnd, AllocatedAmount: 300,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, escParent), "seed escaping-pair parent")
	escChild := &domain.BudgetCategory{
		BudgetID: budget.ID, CategoryKind: "pfc", PFCPrimary: &foodPrimary, PFCDetailed: &foodDetailed,
		MatchLogic: domain.MatchLogicCategoryOnly, AllocatedAmount: 50,
	}
	require.NoError(t, budgetRepo.CreateCategory(ctx, escChild), "seed escaping-pair child")

	loaded, err := budgetRepo.GetByID(ctx, budget.ID)
	require.NoError(t, err)
	require.Len(t, loaded.Categories, 4)

	periodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.January, 31, 23, 59, 59, 0, time.UTC)
	verdict, err := budgetRepo.AuditBudgetHierarchy(ctx, loaded, periodStart, periodEnd, zeroSpentCalculator{})
	require.NoError(t, err)

	// Duplicate-identity is the PREFERRED single-valued outcome when both shapes are present.
	assert.Equal(t, domain.BudgetHierarchyAuditOutcomeReportedDuplicateIdentity, verdict.Outcome)

	// Both typed arrays must be independently populated — this is the whole point: the escaping
	// pair's ids must not be lost, silently merged into the duplicate-identity array, or dropped
	// just because duplicate-identity won the single-valued `outcome` column.
	assert.ElementsMatch(t, []string{dupA.ID, dupB.ID}, verdict.FlaggedDuplicateIdentityCategoryIDs,
		"the duplicate-identity pair's own ids must be in the duplicate-identity column")
	assert.ElementsMatch(t, []string{escParent.ID, escChild.ID}, verdict.FlaggedEscapingPairCategoryIDs,
		"the escaping pair's ids must survive in their OWN typed column even though "+
			"duplicate-identity is the outcome that won")

	// The same two arrays must round-trip through the ledger table, not just the in-memory verdict.
	findings, err := budgetRepo.ListBudgetHierarchyAuditFindings(ctx, "", 10)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.ElementsMatch(t, []string{dupA.ID, dupB.ID}, findings[0].FlaggedDuplicateIdentityCategoryIDs)
	assert.ElementsMatch(t, []string{escParent.ID, escChild.ID}, findings[0].FlaggedEscapingPairCategoryIDs)
}
