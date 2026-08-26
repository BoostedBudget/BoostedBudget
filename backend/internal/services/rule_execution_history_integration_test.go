//go:build integration

package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// Issue #1569 Task 5 (#1572) — the automation audit trail, end to end against real Postgres.
//
// WHY THESE ARE INTEGRATION TESTS AND NOT MOCKS. Every claim in this task is about what Postgres
// does to a row: the FK from rule_execution_history.transaction_id to transactions (which is why
// the writer must run after CreateBatch, not inside ApplyRulesToBatch), the
// `valid_action_type` / `valid_execution_result` CHECKs, the
// `rule_execution_history_user_id_not_null` CHECK, and `unique_rule_transaction`'s effect on a
// second write. pgxmock asserts query strings and never sends one to a planner, so none of that is
// observable against a mock — the pgxmock-never-catches-constraints lesson from this project's own
// feedback notes.
//
// THE FIXTURES GO THROUGH THE PRODUCTION WRITE PATH ON PURPOSE. The history rows here are written
// by ruleApplicationService.EmitBatchSideEffects, driven by ApplyRulesToBatch, over transactions
// inserted by transactionRepository.CreateBatch — the exact three calls
// transaction_sync_service.processAddedTransactions makes (:1027, :1049, :1058). A fixture that
// INSERTed history rows directly would assert a state production never produces.

type ruleHistoryFixture struct {
	tx           pgx.Tx
	userID       string
	ruleID       string
	tagRuleID    string
	accountID    string
	linkedItemID string
}

func seedRuleHistoryFixture(t *testing.T) *ruleHistoryFixture {
	t.Helper()
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	f := &ruleHistoryFixture{
		tx:           tx,
		userID:       uuid.New().String(),
		ruleID:       uuid.New().String(),
		tagRuleID:    uuid.New().String(),
		accountID:    uuid.New().String(),
		linkedItemID: uuid.New().String(),
	}

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, f.userID, "rule-hist|"+f.userID, "rule-hist-"+f.userID+"@example.com")

	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-rule-hist-token')
	`, f.linkedItemID, f.userID, "rule-hist-item-"+f.linkedItemID)

	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Checking', 'depository', 'active')
	`, f.accountID, f.linkedItemID, f.userID, "rule-hist-acct-"+f.accountID)

	mustExec("categorization_rule", `
		INSERT INTO categorization_rules
			(id, user_id, name, enabled, priority, conditions, condition_logic, target_kind,
			 target_pfc_primary, tag_behavior, source)
		VALUES ($1, $2, 'Coffee is Food', true, 10,
		        '[{"type":"merchant_contains","value":"coffee"}]'::jsonb,
		        'AND', 'pfc', 'FOOD_AND_DRINK', 'add', 'manual')
	`, f.ruleID, f.userID)

	mustExec("tag_rule", `
		INSERT INTO categorization_rules
			(id, user_id, name, enabled, priority, conditions, condition_logic, target_tags,
			 tag_behavior, source)
		VALUES ($1, $2, 'Tag my coffee', true, 5,
		        '[{"type":"merchant_contains","value":"coffee"}]'::jsonb,
		        'AND', '["caffeine"]'::jsonb, 'add', 'manual')
	`, f.tagRuleID, f.userID)

	return f
}

// runSyncPath drives the three production calls in the order processAddedTransactions makes them:
// ApplyRulesToBatch, then CreateBatch, then EmitBatchSideEffects gated on CreateBatch's insertedIDs.
func (f *ruleHistoryFixture) runSyncPath(t *testing.T, txns []domain.Transaction) *ruleApplicationService {
	t.Helper()
	ctx := context.Background()

	enc := ruleHistoryEncryptor(t)

	svc := NewRuleApplicationService(repository.NewCategorizationRuleRepository(f.tx))
	svc.SetRuleExecutionHistoryRepository(repository.NewRuleExecutionHistoryRepository(f.tx))

	apps, err := svc.ApplyRulesToBatch(ctx, txns, f.userID)
	require.NoError(t, err)

	insertedIDs, err := repository.NewTransactionRepository(f.tx, enc).CreateBatch(ctx, txns)
	require.NoError(t, err)

	svc.EmitBatchSideEffects(ctx, f.userID, apps, insertedIDs)
	return svc
}

