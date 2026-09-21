//go:build integration

package repository

// Issue #1685 todo 28 — SCHEMA FALSIFICATION. NEGATIVE RESULT.
//
// Round 1's M10 hypothesis was that the #1686 "orphan detailed" shape could trip the #1685
// overlap barrier: a transaction carrying base pfc_primary + pfc_detailed AND a custom category
// override would match BOTH the orphan's PFC-detailed budget line and the custom budget line, so
// CalculateBudgetExpenseLedger would see two competing top-level matches and withhold the publish
// with 'overlapping_individual_lines'.
//
// MEASURED RESULT: THE HYPOTHESIS IS FALSE, BY CONSTRUCTION.
//
//	valid_category_override_discriminator (000065_pfc_v2_canonical.up.sql:127-143) constrains the
//	OVERRIDE columns (pfc_primary_override / pfc_detailed_override / custom_category_override_id /
//	category_override_kind) — NOT the base pfc_primary / pfc_detailed. So a row with base PFC
//	primary+detailed AND a custom override is ACCEPTED. But the generated effective columns
//	(000073_add_effective_category_columns.up.sql:24, :42) return NULL for kind='custom', and the
//	fact query reads COALESCE(t.effective_pfc_primary,'') / COALESCE(t.effective_pfc_detailed,'')
//	(budget_expense_ledger.go:230). The fact therefore carries EMPTY PFC primary/detailed and can
//	only ever match the custom line — never the orphan's detailed line, and never both at once.
//
// This test asserts the ACTUAL MECHANISM rather than the hypothesis:
//  1. the combined row stores NULL effective_pfc_primary / effective_pfc_detailed;
//  2. the fact read surfaces both as '', the orphan's PFC-detailed line stays at 0, the custom
//     line receives the amount, and no 'overlapping_individual_lines' is reported;
//  3. a seed that violates the discriminator's OVERRIDE columns IS rejected, naming the
//     constraint — so the disarmament is the schema's, not the fixture's.
//
// The subtests are positive controls proving the fixture is sound: a LEGAL PFC-detailed
// transaction lands on the detailed line with no overlap (the line is live, so the absence of a
// match for the combined row is attributable to the schema), and the same two budget lines DO
// report 'overlapping_individual_lines' for a fact that carries both match keys at once — i.e.
// the barrier is armed, and only the schema's NULLs keep it from firing.
//
// Run with:
//
//	DATABASE_URL=postgres://... CACHE_REDIS_TEST_URL=... REDIS_TEST_URL=... \
//	  go test ./internal/repository/... -tags integration -p 1 \
//	  -run TestOrphanDetailedPlusCustomCannotShareATransaction -v

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

const (
	// A PFC primary/detailed pair that satisfies valid_pfc_detailed_suffix
	// (pfc_detailed LIKE pfc_primary || '_%').
	orphanDetailedPrimary  = "HOME_IMPROVEMENT"
	orphanDetailedDetailed = "HOME_IMPROVEMENT_HOME_IMPROVEMENT"
	orphanDetailedAmount   = 275.50
	orphanLegalAmount      = 42.25

	orphanDetailedLineID = "orphan-detailed-line"
	orphanCustomLineID   = "orphan-custom-line"
)

// orphanDetailedCategories is the #1686 two-row budget: a PFC line carrying BOTH a primary and a
// detailed (so budgetedParentSetsFor excludes it from the pfc-primaries set via !hasPFCDetailed,
// domain/budget.go:274), plus a custom line nested under that primary. Both are therefore
// TOP-LEVEL for the arithmetic even though the frontend nests the custom under the detailed row.
func orphanDetailedCategories(customCategoryID string) []domain.BudgetCategory {
	primary := orphanDetailedPrimary
	detailed := orphanDetailedDetailed
	return []domain.BudgetCategory{
		{ID: orphanDetailedLineID, CategoryKind: "pfc", PFCPrimary: &primary, PFCDetailed: &detailed, MatchLogic: domain.MatchLogicCategoryOnly},
		{ID: orphanCustomLineID, CategoryKind: "custom", CustomCategoryID: &customCategoryID, CustomParentPFCPrimary: &primary, MatchLogic: domain.MatchLogicCategoryOnly},
	}
}

