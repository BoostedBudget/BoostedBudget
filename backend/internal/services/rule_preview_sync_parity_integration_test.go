//go:build integration

package services

// Issue #1684, suggested fix 4 — THE INVARIANT: preview and sync produce an IDENTICAL match set,
// including on the `modified` path.
//
// WHAT THIS PINS. Preview (rule_preview_service.go:117-127) applies NO guard and evaluates every
// candidate with the shared engine; the sync path evaluates only the batch it is driving and SKIPS
// rows that are manual or already reviewed (rule_application_service.go:334). The invariant is that
// the unguarded preview set and the set the sync path actually applies to are the same set.
//
// ANTI-TAUTOLOGY, because both arms share one stateless engine. A parity assertion that re-evaluated
// pre-mutation values would agree no matter what the engine did. Three deliberate choices make this
// falsifiable:
//
//  1. The sync arm's set is DERIVED FROM PERSISTED STATE, never from a re-evaluation.
//     processModifiedTransactions returns only an error, so the only observation of what it matched
//     is the rows its write actually changed. The fixture therefore guarantees that EVERY rule match
//     CHANGES the category (each rule's target differs from every seeded tuple) — otherwise the
//     persisted-change set would not be the match set and "IDENTICAL" would be the wrong assertion.
//  2. The expected set is HARD-CODED (the fixture's own transaction ids) and the persisted category
//     is asserted on the modified path, so reverting the raw-PFC matcher (T3, commit 24a6969c) or
//     the modified-path persistence (T6, commit b851f659) reddens this test.
//  3. Preview runs BOTH before and after the sync. The post-sync run evaluates the MUTATED rows —
//     the state a user sees once a sync has already filed them — which is where a matcher reading
//     the EFFECTIVE category instead of the raw provider value (pre-T3) drops the two override rows
//     and diverges from the hard-coded set.
//
// FIXTURE SCOPING IS MANDATORY, and stated rather than implied:
//   - preview reads ALL the user's live rows (GetByUserIDWithFilters, capped at
//     PreviewScanDefault = 200 — rule_preview_service.go:24) while sync evaluates only the driven
//     batch. This fixture is ONE user with SEVEN rows and drives ALL SEVEN, so the two universes
//     are the same by construction; ScannedCount is asserted to be exactly 7 to keep it that way.
//   - every row is never-reviewed and non-manual (category_source NULL or 'plaid'), so the sync
//     guard skips nothing and the unguarded preview set is the set sync applies to.
//   - the two defect-3 rows carry an override and are seeded by DIRECT INSERT with category_source
//     NULL and reviewed=false. That shape is deliberately NOT reachable through a production path:
//     UpdateCategory writes `reviewed = true, reviewed_at = now` (transaction_repository.go:1842),
//     the settlement-move queries stamp category_source='manual' (:924, :1081), and a rule path
//     always sets Reviewed = true (rule_application_service.go:261,379). A row seeded through one of
//     those would skip the sync guard — exactly the "IDENTICAL" claim under test. The shape is
//     schema-legal because valid_category_override_discriminator
//     (migrations/000065_pfc_v2_canonical.up.sql:127-142) leaves category_source unconstrained.
//
// SEAM. The modified path is driven through the REAL transactionSyncService, built by
// NewTransactionSyncService over a real transaction repository and a real RuleApplicationService
// (the same wiring rule_execution_history_integration_test.go:108-118 uses for the added path); the
// preview goes through the REAL categorizationRuleService with SetTransactionReader pointed at the
// same repository. No stub stands in for the reader.
//
// WHY INTEGRATION. Every claim is about what Postgres holds after the write: the category tuple the
// discriminator CHECK constrains, the denormalised effective_category_name the UPDATE recomputes,
// and the read-back the preview scan performs. pgxmock never sends SQL to a planner.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/plaid/plaid-go/v23/plaid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// previewSyncParityRow is one driven transaction. `seededKind`/`seededOverride` describe the DIRECT INSERT
// shape of the two defect-3 rows and are nil for the production-seeded ones.
type previewSyncParityRow struct {
	id          string
	externalID  string
	merchant    string
	pfcPrimary  string
	pfcDetailed string
	// Set only on the defect-3 rows, which cannot go through a production write path.
	seededKind     *string
	seededOverride *string
	// secondAccount places the row on the single-rule fixture's SECOND account, whose
	// user_account_id is deliberately NOT the rule's value. Only the account_id parity test uses
	// it; this fixture seeds one account, so every row here leaves it false.
	secondAccount bool
}