// ruleHistoryEncryptor is the one encryptor every path in this file uses. It must be the same on
// the write and the tombstone paths, because external_transaction_id_blind_idx is what
// TombstoneByExternalID looks the row up by — a second encryptor with a different key would look
// up nothing and the test would pass vacuously.
func ruleHistoryEncryptor(t *testing.T) *appcrypto.FieldEncryptor {
	t.Helper()
	enc, err := appcrypto.NewFieldEncryptor(plainPassthroughEncryptionServiceForRuleHistory{},
		[]byte("12345678901234567890123456789012"))
	require.NoError(t, err)
	return enc
}

func (f *ruleHistoryFixture) newTransaction(merchant string) domain.Transaction {
	name := merchant
	return domain.Transaction{
		ID:                    uuid.New().String(),
		UserID:                f.userID,
		AccountID:             f.accountID,
		LinkedItemID:          f.linkedItemID,
		Source:                domain.SourcePlaid,
		ExternalTransactionID: strPtr("plaid-" + uuid.New().String()),
		Amount:                7.25,
		Date:                  time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Name:                  name,
		MerchantName:          &merchant,
	}
}

// ---------------------------------------------------------------------------------------------

// TestRuleExecutionHistory_SyncPathWritesTheTrail_Integration is the headline: before this change
// rule_execution_history had no production writer, so this assertion could not have passed for any
// input at all.
func TestRuleExecutionHistory_SyncPathWritesTheTrail_Integration(t *testing.T) {
	f := seedRuleHistoryFixture(t)
	ctx := context.Background()

	txn := f.newTransaction("Blue Bottle Coffee")
	f.runSyncPath(t, []domain.Transaction{txn})

	rows, total, err := repository.NewRuleExecutionHistoryRepository(f.tx).
		GetByUserID(ctx, f.userID, 20, 0)
	require.NoError(t, err)
	require.Equal(t, 2, total, "one category rule and one tag rule each changed something")
	require.Len(t, rows, 2)

	byRule := map[string]*domain.RuleExecution{}
	for _, r := range rows {
		require.NotNil(t, r.RuleID)
		byRule[*r.RuleID] = r
	}

	cat := byRule[f.ruleID]
	require.NotNil(t, cat, "the category rule must have a row")
	require.NotNil(t, cat.TransactionID)
	assert.Equal(t, txn.ID, *cat.TransactionID)
	assert.Equal(t, f.userID, cat.UserID, "the row's own GDPR erasure path")
	require.NotNil(t, cat.RuleName)
	assert.Equal(t, "Coffee is Food", *cat.RuleName)
	assert.Equal(t, domain.RuleExecutionActionCategory, cat.ActionType)
	assert.Equal(t, domain.RuleExecutionResultApplied, cat.Result)
	assert.False(t, cat.ExecutedAt.IsZero())

	var snap map[string]any
	require.NoError(t, json.Unmarshal(cat.DefinitionSnapshot, &snap))
	assert.Equal(t, "FOOD_AND_DRINK", snap["target_pfc_primary"])
	assert.Equal(t, "rule_application_service", snap["captured_by"])

	tagRow := byRule[f.tagRuleID]
	require.NotNil(t, tagRow, "the tag rule must have its OWN row, not be folded into the category rule's")
	assert.Equal(t, domain.RuleExecutionActionTags, tagRow.ActionType)
}

// TestRuleExecutionHistory_NoRowForATransactionTheUpsertDidNotInsert_Integration is trap 1 of the
// brief made observable. transaction_id carries an FK to transactions, so a row written for a
// conflict-preserved transaction would either violate it or point at nothing. Running the same
// batch twice makes the second pass produce an empty insertedIDs set.
func TestRuleExecutionHistory_NoRowForATransactionTheUpsertDidNotInsert_Integration(t *testing.T) {
	f := seedRuleHistoryFixture(t)
	ctx := context.Background()

	txn := f.newTransaction("Blue Bottle Coffee")
	f.runSyncPath(t, []domain.Transaction{txn})

	before := f.count(t, ctx, `SELECT COUNT(*) FROM rule_execution_history WHERE user_id = $1`, f.userID)
	require.Equal(t, 2, before)

	// A re-sync: the same external transaction id arrives again. CreateBatch's ON CONFLICT keeps
	// the existing row, so it is NOT in insertedIDs, and no second history row may appear.
	resync := f.newTransaction("Blue Bottle Coffee")
	resync.ID = uuid.New().String()
	resync.ExternalTransactionID = txn.ExternalTransactionID
	f.runSyncPath(t, []domain.Transaction{resync})

	after := f.count(t, ctx, `SELECT COUNT(*) FROM rule_execution_history WHERE user_id = $1`, f.userID)
	assert.Equal(t, before, after,
		"the re-synced transaction was conflict-preserved, so it has no new row for the FK to point at")

	orphaned := f.count(t, ctx, `
		SELECT COUNT(*) FROM rule_execution_history h
		WHERE h.user_id = $1
		  AND h.transaction_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM transactions t WHERE t.id = h.transaction_id)
	`, f.userID)
	assert.Zero(t, orphaned, "every non-NULL transaction_id must resolve — the FK says so")
}

