//go:build integration

package services

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

// Issue #1684, Defect 2 — the `modified` upsert branch never applied categorization rules.
//
// THE DEFECT. processModifiedTransactions has two storage branches: the update branch (a live row
// was found) and the upsert branch (`existingTxn == nil`, residual (3) of the #1570 contract). The
// added path applies rules before CreateBatch (:1185-1211) and the update branch protects a
// manual/reviewed row, but the upsert branch stored the edit WITHOUT ever rule-categorizing it. A
// transaction whose row was lost by an earlier sync and later re-delivered as `modified` therefore
// landed with no rule category at all, even though the same payload on the `added` path would have
// been categorized.
//
// WHY INTEGRATION. The claim is about what Postgres holds after the write: the category tuple
// (category_override_kind / pfc_primary_override / category_source) and the CHECKs around it.
// pgxmock never sends the SQL to a planner, so it cannot observe the persisted state; and the
// upsert branch's read-back contract (#1586 gap G7) is only real when CreateBatch actually writes
// the persisted id back into the caller's slice.
//
// ⚠ THE TEST MUST SEED A GENUINELY NEW EXTERNAL ID. "No stored row" means no LIVE row:
// GetByExternalID filters `deleted_at IS NULL`, so a TOMBSTONED row can still be the ON CONFLICT
// arbiter. On that conflict path CreateBatch's SET clause excludes the category columns and
// insertedIDs excludes conflict rows, so a persisted-category assertion would be meaningless. The
// new external id here has no arbiter at all, so the row is a fresh INSERT.

// seedModifiedUpsertAccount creates the minimum tenancy graph the sync path needs and returns the
// identifiers plus the plaid_account_id keyed in the accountMap.
func seedModifiedUpsertAccount(t *testing.T, ctx context.Context, tx pgx.Tx) (userID, linkedItemID, accountID, plaidAccountID string) {
	t.Helper()

	userID = uuid.New().String()
	linkedItemID = uuid.New().String()
	accountID = uuid.New().String()
	plaidAccountID = "plaid-acc-" + accountID

	_, err := tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "mod-upsert|"+userID, "mod-upsert-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	_, err = tx.Exec(ctx, `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-mod-upsert-token')
	`, linkedItemID, userID, "mod-upsert-item-"+linkedItemID)
	require.NoError(t, err, "seed linked_item")

	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (id, linked_item_id, user_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, 'Checking', 'depository', 'active')
	`, accountID, linkedItemID, userID, plaidAccountID)
	require.NoError(t, err, "seed account")

	return userID, linkedItemID, accountID, plaidAccountID
}

