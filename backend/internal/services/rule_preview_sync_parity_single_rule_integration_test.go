//go:build integration

package services

// Issue #1684, suggested fix 4 — the preview==sync parity invariant, pinned PER RULE TYPE.
//
// WHY THESE TWO TESTS EXIST. TestPreviewSyncParity_ModifiedPath_Integration pins the invariant for
// an `existing_pfc_primary` rule and a `merchant_contains` rule. Neither of those conditions reads
// a field that a defect could silently drop from the transaction projection, and neither is the
// `pfc_detailed` matcher whose raw-vs-effective semantics T3 changed. So that test passes even if
// `user_account_id` is missing from transactionSelectColumns (Defect 1) or matchPFC reads the
// effective category (Defect 3). These two tests close that hole: each drives ONE rule of the
// named type through the SAME two arms — the real preview service and the real modified-path sync
// — and asserts the two match sets are identical.
//
// THE INVARIANT, restated for a single rule. Preview applies NO guard and evaluates every
// candidate; the sync path evaluates only the batch it is driving and SKIPS manual/reviewed rows.
// With one rule and a fixture whose every row is never-reviewed and non-manual, the unguarded
// preview set and the set the sync path actually applies to must be the same set.
//
// ANTI-TAUTOLOGY, because both arms share one stateless engine. The sync arm's set is DERIVED FROM
// PERSISTED CATEGORY CHANGES, never from a re-evaluation: processModifiedTransactions returns only
// an error, so the only observation of what it matched is the rows its write actually changed. The
// fixture therefore guarantees that EVERY rule match CHANGES the category (the rule's target
// differs from every seeded tuple) — otherwise the persisted-change set would not be the match set
// and "IDENTICAL" would be the wrong assertion. The expected set is HARD-CODED (the fixture's own
// transaction ids), and the persisted category is asserted per matched row.
//
// FIXTURE SCOPING IS MANDATORY, and stated rather than implied:
//   - preview reads ALL the user's live rows (GetByUserIDWithFilters, capped at
//     PreviewScanDefault = 200 — rule_preview_service.go:24) while sync evaluates only the driven
//     batch. Each fixture is ONE user with FIVE rows and drives ALL FIVE, so the two universes are
//     the same by construction; ScannedCount is asserted to be exactly 5 to keep it that way.
//   - every row is never-reviewed and non-manual (category_source 'plaid'), so the sync guard
//     skips nothing and the unguarded preview set is the set sync applies to.
//   - the match set is a PROPER subset: the account_id fixture puts its no-match rows on a SECOND
//     account whose user_account_id is not the rule's value, and the pfc_detailed fixture seeds
//     rows with a different raw detailed enum. An "everything matched" fixture cannot pass.
//
// SEAM. The modified path is driven through the REAL transactionSyncService, built by
// previewSyncParitySyncService over a real transaction repository and a real RuleApplicationService
// (the same wiring rule_execution_history_integration_test.go:108-118 uses for the added path); the
// preview goes through the REAL categorizationRuleService with SetTransactionReader pointed at the
// same repository. No stub stands in for the reader.
//
// WHY INTEGRATION. Every claim is about what Postgres holds after the write: the category tuple the
// discriminator CHECK constrains, the denormalised effective_category_name the UPDATE recomputes,
// and the read-back the preview scan performs. pgxmock never sends SQL to a planner.

