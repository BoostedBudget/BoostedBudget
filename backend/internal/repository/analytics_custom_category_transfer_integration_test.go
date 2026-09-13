//go:build integration

package repository

// Issue #1609 Item 11 — a custom category must carry the transfer semantics it declares.
//
// WHY THIS IS AN INTEGRATION TEST AND NOT A pgxmock ONE. The whole defect is a resolution
// question: effective_pfc_primary is NULL for every custom-kind row (migration 000073), so
// COALESCE(t.effective_pfc_primary, '') made EVERY custom-categorised transaction escape the
// `NOT LIKE 'TRANSFER%'` filter by accident of NULL-ness rather than by declaration. A category
// named "Paycheck" and one named "Internal Transfer To Savings" behaved identically. pgxmock
// asserts the query STRING and then hands back rows the test author invented, so it cannot tell
// a correct COALESCE order from an inverted one, nor a correct correlated lookup from one joined
// on the wrong column. Only Postgres can. Everything below therefore runs against a real schema.
//
// THE REAL INCIDENT. A user's paychecks arrive as TRANSFER_IN with negative amounts (money in).
// Cash flow with include_transfers=false reported $0.52 of income across four months, hiding
// ~$22,200. An MCP client proposed "assign a custom category to reclassify them as income" —
// which would have APPEARED to work, because the row would stop being recognised as a transfer,
// not because it became income. This test pins that a custom category nested under TRANSFER_IN
// is treated as a transfer, so that workaround can no longer produce a right-looking wrong answer.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

