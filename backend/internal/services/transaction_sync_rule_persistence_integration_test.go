//go:build integration

package services

// Issue #1684, Defect 2 — the `modified` UPDATE branch must load the guard state, apply
// categorization rules, and persist the FULL category tuple coherently.
//
// WHY THIS IS AN INTEGRATION TEST AND NOT A pgxmock ONE. Every claim below is about what Postgres
// does to a row, and none of it is observable against a mock:
//   - the `valid_category_override_discriminator` CHECK rejects a partial tuple with SQLSTATE 23514;
//   - the `valid_category_source` CHECK rejects '' (and the scan cannot tell NULL from '');
//   - `effective_category_name` is written by a CASE that Postgres evaluates against the
//     PRE-update row, so a stale CASE stores the wrong display name and only a read-back shows it;
//   - `reviewed_at` being silently NULLed while `reviewed` stays true is a row state, not a call.
//
// The fixtures go through the PRODUCTION write path (CreateBatch / UpdateCategory), never a
// hand-built INSERT, so the state under test is a state production can actually produce.

import (
	"context"
	"encoding/json"
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

// TestProcessModifiedTransactions_UpdateBranch_PersistsRuleCategory is the RED-first test for
// Defect 2 on the update branch: before this change the branch copied only UserNotes and ID, so
//   - ApplyRulesToBatch's manual/reviewed guard saw an empty CategorySourceField and Reviewed=false
//     and therefore skipped NOTHING, and
//   - the write (a plain provider-fields Update) had nowhere to put the rule's category anyway.
//
// The subtests cover the whole contract: the override lands, the guard's two halves each actually
// guard, a legacy NULL category_source does not become '', and a matched-but-unchanged row is
// excluded from the synthetic insertedIDs that gate side effects.
func TestProcessModifiedTransactions_UpdateBranch_PersistsRuleCategory(t *testing.T) {
	t.Run("rule_match_persists_the_override", func(t *testing.T) {
		f := seedRuleHistoryFixture(t)
		ctx := context.Background()
		svc := f.newModifiedPathService(t)

		externalID := "plaid-rule-persist-" + uuid.New().String()
		storedID := uuid.New().String()
		// The stored row is what Plaid first delivered: no override, category_source 'plaid',
		// unreviewed, and a RAW pfc_primary the rule is about to override.
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

		require.NotNil(t, got.categoryOverrideKind, "the rule's override must reach the persisted row")
		assert.Equal(t, "pfc", *got.categoryOverrideKind)
		require.NotNil(t, got.pfcPrimaryOverride)
		assert.Equal(t, "FOOD_AND_DRINK", *got.pfcPrimaryOverride)
		require.NotNil(t, got.categorySource)
		assert.Equal(t, string(domain.CategorySourceRule), *got.categorySource,
			"a rule-categorized row is category_source='rule'")
		assert.True(t, got.reviewed, "a rule applying a category counts as an automated review")
		require.NotNil(t, got.reviewedAt)

		// THE STALE-CASE HAZARD. `Update`'s CASE reads the PRE-update row, so a write that also
		// sets category_override_kind / pfc_primary_override would store $7 (the raw
		// pfc_primary, GENERAL_MERCHANDISE) here instead of the rule's override.
		require.NotNil(t, got.effectiveCategoryName,
			"a rule setting kind='pfc' + an override on a previously-null row must store the OVERRIDE")
		assert.Equal(t, "FOOD_AND_DRINK", *got.effectiveCategoryName,
			"the display name must be recomputed from the POST-rule tuple, not the pre-update row")

		// Provider columns are still rewritten — the sibling replaces Update's contract, it does
		// not narrow it.
		assert.Equal(t, 7.25, got.amount, "the provider's new amount must land")

		// The OTHER half of the side-effect gating: a row that genuinely changed IS included in
		// the synthetic insertedIDs. Without this, the exclusion subtest below could pass
		// vacuously against an empty map.
		var catMatches int64
		require.NoError(t, f.tx.QueryRow(ctx,
			`SELECT match_count FROM categorization_rules WHERE id = $1`, f.ruleID).Scan(&catMatches))
		assert.EqualValues(t, 1, catMatches,
			"a genuinely-changed row IS included in the synthetic insertedIDs")
		// TWO rows: the fixture's category rule and its tag rule both match this merchant, and both
		// changed something — the category was applied and the tag was newly added.
		assert.EqualValues(t, 2, f.count(t, ctx,
			`SELECT COUNT(*) FROM rule_execution_history WHERE user_id = $1`, f.userID),
			"the change belongs in the automation trail")
	})

	t.Run("manual_row_is_unchanged", func(t *testing.T) {
		f := seedRuleHistoryFixture(t)
		ctx := context.Background()
		svc := f.newModifiedPathService(t)

		externalID := "plaid-manual-" + uuid.New().String()
		storedID := uuid.New().String()
		reviewedAt := time.Now().UTC().Truncate(time.Microsecond)
		kind, primary := "pfc", "PERSONAL_CARE"
		f.seedStoredTransaction(t, &domain.Transaction{
			ID: storedID, UserID: f.userID, AccountID: f.accountID, LinkedItemID: f.linkedItemID,
			Source: domain.SourcePlaid, ExternalTransactionID: &externalID,
			Amount: 5.00, Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			Name: "Blue Bottle Coffee", MerchantName: strPtr("Blue Bottle Coffee"),
			CategoryOverrideKind: &kind, PFCPrimaryOverride: &primary,
			CategorySourceField: domain.CategorySourceManual,
			Tags:                []string{"mine"},
			TagsSource:          []domain.TagSource{{Tag: "mine", Source: "manual"}},
			Reviewed:            true, ReviewedAt: &reviewedAt,
		})

		before := readPersistedRuleState(t, ctx, f.tx, storedID)

		// The payload WOULD match the fixture's "coffee" rule. That is the point: the guard, not
		// the absence of a match, is what must leave this row alone.
		payload := f.modifiedPayload(externalID, "Blue Bottle Coffee")
		require.NoError(t, svc.processModifiedTransactions(ctx, []plaid.Transaction{payload},
			&domain.LinkedItem{ID: f.linkedItemID, UserID: f.userID}, f.accountMap(), nil))

		after := readPersistedRuleState(t, ctx, f.tx, storedID)
		assertUserOwnedTupleUnchanged(t, "a manual row", before, after)

		// The modified write DOES rewrite provider columns and updated_at by design; that is not
		// a violation of "unchanged", it is the path's purpose.
		assert.Equal(t, 7.25, after.amount, "provider columns are rewritten BY DESIGN")
	})

	t.Run("reviewed_row_is_unchanged_including_reviewed_at", func(t *testing.T) {
		f := seedRuleHistoryFixture(t)
		ctx := context.Background()
		svc := f.newModifiedPathService(t)

		externalID := "plaid-reviewed-" + uuid.New().String()
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

		payload := f.modifiedPayload(externalID, "Blue Bottle Coffee")
		require.NoError(t, svc.processModifiedTransactions(ctx, []plaid.Transaction{payload},
			&domain.LinkedItem{ID: f.linkedItemID, UserID: f.userID}, f.accountMap(), nil))

		after := readPersistedRuleState(t, ctx, f.tx, storedID)
		assertUserOwnedTupleUnchanged(t, "a reviewed row", before, after)

		// ⚠ THE LOAD-BEARING HALF. The guard SKIPS rule application for a reviewed row, but the
		// write still executes — so if the branch does not copy ReviewedAt the write nulls
		// reviewed_at while leaving reviewed=true, silently losing the user's review timestamp.
		require.NotNil(t, after.reviewedAt, "reviewed_at must survive a modified re-delivery")
		assert.True(t, after.reviewedAt.Equal(reviewedAt),
			"reviewed_at is the user's own timestamp; a sync must not restamp or drop it")
	})

	t.Run("no_match_null_category_source_becomes_plaid", func(t *testing.T) {
		f := seedRuleHistoryFixture(t)
		ctx := context.Background()
		svc := f.newModifiedPathService(t)

		externalID := "plaid-legacy-null-source-" + uuid.New().String()
		storedID := uuid.New().String()
		f.seedStoredTransaction(t, &domain.Transaction{
			ID: storedID, UserID: f.userID, AccountID: f.accountID, LinkedItemID: f.linkedItemID,
			Source: domain.SourcePlaid, ExternalTransactionID: &externalID,
			Amount: 5.00, Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			Name: "Whole Foods", MerchantName: strPtr("Whole Foods"),
		})

		// A LEGACY row: category_source NULL. The scan assigns only when the column is Valid, so
		// the read path hands the branch "" — and writing "" back violates valid_category_source
		// (category_source IN ('plaid','rule','manual')).
		_, err := f.tx.Exec(ctx, `UPDATE transactions SET category_source = NULL WHERE id = $1`, storedID)
		require.NoError(t, err)
		before := readPersistedRuleState(t, ctx, f.tx, storedID)
		require.Nil(t, before.categorySource, "fixture guard: the stored column really is NULL")

		// No rule matches this merchant, so nothing but the guard-state copy decides the write.
		payload := f.modifiedPayload(externalID, "Whole Foods")
		require.NoError(t, svc.processModifiedTransactions(ctx, []plaid.Transaction{payload},
			&domain.LinkedItem{ID: f.linkedItemID, UserID: f.userID}, f.accountMap(), nil))

		after := readPersistedRuleState(t, ctx, f.tx, storedID)
		require.NotNil(t, after.categorySource, "NULL must be normalised, never written back as NULL-or-''")
		assert.Equal(t, string(domain.CategorySourcePlaid), *after.categorySource,
			"a modified row that NO rule matches and whose category_source is NULL must end as 'plaid'")
	})

	t.Run("matched_but_unchanged_is_excluded_from_side_effects", func(t *testing.T) {
		f := seedRuleHistoryFixture(t)
		ctx := context.Background()
		svc := f.newModifiedPathService(t)

		// Disable the category rule so only the TAG-ONLY rule can match. A category rule always
		// flips `reviewed`, and that flip IS a change (R73), so it can never produce the
		// matched-but-unchanged shape this subtest is about.
		_, err := f.tx.Exec(ctx, `UPDATE categorization_rules SET enabled = false WHERE id = $1`, f.ruleID)
		require.NoError(t, err)

		externalID := "plaid-tag-noop-" + uuid.New().String()
		storedID := uuid.New().String()
		f.seedStoredTransaction(t, &domain.Transaction{
			ID: storedID, UserID: f.userID, AccountID: f.accountID, LinkedItemID: f.linkedItemID,
			Source: domain.SourcePlaid, ExternalTransactionID: &externalID,
			Amount: 5.00, Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			Name: "Blue Bottle Coffee", MerchantName: strPtr("Blue Bottle Coffee"),
			// Already carrying the tag rule's own tag, attributed to that rule.
			Tags:       []string{"caffeine"},
			TagsSource: []domain.TagSource{{Tag: "caffeine", Source: "rule", RuleID: &f.tagRuleID}},
		})

		payload := f.modifiedPayload(externalID, "Blue Bottle Coffee")
		require.NoError(t, svc.processModifiedTransactions(ctx, []plaid.Transaction{payload},
			&domain.LinkedItem{ID: f.linkedItemID, UserID: f.userID}, f.accountMap(), nil))

		// The tag rule MATCHED (its condition is satisfied) but changed nothing. The synthetic
		// insertedIDs must therefore EXCLUDE this transaction: no match-count increment and no
		// rule-execution history row. This is the UPPER bound on the match-count inflation the
		// side-effect gating exists to prevent.
		var tagMatches int64
		require.NoError(t, f.tx.QueryRow(ctx,
			`SELECT match_count FROM categorization_rules WHERE id = $1`, f.tagRuleID).Scan(&tagMatches))
		assert.Zero(t, tagMatches,
			"a matched-but-unchanged row must not inflate the match count")

		assert.Zero(t, f.count(t, ctx,
			`SELECT COUNT(*) FROM rule_execution_history WHERE user_id = $1`, f.userID),
			"nothing changed, so nothing belongs in the automation trail")
	})
}

// newModifiedPathService wires the modified path's real collaborators against the fixture's
// Postgres transaction — a real transaction repository and a real RuleApplicationService, exactly
// as rule_execution_history_integration_test.go's runSyncPath does for the added path.
func (f *ruleHistoryFixture) newModifiedPathService(t *testing.T) *transactionSyncService {
	t.Helper()
	enc := ruleHistoryEncryptor(t)

	ruleSvc := NewRuleApplicationService(repository.NewCategorizationRuleRepository(f.tx))
	ruleSvc.SetRuleExecutionHistoryRepository(repository.NewRuleExecutionHistoryRepository(f.tx))

	return NewTransactionSyncService(
		nil, nil,
		repository.NewTransactionRepository(f.tx, enc),
		nil, nil, nil, nil, nil,
		ruleSvc,
		nil, nil, nil, nil,
	).(*transactionSyncService)
}

// accountMap is the plaid_account_id → accounts.id mapping the fixture's account row establishes.
func (f *ruleHistoryFixture) accountMap() map[string]string {
	return map[string]string{"rule-hist-acct-" + f.accountID: f.accountID}
}

// modifiedPayload is a Plaid `modified` entry for the fixture's account. The merchant name is a
// parameter because the fixture's rules match on `merchant_contains: coffee`.
func (f *ruleHistoryFixture) modifiedPayload(externalID, merchant string) plaid.Transaction {
	pfc := plaid.NewPersonalFinanceCategory("GENERAL_MERCHANDISE", "GENERAL_MERCHANDISE_SUPERMARKETS")
	txn := plaid.Transaction{
		TransactionId: externalID,
		AccountId:     "rule-hist-acct-" + f.accountID,
		Amount:        7.25,
		Date:          "2026-01-15",
		Name:          merchant,
		MerchantName:  *plaid.NewNullableString(&merchant),
	}
	txn.PersonalFinanceCategory = *plaid.NewNullablePersonalFinanceCategory(pfc)
	return txn
}

// seedStoredTransaction writes the row the modified path must find, through the production
// CreateBatch so external_transaction_id_blind_idx matches what GetByExternalID will look up.
//
// ⚠ CreateBatch encrypts ExternalTransactionID in place ON THE SLICE ELEMENT it was handed; the
// caller's own struct is untouched, but every caller here captures the plaintext id first anyway.
//
// ⚠ tags / tags_source ARE NOT IN CreateBatch's INSERT COLUMN LIST — a pre-existing persistence gap
// that the upsert's own comment documents. A fixture that passed them to CreateBatch would seed a
// row with EMPTY tags while claiming otherwise, and the tag assertions would pass vacuously. They
// go through UpdateTags, the same path the user's own tag endpoint uses.
func (f *ruleHistoryFixture) seedStoredTransaction(t *testing.T, txn *domain.Transaction) {
	t.Helper()
	ctx := context.Background()
	repo := repository.NewTransactionRepository(f.tx, ruleHistoryEncryptor(t))
	_, err := repo.CreateBatch(ctx, []domain.Transaction{*txn})
	require.NoError(t, err, "seed stored transaction")

	if len(txn.Tags) == 0 && len(txn.TagsSource) == 0 {
		return
	}
	require.NoError(t, repo.UpdateTags(ctx, txn.ID, txn.UserID, txn.Tags, txn.TagsSource),
		"seed the row's tags through the path that actually writes them")
}

// persistedRuleState is the read-back shape for every assertion here. It is deliberately raw SQL
// rather than a repository read: a *string distinguishes SQL NULL (nil) from a value, and the
// NULL-vs-'' distinction is exactly what valid_category_source turns on.
type persistedRuleState struct {
	categoryOverrideKind     *string
	pfcPrimaryOverride       *string
	pfcDetailedOverride      *string
	customCategoryOverrideID *string
	categorySource           *string
	tags                     []string
	tagsSource               []domain.TagSource
	reviewed                 bool
	reviewedAt               *time.Time
	effectiveCategoryName    *string
	name                     string
	amount                   float64
}

func readPersistedRuleState(t *testing.T, ctx context.Context, tx pgx.Tx, id string) persistedRuleState {
	t.Helper()
	var (
		s       persistedRuleState
		tagsRaw []byte
		srcRaw  []byte
	)
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT category_override_kind, pfc_primary_override, pfc_detailed_override,
		       custom_category_override_id, category_source, tags, tags_source,
		       reviewed, reviewed_at, effective_category_name, name, amount
		FROM transactions WHERE id = $1
	`, id).Scan(
		&s.categoryOverrideKind, &s.pfcPrimaryOverride, &s.pfcDetailedOverride,
		&s.customCategoryOverrideID, &s.categorySource, &tagsRaw, &srcRaw,
		&s.reviewed, &s.reviewedAt, &s.effectiveCategoryName, &s.name, &s.amount,
	))
	require.NoError(t, json.Unmarshal(tagsRaw, &s.tags))
	require.NoError(t, json.Unmarshal(srcRaw, &s.tagsSource))
	return s
}

// assertUserOwnedTupleUnchanged compares the nine columns the sync must never touch. Provider
// columns and updated_at are EXCLUDED on purpose: the modified write rewrites them BY DESIGN, so a
// literal whole-row comparison is impossible and would be wrong.
func assertUserOwnedTupleUnchanged(t *testing.T, where string, before, after persistedRuleState) {
	t.Helper()
	assert.Equal(t, before.categoryOverrideKind, after.categoryOverrideKind,
		"%s: category_override_kind", where)
	assert.Equal(t, before.pfcPrimaryOverride, after.pfcPrimaryOverride,
		"%s: pfc_primary_override", where)
	assert.Equal(t, before.pfcDetailedOverride, after.pfcDetailedOverride,
		"%s: pfc_detailed_override", where)
	assert.Equal(t, before.customCategoryOverrideID, after.customCategoryOverrideID,
		"%s: custom_category_override_id", where)
	assert.Equal(t, before.categorySource, after.categorySource, "%s: category_source", where)
	assert.Equal(t, before.tags, after.tags, "%s: tags", where)
	assert.Equal(t, before.tagsSource, after.tagsSource, "%s: tags_source", where)
	assert.Equal(t, before.reviewed, after.reviewed, "%s: reviewed", where)
	assert.Equal(t, before.reviewedAt, after.reviewedAt, "%s: reviewed_at", where)
}