import (
	"context"
	"fmt"
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

// previewSyncParitySingleRuleFixture is ONE user, TWO accounts, and a small set of driven rows.
//
// WHY A SECOND FIXTURE. The two-rule fixture in rule_preview_sync_parity_integration_test.go
// cannot express "this rule matches a PROPER subset of the driven rows" for an account_id rule:
// every row it seeds shares one account, so an account_id rule would match all of them and an
// "everything matched" fixture cannot fail. This fixture seeds a SECOND account whose
// user_account_id is deliberately NOT the rule's value, so the account_id rule has rows it must
// not match. The pfc_detailed test leaves that account rowless — it discriminates on the raw PFC
// value instead.
type previewSyncParitySingleRuleFixture struct {
	tx pgx.Tx

	userID       string
	linkedItemID string

	// The account the rule's condition names (account_id rules) or the only account (pfc rules).
	userAccountID  string
	accountID      string
	plaidAccountID string

	// A second account, seeded so an account_id rule has rows it must NOT match.
	otherUserAccountID  string
	otherAccountID      string
	otherPlaidAccountID string

	ruleID string

	// matchRows are the rows the rule must match; noMatchRows are the rows it must not.
	matchRows   []previewSyncParityRow
	noMatchRows []previewSyncParityRow
}

// allRows is the fixture's driven universe, in a stable order. Every row is driven and every
// driven row is seeded — that is what makes "preview's scan" and "the sync's batch" the same
// universe.
func (f *previewSyncParitySingleRuleFixture) allRows() []previewSyncParityRow {
	return append(append([]previewSyncParityRow{}, f.matchRows...), f.noMatchRows...)
}

// seedPreviewSyncParitySingleRuleFixture builds ONE user, TWO accounts, and the driven rows inside
// one rolled-back transaction. The rule is inserted separately (insertRule) because an account_id
// rule's condition value is the fixture's own user_account_id — an id that does not exist until
// the fixture does.
func seedPreviewSyncParitySingleRuleFixture(
	t *testing.T,
	matchRows []previewSyncParityRow,
	noMatchRows []previewSyncParityRow,
) *previewSyncParitySingleRuleFixture {
	t.Helper()
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	f := &previewSyncParitySingleRuleFixture{
		tx:                 tx,
		userID:             uuid.New().String(),
		linkedItemID:       uuid.New().String(),
		userAccountID:      uuid.New().String(),
		accountID:          uuid.New().String(),
		otherUserAccountID: uuid.New().String(),
		otherAccountID:     uuid.New().String(),
		matchRows:          matchRows,
		noMatchRows:        noMatchRows,
	}
	f.plaidAccountID = "plaid-acc-parity-single-" + f.accountID
	f.otherPlaidAccountID = "plaid-acc-parity-single-other-" + f.otherAccountID

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, f.userID, "parity-single|"+f.userID, "parity-single-"+f.userID+"@example.com")

	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-parity-single-token')
	`, f.linkedItemID, f.userID, "parity-single-item-"+f.linkedItemID)

	// MANDATORY BEFORE ANY transactions INSERT: transactions.user_account_id carries a LIVE FK to
	// user_accounts(id), so both targets must exist first.
	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, $3)
	`, f.userAccountID, f.userID, "ins-parity-single-"+f.userAccountID)
	mustExec("other user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id)
		VALUES ($1, $2, $3)
	`, f.otherUserAccountID, f.userID, "ins-parity-single-other-"+f.otherUserAccountID)

	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Checking', 'depository', 'active', $5)
	`, f.accountID, f.linkedItemID, f.userID, f.plaidAccountID, f.userAccountID)
	mustExec("other account", `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status, user_account_id)
		VALUES ($1, $2, $3, $4, 'Savings', 'depository', 'active', $5)
	`, f.otherAccountID, f.linkedItemID, f.userID, f.otherPlaidAccountID, f.otherUserAccountID)

	// The rows go through the PRODUCTION write path (CreateBatch), so the fixture is the shape
	// production can actually produce: never reviewed, never manually categorized.
	enc := ruleHistoryEncryptor(t)
	repo := repository.NewTransactionRepository(tx, enc)
	rows := f.allRows()
	seeded := make([]domain.Transaction, 0, len(rows))
	for i, row := range rows {
		merchant := row.merchant
		externalID := row.externalID
		primary, detailed := row.pfcPrimary, row.pfcDetailed
		accountID, userAccountID := f.accountID, f.userAccountID
		if row.secondAccount {
			accountID, userAccountID = f.otherAccountID, f.otherUserAccountID
		}
		seeded = append(seeded, domain.Transaction{
			ID:                    row.id,
			UserID:                f.userID,
			AccountID:             accountID,
			LinkedItemID:          f.linkedItemID,
			UserAccountID:         &userAccountID,
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID,
			Amount:                7.25,
			Date:                  time.Date(2026, 2, 1+i, 0, 0, 0, 0, time.UTC),
			Name:                  merchant,
			MerchantName:          &merchant,
			PFCPrimary:            &primary,
			PFCDetailed:           &detailed,
			// Never reviewed, never manually categorized: the sync guard must skip NOTHING.
			CategorySourceField: domain.CategorySourcePlaid,
		})
	}
	_, err = repo.CreateBatch(ctx, seeded)
	require.NoError(t, err, "seed the driven rows")

	return f
}