// TestProcessModifiedTransactions_UpsertBranch_PersistsRuleCategory is the headline RED: a
// `modified` transaction with no live stored row must be rule-categorized ON THE WAY IN, and the
// PERSISTED row must carry that category.
func TestProcessModifiedTransactions_UpsertBranch_PersistsRuleCategory(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	userID, linkedItemID, accountID, plaidAccountID := seedModifiedUpsertAccount(t, ctx, tx)

	// A category rule on the merchant name, exactly the shape the rule history fixture uses.
	_, err = tx.Exec(ctx, `
		INSERT INTO categorization_rules
			(id, user_id, name, enabled, priority, conditions, condition_logic, target_kind,
			 target_pfc_primary, tag_behavior, source)
		VALUES ($1, $2, 'Coffee is Food', true, 10,
		        '[{"type":"merchant_contains","value":"coffee"}]'::jsonb,
		        'AND', 'pfc', 'FOOD_AND_DRINK', 'add', 'manual')
	`, uuid.New().String(), userID)
	require.NoError(t, err, "seed categorization rule")

	enc := ruleHistoryEncryptor(t)
	repo := repository.NewTransactionRepository(tx, enc)

	ruleSvc := NewRuleApplicationService(repository.NewCategorizationRuleRepository(tx))
	ruleSvc.SetRuleExecutionHistoryRepository(repository.NewRuleExecutionHistoryRepository(tx))

	svc := NewTransactionSyncService(
		nil, nil, repo, nil, nil, nil, nil, nil, ruleSvc, nil, nil, nil, nil,
	).(*transactionSyncService)

	merchant := "Blue Bottle Coffee"
	newExternalID := "plaid-mod-upsert-" + uuid.New().String()
	modified := plaid.Transaction{
		TransactionId: newExternalID,
		AccountId:     plaidAccountID,
		Amount:        7.25,
		Date:          "2026-01-15",
		Name:          merchant,
	}
	modified.MerchantName = *plaid.NewNullableString(&merchant)

	require.NoError(t, svc.processModifiedTransactions(ctx,
		[]plaid.Transaction{modified},
		&domain.LinkedItem{ID: linkedItemID, UserID: userID},
		map[string]string{plaidAccountID: accountID}, nil))

	// Real DB read-back. The external id is genuinely new, so this row is a fresh INSERT and the
	// category tuple is the one the rule wrote.
	var kind, pfcPrimary, categorySource *string
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT category_override_kind, pfc_primary_override, category_source
		FROM transactions
		WHERE user_id = $1
		  AND source = 'plaid'
		  AND external_transaction_id_blind_idx = $2
		  AND deleted_at IS NULL
	`, userID, enc.BlindIndex(newExternalID)).Scan(&kind, &pfcPrimary, &categorySource))

	require.NotNil(t, kind,
		"the upsert branch stored the edit WITHOUT rule-categorizing it — the category is NULL")
	assert.Equal(t, "pfc", *kind)
	require.NotNil(t, pfcPrimary, "the rule's target category must be persisted, not only in memory")
	assert.Equal(t, "FOOD_AND_DRINK", *pfcPrimary)
	require.NotNil(t, categorySource)
	assert.Equal(t, string(domain.CategorySourceRule), *categorySource,
		"the persisted category_source must record that a RULE set it, not the provider")
}

// TestProcessModifiedTransactions_UpsertBranch_BatchReadBackIsPerElement_Integration guards the
// #1586 gap-G7 read-back across the batching T5 introduces.
//
// The upsert branch used to write ONE row at a time and read the persisted id out of `batch[0]`.
// Batching it means several rows share one CreateBatch call, so a read-back that still uses
// `batch[0]` hands every element the FIRST row's id. settledCandidates is what feeds
// pullForwardStrandedSettlements, so the repair then runs over [twin0, twin0, …] and leaves every
// other donor's annotations stranded on its tombstoned pending row — silently, since the repair
// reports success.
//
// ⚠ NO EXISTING TEST CATCHES THIS. TestProcessModifiedTransactions_RepairsAgainstThePersistedID
// drives ONE transaction (batch[0] == batch[i]), and
// TestSettledPending_UpdatePathAlsoLearnsThePointer_Integration is repository-level and never
// invokes the service. This drives TWO upsert-branch transactions and asserts the repair reached
// BOTH twins through real Postgres.
func TestProcessModifiedTransactions_UpsertBranch_BatchReadBackIsPerElement_Integration(t *testing.T) {
	pool := connectIntegrationDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	userID, linkedItemID, accountID, plaidAccountID := seedModifiedUpsertAccount(t, ctx, tx)

	enc := ruleHistoryEncryptor(t)
	repo := repository.NewTransactionRepository(tx, enc)

	// Two tombstoned PENDING donors, each carrying a stranded annotation. They are written through
	// CreateBatch and tombstoned through the production TombstoneByExternalID path, so the fixture
	// reaches exactly the state the repair exists for rather than inventing a state production
	// cannot produce.
	donorExternalIDs := []string{
		"plaid-pending-a-" + uuid.New().String(),
		"plaid-pending-b-" + uuid.New().String(),
	}
	donorNotes := []string{"stranded note A", "stranded note B"}

	for i := range donorExternalIDs {
		notes := donorNotes[i]
		donor := domain.Transaction{
			ID:                    uuid.New().String(),
			UserID:                userID,
			AccountID:             accountID,
			LinkedItemID:          linkedItemID,
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &donorExternalIDs[i],
			Amount:                11,
			Date:                  time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
			Name:                  "Pending charge",
			Pending:               true,
			UserNotes:             &notes,
		}
		_, err := repo.CreateBatch(ctx, []domain.Transaction{donor})
		require.NoError(t, err, "seed donor %d", i)

		reason, err := repo.TombstoneByExternalID(ctx, domain.SourcePlaid, donorExternalIDs[i], userID, domain.TxnDeletedByPlaidSync)
		require.NoError(t, err, "tombstone donor %d", i)
		require.Equal(t, domain.TxnDeletedProviderRemovedPending, reason,
			"the donor must be a SETTLEMENT donor, not a reversal — the repair only reads pending removals")
	}

	// No rule service: this test isolates the read-back, and rules would otherwise rewrite the
	// category the repair moves.
	svc := NewTransactionSyncService(
		nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	).(*transactionSyncService)

	modified := make([]plaid.Transaction, 0, len(donorExternalIDs))
	for i := range donorExternalIDs {
		twin := plaid.Transaction{
			TransactionId: "plaid-posted-" + uuid.New().String(),
			AccountId:     plaidAccountID,
			Amount:        11,
			Date:          "2026-01-12",
			Name:          "Posted charge",
		}
		pending := donorExternalIDs[i]
		twin.PendingTransactionId = *plaid.NewNullableString(&pending)
		modified = append(modified, twin)
	}

	require.NoError(t, svc.processModifiedTransactions(ctx, modified,
		&domain.LinkedItem{ID: linkedItemID, UserID: userID},
		map[string]string{plaidAccountID: accountID}, nil))

	// Each twin must carry ITS OWN donor's note. Correct per-element read-back hands the repair
	// [twin0.id, twin1.id]; a batch[0] read-back hands it [twin0.id, twin0.id] and leaves the
	// second donor's annotation stranded.
	for i := range modified {
		var notes *string
		require.NoError(t, tx.QueryRow(ctx, `
			SELECT user_notes FROM transactions
			WHERE user_id = $1
			  AND source = 'plaid'
			  AND external_transaction_id_blind_idx = $2
			  AND deleted_at IS NULL
		`, userID, enc.BlindIndex(modified[i].TransactionId)).Scan(&notes))

		require.NotNil(t, notes,
			"twin %d was not repaired — the batched read-back did not carry its persisted id to "+
				"the settlement repair", i)
		assert.Equal(t, donorNotes[i], *notes)
	}
}