// previewSyncParityFixture is one user, two rules with DISJOINT match sets, and seven driven rows.
type previewSyncParityFixture struct {
	tx pgx.Tx

	userID         string
	linkedItemID   string
	userAccountID  string
	accountID      string
	plaidAccountID string
	customCatID    string

	ruleRawPFCID   string
	ruleMerchantID string

	rawMatchA         previewSyncParityRow
	rawMatchB         previewSyncParityRow
	customOverrideRow previewSyncParityRow
	pfcOverrideRow    previewSyncParityRow
	merchantMatchA    previewSyncParityRow
	merchantMatchB    previewSyncParityRow
	noMatchRow        previewSyncParityRow
}

// allRows is the fixture's driven universe, in a stable order. Every row is driven and every driven
// row is seeded — that is what makes "preview's scan" and "the sync's batch" the same universe.
func (f *previewSyncParityFixture) allRows() []previewSyncParityRow {
	return []previewSyncParityRow{
		f.rawMatchA, f.rawMatchB, f.customOverrideRow, f.pfcOverrideRow,
		f.merchantMatchA, f.merchantMatchB, f.noMatchRow,
	}
}

// seedPreviewSyncParityFixture builds the whole fixture inside one rolled-back transaction.
func seedPreviewSyncParityFixture(t *testing.T) *previewSyncParityFixture {
	t.Helper()
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	f := &previewSyncParityFixture{
		tx:             tx,
		userID:         uuid.New().String(),
		linkedItemID:   uuid.New().String(),
		userAccountID:  uuid.New().String(),
		accountID:      uuid.New().String(),
		customCatID:    uuid.New().String(),
		ruleRawPFCID:   uuid.New().String(),
		ruleMerchantID: uuid.New().String(),
	}
	f.plaidAccountID = "plaid-acc-parity-" + f.accountID

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, f.userID, "parity|"+f.userID, "parity-"+f.userID+"@example.com")

	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-parity-token')
	`, f.linkedItemID, f.userID, "parity-item-"+f.linkedItemID)

	// MANDATORY BEFORE ANY transactions INSERT: transactions.user_account_id carries a LIVE FK to
	// user_accounts(id), so the target must exist first.
	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, $3)
	`, f.userAccountID, f.userID, "ins-parity-"+f.userAccountID)

	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Checking', 'depository', 'active', $5)
	`, f.accountID, f.linkedItemID, f.userID, f.plaidAccountID, f.userAccountID)

	// The custom category the defect-3 custom-override row is filed under. It is also the FK target
	// the discriminator CHECK's 'custom' branch requires.
	mustExec("custom_category", `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Coffee', 'FOOD_AND_DRINK')
	`, f.customCatID, f.userID)

	// RULE 1 — the defect-3 subject: a legacy `existing_pfc_primary` condition, i.e. the fused type
	// whose matching semantics T3 changed. Its set must include both override rows.
	mustExec("raw-pfc rule", `
		INSERT INTO categorization_rules
			(id, user_id, name, enabled, priority, conditions, condition_logic, target_kind,
			 target_pfc_primary, tag_behavior, source)
		VALUES ($1, $2, 'Raw PFC is Food', true, 10,
		        '[{"type":"existing_pfc_primary","value":"GENERAL_MERCHANDISE"}]'::jsonb,
		        'AND', 'pfc', 'FOOD_AND_DRINK', 'add', 'manual')
	`, f.ruleRawPFCID, f.userID)

	// RULE 2 — a merchant rule with a DISJOINT match set and a DIFFERENT target. Disjointness is
	// load-bearing: EvaluateRules is first-match-wins for the category, so two category rules that
	// both matched one row would leave the loser's action unapplied and break the "every match
	// changes the category" guarantee the sync arm's derivation depends on.
	mustExec("merchant rule", `
		INSERT INTO categorization_rules
			(id, user_id, name, enabled, priority, conditions, condition_logic, target_kind,
			 target_pfc_primary, tag_behavior, source)
		VALUES ($1, $2, 'Coffee is Entertainment', true, 5,
		        '[{"type":"merchant_contains","value":"coffee"}]'::jsonb,
		        'AND', 'pfc', 'ENTERTAINMENT', 'add', 'manual')
	`, f.ruleMerchantID, f.userID)

	f.rawMatchA = previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-a-" + uuid.New().String(),
		merchant: "Whole Foods", pfcPrimary: "GENERAL_MERCHANDISE",
		pfcDetailed: "GENERAL_MERCHANDISE_SUPERMARKETS",
	}
	f.rawMatchB = previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-b-" + uuid.New().String(),
		merchant: "Shell Gas Station", pfcPrimary: "GENERAL_MERCHANDISE",
		pfcDetailed: "GENERAL_MERCHANDISE_CONVENIENCE_STORES",
	}
	f.merchantMatchA = previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-c-" + uuid.New().String(),
		merchant: "Blue Bottle Coffee", pfcPrimary: "TRANSPORTATION",
		pfcDetailed: "TRANSPORTATION_TAXIS_AND_RIDE_SHARES",
	}
	f.merchantMatchB = previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-d-" + uuid.New().String(),
		merchant: "Corner Coffee Shop", pfcPrimary: "TRANSPORTATION",
		pfcDetailed: "TRANSPORTATION_PUBLIC_TRANSIT",
	}
	// Matches NEITHER rule: raw PFC is TRANSPORTATION and the merchant carries no "coffee". It is
	// what keeps the match set a PROPER subset, so an "everything matched" fixture cannot pass.
	f.noMatchRow = previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-e-" + uuid.New().String(),
		merchant: "Whole Foods Market", pfcPrimary: "TRANSPORTATION",
		pfcDetailed: "TRANSPORTATION_PUBLIC_TRANSIT",
	}

	// DEFECT 3, case 1 — a row filed under a CUSTOM category. Its raw provider PFC is still
	// GENERAL_MERCHANDISE, so the raw-PFC rule must match it on both paths.
	customKind := "custom"
	f.customOverrideRow = previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-f-" + uuid.New().String(),
		merchant: "Blue Bottle", pfcPrimary: "GENERAL_MERCHANDISE",
		pfcDetailed: "GENERAL_MERCHANDISE_SUPERMARKETS",
		seededKind:  &customKind,
	}
	// DEFECT 3, case 2 — a row carrying a PFC OVERRIDE of its own. The rule's condition value is
	// the RAW provider value; a matcher that read the effective category would see ENTERTAINMENT
	// and miss this row, so its membership in the match set IS the assertion that raw was read.
	pfcKind := "pfc"
	override := "ENTERTAINMENT"
	f.pfcOverrideRow = previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-g-" + uuid.New().String(),
		merchant: "Blue Bottle Roasters", pfcPrimary: "GENERAL_MERCHANDISE",
		pfcDetailed: "GENERAL_MERCHANDISE_SUPERMARKETS",
		seededKind:  &pfcKind, seededOverride: &override,
	}

	// The five rows production can produce go through the PRODUCTION write path (CreateBatch).
	enc := ruleHistoryEncryptor(t)
	repo := repository.NewTransactionRepository(tx, enc)
	productionRows := []previewSyncParityRow{f.rawMatchA, f.rawMatchB, f.merchantMatchA, f.merchantMatchB, f.noMatchRow}
	seeded := make([]domain.Transaction, 0, len(productionRows))
	for i, row := range productionRows {
		merchant := row.merchant
		externalID := row.externalID
		primary, detailed := row.pfcPrimary, row.pfcDetailed
		seeded = append(seeded, domain.Transaction{
			ID:                    row.id,
			UserID:                f.userID,
			AccountID:             f.accountID,
			LinkedItemID:          f.linkedItemID,
			UserAccountID:         &f.userAccountID,
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID,
			Amount:                7.25,
			Date:                  time.Date(2026, 1, 10+i, 0, 0, 0, 0, time.UTC),
			Name:                  merchant,
			MerchantName:          &merchant,
			PFCPrimary:            &primary,
			PFCDetailed:           &detailed,
			// Never reviewed, never manually categorized: the sync guard must skip NOTHING.
			CategorySourceField: domain.CategorySourcePlaid,
		})
	}
	_, err = repo.CreateBatch(ctx, seeded)
	require.NoError(t, err, "seed the five production-shaped rows")

	// The two defect-3 rows are DIRECT INSERTS. See the file header for why no production path can
	// produce this shape, and note category_source is NULL rather than 'manual' — the discriminator
	// CHECK does not constrain it, and 'manual' would make the row skip the sync guard.
	for _, row := range []previewSyncParityRow{f.customOverrideRow, f.pfcOverrideRow} {
		effective := "Coffee"
		var customID, pfcOverride any
		switch *row.seededKind {
		case "custom":
			customID = f.customCatID
		case "pfc":
			customID = nil
			pfcOverride = *row.seededOverride
			effective = *row.seededOverride
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO transactions (
				id, user_id, account_id, linked_item_id, user_account_id,
				source, external_transaction_id, external_transaction_id_blind_idx,
				amount, date, name, merchant_name, pending,
				pfc_primary, pfc_detailed,
				category_override_kind, pfc_primary_override, custom_category_override_id,
				category_source, category_override_date, effective_category_name,
				reviewed, reviewed_at,
				tags, tags_source,
				created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5,
				'plaid', $6, $7,
				7.25, $8, $9, $9, false,
				$10, $11,
				$12, $13, $14,
				NULL, NULL, $15,
				false, NULL,
				'[]'::jsonb, '[]'::jsonb,
				NOW(), NOW()
			)
		`, row.id, f.userID, f.accountID, f.linkedItemID, f.userAccountID,
			row.externalID, enc.BlindIndex(row.externalID),
			time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC), row.merchant,
			row.pfcPrimary, row.pfcDetailed,
			*row.seededKind, pfcOverride, customID,
			effective)
		require.NoError(t, err, "direct-insert the override row %s", row.id)
	}

	return f
}