// insertRule persists the ONE rule the sync path will load. conditionsJSON is the raw JSONB
// literal, so each test expresses its own condition shape: a field+operator triple for account_id,
// the legacy fused type for pfc_detailed.
func (f *previewSyncParitySingleRuleFixture) insertRule(t *testing.T, name, conditionsJSON, targetPFC string) {
	t.Helper()
	f.ruleID = uuid.New().String()
	_, err := f.tx.Exec(context.Background(), `
		INSERT INTO categorization_rules
			(id, user_id, name, enabled, priority, conditions, condition_logic, target_kind,
			 target_pfc_primary, tag_behavior, source)
		VALUES ($1, $2, $3, true, 10, $4::jsonb, 'AND', 'pfc', $5, 'add', 'manual')
	`, f.ruleID, f.userID, name, conditionsJSON, targetPFC)
	require.NoError(t, err, "seed the rule")
}

// modifiedPayloads is the Plaid `modified` delivery for every driven row, carrying the SAME raw
// provider PFC and merchant name the row was seeded with, and the plaid_account_id of the account
// the row actually belongs to. That equality is what makes the two arms comparable: preview
// evaluates the persisted row, the sync path evaluates the mapped payload.
func (f *previewSyncParitySingleRuleFixture) modifiedPayloads() []plaid.Transaction {
	rows := f.allRows()
	out := make([]plaid.Transaction, 0, len(rows))
	for _, row := range rows {
		merchant := row.merchant
		pfc := plaid.NewPersonalFinanceCategory(row.pfcPrimary, row.pfcDetailed)
		accountID := f.plaidAccountID
		if row.secondAccount {
			accountID = f.otherPlaidAccountID
		}
		txn := plaid.Transaction{
			TransactionId: row.externalID,
			AccountId:     accountID,
			Amount:        7.25,
			Date:          "2026-02-01",
			Name:          merchant,
		}
		txn.MerchantName = *plaid.NewNullableString(&merchant)
		txn.PersonalFinanceCategory = *plaid.NewNullablePersonalFinanceCategory(pfc)
		out = append(out, txn)
	}
	return out
}

func (f *previewSyncParitySingleRuleFixture) accountMap() map[string]string {
	return map[string]string{
		f.plaidAccountID:      f.accountID,
		f.otherPlaidAccountID: f.otherAccountID,
	}
}

func (f *previewSyncParitySingleRuleFixture) userAccountMap() map[string]string {
	return map[string]string{
		f.plaidAccountID:      f.userAccountID,
		f.otherPlaidAccountID: f.otherUserAccountID,
	}
}

// previewSyncParitySyncService builds the REAL transactionSyncService over the fixture's
// transaction, wired exactly as rule_execution_history_integration_test.go wires it for the added
// path: a real transaction repository and a real RuleApplicationService. No stub stands in for
// the reader or the writer.
func previewSyncParitySyncService(t *testing.T, tx pgx.Tx) *transactionSyncService {
	t.Helper()
	ruleSvc := NewRuleApplicationService(repository.NewCategorizationRuleRepository(tx))
	ruleSvc.SetRuleExecutionHistoryRepository(repository.NewRuleExecutionHistoryRepository(tx))
	return NewTransactionSyncService(
		nil, nil,
		repository.NewTransactionRepository(tx, ruleHistoryEncryptor(t)),
		nil, nil, nil, nil, nil,
		ruleSvc,
		nil, nil, nil, nil,
	).(*transactionSyncService)
}

// previewSyncParityAccountIDCandidate is the candidate the rule builder would preview for the
// fixture's account_id rule. It is built from a request, not loaded from the rule row, exactly as
// PreviewRule is called in production — and it is rebuilt for every preview call because
// PreviewRule mutates the rule it is handed (Enabled, defaults, and condition normalization).
func previewSyncParityAccountIDCandidate(userAccountID string) *domain.CategorizationRule {
	return &domain.CategorizationRule{
		Name: "Account A is Food",
		Conditions: []domain.RuleCondition{
			{Field: domain.FieldAccountID, Operator: string(domain.RuleOpEq), Value: userAccountID},
		},
		ConditionLogic:   "AND",
		TargetKind:       strPtr("pfc"),
		TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
		TagBehavior:      "add",
	}
}