// TestRuleExecutionHistory_ConflictKeepsTheFirstSnapshot_Integration pins the ON CONFLICT DO
// NOTHING decision. unique_rule_transaction UNIQUE (rule_id, transaction_id) means the table can
// hold only one execution per pair; DO UPDATE would keep the latest and OVERWRITE the original
// point-in-time snapshot, which is the drift the snapshot columns exist to prevent.
func TestRuleExecutionHistory_ConflictKeepsTheFirstSnapshot_Integration(t *testing.T) {
	f := seedRuleHistoryFixture(t)
	ctx := context.Background()

	txn := f.newTransaction("Blue Bottle Coffee")
	f.runSyncPath(t, []domain.Transaction{txn})

	repo := repository.NewRuleExecutionHistoryRepository(f.tx)

	// A second write for the same (rule, transaction), carrying a DIFFERENT name and snapshot —
	// what a DO UPDATE would happily install over the original.
	ruleID, txnID := f.ruleID, txn.ID
	later := "Renamed after the fact"
	losing := &domain.RuleExecution{
		RuleID:             &ruleID,
		TransactionID:      &txnID,
		UserID:             f.userID,
		RuleName:           &later,
		ActionType:         domain.RuleExecutionActionBoth,
		Result:             domain.RuleExecutionResultApplied,
		DefinitionSnapshot: json.RawMessage(`{"name":"Renamed after the fact"}`),
	}
	require.NoError(t, repo.Create(ctx, losing),
		"a conflict is not an error — the first execution simply wins")

	// D7 (review round 1): a conflicted Create must not hand back a UUID that names no row.
	assert.Empty(t, losing.ID,
		"an empty ID after a nil-error Create is how a caller tells 'kept the existing row' from 'wrote one'")
	assert.True(t, losing.ExecutedAt.IsZero(),
		"this call wrote no row, so it has no execution time to report")

	var name, action string
	require.NoError(t, f.tx.QueryRow(ctx, `
		SELECT rule_name, action_type FROM rule_execution_history
		WHERE rule_id = $1 AND transaction_id = $2
	`, f.ruleID, txn.ID).Scan(&name, &action))

	assert.Equal(t, "Coffee is Food", name,
		"DO NOTHING keeps the FIRST execution; a later write must never rewrite what history says")
	assert.Equal(t, domain.RuleExecutionActionCategory, action)

	assert.Equal(t, 1, f.count(t, ctx,
		`SELECT COUNT(*) FROM rule_execution_history WHERE rule_id = $1 AND transaction_id = $2`,
		f.ruleID, txn.ID), "unique_rule_transaction permits exactly one row for the pair")
}