// previewSyncParityRawPFCCandidate is the candidate the rule builder would preview for the fixture's
// `existing_pfc_primary` rule. It is built from a request, not loaded from the rule row, exactly as
// PreviewRule is called in production — and it is rebuilt for every preview call because PreviewRule
// mutates the rule it is handed (Enabled, defaults, and condition normalization).
func previewSyncParityRawPFCCandidate() *domain.CategorizationRule {
	return &domain.CategorizationRule{
		Name: "Raw PFC is Food",
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionExistingPFCPrimary, Value: "GENERAL_MERCHANDISE"},
		},
		ConditionLogic:   "AND",
		TargetKind:       strPtr("pfc"),
		TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
		TagBehavior:      "add",
	}
}

// previewSyncParityMerchantCandidate is the same for the fixture's merchant rule.
func previewSyncParityMerchantCandidate() *domain.CategorizationRule {
	return &domain.CategorizationRule{
		Name: "Coffee is Entertainment",
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionMerchantContains, Value: "coffee"},
		},
		ConditionLogic:   "AND",
		TargetKind:       strPtr("pfc"),
		TargetPFCPrimary: strPtr("ENTERTAINMENT"),
		TagBehavior:      "add",
	}
}

// preview runs ONE candidate through the real preview service over the fixture's persisted rows.
func (f *previewSyncParityFixture) preview(t *testing.T, ctx context.Context, candidate *domain.CategorizationRule) *domain.RulePreviewResult {
	t.Helper()
	return previewSyncParityPreview(t, ctx, f.tx, f.userID, candidate)
}