// previewSyncParityPFCDetailedCandidate is the same for the fixture's pfc_detailed rule. The
// legacy fused type normalizes to `pfc_detailed eq`, which is the condition the engine evaluates.
func previewSyncParityPFCDetailedCandidate() *domain.CategorizationRule {
	return &domain.CategorizationRule{
		Name: "Supermarkets are Food",
		Conditions: []domain.RuleCondition{
			{Type: domain.ConditionExistingPFCDetailed, Value: "GENERAL_MERCHANDISE_SUPERSTORES"},
		},
		ConditionLogic:   "AND",
		TargetKind:       strPtr("pfc"),
		TargetPFCPrimary: strPtr("FOOD_AND_DRINK"),
		TagBehavior:      "add",
	}
}

// previewSyncParityAssertSeededGuardState is the fixture guard both tests share: no driven row may
// be manual or reviewed, because the sync guard would skip it and the unguarded preview set would
// no longer be the set sync applies to.
func previewSyncParityAssertSeededGuardState(t *testing.T, rows []previewSyncParityRow, seeded map[string]previewSyncParityState) {
	t.Helper()
	for _, row := range rows {
		if seeded[row.id].categorySource != nil {
			require.NotEqual(t, string(domain.CategorySourceManual), *seeded[row.id].categorySource,
				"no parity row may be manual — the sync guard would skip it")
		}
		require.False(t, seeded[row.id].reviewed, "no parity row may be reviewed — the sync guard would skip it")
	}
}

// previewSyncParityAssertPersistedCategory is the per-row half of the sync arm: the rule's target
// must have reached the persisted tuple, the discriminator's other branch must be cleared, and the
// denormalised display name must be recomputed from the POST-rule tuple.
func previewSyncParityAssertPersistedCategory(t *testing.T, ctx context.Context, tx pgx.Tx, row previewSyncParityRow, target string) {
	t.Helper()
	got := previewSyncParityReadState(t, ctx, tx, row.id)
	require.NotNil(t, got.tuple.kind, "the rule's category must reach the persisted row")
	assert.Equal(t, "pfc", *got.tuple.kind)
	require.NotNil(t, got.tuple.primary, "the rule's target must be persisted, not only in memory")
	assert.Equal(t, target, *got.tuple.primary)
	assert.Nil(t, got.tuple.customID,
		"applyCategoryOverride clears the other discriminator branch, and the CHECK requires it")
	require.NotNil(t, got.categorySource)
	assert.Equal(t, string(domain.CategorySourceRule), *got.categorySource)
	assert.True(t, got.reviewed, "a rule applying a category counts as an automated review")
	require.NotNil(t, got.effectiveName,
		"the display name must be recomputed from the POST-rule tuple, not the pre-update row")
	assert.Equal(t, target, *got.effectiveName)
}

// previewSyncParityAssertUnmatchedUntouched is the other half of "the derived set is the match
// set": the write still rewrites a row's provider fields BY DESIGN, but the category tuple and the
// review state are untouched, so an unmatched row must not appear in the derived set.
func previewSyncParityAssertUnmatchedUntouched(t *testing.T, ctx context.Context, tx pgx.Tx, row previewSyncParityRow, seeded previewSyncParityState) {
	t.Helper()
	got := previewSyncParityReadState(t, ctx, tx, row.id)
	assert.Equal(t, seeded.tuple, got.tuple,
		"a row the rule did not match must not have its category tuple rewritten")
	require.NotNil(t, got.categorySource)
	assert.Equal(t, string(domain.CategorySourcePlaid), *got.categorySource)
	assert.False(t, got.reviewed)
}