func TestCashFlow_CustomCategoryResolvesParentPFCPrimaryForTransferFilter_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	accountID := uuid.New().String()
	linkedItemID := uuid.New().String()
	userAccountID := uuid.New().String()

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "cc-transfer-1609-"+userID+"@example.com")

	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, 'ins-cc-transfer-1609', 'depository')
	`, userAccountID, userID)

	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "cc-transfer-item-"+linkedItemID)

	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, $5, 'CC Transfer Checking', 'depository', 'active')
	`, accountID, linkedItemID, userID, userAccountID, "plaid-acc-cc-"+accountID)

	// Four custom categories, differing ONLY in what they declare themselves to be.
	transferCatID := uuid.New().String()   // nested under TRANSFER_IN
	incomeCatID := uuid.New().String()     // nested under INCOME
	unparentedCatID := uuid.New().String() // parent_pfc_primary IS NULL
	deletedCatID := uuid.New().String()    // nested under TRANSFER_IN, then tombstoned

	mustExec("custom_category Internal Transfer To Savings", `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Internal Transfer To Savings 1609', 'TRANSFER_IN')
	`, transferCatID, userID)

	mustExec("custom_category Paycheck", `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Paycheck 1609', 'INCOME')
	`, incomeCatID, userID)

	mustExec("custom_category Misc (no parent)", `
		INSERT INTO custom_categories (id, user_id, name)
		VALUES ($1, $2, 'Misc 1609')
	`, unparentedCatID, userID)

	mustExec("custom_category Old Transfer (tombstoned)", `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary, deleted_at, deleted_reason, deleted_by)
		VALUES ($1, $2, 'Old Transfer 1609', 'TRANSFER_IN', NOW(), 'user_deleted', 'user')
	`, deletedCatID, userID)

	seq := 0
	insertCustom := func(what string, amount string, customCatID string) {
		t.Helper()
		seq++
		id := uuid.New().String()
		ext := "ext-cc-1609-" + id
		mustExec(what, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id,
				source, external_transaction_id, external_transaction_id_blind_idx,
				amount, date, name, pending, pfc_primary,
				category_override_kind, custom_category_override_id, category_source,
				category_override_date, effective_category_name, created_at, updated_at)
			VALUES ($1, $2, $3, $4,
				'plaid', $5, $6,
				$7::numeric, CURRENT_DATE - 5, $8, false, 'TRANSFER_IN',
				'custom', $9, 'manual',
				NOW(), $10, NOW(), NOW())
		`, id, userID, accountID, linkedItemID, ext, ext, amount, what, customCatID, what)
	}

	insertPFC := func(what string, amount string, kind *string, override *string) {
		t.Helper()
		seq++
		id := uuid.New().String()
		ext := "ext-pfc-1609-" + id
		mustExec(what, `
			INSERT INTO transactions (id, user_id, account_id, linked_item_id,
				source, external_transaction_id, external_transaction_id_blind_idx,
				amount, date, name, pending, pfc_primary,
				category_override_kind, pfc_primary_override, category_source,
				created_at, updated_at)
			VALUES ($1, $2, $3, $4,
				'plaid', $5, $6,
				$7::numeric, CURRENT_DATE - 5, $8, false, 'INCOME',
				$9, $10, 'manual',
				NOW(), NOW())
		`, id, userID, accountID, linkedItemID, ext, ext, amount, what, kind, override)
	}

	pfcKind := "pfc"
	transferIn := "TRANSFER_IN"
	incomePrimary := "INCOME"

	// Case 1 — custom category nested under TRANSFER_IN. MUST be excluded.
	insertCustom("custom-under-transfer", "-1000.00", transferCatID)
	// Case 2 — custom category nested under INCOME. MUST be included and count as income.
	insertCustom("custom-under-income", "-2000.00", incomeCatID)
	// Case 3 — custom category with parent_pfc_primary IS NULL. Today's behaviour: included.
	insertCustom("custom-unparented", "-300.00", unparentedCatID)
	// Case 4 — a TOMBSTONED custom category nested under TRANSFER_IN still resolves: excluded.
	insertCustom("custom-under-deleted-transfer", "-400.00", deletedCatID)
	// Case 5 — the non-custom rows, unaffected in either direction.
	insertPFC("no-override-income", "-50.00", nil, nil)
	insertPFC("pfc-override-income", "-70.00", &pfcKind, &incomePrimary)
	insertPFC("pfc-override-transfer", "-60.00", &pfcKind, &transferIn)

	repo := NewAnalyticsRepository(tx)
	filters := domain.CashFlowFilters{
		StartDate:        time.Now().AddDate(0, 0, -30),
		EndDate:          time.Now().AddDate(0, 0, 1),
		IncludeTransfers: false,
		Grouping:         "monthly",
	}

	agg, err := repo.GetCashFlowAggregation(ctx, userID, filters)
	require.NoError(t, err)

	// 2000 (custom under INCOME) + 300 (custom, unparented) + 50 (no override) + 70 (pfc override).
	// The 1000 and the 400 are the behaviour change: both are custom-categorised rows whose
	// category declares itself TRANSFER_IN, and both used to land in this figure.
	require.Equal(t, 4, agg.IncomeCount,
		"expected exactly the four non-transfer inflows; a count of 6 means custom categories are still escaping the transfer filter")
	require.InDelta(t, 2420.00, agg.Income, 0.001,
		"income must exclude the two custom-categorised TRANSFER_IN rows (1000 live category + 400 tombstoned category)")

	// The transfer rows must not have leaked into the expense side either.
	require.Equal(t, 0, agg.ExpenseCount, "no outflows were seeded")
}

// TestCashFlow_CustomCategoryTransferFilter_IncludeTransfersStillIncludesEverything_Integration
// pins that the new resolution is scoped to the include_transfers=false branch: with transfers
// requested, every seeded row still counts. Without this, an over-broad expression that leaked
// into the base WHERE clause would pass the test above for the wrong reason.
func TestCashFlow_CustomCategoryTransferFilter_IncludeTransfersStillIncludesEverything_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	accountID := uuid.New().String()
	linkedItemID := uuid.New().String()
	userAccountID := uuid.New().String()
	catID := uuid.New().String()
	txnID := uuid.New().String()

	mustExec := func(what, sql string, args ...any) {
		t.Helper()
		_, execErr := tx.Exec(ctx, sql, args...)
		require.NoError(t, execErr, "seed %s", what)
	}

	mustExec("user", `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "cc-transfer-inc-1609-"+userID+"@example.com")
	mustExec("user_account", `
		INSERT INTO user_accounts (id, user_id, institution_id, primary_type)
		VALUES ($1, $2, 'ins-cc-inc-1609', 'depository')
	`, userAccountID, userID)
	mustExec("linked_item", `
		INSERT INTO linked_items (id, user_id, item_id, access_token_encrypted)
		VALUES ($1, $2, $3, 'enc-test-token')
	`, linkedItemID, userID, "cc-inc-item-"+linkedItemID)
	mustExec("account", `
		INSERT INTO accounts (id, linked_item_id, user_id, user_account_id, plaid_account_id, name, type, status)
		VALUES ($1, $2, $3, $4, $5, 'CC Inc Checking', 'depository', 'active')
	`, accountID, linkedItemID, userID, userAccountID, "plaid-acc-cc-inc-"+accountID)
	mustExec("custom_category", `
		INSERT INTO custom_categories (id, user_id, name, parent_pfc_primary)
		VALUES ($1, $2, 'Transfer To Savings inc 1609', 'TRANSFER_IN')
	`, catID, userID)
	mustExec("transaction", `
		INSERT INTO transactions (id, user_id, account_id, linked_item_id,
			source, external_transaction_id, external_transaction_id_blind_idx,
			amount, date, name, pending, pfc_primary,
			category_override_kind, custom_category_override_id, category_source,
			category_override_date, effective_category_name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'plaid', $5, $6,
			-1000.00, CURRENT_DATE - 5, 'inflow', false, 'TRANSFER_IN',
		'custom', $7, 'manual', NOW(), 'Transfer To Savings inc 1609', NOW(), NOW())
	`, txnID, userID, accountID, linkedItemID, "ext-cc-inc-1609-"+txnID, "ext-cc-inc-1609-"+txnID, catID)

	repo := NewAnalyticsRepository(tx)
	agg, err := repo.GetCashFlowAggregation(ctx, userID, domain.CashFlowFilters{
		StartDate:        time.Now().AddDate(0, 0, -30),
		EndDate:          time.Now().AddDate(0, 0, 1),
		IncludeTransfers: true,
		Grouping:         "monthly",
	})
	require.NoError(t, err)
	require.Equal(t, 1, agg.IncomeCount, "include_transfers=true must still count the transfer row")
	require.InDelta(t, 1000.00, agg.Income, 0.001)
}