// previewSyncParityPreview is the shared preview wiring: the REAL categorizationRuleService over
// the REAL transaction repository, no stub. Both parity fixtures call it so the two cannot drift.
func previewSyncParityPreview(t *testing.T, ctx context.Context, tx pgx.Tx, userID string, candidate *domain.CategorizationRule) *domain.RulePreviewResult {
	t.Helper()
	svc := NewCategorizationRuleService(
		repository.NewCategorizationRuleRepository(tx),
		repository.NewCustomCategoryRepository(tx),
	).(*categorizationRuleService)
	svc.SetTransactionReader(repository.NewTransactionRepository(tx, ruleHistoryEncryptor(t)))

	res, err := svc.PreviewRule(ctx, userID, candidate, 0)
	require.NoError(t, err, "preview must not error — an error would make every assertion vacuous")
	return res
}

// modifiedPayloads is the Plaid `modified` delivery for every driven row, carrying the SAME raw
// provider PFC and merchant name the row was seeded with. That equality is what makes the two arms
// comparable: preview evaluates the persisted row, the sync path evaluates the mapped payload.
func (f *previewSyncParityFixture) modifiedPayloads() []plaid.Transaction {
	rows := f.allRows()
	out := make([]plaid.Transaction, 0, len(rows))
	for _, row := range rows {
		merchant := row.merchant
		pfc := plaid.NewPersonalFinanceCategory(row.pfcPrimary, row.pfcDetailed)
		txn := plaid.Transaction{
			TransactionId: row.externalID,
			AccountId:     f.plaidAccountID,
			Amount:        7.25,
			Date:          "2026-01-10",
			Name:          merchant,
		}
		txn.MerchantName = *plaid.NewNullableString(&merchant)
		txn.PersonalFinanceCategory = *plaid.NewNullablePersonalFinanceCategory(pfc)
		out = append(out, txn)
	}
	return out
}