// TestPreviewSyncParity_AccountIDRule_Integration pins the invariant for an `account_id eq`
// condition whose value is the STABLE user_account_id, not the raw account_id.
//
// WHAT IT CATCHES THAT THE TWO-RULE TEST CANNOT. Defect 1 was `user_account_id` missing from
// transactionSelectColumns, so every transaction the preview read carried a nil UserAccountID and
// matchAccountID fell through to the raw AccountID — which is a different UUID. Preview then
// matched NOTHING while the sync path, whose mapper fills UserAccountID from the payload's
// plaid_account_id, still matched. The two arms disagree and this test reddens; the two-rule test
// never reads the field and stays green.
func TestPreviewSyncParity_AccountIDRule_Integration(t *testing.T) {
	// HARD-CODED rows, built before the fixture so the expected set is the fixture's own ids and
	// nothing the code under test computes.
	matchA := previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-acct-a-" + uuid.New().String(),
		merchant: "Whole Foods", pfcPrimary: "GENERAL_MERCHANDISE",
		pfcDetailed: "GENERAL_MERCHANDISE_SUPERSTORES",
	}
	matchB := previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-acct-b-" + uuid.New().String(),
		merchant: "Shell Gas Station", pfcPrimary: "TRANSPORTATION",
		pfcDetailed: "TRANSPORTATION_GAS",
	}
	matchC := previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-acct-c-" + uuid.New().String(),
		merchant: "Blue Bottle Coffee", pfcPrimary: "FOOD_AND_DRINK",
		pfcDetailed: "FOOD_AND_DRINK_COFFEE",
	}
	// The no-match rows sit on the SECOND account, whose user_account_id is NOT the rule's value.
	// They are what keeps the match set a PROPER subset, so an "everything matched" fixture cannot
	// pass.
	noMatchA := previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-acct-d-" + uuid.New().String(),
		merchant: "Corner Coffee Shop", pfcPrimary: "GENERAL_MERCHANDISE",
		pfcDetailed:   "GENERAL_MERCHANDISE_CONVENIENCE_STORES",
		secondAccount: true,
	}
	noMatchB := previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-acct-e-" + uuid.New().String(),
		merchant: "Whole Foods Market", pfcPrimary: "TRANSPORTATION",
		pfcDetailed:   "TRANSPORTATION_PUBLIC_TRANSIT",
		secondAccount: true,
	}

	f := seedPreviewSyncParitySingleRuleFixture(t,
		[]previewSyncParityRow{matchA, matchB, matchC},
		[]previewSyncParityRow{noMatchA, noMatchB})
	// The rule names the FIRST account's user_account_id — the stable logical link, not the raw
	// account_id. That choice is the whole point: a projection that drops user_account_id makes
	// this rule match NOTHING in preview while the sync path still matches.
	f.insertRule(t, "Account A is Food",
		fmt.Sprintf(`[{"field":"account_id","operator":"eq","value":%q}]`, f.userAccountID),
		"FOOD_AND_DRINK")

	ctx := context.Background()
	expected := []string{matchA.id, matchB.id, matchC.id}
	rows := f.allRows()

	seeded := make(map[string]previewSyncParityState, len(rows))
	for _, row := range rows {
		seeded[row.id] = previewSyncParityReadState(t, ctx, f.tx, row.id)
	}
	previewSyncParityAssertSeededGuardState(t, rows, seeded)

	// ── ARM 1 — preview on the SEEDED state: what the rule builder shows before anything ran.
	before := previewSyncParityPreview(t, ctx, f.tx, f.userID, previewSyncParityAccountIDCandidate(f.userAccountID))
	require.Equal(t, len(rows), before.ScannedCount,
		"preview must scan exactly this user's %d seeded rows: the fixture must stay under the "+
			"200-row PreviewScanDefault cap, and no other test's rows may be in scope", len(rows))
	assert.ElementsMatch(t, expected, previewSyncParityMatchIDs(before))
	require.NotEmpty(t, previewSyncParityMatchIDs(before),
		"an empty preview set would make the parity assertion vacuous — the account_id rule matched nothing")
	t.Logf("preview BEFORE sync: account_id rule matched %d/%d scanned", before.MatchCount, before.ScannedCount)

	// ── ARM 2 — the MODIFIED path. The update branch (a live stored row) must load the guard
	// state, apply rules, and persist the result through the sibling write.
	svc := previewSyncParitySyncService(t, f.tx)
	require.NoError(t, svc.processModifiedTransactions(ctx, f.modifiedPayloads(),
		&domain.LinkedItem{ID: f.linkedItemID, UserID: f.userID},
		f.accountMap(), f.userAccountMap()))

	// ── ARM 3 — preview on the PERSISTED, post-rule state. The condition describes the account
	// the transaction belongs to, so it must be stable under the rule's own action.
	after := previewSyncParityPreview(t, ctx, f.tx, f.userID, previewSyncParityAccountIDCandidate(f.userAccountID))
	require.Equal(t, len(rows), after.ScannedCount,
		"the post-sync scan must still be exactly the driven universe")
	assert.ElementsMatch(t, expected, previewSyncParityMatchIDs(after),
		"preview over the MUTATED rows must still report the account_id matches")
	t.Logf("preview AFTER sync: account_id rule matched %d/%d scanned", after.MatchCount, after.ScannedCount)

	// ── ARM 4 — the sync arm's own match set, derived from PERSISTED CATEGORY CHANGES.
	// processModifiedTransactions returns only an error, so the write is the observation. The
	// fixture guarantees every match changes the tuple, which is what makes the two sets identical.
	var syncMatched []string
	for _, row := range rows {
		afterState := previewSyncParityReadState(t, ctx, f.tx, row.id)
		if !afterState.tuple.equal(seeded[row.id].tuple) {
			syncMatched = append(syncMatched, row.id)
		}
	}
	t.Logf("sync (modified path) derived match set (%d rows): %v", len(syncMatched), syncMatched)
	assert.NotEmpty(t, syncMatched,
		"an empty derived set would make the comparison vacuous — the modified path applied nothing")
	assert.ElementsMatch(t, expected, syncMatched,
		"the modified path's persisted category changes must be exactly the hard-coded match set")

	for _, row := range f.matchRows {
		previewSyncParityAssertPersistedCategory(t, ctx, f.tx, row, "FOOD_AND_DRINK")
	}
	for _, row := range f.noMatchRows {
		previewSyncParityAssertUnmatchedUntouched(t, ctx, f.tx, row, seeded[row.id])
	}

	// ── The per-rule match count. Non-zero and exact, because a parity assertion over an empty
	// set proves nothing: the count is what the rule builder shows the user.
	assert.EqualValues(t, len(expected), previewSyncParityMatchCount(t, ctx, f.tx, f.ruleID),
		"the account_id rule matched nothing — the parity assertion above would be vacuous")
}