// TestRuleExecutionHistory_SnapshotSurvivesARenameOfThePersistedRule_Integration pins the
// survives-a-rename property AT THE SQL LEVEL, which nothing did.
//
// ⚠ WHAT WAS AND WAS NOT ALREADY COVERED, because the distinction is the whole reason this test
// exists. rule_name and definition_snapshot are captured inside ApplyRulesToBatch and that capture
// is asserted in Go (SyncPathWritesTheTrail above). audit_trail_orphan_integration_test.go DOES
// `SELECT rule_name, definition_snapshot` back out of Postgres — but its fixture INSERTs into
// rule_execution_history by hand, so what it pins is FK/orphan behaviour over a row production
// never wrote. Neither one mutates the PERSISTED categorization_rules row and re-reads what the
// production writer stored. This does: the trail is written through
// ApplyRulesToBatch -> CreateBatch -> EmitBatchSideEffects, and only then is the live rule renamed
// and re-pointed.
//
// ⚠ THE TWO HALVES ARE NOT EQUALLY STRONG, and saying so is the point.
//
//   - The raw `SELECT rule_name, definition_snapshot` is a POST-CONDITION. Postgres does not
//     recompute a stored column, so it holds for any writer that put bytes there. What it does buy
//     is narrow and real: it proves the trail's copy is STORED rather than derived, so a later
//     change making either column a view, a generated column or a join result fails here.
//   - The GetByUserID assertion is the DISCRIMINATING half. The obvious future "improvement" to
//     visibleRuleExecutions is to LEFT JOIN categorization_rules and show the rule's CURRENT name
//     (`COALESCE(r.name, h.rule_name)`) so the history reads more nicely. That is exactly the drift
//     rule_name exists to prevent, and it is caught here and nowhere else — verified by applying it
//     to ruleExecutionColumns and watching this test, and only this test, go red.
func TestRuleExecutionHistory_SnapshotSurvivesARenameOfThePersistedRule_Integration(t *testing.T) {
	f := seedRuleHistoryFixture(t)
	ctx := context.Background()

	txn := f.newTransaction("Blue Bottle Coffee")
	f.runSyncPath(t, []domain.Transaction{txn})

	// The rename happens on the PERSISTED row, through an UPDATE, after the trail was written —
	// which is what a user renaming and re-targeting a rule in the UI does to it.
	tag, err := f.tx.Exec(ctx, `
		UPDATE categorization_rules
		SET name = 'Coffee is Entertainment', target_pfc_primary = 'ENTERTAINMENT'
		WHERE id = $1 AND user_id = $2
	`, f.ruleID, f.userID)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected(), "the rename must have landed, or the test proves nothing")

	var liveName string
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT name FROM categorization_rules WHERE id = $1`, f.ruleID).Scan(&liveName))
	require.Equal(t, "Coffee is Entertainment", liveName,
		"guard against a vacuous pass: the live rule really is renamed at this point")

	// Half 1 — the stored bytes, read back out of Postgres.
	var storedName string
	var storedSnapshot []byte
	require.NoError(t, f.tx.QueryRow(ctx, `
		SELECT rule_name, definition_snapshot FROM rule_execution_history
		WHERE rule_id = $1 AND transaction_id = $2
	`, f.ruleID, txn.ID).Scan(&storedName, &storedSnapshot))

	assert.Equal(t, "Coffee is Food", storedName,
		"the trail records the rule AS IT WAS when it ran; the rename must not reach backwards")

	var snap map[string]any
	require.NoError(t, json.Unmarshal(storedSnapshot, &snap))
	assert.Equal(t, "FOOD_AND_DRINK", snap["target_pfc_primary"],
		"the snapshot is the definition at execution time, not the definition now")

	// Half 2 — the production READ, which is the one a join could break.
	rows, _, err := repository.NewRuleExecutionHistoryRepository(f.tx).
		GetByUserID(ctx, f.userID, 20, 0)
	require.NoError(t, err)

	var found *domain.RuleExecution
	for _, r := range rows {
		if r.RuleID != nil && *r.RuleID == f.ruleID {
			found = r
		}
	}
	require.NotNil(t, found, "the renamed rule's execution must still be in the user's history")
	require.NotNil(t, found.RuleName)
	assert.Equal(t, "Coffee is Food", *found.RuleName,
		"the read must serve the trail's own copy, never the live rule's current name")

	var readSnap map[string]any
	require.NoError(t, json.Unmarshal(found.DefinitionSnapshot, &readSnap))
	assert.Equal(t, "FOOD_AND_DRINK", readSnap["target_pfc_primary"])
}

// TestRuleExecutionHistory_DeletedRuleStillReadable_Integration is why the read is keyed on
// user_id. Deleting a rule in the UI is a HARD delete and the FK is ON DELETE SET NULL, so a read
// keyed on rule_id would make the executions of every deleted rule invisible — retention that reads
// as deletion.
func TestRuleExecutionHistory_DeletedRuleStillReadable_Integration(t *testing.T) {
	f := seedRuleHistoryFixture(t)
	ctx := context.Background()

	f.runSyncPath(t, []domain.Transaction{f.newTransaction("Blue Bottle Coffee")})

	require.NoError(t, repository.NewCategorizationRuleRepository(f.tx).Delete(ctx, f.ruleID))

	rows, total, err := repository.NewRuleExecutionHistoryRepository(f.tx).
		GetByUserID(ctx, f.userID, 20, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total, "the deleted rule's execution is still the user's history")

	var orphan *domain.RuleExecution
	for _, r := range rows {
		if r.RuleID == nil {
			orphan = r
		}
	}
	require.NotNil(t, orphan, "the deleted rule's row must come back with rule_id NULL, not vanish")
	require.NotNil(t, orphan.RuleName)
	assert.Equal(t, "Coffee is Food", *orphan.RuleName,
		"rule_name is what a reader shows in place of the rule that is gone")
	assert.NotEmpty(t, orphan.DefinitionSnapshot)
}

// TestRuleExecutionHistory_ReadIsTenancyScoped_Integration — one user's automation history is never
// another's.
func TestRuleExecutionHistory_ReadIsTenancyScoped_Integration(t *testing.T) {
	f := seedRuleHistoryFixture(t)
	ctx := context.Background()

	f.runSyncPath(t, []domain.Transaction{f.newTransaction("Blue Bottle Coffee")})

	stranger := uuid.New().String()
	_, err := f.tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, stranger, "rule-hist-other|"+stranger, "rule-hist-other-"+stranger+"@example.com")
	require.NoError(t, err)

	svc := NewRuleExecutionHistoryService(repository.NewRuleExecutionHistoryRepository(f.tx))

	mine, mineTotal, err := svc.GetUserExecutionHistory(ctx, f.userID, 20, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, mineTotal)
	assert.Len(t, mine, 2)

	theirs, theirTotal, err := svc.GetUserExecutionHistory(ctx, stranger, 20, 0)
	require.NoError(t, err)
	assert.Zero(t, theirTotal)
	assert.Empty(t, theirs)
}

// TestRuleExecutionHistory_TagNoOpRecordsNothingButStillCounts_Integration is what survives of the
// product ruling after R73 (review round 1, D2), against a live database.
//
// ⚠ THIS TEST USED TO ASSERT ZERO ROWS AND IT WAS WRONG. Its fixture hand-built a transaction whose
// category already matched the rule's target, and concluded that nothing changed. But the category
// rule also flips `reviewed` false→true, permanently — so ONE row is correct, and asserting zero
// pinned the D2 defect in place. The tag rule genuinely contributes nothing and still records
// nothing, which is the distinction that remains.
//
// ⚠ THE FIXTURE IS NOT PRODUCTION-SHAPED, and that is now stated rather than implied: nothing on
// the processAddedTransactions path sets CategoryOverrideKind, Tags or TagsSource before rules run
// (services/plaid_mapper.go sets none of them). This constructs the state deliberately to reach the
// no-op branch. See buildRuleExecutionRecords' doc comment.
func TestRuleExecutionHistory_TagNoOpRecordsNothingButStillCounts_Integration(t *testing.T) {
	f := seedRuleHistoryFixture(t)
	ctx := context.Background()

	txn := f.newTransaction("Blue Bottle Coffee")
	txn.CategoryOverrideKind = strPtr("pfc")
	txn.PFCPrimaryOverride = strPtr("FOOD_AND_DRINK")
	// Already carrying the tag rule's tag, already attributed to THAT rule — the only shape that
	// reaches the tagsBefore clause (Source:"manual" exits one clause earlier and tests nothing).
	txn.Tags = []string{"caffeine"}
	txn.TagsSource = []domain.TagSource{{Tag: "caffeine", Source: "rule", RuleID: &f.tagRuleID}}

	f.runSyncPath(t, []domain.Transaction{txn})

	rows, total, err := repository.NewRuleExecutionHistoryRepository(f.tx).
		GetByUserID(ctx, f.userID, 20, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total,
		"the category rule flipped `reviewed` (R73), so it records; the tag rule changed nothing")
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].RuleID)
	assert.Equal(t, f.ruleID, *rows[0].RuleID, "the row belongs to the category rule, not the tag rule")
	assert.Equal(t, domain.RuleExecutionActionCategory, rows[0].ActionType)

	for _, id := range []string{f.ruleID, f.tagRuleID} {
		var matchCount int64
		require.NoError(t, f.tx.QueryRow(ctx,
			`SELECT match_count FROM categorization_rules WHERE id = $1`, id).Scan(&matchCount))
		assert.EqualValues(t, 1, matchCount,
			"a no-op match is still a match — this is what separates match_count from history")
	}
}

// TestRuleExecutionHistory_SettlementDoesNotShowTheSameChargeTwice_Integration is review round 1's
// D3 (ruling R74), and the duplicate it pins was live.
//
// Rules run before CreateBatch, so a pending transaction gets a trail row; when it settles, the
// posted twin is a DIFFERENT id, gets its own trail row from the same rule, and the pending row is
// tombstoned. unique_rule_transaction UNIQUE (rule_id, transaction_id) never fires, because the ids
// differ. One charge, two "the automation ran" rows, one of them pointing at a transaction every
// other read in the app hides.
func TestRuleExecutionHistory_SettlementDoesNotShowTheSameChargeTwice_Integration(t *testing.T) {
	f := seedRuleHistoryFixture(t)
	ctx := context.Background()

	pending := f.newTransaction("Blue Bottle Coffee")
	f.runSyncPath(t, []domain.Transaction{pending})

	// It settles: a new row with a new id, and the pending one is tombstoned. The tombstone goes
	// through the PRODUCTION path (TombstoneByExternalID), not a hand-written UPDATE — a bare
	// `SET deleted_at = NOW()` is rejected outright by the transactions_tombstone_consistency CHECK,
	// which is the database saying a fixture must not invent a state production cannot produce.
	posted := f.newTransaction("Blue Bottle Coffee")
	f.runSyncPath(t, []domain.Transaction{posted})

	reason, err := repository.NewTransactionRepository(f.tx, ruleHistoryEncryptor(t)).
		TombstoneByExternalID(ctx, domain.SourcePlaid, *pending.ExternalTransactionID,
			f.userID, domain.TxnDeletedByPlaidSync)
	require.NoError(t, err)
	require.NotEmpty(t, reason, "the pending row must actually have been tombstoned")

	var tombstoned bool
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT deleted_at IS NOT NULL FROM transactions WHERE id = $1`, pending.ID).Scan(&tombstoned))
	require.True(t, tombstoned, "the fixture is only meaningful if the pending row is tombstoned")

	// Both sets of rows are RETAINED — this is a visibility decision, not a deletion.
	assert.Equal(t, 4, f.count(t, ctx,
		`SELECT COUNT(*) FROM rule_execution_history WHERE user_id = $1`, f.userID),
		"the pending row's history is retained; nothing is destroyed")

	rows, total, err := repository.NewRuleExecutionHistoryRepository(f.tx).
		GetByUserID(ctx, f.userID, 20, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total,
		"the default read shows ONE charge: two rules against the posted twin, none against the "+
			"tombstoned pending row")
	require.Len(t, rows, 2, "total and the page must agree — they share visibleRuleExecutions")

	for _, r := range rows {
		require.NotNil(t, r.TransactionID)
		assert.Equal(t, posted.ID, *r.TransactionID,
			"a row pointing at the tombstoned pending transaction leaked into the default read")
	}
}

