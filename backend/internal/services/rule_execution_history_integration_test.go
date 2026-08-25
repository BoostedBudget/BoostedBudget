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

	enc, err := appcrypto.NewFieldEncryptor(plainPassthroughEncryptionServiceForRuleHistory{},
		[]byte("12345678901234567890123456789012"))
	require.NoError(t, err)

	svc := NewRuleApplicationService(repository.NewCategorizationRuleRepository(f.tx))
	svc.SetRuleExecutionHistoryRepository(repository.NewRuleExecutionHistoryRepository(f.tx))

	apps, err := svc.ApplyRulesToBatch(ctx, txns, f.userID)
	require.NoError(t, err)

	insertedIDs, err := repository.NewTransactionRepository(f.tx, enc).CreateBatch(ctx, txns)
	require.NoError(t, err)

	svc.EmitBatchSideEffects(ctx, f.userID, apps, insertedIDs)
	return svc
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
	require.NoError(t, repo.Create(ctx, &domain.RuleExecution{
		RuleID:             &ruleID,
		TransactionID:      &txnID,
		UserID:             f.userID,
		RuleName:           &later,
		ActionType:         domain.RuleExecutionActionBoth,
		Result:             domain.RuleExecutionResultApplied,
		DefinitionSnapshot: json.RawMessage(`{"name":"Renamed after the fact"}`),
	}), "a conflict is not an error — the first execution simply wins")

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

// TestRuleExecutionHistory_NoRowWhenTheRuleChangesNothing_Integration is the user's product ruling
// against a live database: the rule matched, its match count moved, and the trail stayed silent.
func TestRuleExecutionHistory_NoRowWhenTheRuleChangesNothing_Integration(t *testing.T) {
	f := seedRuleHistoryFixture(t)
	ctx := context.Background()

	// Already categorized exactly as the category rule would categorize it, and already carrying
	// the tag rule's tag. Not manual, not reviewed, so the match loop still evaluates it.
	txn := f.newTransaction("Blue Bottle Coffee")
	txn.CategoryOverrideKind = strPtr("pfc")
	txn.PFCPrimaryOverride = strPtr("FOOD_AND_DRINK")
	txn.Tags = []string{"caffeine"}
	txn.TagsSource = []domain.TagSource{{Tag: "caffeine", Source: "manual"}}

	f.runSyncPath(t, []domain.Transaction{txn})

	assert.Zero(t,
		f.count(t, ctx, `SELECT COUNT(*) FROM rule_execution_history WHERE user_id = $1`, f.userID),
		"both rules matched but neither changed anything, so the trail records nothing")

	var matchCount int64
	require.NoError(t, f.tx.QueryRow(ctx,
		`SELECT match_count FROM categorization_rules WHERE id = $1`, f.ruleID).Scan(&matchCount))
	assert.EqualValues(t, 1, matchCount,
		"a no-op match is still a match — this is what separates match_count from history")
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