// TestCustomCategory_ParentPFCPrimary_APIRoundTrip_Integration — Issue #1609 Item 11, brief case 6.
// The field is now load-bearing (it decides transfer classification), so "the client can set it and
// read it back" has to be proven end to end through the layers a client actually uses, not asserted
// from the fact that the INSERT names the column. Service on top of a real repository: create with
// a value, read it back, change it, read it back, clear it.
func TestCustomCategory_ParentPFCPrimary_APIRoundTrip_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck

	userID := uuid.New().String()
	_, err = tx.Exec(ctx, `
		INSERT INTO users (id, clerk_user_id, email, subscription_tier, subscription_status, primary_region)
		VALUES ($1, $2, $3, 'premium', 'active', 'iad')
	`, userID, "test|"+userID, "cc-roundtrip-1609-"+userID+"@example.com")
	require.NoError(t, err, "seed user")

	repo := NewCustomCategoryRepository(tx)

	transferIn := "TRANSFER_IN"
	created := &domain.CustomCategory{UserID: userID, Name: "Round Trip 1609", ParentPFCPrimary: &transferIn}
	require.NoError(t, created.Validate())
	require.NoError(t, repo.Create(ctx, created))

	readBack, err := repo.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, readBack.ParentPFCPrimary, "parent_pfc_primary must survive the write")
	require.Equal(t, "TRANSFER_IN", *readBack.ParentPFCPrimary)

	income := "INCOME"
	readBack.ParentPFCPrimary = &income
	require.NoError(t, readBack.Validate())
	// Hierarchy mutations use the transaction-aware writer; Update changes metadata only.
	require.NoError(t, LockBudgetConfiguration(ctx, tx, userID))
	require.NoError(t, repo.UpdateInTx(ctx, tx, readBack))

	afterUpdate, err := repo.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, afterUpdate.ParentPFCPrimary)
	require.Equal(t, "INCOME", *afterUpdate.ParentPFCPrimary)

	// Clearing it is how a client says "this category declares nothing" — it must reach NULL, not
	// the empty string, which the DB CHECK would reject.
	afterUpdate.ParentPFCPrimary = nil
	require.NoError(t, repo.UpdateInTx(ctx, tx, afterUpdate))
	cleared, err := repo.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Nil(t, cleared.ParentPFCPrimary)
}