// previewSyncParityCategoryTuple is the four columns valid_category_override_discriminator
// constrains.
type previewSyncParityCategoryTuple struct {
	kind     *string
	primary  *string
	detailed *string
	customID *string
}

// equal compares two tuples BY VALUE, and it has to be spelled out.
//
// ⚠ Go's struct equality compares POINTER fields by ADDRESS. `after.tuple != before.tuple` would
// therefore report "changed" for every row whose tuple was read twice — two scans allocate two
// *string for the same text — so the derivation below could not fail no matter what the write
// persisted. That is the exact class of unfalsifiable assertion this test exists to avoid, and it
// was caught by running the T3 failure demo: the derived set came back correct while the matcher
// under test was broken.
func (t previewSyncParityCategoryTuple) equal(other previewSyncParityCategoryTuple) bool {
	return previewSyncParitySameString(t.kind, other.kind) &&
		previewSyncParitySameString(t.primary, other.primary) &&
		previewSyncParitySameString(t.detailed, other.detailed) &&
		previewSyncParitySameString(t.customID, other.customID)
}

func previewSyncParitySameString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// previewSyncParityState is the read-back shape every assertion here uses. Raw SQL rather than a
// repository read, because a *string distinguishes SQL NULL from an empty string and that
// distinction is exactly what the discriminator and the legacy-row handling turn on.
type previewSyncParityState struct {
	tuple          previewSyncParityCategoryTuple
	categorySource *string
	reviewed       bool
	effectiveName  *string
}