// TestRuleExecutionHistory_OrphanedTransactionPointerStaysVisible_Integration pins the leg of
// visibleRuleExecutions that the LEFT JOIN's three-valued logic would otherwise supply by accident:
// a hard-deleted transaction leaves transaction_id NULL, and that row is still the user's history.
func TestRuleExecutionHistory_OrphanedTransactionPointerStaysVisible_Integration(t *testing.T) {
	f := seedRuleHistoryFixture(t)
	ctx := context.Background()

	txn := f.newTransaction("Blue Bottle Coffee")
	f.runSyncPath(t, []domain.Transaction{txn})

	// A genuine hard delete, not a tombstone — the GDPR/purge shape. The FK is ON DELETE SET NULL.
	_, err := f.tx.Exec(ctx, `DELETE FROM transactions WHERE id = $1`, txn.ID)
	require.NoError(t, err)

	rows, total, err := repository.NewRuleExecutionHistoryRepository(f.tx).
		GetByUserID(ctx, f.userID, 20, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total, "an orphaned execution is still the user's automation history")
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.Nil(t, r.TransactionID, "the FK set it NULL; the row must not vanish with it")
		require.NotNil(t, r.RuleName)
	}
}

func (f *ruleHistoryFixture) count(t *testing.T, ctx context.Context, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, f.tx.QueryRow(ctx, sql, args...).Scan(&n))
	return n
}

// plainPassthroughEncryptionServiceForRuleHistory is the no-KMS encryption service these tests use,
// matching the repository package's own integration-test helper.
type plainPassthroughEncryptionServiceForRuleHistory struct{}

func (plainPassthroughEncryptionServiceForRuleHistory) Encrypt(plaintext string) (string, error) {
	return plaintext, nil
}

func (plainPassthroughEncryptionServiceForRuleHistory) Decrypt(ciphertext string) (string, error) {
	return ciphertext, nil
}