func TestOrphanDetailedPlusCustomCannotShareATransaction(t *testing.T) {
	ctx := context.Background()
	pool := connectTestDB(t)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID, linkedItemID, accountID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO users (id,clerk_user_id,email,subscription_tier,subscription_status,primary_region) VALUES ($1,$2,$3,'premium','active','iad')`, userID, "clerk_"+userID, userID+"@example.com")
	require.NoError(t, err, "seed user")
	_, err = tx.Exec(ctx, `INSERT INTO linked_items (id,user_id,item_id,access_token_encrypted) VALUES ($1,$2,$3,'fixture')`, linkedItemID, userID, "item_"+linkedItemID)
	require.NoError(t, err, "seed linked item")
	_, err = tx.Exec(ctx, `INSERT INTO accounts (id,user_id,linked_item_id,plaid_account_id,name,type,currency_code) VALUES ($1,$2,$3,$4,'Fixture','depository','USD')`, accountID, userID, linkedItemID, "account_"+accountID)
	require.NoError(t, err, "seed account")

	// The #1686 nesting linkage: the custom category names the orphan PFC primary as its parent.
	customCategoryID := uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO custom_categories (id,user_id,name,parent_pfc_primary) VALUES ($1,$2,'Orphan Detailed Child',$3)`, customCategoryID, userID, orphanDetailedPrimary)
	require.NoError(t, err, "seed custom category nested under the orphan primary")

	txDate := time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC)
	periodStart := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, -1)
	categories := orphanDetailedCategories(customCategoryID)

	// The combined row under test: base PFC primary+detailed AND a custom override. The
	// discriminator constrains only the OVERRIDE columns, so this INSERT is legal.
	combinedID := uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO transactions (id,user_id,account_id,linked_item_id,source,amount,name,date,pending,pfc_primary,pfc_detailed,category_override_kind,custom_category_override_id) VALUES ($1,$2,$3,$4,'manual',$5,'Orphan Add-on',$6,false,$7,$8,'custom',$9)`,
		combinedID, userID, accountID, linkedItemID, orphanDetailedAmount, txDate, orphanDetailedPrimary, orphanDetailedDetailed, customCategoryID)
	require.NoError(t, err, "base PFC primary+detailed plus a custom override must be ACCEPTED")

	// 1. The generated columns are NULL for kind='custom' — the schema's disarmament.
	var effPrimary, effDetailed *string
	require.NoError(t, tx.QueryRow(ctx, `SELECT effective_pfc_primary,effective_pfc_detailed FROM transactions WHERE id=$1`, combinedID).Scan(&effPrimary, &effDetailed))
	require.Nil(t, effPrimary, "effective_pfc_primary must be NULL when category_override_kind='custom'")
	require.Nil(t, effDetailed, "effective_pfc_detailed must be NULL when category_override_kind='custom'")

	// 2. The fact query surfaces both as '' — so the orphan's detailed line cannot match. The
	// custom linkage IS visible (cc.parent_pfc_primary), which is what the frontend nests on;
	// only the PFC match keys are withheld.
	facts, err := readBudgetExpenseFacts(ctx, tx, userID, periodStart, periodEnd)
	require.NoError(t, err)
	require.Len(t, facts, 1, "exactly the combined row is in the expense universe")
	require.Equal(t, "", facts[0].PFCPrimary, "the base pfc_primary must not reach the fact")
	require.Equal(t, "", facts[0].PFCDetailed, "the base pfc_detailed must not reach the fact")
	require.Equal(t, customCategoryID, facts[0].CustomCategoryID, "the custom override is the fact's only match key")
	require.Equal(t, orphanDetailedPrimary, facts[0].CustomPFCPrimary, "the nesting linkage is still visible to the read")

	// 3. The production ledger path: the custom line takes the amount, the detailed line stays at
	// zero, and the barrier is not tripped.
	ledger, err := ReadBudgetExpenseLedger(ctx, tx, userID, categories, periodStart, periodEnd)
	require.NoError(t, err)
	require.NotContains(t, ledger.Reasons, "overlapping_individual_lines",
		"the combined row cannot match both top-level lines")
	require.Equal(t, 0.0, *ledger.Lines[orphanDetailedLineID].Amount,
		"the orphan's PFC-detailed line must not receive a custom-override fact")
	require.Equal(t, orphanDetailedAmount, *ledger.Lines[orphanCustomLineID].Amount,
		"the custom line receives the fact")

	// 4. The discriminator is real, and it guards the OVERRIDE columns only. Each violating seed
	// below is run in its own savepoint so the outer fixture survives the rejection.
	t.Run("discriminator_rejects_override_column_violations", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			kind    *string
			pfcOver *string
			custom  *string
		}{
			{"custom_kind_carrying_a_pfc_override", strPtr("custom"), strPtr(orphanDetailedPrimary), strPtr(customCategoryID)},
			{"custom_kind_without_a_custom_reference", strPtr("custom"), nil, nil},
			{"pfc_kind_carrying_a_custom_reference", strPtr("pfc"), strPtr(orphanDetailedPrimary), strPtr(customCategoryID)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				subtx, err := tx.Begin(ctx)
				require.NoError(t, err)
				_, err = subtx.Exec(ctx, `INSERT INTO transactions (id,user_id,account_id,linked_item_id,source,amount,name,date,pending,category_override_kind,pfc_primary_override,custom_category_override_id) VALUES ($1,$2,$3,$4,'manual',1,'Violating Seed',$5,false,$6,$7,$8)`,
					uuid.NewString(), userID, accountID, linkedItemID, txDate, tc.kind, tc.pfcOver, tc.custom)
				require.Error(t, err, "the discriminator must reject this override-column combination")
				require.ErrorContains(t, err, "valid_category_override_discriminator",
					"the rejection must name the discriminator constraint")
				var pgErr *pgconn.PgError
				require.ErrorAs(t, err, &pgErr)
				require.Equal(t, "valid_category_override_discriminator", pgErr.ConstraintName)
				require.NoError(t, subtx.Rollback(ctx))
			})
		}
	})

	// Positive control 1: the fixture's PFC-detailed line is live. A LEGAL PFC-detailed
	// transaction (no override) lands on it and reports no overlap, so the combined row's zero
	// on that same line is the schema's doing, not a dead fixture.
	t.Run("positive_control_legal_pfc_detailed_transaction", func(t *testing.T) {
		legalID := uuid.NewString()
		_, err := tx.Exec(ctx, `INSERT INTO transactions (id,user_id,account_id,linked_item_id,source,amount,name,date,pending,pfc_primary,pfc_detailed) VALUES ($1,$2,$3,$4,'manual',$5,'Legal Home Improvement',$6,false,$7,$8)`,
			legalID, userID, accountID, linkedItemID, orphanLegalAmount, txDate, orphanDetailedPrimary, orphanDetailedDetailed)
		require.NoError(t, err, "seed a legal PFC-detailed transaction")

		legalFacts, err := readBudgetExpenseFacts(ctx, tx, userID, periodStart, periodEnd)
		require.NoError(t, err)
		require.Len(t, legalFacts, 2, "both the combined row and the legal row are in the universe")

		legalLedger := domain.CalculateBudgetExpenseLedger(categories, legalFacts)
		require.NotContains(t, legalLedger.Reasons, "overlapping_individual_lines",
			"a legal PFC-detailed fact matches exactly one top-level line")
		require.Equal(t, orphanLegalAmount, *legalLedger.Lines[orphanDetailedLineID].Amount,
			"the detailed line is live: it matches a legal PFC-detailed fact")
		require.Equal(t, orphanDetailedAmount, *legalLedger.Lines[orphanCustomLineID].Amount,
			"the custom line takes only the combined row")
	})

	// Positive control 2: the barrier is armed. Given a fact that carries BOTH match keys — the
	// M10 premise, which the schema never produces — the same two lines DO overlap. This isolates
	// the schema's NULLs as the sole reason the barrier stays silent.
	t.Run("positive_control_barrier_is_armed_for_a_fact_carrying_both_match_keys", func(t *testing.T) {
		hypothetical := domain.BudgetExpenseFact{
			ID:               "m10-hypothetical",
			PFCPrimary:       orphanDetailedPrimary,
			PFCDetailed:      orphanDetailedDetailed,
			CustomCategoryID: customCategoryID,
			CurrencyCode:     "USD",
			Amount:           orphanDetailedAmount,
		}
		hypotheticalLedger := domain.CalculateBudgetExpenseLedger(categories, []domain.BudgetExpenseFact{hypothetical})
		require.Contains(t, hypotheticalLedger.Reasons, "overlapping_individual_lines",
			"the barrier fires the moment one fact carries both match keys")
	})
}