func previewSyncParityReadState(t *testing.T, ctx context.Context, tx pgx.Tx, id string) previewSyncParityState {
	t.Helper()
	var got previewSyncParityState
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT category_override_kind, pfc_primary_override, pfc_detailed_override,
		       custom_category_override_id, category_source, reviewed, effective_category_name
		FROM transactions WHERE id = $1
	`, id).Scan(
		&got.tuple.kind, &got.tuple.primary, &got.tuple.detailed, &got.tuple.customID,
		&got.categorySource, &got.reviewed, &got.effectiveName,
	))
	return got
}

func previewSyncParityMatchIDs(res *domain.RulePreviewResult) []string {
	ids := make([]string, 0, len(res.Matches))
	for _, m := range res.Matches {
		ids = append(ids, m.TransactionID)
	}
	return ids
}

func previewSyncParityMatchCount(t *testing.T, ctx context.Context, tx pgx.Tx, ruleID string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, tx.QueryRow(ctx,
		`SELECT match_count FROM categorization_rules WHERE id = $1`, ruleID).Scan(&n))
	return n
}

// TestPreviewSyncParity_ModifiedPath_Integration is the invariant. One sync run, four observations:
// preview before, the modified write, preview after, and the match set derived from what the write
// persisted.
func TestPreviewSyncParity_ModifiedPath_Integration(t *testing.T) {
	f := seedPreviewSyncParityFixture(t)
	ctx := context.Background()

	// HARD-CODED, not derived from anything the code under test computes. The union is what the
	// sync path must apply to; the two halves are each rule's own preview set.
	expectedRawPFC := []string{f.rawMatchA.id, f.rawMatchB.id, f.customOverrideRow.id, f.pfcOverrideRow.id}
	expectedMerchant := []string{f.merchantMatchA.id, f.merchantMatchB.id}
	expectedUnion := append(append([]string{}, expectedRawPFC...), expectedMerchant...)

	rows := f.allRows()
	seeded := make(map[string]previewSyncParityState, len(rows))
	for _, row := range rows {
		seeded[row.id] = previewSyncParityReadState(t, ctx, f.tx, row.id)
	}
	// FIXTURE GUARDS. If either of these drifts the test stops proving what it claims: a
	// manual/reviewed row would be skipped by the sync guard, and a row that already carried the
	// rule's own target would be a matched-but-unchanged row.
	for _, row := range rows {
		if seeded[row.id].categorySource != nil {
			require.NotEqual(t, string(domain.CategorySourceManual), *seeded[row.id].categorySource,
				"no parity row may be manual — the sync guard would skip it")
		}
		require.False(t, seeded[row.id].reviewed, "no parity row may be reviewed — the sync guard would skip it")
	}
	require.Nil(t, seeded[f.customOverrideRow.id].categorySource,
		"fixture guard: the custom-override row is seeded with category_source NULL by direct INSERT")
	require.Nil(t, seeded[f.pfcOverrideRow.id].categorySource,
		"fixture guard: the pfc-override row is seeded with category_source NULL by direct INSERT")
	require.Equal(t, "custom", previewSyncParityText(seeded[f.customOverrideRow.id].tuple.kind),
		"fixture guard: the custom-override row really carries a custom override")
	require.Equal(t, "ENTERTAINMENT", previewSyncParityText(seeded[f.pfcOverrideRow.id].tuple.primary),
		"fixture guard: the pfc-override row really carries its own pfc override")

	// ── ARM 1 — preview on the SEEDED state: what the rule builder shows before anything ran.
	beforeRawPFC := f.preview(t, ctx, previewSyncParityRawPFCCandidate())
	beforeMerchant := f.preview(t, ctx, previewSyncParityMerchantCandidate())

	require.Equal(t, len(rows), beforeRawPFC.ScannedCount,
		"preview must scan exactly this user's %d seeded rows: the fixture must stay under the "+
			"200-row PreviewScanDefault cap, and no other test's rows may be in scope", len(rows))
	require.Equal(t, len(rows), beforeMerchant.ScannedCount)
	assert.ElementsMatch(t, expectedRawPFC, previewSyncParityMatchIDs(beforeRawPFC))
	assert.ElementsMatch(t, expectedMerchant, previewSyncParityMatchIDs(beforeMerchant))
	t.Logf("preview BEFORE sync: raw-pfc rule matched %d/%d scanned; merchant rule matched %d/%d scanned",
		beforeRawPFC.MatchCount, beforeRawPFC.ScannedCount,
		beforeMerchant.MatchCount, beforeMerchant.ScannedCount)

	// ── ARM 2 — the MODIFIED path. Defect 2 lives here: the update branch (a live stored row)
	// must load the guard state, apply rules, and persist the result through the sibling write.
	ruleSvc := NewRuleApplicationService(repository.NewCategorizationRuleRepository(f.tx))
	ruleSvc.SetRuleExecutionHistoryRepository(repository.NewRuleExecutionHistoryRepository(f.tx))
	svc := NewTransactionSyncService(
		nil, nil,
		repository.NewTransactionRepository(f.tx, ruleHistoryEncryptor(t)),
		nil, nil, nil, nil, nil,
		ruleSvc,
		nil, nil, nil, nil,
	).(*transactionSyncService)

	require.NoError(t, svc.processModifiedTransactions(ctx, f.modifiedPayloads(),
		&domain.LinkedItem{ID: f.linkedItemID, UserID: f.userID},
		map[string]string{f.plaidAccountID: f.accountID},
		map[string]string{f.plaidAccountID: f.userAccountID}))

	// ── ARM 3 — preview on the PERSISTED, post-rule state. This is the arm that discriminates the
	// raw-vs-effective matcher: every matched row now carries the rule's own override, so a matcher
	// reading the EFFECTIVE category would stop matching the two rows whose override the rule just
	// wrote — and preview would no longer agree with the hard-coded set.
	afterRawPFC := f.preview(t, ctx, previewSyncParityRawPFCCandidate())
	afterMerchant := f.preview(t, ctx, previewSyncParityMerchantCandidate())

	require.Equal(t, len(rows), afterRawPFC.ScannedCount,
		"the post-sync scan must still be exactly the driven universe")
	require.Equal(t, len(rows), afterMerchant.ScannedCount)
	assert.ElementsMatch(t, expectedRawPFC, previewSyncParityMatchIDs(afterRawPFC),
		"preview over the MUTATED rows must still report the raw-PFC matches: the condition describes "+
			"the source data the bank reported and must be stable under the rule's own action")
	assert.ElementsMatch(t, expectedMerchant, previewSyncParityMatchIDs(afterMerchant))
	t.Logf("preview AFTER sync: raw-pfc rule matched %d/%d scanned; merchant rule matched %d/%d scanned",
		afterRawPFC.MatchCount, afterRawPFC.ScannedCount,
		afterMerchant.MatchCount, afterMerchant.ScannedCount)

	// ── ARM 4 — the sync arm's own match set, derived from PERSISTED CATEGORY CHANGES.
	// processModifiedTransactions returns only an error, so the write is the observation. The
	// fixture guarantees every match changes the tuple, which is what makes the two sets identical.
	var syncMatched []string
	for _, row := range rows {
		after := previewSyncParityReadState(t, ctx, f.tx, row.id)
		if !after.tuple.equal(seeded[row.id].tuple) {
			syncMatched = append(syncMatched, row.id)
		}
	}
	t.Logf("sync (modified path) derived match set (%d rows): %v", len(syncMatched), syncMatched)
	assert.NotEmpty(t, syncMatched,
		"an empty derived set would make the comparison vacuous — the modified path applied nothing")
	assert.ElementsMatch(t, expectedUnion, syncMatched,
		"the modified path's persisted category changes must be exactly the hard-coded match set")
	t.Logf("sync (modified path) changed the category on %d of %d driven rows", len(syncMatched), len(rows))

	// ── The persisted category, asserted per row. This is the half that fails if the modified
	// write stops persisting the rule's output (T6) — and it pins the denormalised display name,
	// which the UPDATE recomputes from the NEW parameter values rather than from the old row.
	for _, want := range []struct {
		row       previewSyncParityRow
		target    string
		effective string
		why       string
	}{
		{f.rawMatchA, "FOOD_AND_DRINK", "FOOD_AND_DRINK", "an ordinary raw-PFC match"},
		{f.rawMatchB, "FOOD_AND_DRINK", "FOOD_AND_DRINK", "an ordinary raw-PFC match"},
		{f.customOverrideRow, "FOOD_AND_DRINK", "FOOD_AND_DRINK",
			"defect 3: a custom-override row matched the raw-PFC rule and the rule's pfc override replaced the custom one"},
		{f.pfcOverrideRow, "FOOD_AND_DRINK", "FOOD_AND_DRINK",
			"defect 3: a pfc-override row matched on its RAW provider value and the rule's target replaced the override"},
		{f.merchantMatchA, "ENTERTAINMENT", "ENTERTAINMENT", "a merchant-rule match"},
		{f.merchantMatchB, "ENTERTAINMENT", "ENTERTAINMENT", "a merchant-rule match"},
	} {
		got := previewSyncParityReadState(t, ctx, f.tx, want.row.id)
		require.NotNil(t, got.tuple.kind, "%s: the rule's category must reach the persisted row", want.why)
		assert.Equal(t, "pfc", *got.tuple.kind, "%s", want.why)
		require.NotNil(t, got.tuple.primary, "%s: the rule's target must be persisted, not only in memory", want.why)
		assert.Equal(t, want.target, *got.tuple.primary, "%s", want.why)
		assert.Nil(t, got.tuple.customID,
			"%s: applyCategoryOverride clears the other discriminator branch, and the CHECK requires it", want.why)
		require.NotNil(t, got.categorySource)
		assert.Equal(t, string(domain.CategorySourceRule), *got.categorySource, "%s", want.why)
		assert.True(t, got.reviewed, "%s: a rule applying a category counts as an automated review", want.why)
		require.NotNil(t, got.effectiveName,
			"%s: the display name must be recomputed from the POST-rule tuple, not the pre-update row", want.why)
		assert.Equal(t, want.effective, *got.effectiveName, "%s", want.why)
	}

	// The unmatched row is the other half of "the derived set is the match set": the write still
	// rewrites its provider fields BY DESIGN, but the category tuple and the review state are
	// untouched, so it must not appear in the derived set.
	unmatched := previewSyncParityReadState(t, ctx, f.tx, f.noMatchRow.id)
	assert.Equal(t, seeded[f.noMatchRow.id].tuple, unmatched.tuple,
		"a row no rule matched must not have its category tuple rewritten")
	require.NotNil(t, unmatched.categorySource)
	assert.Equal(t, string(domain.CategorySourcePlaid), *unmatched.categorySource)
	assert.False(t, unmatched.reviewed)

	// ── The per-rule match counts. Non-zero for EACH rule, and exact, because a parity assertion
	// over an empty set proves nothing: the counts are what the rule builder shows the user.
	assert.EqualValues(t, len(expectedRawPFC), previewSyncParityMatchCount(t, ctx, f.tx, f.ruleRawPFCID),
		"the raw-PFC rule matched nothing — the parity assertion above would be vacuous")
	assert.EqualValues(t, len(expectedMerchant), previewSyncParityMatchCount(t, ctx, f.tx, f.ruleMerchantID),
		"the merchant rule matched nothing — the parity assertion above would be vacuous")

	// ── Defect 3, stated explicitly so a regression names itself rather than surfacing as a set
	// diff. Case 1: a custom override does not hide the row from a raw-PFC rule. Case 2: a pfc
	// override is NOT the subject — the RAW provider value is — so the row still matches a rule
	// whose condition names GENERAL_MERCHANDISE even though its effective category is ENTERTAINMENT.
	assert.Contains(t, previewSyncParityMatchIDs(beforeRawPFC), f.customOverrideRow.id,
		"defect 3: a custom-override row must match a raw-PFC rule BEFORE the sync")
	assert.Contains(t, previewSyncParityMatchIDs(afterRawPFC), f.customOverrideRow.id,
		"defect 3: a custom-override row must match a raw-PFC rule AFTER the sync")
	assert.Contains(t, syncMatched, f.customOverrideRow.id,
		"defect 3: the modified path must apply the rule to a custom-override row")
	assert.Contains(t, previewSyncParityMatchIDs(beforeRawPFC), f.pfcOverrideRow.id,
		"defect 3: a pfc-override row must match on its RAW provider value BEFORE the sync")
	assert.Contains(t, previewSyncParityMatchIDs(afterRawPFC), f.pfcOverrideRow.id,
		"defect 3: a pfc-override row must match on its RAW provider value AFTER the sync")
	assert.Contains(t, syncMatched, f.pfcOverrideRow.id,
		"defect 3: the modified path must apply the rule to a pfc-override row")
	// Neither override row belongs to the merchant rule, and the unmatched row belongs to neither.
	assert.NotContains(t, previewSyncParityMatchIDs(beforeMerchant), f.customOverrideRow.id)
	assert.NotContains(t, previewSyncParityMatchIDs(beforeMerchant), f.pfcOverrideRow.id)
	assert.NotContains(t, syncMatched, f.noMatchRow.id)
	assert.NotContains(t, previewSyncParityMatchIDs(afterRawPFC), f.noMatchRow.id)
}

// previewSyncParityText dereferences a nullable string for comparisons that must spell NULL as "".
func previewSyncParityText(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