// TestPreviewSyncParity_PFCDetailedRule_Integration pins the invariant for a `pfc_detailed eq`
// condition (the legacy `existing_pfc_detailed` type normalizes to it).
//
// WHAT IT CATCHES THAT THE TWO-RULE TEST CANNOT. Defect 3 was matchPFC reading the EFFECTIVE
// category instead of the RAW provider value. The two-rule test pins that for `pfc_primary`; this
// one pins it for `pfc_detailed`, the other half of the same matcher. The discriminating arm is
// the POST-sync preview: every matched row now carries the rule's own pfc override, so a matcher
// reading the effective category sees the override instead of the raw detailed enum and stops
// matching the very rows the rule just filed — preview then disagrees with the hard-coded set.
func TestPreviewSyncParity_PFCDetailedRule_Integration(t *testing.T) {
	matchA := previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-pfcd-a-" + uuid.New().String(),
		merchant: "Whole Foods", pfcPrimary: "GENERAL_MERCHANDISE",
		pfcDetailed: "GENERAL_MERCHANDISE_SUPERSTORES",
	}
	matchB := previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-pfcd-b-" + uuid.New().String(),
		merchant: "Trader Joe's", pfcPrimary: "GENERAL_MERCHANDISE",
		pfcDetailed: "GENERAL_MERCHANDISE_SUPERSTORES",
	}
	matchC := previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-pfcd-c-" + uuid.New().String(),
		merchant: "Safeway", pfcPrimary: "GENERAL_MERCHANDISE",
		pfcDetailed: "GENERAL_MERCHANDISE_SUPERSTORES",
	}
	// A DIFFERENT raw detailed enum, so the match set is a PROPER subset and an "everything
	// matched" fixture cannot pass.
	noMatchA := previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-pfcd-d-" + uuid.New().String(),
		merchant: "Shell Gas Station", pfcPrimary: "TRANSPORTATION",
		pfcDetailed: "TRANSPORTATION_GAS",
	}
	noMatchB := previewSyncParityRow{
		id: uuid.New().String(), externalID: "plaid-parity-pfcd-e-" + uuid.New().String(),
		merchant: "City Transit", pfcPrimary: "TRANSPORTATION",
		pfcDetailed: "TRANSPORTATION_PUBLIC_TRANSIT",
	}

	f := seedPreviewSyncParitySingleRuleFixture(t,
		[]previewSyncParityRow{matchA, matchB, matchC},
		[]previewSyncParityRow{noMatchA, noMatchB})
	// The legacy fused type normalizes to `pfc_detailed eq`, and the value is the RAW provider
	// detailed enum — not the effective category the app displays.
	f.insertRule(t, "Supermarkets are Food",
		`[{"type":"existing_pfc_detailed","value":"GENERAL_MERCHANDISE_SUPERSTORES"}]`,
		"FOOD_AND_DRINK")

	ctx := context.Background()
	expected := []string{matchA.id, matchB.id, matchC.id}
	rows := f.allRows()

	seeded := make(map[string]previewSyncParityState, len(rows))
	for _, row := range rows {
		seeded[row.id] = previewSyncParityReadState(t, ctx, f.tx, row.id)
	}
	previewSyncParityAssertSeededGuardState(t, rows, seeded)

	// ── ARM 1 — preview on the SEEDED state.
	before := previewSyncParityPreview(t, ctx, f.tx, f.userID, previewSyncParityPFCDetailedCandidate())
	require.Equal(t, len(rows), before.ScannedCount,
		"preview must scan exactly this user's %d seeded rows: the fixture must stay under the "+
			"200-row PreviewScanDefault cap, and no other test's rows may be in scope", len(rows))
	assert.ElementsMatch(t, expected, previewSyncParityMatchIDs(before))
	require.NotEmpty(t, previewSyncParityMatchIDs(before),
		"an empty preview set would make the parity assertion vacuous — the pfc_detailed rule matched nothing")
	t.Logf("preview BEFORE sync: pfc_detailed rule matched %d/%d scanned", before.MatchCount, before.ScannedCount)

	// ── ARM 2 — the MODIFIED path.
	svc := previewSyncParitySyncService(t, f.tx)
	require.NoError(t, svc.processModifiedTransactions(ctx, f.modifiedPayloads(),
		&domain.LinkedItem{ID: f.linkedItemID, UserID: f.userID},
		f.accountMap(), f.userAccountMap()))

	// ── ARM 3 — preview on the PERSISTED, post-rule state. This is the arm that discriminates the
	// raw-vs-effective matcher: every matched row now carries the rule's own pfc override, so a
	// matcher reading the EFFECTIVE category would stop matching the rows whose override the rule
	// just wrote — and preview would no longer agree with the hard-coded set.
	after := previewSyncParityPreview(t, ctx, f.tx, f.userID, previewSyncParityPFCDetailedCandidate())
	require.Equal(t, len(rows), after.ScannedCount,
		"the post-sync scan must still be exactly the driven universe")
	assert.ElementsMatch(t, expected, previewSyncParityMatchIDs(after),
		"preview over the MUTATED rows must still report the raw-pfc_detailed matches: the condition "+
			"describes the source data the bank reported and must be stable under the rule's own action")
	t.Logf("preview AFTER sync: pfc_detailed rule matched %d/%d scanned", after.MatchCount, after.ScannedCount)

	// ── ARM 4 — the sync arm's own match set, derived from PERSISTED CATEGORY CHANGES.
	var syncMatched []string
	for _, row := range rows {
		afterState := previewSyncParityReadState(t, ctx, f.tx, row.id)
		if !afterState.tuple.equal(seeded[row.id].tuple) {
			syncMatched = append(syncMatched, row.id)
		}
	}
	t.Logf("sync (modified path) derived match set (%d rows): %v", len(syncMatched), syncMatched)
	assert.NotEmpty(t, syncMatched,
		"an empty derived set would make the comparison vacuous — the modified path applied nothing")
	assert.ElementsMatch(t, expected, syncMatched,
		"the modified path's persisted category changes must be exactly the hard-coded match set")

	for _, row := range f.matchRows {
		previewSyncParityAssertPersistedCategory(t, ctx, f.tx, row, "FOOD_AND_DRINK")
	}
	for _, row := range f.noMatchRows {
		previewSyncParityAssertUnmatchedUntouched(t, ctx, f.tx, row, seeded[row.id])
	}

	// ── The per-rule match count. Non-zero and exact, because a parity assertion over an empty
	// set proves nothing: the count is what the rule builder shows the user.
	assert.EqualValues(t, len(expected), previewSyncParityMatchCount(t, ctx, f.tx, f.ruleID),
		"the pfc_detailed rule matched nothing — the parity assertion above would be vacuous")
}
