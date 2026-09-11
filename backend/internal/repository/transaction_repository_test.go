package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestTransactionSelectColumns guards against duplicate, missing, or reordered columns
// in the transactionSelectColumns slice that feeds scanTransactions.
// scanTransactions.Scan() is positional — order matters as much as count.
func TestTransactionSelectColumns(t *testing.T) {
	// Canonical expected order must exactly match the Scan() destinations in scanTransactions.
	expectedColumns := []string{
		"id", "user_id", "account_id", "linked_item_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name", "merchant_id",
		"pfc_primary", "pfc_detailed", "pfc_confidence_level", "pfc_category_icon_url",
		"payment_channel", "pending", "user_notes", "created_at", "updated_at",
		"tags", "tags_source",
		"category_override_kind", "pfc_primary_override", "pfc_detailed_override",
		"custom_category_override_id", "category_source", "category_override_date",
		"effective_pfc_primary", "effective_pfc_detailed", "effective_category_name",
		"reviewed", "reviewed_at",
		// #1569 tombstone provenance, appended last so no existing scan offset moves.
		"deleted_at", "deleted_reason", "deleted_by",
	}

	t.Run("exact order and count match scanTransactions destinations", func(t *testing.T) {
		assert.Equal(t, expectedColumns, transactionSelectColumns,
			"transactionSelectColumns order must match the positional Scan() in scanTransactions")
	})

	// #1569 round 1. transactionSelectColumns and scanTransactionDestSliceWithState are coupled
	// POSITIONALLY, and the test above only compares the column list to a hardcoded copy of itself —
	// it never touches the destination slice. Four hand-maintained copies of that slice existed
	// before #1569; consolidating them removed the drift-between-copies risk and left the survivor
	// with no guard at all. This is the cheap half of the fix (a length check catches an
	// added-to-one-not-the-other mistake); the half that catches a same-typed SWAP is
	// TestScanDestinations_MapColumnsToTheRightFields_Integration, because only a real row can
	// reveal that pfc_primary landed in PFCDetailed.
	t.Run("scan destination count matches the column list", func(t *testing.T) {
		repo := &transactionRepository{}
		dest, _ := repo.scanTransactionDestSliceWithState(&domain.Transaction{})
		assert.Len(t, dest, len(transactionSelectColumns),
			"scanTransactionDestSliceWithState must have exactly one destination per column in "+
				"transactionSelectColumns; a mismatch is a runtime Scan error on every read")
	})

	t.Run("no duplicate column names", func(t *testing.T) {
		seen := make(map[string]bool, len(transactionSelectColumns))
		for _, col := range transactionSelectColumns {
			assert.False(t, seen[col], "duplicate column in transactionSelectColumns: %q", col)
			seen[col] = true
		}
	})
}

// TestTransactionRepository_CreateBatch_Success tests batch transaction creation
func TestTransactionRepository_CreateBatch_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID1 := "plaid_tx_1"
	externalID2 := "plaid_tx_2"
	transactions := []domain.Transaction{
		{
			UserID:                "user-123",
			AccountID:             "account-123",
			LinkedItemID:          "item-123",
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID1,
			Amount:                -50.00,
			Date:                  time.Now(),
			Name:                  "Transaction 1",
		},
		{
			UserID:                "user-123",
			AccountID:             "account-123",
			LinkedItemID:          "item-123",
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID2,
			Amount:                -25.00,
			Date:                  time.Now(),
			Name:                  "Transaction 2",
		},
	}

	// Expect batch INSERT with multiple value sets (33 fields per transaction * 2 transactions = 66 args)
	// Issue #171: user_account_id; Issue #1123: effective_category_name; Issue #1163: merchant_id
	// Issue #1203: category override columns (4) + category_source + reviewed + reviewed_at
	// Issue #1213: RETURNING id, (xmax::text::bigint = 0) AS inserted
	// Issue #1578: pending_transaction_id_blind_idx
	args := make([]interface{}, 66)
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}
	rows := pgxmock.NewRows([]string{"id", "inserted", "external_transaction_id_blind_idx"}).
		AddRow("test-id-1", true, nil).
		AddRow("test-id-2", true, nil)
	mock.ExpectQuery("INSERT INTO transactions").
		WithArgs(args...).
		WillReturnRows(rows)

	_, err = repo.CreateBatch(ctx, transactions)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_Update_Success tests transaction update
func TestTransactionRepository_Update_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "plaid_tx_123"
	transaction := &domain.Transaction{
		ID:                    "tx-uuid-123",
		UserID:                "user-123",
		AccountID:             "account-123",
		LinkedItemID:          "item-123",
		Source:                domain.SourcePlaid,
		ExternalTransactionID: &externalID,
		Amount:                -55.00, // Updated amount
		Date:                  time.Now(),
		Name:                  "Starbucks - Updated",
		Pending:               false,
	}

	// Expect UPDATE query (18 args: merchant_id added at $6, shifts all subsequent params;
	// #1578 appended pending_transaction_id_blind_idx at $18)
	mock.ExpectExec("UPDATE transactions SET").
		WithArgs(
			transaction.Amount,
			pgxmock.AnyArg(), // date
			pgxmock.AnyArg(), // authorized_date
			transaction.Name,
			pgxmock.AnyArg(), // merchant_name
			pgxmock.AnyArg(), // merchant_id
			pgxmock.AnyArg(), // pfc_primary
			pgxmock.AnyArg(), // pfc_detailed
			pgxmock.AnyArg(), // pfc_confidence_level
			pgxmock.AnyArg(), // pfc_category_icon_url
			pgxmock.AnyArg(), // payment_channel
			transaction.Pending,
			pgxmock.AnyArg(), // external_metadata
			pgxmock.AnyArg(), // user_notes
			pgxmock.AnyArg(), // updated_at
			transaction.ID,
			transaction.UserID, // $17: user_id scope
			pgxmock.AnyArg(),   // $18: pending_transaction_id_blind_idx (#1578)
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.Update(ctx, transaction)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserID_Success tests retrieving transactions by user
func TestTransactionRepository_GetByUserID_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID1 := "plaid_tx_1"
	extID2 := "plaid_tx_2"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID1, nil,
			-50.00, now, nil, "Transaction 1", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		).
		AddRow(
			"tx-2", userID, "account-2", "item-1",
			"plaid", &extID2, nil,
			-25.00, now, nil, "Transaction 2", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Expect SELECT query
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE user_id").
		WithArgs(userID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserID(ctx, userID, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 2)
	assert.Equal(t, "Transaction 1", transactions[0].Name)
	assert.Equal(t, "Transaction 2", transactions[1].Name)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByAccountID_Success tests retrieving transactions by account via chain-aware subquery (Issue #1109).
func TestTransactionRepository_GetByAccountID_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	accountID := "account-123"

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", "user-1", accountID, "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Expect chain-aware SELECT query (subquery expands via user_account_id)
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE .+user_account_id").
		WithArgs(accountID).
		WillReturnRows(rows)

	transactions, err := repo.GetByAccountID(ctx, accountID)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, accountID, transactions[0].AccountID)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByAccountID_ChainAware verifies chain-aware subquery returns pre-relink transactions from replaced accounts (Issue #1109, W3-G4).
func TestTransactionRepository_GetByAccountID_ChainAware(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	activeAccountID := "account-active-456"
	replacedAccountID := "account-replaced-123"

	extID := "plaid_tx_chain"
	now := time.Now()

	// Transaction with replaced account_id (simulates user_account_id subquery expansion)
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-chain-1", "user-1", replacedAccountID, "item-1",
			"plaid", &extID, nil,
			-99.00, now, nil, "Pre-Relink Transaction", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Chain subquery includes replaced account in result
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE .+user_account_id").
		WithArgs(activeAccountID).
		WillReturnRows(rows)

	transactions, err := repo.GetByAccountID(ctx, activeAccountID)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, replacedAccountID, transactions[0].AccountID)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetBySource_Success tests retrieving transactions by source
func TestTransactionRepository_GetBySource_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	source := domain.SourcePlaid

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Expect SELECT query (source is passed as string, not domain type)
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE source").
		WithArgs(string(source), userID).
		WillReturnRows(rows)

	transactions, err := repo.GetBySource(ctx, source, userID)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, source, transactions[0].Source)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_NoFilters tests retrieving transactions without filters
func TestTransactionRepository_GetByUserIDWithFilters_NoFilters(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{} // No filters

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Expect SELECT query with only user_id filter (FROM transactions t with t. prefix)
	mock.ExpectQuery("SELECT (.+) FROM transactions t WHERE t.user_id").
		WithArgs(userID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_DateRange tests filtering by date range
func TestTransactionRepository_GetByUserIDWithFilters_DateRange(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0
	startDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)
	filters := domain.TransactionFilters{
		StartDate: &startDate,
		EndDate:   &endDate,
	}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	txDate := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-50.00, txDate, nil, "Transaction 1", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Expect SELECT query with date range filter (FROM transactions t, t.-qualified columns)
	mock.ExpectQuery(`SELECT (.+) FROM transactions t WHERE t.user_id(.+)AND t.date >= (.+)AND t.date <= `).
		WithArgs(userID, startDate, endDate, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_AccountID tests filtering by account.
// Issue #1109: expects chain-expanded WHERE using user_account_id subquery.
func TestTransactionRepository_GetByUserIDWithFilters_AccountID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	accountID := "account-456"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		AccountID: &accountID,
	}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, accountID, "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Chain-expanded WHERE clause (account_id appears twice for subquery; t. prefix on cols)
	mock.ExpectQuery(`SELECT (.+) FROM transactions t WHERE t.user_id(.+)AND \(t.user_account_id`).
		WithArgs(userID, accountID, accountID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, accountID, transactions[0].AccountID)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_ChainExpand_AccountID verifies active account_id filter returns pre-relink transactions from replaced accounts (Issue #1109).
func TestTransactionRepository_ChainExpand_AccountID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-chain-001"
	activeAccountID := "account-active-new"
	replacedAccountID := "account-replaced-old"
	limit := 20
	offset := 0

	// Filter by active account ID (post-relink)
	filters := domain.TransactionFilters{
		AccountID: &activeAccountID,
	}

	extID := "plaid_tx_prerelink"
	now := time.Now()

	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-prerelink-1", userID, replacedAccountID, "item-1",
			"plaid", &extID, nil,
			-100.00, now, nil, "Pre-Relink Txn", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Chain-expanded WHERE clause with activeAccountID twice (subquery + fallback; t. prefix)
	mock.ExpectQuery(`SELECT (.+) FROM transactions t WHERE t.user_id(.+)AND \(t.user_account_id = \(SELECT user_account_id FROM accounts WHERE id = \$\d+`).
		WithArgs(userID, activeAccountID, activeAccountID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	require.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, replacedAccountID, transactions[0].AccountID)

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestTransactionRepository_ChainExpand_AccountIDs verifies chain-expansion for multi-value account_ids filter (Issue #1109).
func TestTransactionRepository_ChainExpand_AccountIDs(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-chain-002"
	activeAccountID1 := "account-active-A"
	activeAccountID2 := "account-active-B"
	replacedAccountID := "account-replaced-X"
	limit := 20
	offset := 0

	filters := domain.TransactionFilters{
		AccountIDs: []string{activeAccountID1, activeAccountID2},
	}

	extID := "plaid_tx_prerelink_multi"
	now := time.Now()

	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-prerelink-2", userID, replacedAccountID, "item-1",
			"plaid", &extID, nil,
			-200.00, now, nil, "Pre-Relink Txn Multi", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Chain-expanded WHERE with IN subquery (args appear twice; t. prefix)
	mock.ExpectQuery(`SELECT (.+) FROM transactions t WHERE t.user_id(.+)AND \(t.user_account_id IN \(SELECT user_account_id FROM accounts WHERE id IN`).
		WithArgs(userID, activeAccountID1, activeAccountID2, activeAccountID1, activeAccountID2, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	require.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, replacedAccountID, transactions[0].AccountID)

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestTransactionRepository_GetByUserIDWithFilters_Search tests searching by merchant/name
func TestTransactionRepository_GetByUserIDWithFilters_Search(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	searchQuery := "starbucks"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		Search: &searchQuery,
	}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	merchantName := "Starbucks Coffee"
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-5.49, now, nil, "Starbucks", &merchantName, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Expect SELECT query with ILIKE search (3 ILIKE conditions, single pattern arg via JOINs)
	// Search now includes effectiveMerchantNameExpr, requiring merchantSortJoins and one $N arg.
	mock.ExpectQuery(`SELECT (.+) FROM transactions t.+LEFT JOIN merchant_name_overrides.+WHERE t.user_id(.+)AND \(t.name ILIKE`).
		WithArgs(userID, "%"+searchQuery+"%", limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Contains(t, transactions[0].Name, "Starbucks")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_PFCPrimaries tests filtering by PFC primary enum(s)
func TestTransactionRepository_GetByUserIDWithFilters_PFCPrimaries(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		PFCPrimaries: []string{"FOOD_AND_DRINK"},
	}

	extID := "plaid_tx_1"
	now := time.Now()
	pfcPrimary := "FOOD_AND_DRINK"

	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-25.00, now, nil, "Restaurant", nil, nil,
			&pfcPrimary, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	mock.ExpectQuery(`SELECT (.+) FROM transactions t WHERE t.user_id(.+)AND \(t.effective_pfc_primary IN`).
		WithArgs(userID, pgxmock.AnyArg(), limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_Pending tests filtering by pending status
func TestTransactionRepository_GetByUserIDWithFilters_Pending(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pendingOnly := true
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		Pending: &pendingOnly,
	}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-100.00, now, nil, "Pending Transaction", nil, nil,
			nil, nil, nil, nil,
			nil, true, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Expect SELECT query with pending filter (t. prefix)
	mock.ExpectQuery(`SELECT (.+) FROM transactions t WHERE t.user_id(.+)AND t.pending`).
		WithArgs(userID, pendingOnly, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.True(t, transactions[0].Pending)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_MultipleFilters tests combining multiple filters
func TestTransactionRepository_GetByUserIDWithFilters_MultipleFilters(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	accountID := "account-456"
	startDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	pendingOnly := false
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		AccountID: &accountID,
		StartDate: &startDate,
		Pending:   &pendingOnly,
	}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	txDate := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	now := time.Now()

	// Mock result rows
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, accountID, "item-1",
			"plaid", &extID, nil,
			-50.00, txDate, nil, "Transaction 1", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Issue #1109: chain-expanded account_id filter uses subquery; accountID appears twice. (t. prefix)
	mock.ExpectQuery(`SELECT (.+) FROM transactions t WHERE t.user_id(.+)AND \(t.user_account_id(.+)AND t.date >=(.+)AND t.pending`).
		WithArgs(userID, accountID, accountID, startDate, pendingOnly, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, accountID, transactions[0].AccountID)
	assert.False(t, transactions[0].Pending)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_CountByUserID_NoFilters tests counting transactions without filters
func TestTransactionRepository_CountByUserID_NoFilters(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	filters := domain.TransactionFilters{}

	// Mock result rows
	rows := pgxmock.NewRows([]string{"count"}).AddRow(42)

	// Expect COUNT query
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM transactions t WHERE t\.user_id`).
		WithArgs(userID).
		WillReturnRows(rows)

	count, err := repo.CountByUserID(ctx, userID, filters)
	assert.NoError(t, err)
	assert.Equal(t, 42, count)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_CountByUserID_WithFilters tests counting transactions with filters
func TestTransactionRepository_CountByUserID_WithFilters(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	accountID := "account-456"
	pendingOnly := true
	filters := domain.TransactionFilters{
		AccountID: &accountID,
		Pending:   &pendingOnly,
	}

	// Mock result rows
	rows := pgxmock.NewRows([]string{"count"}).AddRow(5)

	// Issue #1109: chain-expanded account_id filter; accountID appears twice.
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM transactions t WHERE t\.user_id(.+)AND \(t\.user_account_id(.+)AND t\.pending`).
		WithArgs(userID, accountID, accountID, pendingOnly).
		WillReturnRows(rows)

	count, err := repo.CountByUserID(ctx, userID, filters)
	assert.NoError(t, err)
	assert.Equal(t, 5, count)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByExternalID_Success tests retrieving a transaction by external ID
func TestTransactionRepository_GetByExternalID_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "plaid_tx_123"
	userID := "user-123"
	source := domain.SourcePlaid

	// Use pointers for nullable columns (pgxmock v4 requirement)
	merchantName := "Starbucks Coffee"
	categoryJSON := `["Food and Drink", "Coffee"]`
	now := time.Now()

	// Mock result row
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-uuid-123", userID, "account-456", "item-789",
			"plaid", &externalID, nil,
			-25.50, now, nil, "Starbucks", &merchantName, nil,
			&categoryJSON, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// Expect SELECT query with WHERE source = $1 AND external_transaction_id_blind_idx = $2 AND user_id = $3
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE source (.+) AND external_transaction_id_blind_idx (.+) AND user_id").
		WithArgs(string(source), pgxmock.AnyArg(), userID).
		WillReturnRows(rows)

	transaction, err := repo.GetByExternalID(ctx, source, externalID, userID)
	assert.NoError(t, err)
	assert.NotNil(t, transaction)
	assert.Equal(t, "tx-uuid-123", transaction.ID)
	assert.Equal(t, externalID, *transaction.ExternalTransactionID)
	assert.Equal(t, userID, transaction.UserID)
	assert.Equal(t, source, transaction.Source)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByExternalID_NotFound tests when transaction doesn't exist
func TestTransactionRepository_GetByExternalID_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "nonexistent_tx"
	userID := "user-123"
	source := domain.SourcePlaid

	// Expect SELECT query to return pgx.ErrNoRows (which gets converted to ErrTransactionNotFound)
	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE source (.+) AND external_transaction_id_blind_idx (.+) AND user_id").
		WithArgs(string(source), pgxmock.AnyArg(), userID).
		WillReturnError(pgx.ErrNoRows)

	transaction, err := repo.GetByExternalID(ctx, source, externalID, userID)
	assert.Error(t, err)
	assert.Equal(t, ErrTransactionNotFound, err)
	assert.Nil(t, transaction)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_ReturnsTags tests that tags are returned
// TDD RED: This test will FAIL until GetByUserIDWithFilters includes tags in SELECT
func TestTransactionRepository_GetByUserIDWithFilters_ReturnsTags(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{}

	// Use pointers for nullable columns (pgxmock v4 requirement)
	extID := "plaid_tx_1"
	now := time.Now()

	// Mock result rows including tags and tags_source
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			[]byte(`["Food", "Shopping"]`), []byte(`[{"tag":"Food","source":"user"},{"tag":"Shopping","source":"user"}]`),
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	mock.ExpectQuery(`SELECT (.+) FROM transactions t WHERE t\.user_id`).
		WithArgs(userID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	require.Len(t, transactions, 1)

	// Assert tags are returned
	assert.Equal(t, []string{"Food", "Shopping"}, transactions[0].Tags)
	assert.Len(t, transactions[0].TagsSource, 2)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByUserIDWithFilters_ReturnsCategoryOverride tests PFC v2 override fields
// are included in SELECT and properly scanned.
func TestTransactionRepository_GetByUserIDWithFilters_ReturnsCategoryOverride(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{}

	extID := "plaid_tx_1"
	now := time.Now()
	overrideDate := now
	pfcPrimary := "FOOD_AND_DRINK"
	overrideKind := "pfc"
	pfcPrimaryOverride := "GENERAL_MERCHANDISE"

	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil, nil,
			&pfcPrimary, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			&overrideKind, &pfcPrimaryOverride, nil,
			nil, "manual", &overrideDate,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	mock.ExpectQuery(`SELECT (.+) FROM transactions t WHERE t\.user_id`).
		WithArgs(userID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	require.Len(t, transactions, 1)

	assert.NotNil(t, transactions[0].CategoryOverrideKind)
	assert.Equal(t, "pfc", *transactions[0].CategoryOverrideKind)
	assert.NotNil(t, transactions[0].PFCPrimaryOverride)
	assert.Equal(t, "GENERAL_MERCHANDISE", *transactions[0].PFCPrimaryOverride)
	assert.Equal(t, domain.CategorySource("manual"), transactions[0].CategorySourceField)
	assert.NotNil(t, transactions[0].CategoryOverrideDate)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetByID_ReturnsCategoryOverrideFields tests GetByID returns all category fields
// TestTransactionRepository_GetByID_ReturnsCategoryOverrideFields tests GetByID returns PFC v2 override fields.
func TestTransactionRepository_GetByID_ReturnsCategoryOverrideFields(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	txID := "tx-123"
	extID := "plaid_tx_1"
	now := time.Now()
	overrideDate := now
	pfcPrimary := "FOOD_AND_DRINK"
	overrideKind := "pfc"
	pfcPrimaryOverride := "GENERAL_MERCHANDISE"

	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			txID, "user-123", "account-1", "item-1", "plaid", &extID,
			nil, -50.00, now, nil, "Transaction 1", nil, nil,
			&pfcPrimary, nil, nil, nil, nil, false, nil,
			now, now, []byte(`["Food"]`), []byte(`[{"tag":"Food","source":"user"}]`),
			&overrideKind, &pfcPrimaryOverride, nil,
			nil, "manual", &overrideDate,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	mock.ExpectQuery("SELECT (.+) FROM transactions WHERE id").
		WithArgs(txID).
		WillReturnRows(rows)

	transaction, err := repo.GetByID(ctx, txID)
	assert.NoError(t, err)
	require.NotNil(t, transaction)

	assert.Equal(t, []string{"Food"}, transaction.Tags)
	assert.NotNil(t, transaction.CategoryOverrideKind)
	assert.Equal(t, "pfc", *transaction.CategoryOverrideKind)
	assert.NotNil(t, transaction.PFCPrimaryOverride)
	assert.Equal(t, "GENERAL_MERCHANDISE", *transaction.PFCPrimaryOverride)
	assert.Equal(t, domain.CategorySource("manual"), transaction.CategorySourceField)
	assert.NotNil(t, transaction.CategoryOverrideDate)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_UpdateCategory_PFCOverride tests that UpdateCategory
// correctly writes the PFC v2 discriminated union override columns.
func TestTransactionRepository_UpdateCategory_PFCOverride(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	txID := "tx-123"
	userID := "user-123"
	kind := "pfc"
	primary := "FOOD_AND_DRINK"

	// Lock-first (#1569 Task 6, review fix R79): the capture now runs inside a transaction that has
	// already taken the row lock, so the mock has to see Begin -> the FOR UPDATE -> the capturing
	// query -> Commit. A mock cannot prove the lock WORKS — that is
	// TestAnnotationHistory_ConcurrentWriteDoesNotFabricateATransition_Integration's job — but it can
	// prove the sequence is still issued.
	mock.ExpectBegin()
	mock.ExpectExec("FOR UPDATE").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	// #1569 Task 6 (#1579): the statement is now a CTE that also records the prior annotation, so
	// it is a QUERY returning the `updated` CTE's count rather than an Exec whose statement tag is
	// read. The tag would now count the recording INSERT, which is 0 for a write that changed
	// nothing — reading it would report ErrTransactionNotFound for a successful no-op re-save.
	mock.ExpectQuery("UPDATE transactions").
		WithArgs(
			&kind,
			&primary,
			(*string)(nil), // pfc_detailed_override
			(*string)(nil), // custom_category_override_id
			domain.CategorySourceManual,
			pgxmock.AnyArg(), // category_override_date
			pgxmock.AnyArg(), // updated_at
			txID,
			userID,
		).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectCommit()

	err = repo.UpdateCategory(ctx, txID, userID, &kind, &primary, nil, nil, domain.CategorySourceManual)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_SetReviewed marks transactions reviewed, user-scoped (Issue #1203).
func TestTransactionRepository_SetReviewed(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()
	userID := "user-123"
	ids := []string{"tx-1", "tx-2"}

	mock.ExpectBegin()
	mock.ExpectExec("FOR UPDATE").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("UPDATE transactions").
		WithArgs(true, pgxmock.AnyArg(), userID, ids).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(2)))
	mock.ExpectCommit()

	n, err := repo.SetReviewed(ctx, userID, ids, true)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestTransactionRepository_SetReviewed_Unreview clears review state (reviewed_at -> NULL).
func TestTransactionRepository_SetReviewed_Unreview(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()
	userID := "user-123"
	ids := []string{"tx-1"}

	mock.ExpectBegin()
	mock.ExpectExec("FOR UPDATE").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("UPDATE transactions").
		WithArgs(false, (*time.Time)(nil), userID, ids).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectCommit()

	n, err := repo.SetReviewed(ctx, userID, ids, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestTransactionRepository_SetReviewed_EmptyIDs is a no-op without hitting the DB.
func TestTransactionRepository_SetReviewed_EmptyIDs(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	n, err := repo.SetReviewed(context.Background(), "user-123", nil, true)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestTransactionRepository_UpdateCategory_ClearOverride tests that passing nil kind clears overrides.
func TestTransactionRepository_UpdateCategory_ClearOverride(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	txID := "tx-clear"
	userID := "user-123"

	mock.ExpectBegin()
	mock.ExpectExec("FOR UPDATE").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery("UPDATE transactions").
		WithArgs(
			(*string)(nil), // category_override_kind
			(*string)(nil), // pfc_primary_override
			(*string)(nil), // pfc_detailed_override
			(*string)(nil), // custom_category_override_id
			domain.CategorySourceManual,
			pgxmock.AnyArg(), // category_override_date
			pgxmock.AnyArg(), // updated_at
			txID,
			userID,
		).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectCommit()

	err = repo.UpdateCategory(ctx, txID, userID, nil, nil, nil, nil, domain.CategorySourceManual)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_UpdateEffectiveCategoryNameByCustomCategoryID_HappyPath tests the bulk rename
// of effective_category_name for all transactions assigned to a given custom category.
func TestTransactionRepository_UpdateEffectiveCategoryNameByCustomCategoryID_HappyPath(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	customCategoryID := "cc-uuid-456"
	mock.ExpectExec("UPDATE transactions").
		WithArgs(userID, customCategoryID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 3))

	rowsUpdated, err := repo.UpdateEffectiveCategoryNameByCustomCategoryID(ctx, userID, customCategoryID)
	assert.NoError(t, err)
	assert.Equal(t, int64(3), rowsUpdated)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_UpdateEffectiveCategoryNameByCustomCategoryID_ZeroRows tests
// that zero rows updated (no matching transactions) returns 0 without error.
func TestTransactionRepository_UpdateEffectiveCategoryNameByCustomCategoryID_ZeroRows(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	customCategoryID := "cc-uuid-nonexistent"

	mock.ExpectExec("UPDATE transactions").
		WithArgs(userID, customCategoryID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	rowsUpdated, err := repo.UpdateEffectiveCategoryNameByCustomCategoryID(ctx, userID, customCategoryID)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), rowsUpdated)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_UpdateEffectiveCategoryNameByCustomCategoryID_DBError tests
// that a database error is wrapped and returned correctly.
func TestTransactionRepository_UpdateEffectiveCategoryNameByCustomCategoryID_DBError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	customCategoryID := "cc-uuid-456"

	mock.ExpectExec("UPDATE transactions").
		WithArgs(userID, customCategoryID).
		WillReturnError(errors.New("db connection error"))

	_, err = repo.UpdateEffectiveCategoryNameByCustomCategoryID(ctx, userID, customCategoryID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update effective category name")
}

// TestTransactionRepository_GetDistinctPlaidCategories_Success tests retrieving distinct Plaid categories
// Returns primary category from personal_finance_category JSONB (Plaid v2 format)
func TestTransactionRepository_GetDistinctPlaidCategories_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"

	// Mock result rows - distinct primary categories from personal_finance_category JSONB
	rows := pgxmock.NewRows([]string{"cat"}).
		AddRow("FOOD_AND_DRINK").
		AddRow("GENERAL_MERCHANDISE").
		AddRow("TRAVEL")

	// Expect SELECT DISTINCT query that extracts primary category from personal_finance_category
	mock.ExpectQuery("SELECT DISTINCT").
		WithArgs(userID).
		WillReturnRows(rows)

	categories, err := repo.GetDistinctPlaidCategories(ctx, userID)
	assert.NoError(t, err)
	assert.Len(t, categories, 3)
	assert.Contains(t, categories, "FOOD_AND_DRINK")
	assert.Contains(t, categories, "GENERAL_MERCHANDISE")
	assert.Contains(t, categories, "TRAVEL")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetDistinctPlaidCategories_Empty tests when user has no categorized transactions
func TestTransactionRepository_GetDistinctPlaidCategories_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-no-transactions"

	// Mock empty result
	rows := pgxmock.NewRows([]string{"cat"})

	mock.ExpectQuery("SELECT DISTINCT").
		WithArgs(userID).
		WillReturnRows(rows)

	categories, err := repo.GetDistinctPlaidCategories(ctx, userID)
	assert.NoError(t, err)
	assert.Empty(t, categories)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// =============================================================================
// Issue #492: Income Source Matching Service Tests
// =============================================================================

// TestTransactionRepository_GetIncomeSumByCategory_Success tests successful sum calculation for matching category
func TestTransactionRepository_GetIncomeSumByCategory_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := "Paycheck"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 5000.00

	// Category is formatted as JSON array: ["Paycheck"]
	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, category).
		WillReturnRows(rows)

	sum, err := repo.GetIncomeSumByCategory(ctx, userID, category, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetIncomeSumByCategory_NoMatches tests returning 0 when no transactions match
func TestTransactionRepository_GetIncomeSumByCategory_NoMatches(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := "NonExistentCategory"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	// COALESCE ensures 0 is returned for no matches
	rows := pgxmock.NewRows([]string{"sum"}).AddRow(0.0)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, category).
		WillReturnRows(rows)

	sum, err := repo.GetIncomeSumByCategory(ctx, userID, category, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, 0.0, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetIncomeSumByCategory_DBError tests database error handling
func TestTransactionRepository_GetIncomeSumByCategory_DBError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := "Paycheck"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, category).
		WillReturnError(errors.New("connection done"))

	sum, err := repo.GetIncomeSumByCategory(ctx, userID, category, startDate, endDate)
	assert.Error(t, err)
	assert.Equal(t, 0.0, sum)
	assert.Contains(t, err.Error(), "failed to get income sum by category")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetIncomeSumByCategory_PFCPrimary tests that the PFC primary enum
// is matched directly via personal_finance_category->>'primary' = $5.
func TestTransactionRepository_GetIncomeSumByCategory_PFCPrimary(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	category := "INCOME"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 4200.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	// $4 is the PFC primary enum matched against the effective category
	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, category).
		WillReturnRows(rows)

	sum, err := repo.GetIncomeSumByCategory(ctx, userID, category, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetIncomeSumByTag_Success tests successful sum calculation for matching tag
func TestTransactionRepository_GetIncomeSumByTag_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	tag := "salary"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 3000.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, tag).
		WillReturnRows(rows)

	sum, err := repo.GetIncomeSumByTag(ctx, userID, tag, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetIncomeSumByTag_NoMatches tests returning 0 when no transactions have the tag
func TestTransactionRepository_GetIncomeSumByTag_NoMatches(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	tag := "nonexistent-tag"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(0.0)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, tag).
		WillReturnRows(rows)

	sum, err := repo.GetIncomeSumByTag(ctx, userID, tag, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, 0.0, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetIncomeSumByTag_DBError tests database error handling
func TestTransactionRepository_GetIncomeSumByTag_DBError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	tag := "salary"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, tag).
		WillReturnError(errors.New("connection done"))

	sum, err := repo.GetIncomeSumByTag(ctx, userID, tag, startDate, endDate)
	assert.Error(t, err)
	assert.Equal(t, 0.0, sum)
	assert.Contains(t, err.Error(), "failed to get income sum by tag")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// =============================================================================
// GetSpentSumForBudgetCategory Tests (PFC v2 discriminated union)
// =============================================================================

// TestTransactionRepository_GetSpentSumForBudgetCategory_CategoryOnly tests
// category_only match logic using a PFC budget category.
func TestTransactionRepository_GetSpentSumForBudgetCategory_CategoryOnly(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	pfcPrimary := "FOOD_AND_DRINK"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		MatchLogic:   domain.MatchLogicCategoryOnly,
	}
	userID := "user-123"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 250.50

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\).+effective_pfc_primary = \$4`).
		WithArgs(userID, startDate, endDate, pfcPrimary).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, budgetCat, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_PFCPrimaryRollsUpCustomChildren tests
// that a PFC-primary budget rolls up custom categories nested under it (parent_pfc_primary) —
// the query must reference the custom-children lookup subquery (#1203).
func TestTransactionRepository_GetSpentSumForBudgetCategory_PFCPrimaryRollsUpCustomChildren(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	pfcPrimary := "FOOD_AND_DRINK"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		MatchLogic:   domain.MatchLogicCategoryOnly,
	}
	userID := "user-123"
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 320.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	// The PFC-primary clause must OR in custom categories whose parent_pfc_primary = $4 (the
	// direct child) AND, two hops down (Issue #1630 Task B2), a custom grandchild nested via
	// parent_id under that direct child.
	mock.ExpectQuery(`custom_category_override_id IN \(\s*SELECT id FROM custom_categories WHERE user_id = \$1 AND parent_pfc_primary = \$4\s*UNION\s*SELECT g\.id FROM custom_categories g\s*JOIN custom_categories c ON c\.id = g\.parent_id AND c\.user_id = g\.user_id\s*WHERE g\.user_id = \$1 AND c\.parent_pfc_primary = \$4\s*\)`).
		WithArgs(userID, startDate, endDate, pfcPrimary).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, budgetCat, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_TagOnly tests
// tag_only match logic - matches transactions by tag existence regardless of category
func TestTransactionRepository_GetSpentSumForBudgetCategory_TagOnly(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		Tags:         []string{"groceries", "food"},
		MatchLogic:   domain.MatchLogicTagOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 175.25

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, pgxmock.AnyArg()).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, budgetCat, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_And tests
// and match logic - requires BOTH category AND tag to match
func TestTransactionRepository_GetSpentSumForBudgetCategory_And(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	pfcDetailed := "FOOD_AND_DRINK_RESTAURANTS"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		PFCDetailed:  &pfcDetailed,
		Tags:         []string{"dinner", "date-night"},
		MatchLogic:   domain.MatchLogicAnd,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 125.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\).+effective_pfc_primary = \$4 AND effective_pfc_detailed = \$5`).
		WithArgs(userID, startDate, endDate, pfcPrimary, pfcDetailed, pgxmock.AnyArg()).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, budgetCat, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_Or tests
// or match logic - matches if EITHER category OR tag matches
func TestTransactionRepository_GetSpentSumForBudgetCategory_Or(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "ENTERTAINMENT"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		Tags:         []string{"streaming", "subscriptions"},
		MatchLogic:   domain.MatchLogicOr,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 89.97

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\).+effective_pfc_primary = \$4`).
		WithArgs(userID, startDate, endDate, pfcPrimary, pgxmock.AnyArg()).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, budgetCat, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_CustomTagOnly tests that a
// custom-category budget with tag_only match logic matches purely by tag existence —
// the custom category id must NOT appear in the query (Issue #1203, Codex R16).
func TestTransactionRepository_GetSpentSumForBudgetCategory_CustomTagOnly(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	customID := "11111111-1111-1111-1111-111111111111"
	budgetCat := &domain.BudgetCategory{
		CategoryKind:     "custom",
		CustomCategoryID: &customID,
		Tags:             []string{"vacation"},
		MatchLogic:       domain.MatchLogicTagOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 60.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	// tag_only ignores the category entirely: args are user/start/end + tags only,
	// and the custom category id is never bound.
	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\).+tags \?\| \$4`).
		WithArgs(userID, startDate, endDate, pgxmock.AnyArg()).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, budgetCat, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_CustomAnd tests that a
// custom-category budget with AND match logic combines the custom-category rollup clause
// (id or parent_id) with the tag clause (Issue #1203, Codex R16).
func TestTransactionRepository_GetSpentSumForBudgetCategory_CustomAnd(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	customID := "22222222-2222-2222-2222-222222222222"
	budgetCat := &domain.BudgetCategory{
		CategoryKind:     "custom",
		CustomCategoryID: &customID,
		Tags:             []string{"dining"},
		MatchLogic:       domain.MatchLogicAnd,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 45.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	// custom clause binds the id at $4 (id::text = $4 OR parent_id::text = $4) and tags at $5.
	mock.ExpectQuery(`custom_category_override_id IN .+parent_id::text = \$4.+tags \?\| \$5`).
		WithArgs(userID, startDate, endDate, customID, pgxmock.AnyArg()).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, budgetCat, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_EmptyMatchLogic tests
// that empty match_logic defaults to category_only behavior
func TestTransactionRepository_GetSpentSumForBudgetCategory_EmptyMatchLogic(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "GENERAL_MERCHANDISE"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		MatchLogic:   domain.MatchLogic(""), // empty defaults to category_only
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 500.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\).+effective_pfc_primary = \$4`).
		WithArgs(userID, startDate, endDate, pfcPrimary).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, budgetCat, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_NoMatches tests
// that COALESCE returns 0 when no transactions match
func TestTransactionRepository_GetSpentSumForBudgetCategory_NoMatches(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		MatchLogic:   domain.MatchLogicCategoryOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(0.0)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\).+effective_pfc_primary = \$4`).
		WithArgs(userID, startDate, endDate, pfcPrimary).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, budgetCat, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, 0.0, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_DBError tests
// database error handling
func TestTransactionRepository_GetSpentSumForBudgetCategory_DBError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		MatchLogic:   domain.MatchLogicCategoryOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\).+effective_pfc_primary = \$4`).
		WithArgs(userID, startDate, endDate, pfcPrimary).
		WillReturnError(errors.New("connection done"))

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, budgetCat, startDate, endDate)
	assert.Error(t, err)
	assert.Equal(t, 0.0, sum)
	assert.Contains(t, err.Error(), "failed to get spent sum for budget category")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetSpentSumForBudgetCategory_HierarchicalCategory tests
// that PFC primary + detailed combination works correctly
func TestTransactionRepository_GetSpentSumForBudgetCategory_HierarchicalCategory(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	pfcDetailed := "FOOD_AND_DRINK_RESTAURANTS"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		PFCDetailed:  &pfcDetailed,
		MatchLogic:   domain.MatchLogicCategoryOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedSum := 350.00

	rows := pgxmock.NewRows([]string{"sum"}).AddRow(expectedSum)

	mock.ExpectQuery(`SELECT COALESCE\(ABS\(SUM\(amount\)\), 0\).+effective_pfc_primary = \$4 AND effective_pfc_detailed = \$5`).
		WithArgs(userID, startDate, endDate, pfcPrimary, pfcDetailed).
		WillReturnRows(rows)

	sum, err := repo.GetSpentSumForBudgetCategory(ctx, userID, budgetCat, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// =============================================================================
// Issue #497: GetMatchingTransactionStats Tests (Preview Feature)
// =============================================================================

// TestTransactionRepository_GetMatchingTransactionStats_SingleCategory tests
// preview with a PFC primary+detailed category using category_only match logic
func TestTransactionRepository_GetMatchingTransactionStats_SingleCategory(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	pfcDetailed := "FOOD_AND_DRINK_RESTAURANTS"
	tags := []string{}
	matchLogic := domain.MatchLogicCategoryOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedCount := 5
	expectedSum := 250.00

	rows := pgxmock.NewRows([]string{"count", "sum"}).AddRow(expectedCount, expectedSum)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\).+effective_pfc_primary = \$4 AND effective_pfc_detailed = \$5`).
		WithArgs(userID, startDate, endDate, pfcPrimary, pfcDetailed).
		WillReturnRows(rows)

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, "pfc", &pfcPrimary, &pfcDetailed, nil, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetMatchingTransactionStats_CustomCategory tests
// preview with a custom category override
func TestTransactionRepository_GetMatchingTransactionStats_CustomCategory(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	customCategoryID := "custom-cat-uuid-456"
	tags := []string{}
	matchLogic := domain.MatchLogicCategoryOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedCount := 12
	expectedSum := 650.00

	rows := pgxmock.NewRows([]string{"count", "sum"}).AddRow(expectedCount, expectedSum)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\).+custom_category_override_id IN \(SELECT id FROM custom_categories WHERE user_id = \$1 AND \(id::text = \$4 OR parent_id::text = \$4\)\)`).
		WithArgs(userID, startDate, endDate, customCategoryID).
		WillReturnRows(rows)

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, "custom", nil, nil, &customCategoryID, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetMatchingTransactionStats_TagOnly tests
// preview with tag_only match logic
func TestTransactionRepository_GetMatchingTransactionStats_TagOnly(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	tags := []string{"Essential", "groceries"}
	matchLogic := domain.MatchLogicTagOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedCount := 8
	expectedSum := 400.00

	rows := pgxmock.NewRows([]string{"count", "sum"}).AddRow(expectedCount, expectedSum)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, pgxmock.AnyArg()).
		WillReturnRows(rows)

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, "", nil, nil, nil, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetMatchingTransactionStats_And tests
// preview with AND match logic (both category and tags must match)
func TestTransactionRepository_GetMatchingTransactionStats_And(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	pfcDetailed := "FOOD_AND_DRINK_RESTAURANTS"
	tags := []string{"Essential"}
	matchLogic := domain.MatchLogicAnd
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedCount := 3
	expectedSum := 150.00

	rows := pgxmock.NewRows([]string{"count", "sum"}).AddRow(expectedCount, expectedSum)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\).+effective_pfc_primary = \$4 AND effective_pfc_detailed = \$5`).
		WithArgs(userID, startDate, endDate, pfcPrimary, pfcDetailed, pgxmock.AnyArg()).
		WillReturnRows(rows)

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, "pfc", &pfcPrimary, &pfcDetailed, nil, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetMatchingTransactionStats_Or tests
// preview with OR match logic (either category or tags match)
func TestTransactionRepository_GetMatchingTransactionStats_Or(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	pfcDetailed := "FOOD_AND_DRINK_RESTAURANTS"
	tags := []string{"Essential"}
	matchLogic := domain.MatchLogicOr
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	expectedCount := 10
	expectedSum := 500.00

	rows := pgxmock.NewRows([]string{"count", "sum"}).AddRow(expectedCount, expectedSum)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\).+effective_pfc_primary = \$4 AND effective_pfc_detailed = \$5`).
		WithArgs(userID, startDate, endDate, pfcPrimary, pfcDetailed, pgxmock.AnyArg()).
		WillReturnRows(rows)

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, "pfc", &pfcPrimary, &pfcDetailed, nil, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, expectedCount, count)
	assert.Equal(t, expectedSum, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetMatchingTransactionStats_NoMatches tests
// that no matching transactions returns 0, 0
func TestTransactionRepository_GetMatchingTransactionStats_NoMatches(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	tags := []string{}
	matchLogic := domain.MatchLogicCategoryOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	rows := pgxmock.NewRows([]string{"count", "sum"}).AddRow(0, 0)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, pgxmock.AnyArg()).
		WillReturnRows(rows)

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, "pfc", &pfcPrimary, nil, nil, tags, matchLogic, startDate, endDate)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, 0.0, sum)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_GetMatchingTransactionStats_DBError tests
// database error handling
func TestTransactionRepository_GetMatchingTransactionStats_DBError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	tags := []string{}
	matchLogic := domain.MatchLogicCategoryOnly
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	mock.ExpectQuery(`SELECT COUNT\(\*\), COALESCE\(ABS\(SUM\(amount\)\), 0\)`).
		WithArgs(userID, startDate, endDate, pgxmock.AnyArg()).
		WillReturnError(errors.New("connection done"))

	count, sum, err := repo.GetMatchingTransactionStats(ctx, userID, "pfc", &pfcPrimary, nil, nil, tags, matchLogic, startDate, endDate)
	assert.Error(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, 0.0, sum)
	assert.Contains(t, err.Error(), "failed to get matching transaction stats")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// =============================================================================
// Issue #171: Re-Link Handling Tests
// =============================================================================

// TestTransactionRepository_CreateBatch_WithUserAccountID tests batch creation with user_account_id
// Issue #171: UserAccountID is added to transactions for stable queries across re-links
func TestTransactionRepository_CreateBatch_WithUserAccountID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "plaid_tx_relink_1"
	userAccountID := "user-account-uuid-123"
	transactions := []domain.Transaction{
		{
			UserID:                "user-123",
			AccountID:             "account-new-123",
			LinkedItemID:          "item-new-123",
			UserAccountID:         &userAccountID,
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID,
			Amount:                -50.00,
			Date:                  time.Now(),
			Name:                  "Starbucks",
		},
	}

	// Expect batch INSERT (32 fields per transaction; Issue #1203 added override columns + review state)
	// Issue #1213: RETURNING id, (xmax::text::bigint = 0) AS inserted
	args := make([]interface{}, 33) // #1578: pending_transaction_id_blind_idx is the 33rd
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}
	rows := pgxmock.NewRows([]string{"id", "inserted", "external_transaction_id_blind_idx"}).
		AddRow("test-id-relink-1", true, nil)
	mock.ExpectQuery("INSERT INTO transactions").
		WithArgs(args...).
		WillReturnRows(rows)

	_, err = repo.CreateBatch(ctx, transactions)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_CreateBatch_DOUpdateBehavior verifies that ON CONFLICT DO UPDATE is used
// Issue #171: When a re-link occurs, duplicate transactions should UPDATE FKs, not be ignored
func TestTransactionRepository_CreateBatch_DOUpdateBehavior(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "plaid_tx_duplicate_1"
	userAccountID := "user-account-uuid-123"

	// Simulate a re-link scenario: same transaction, new account_id
	transactions := []domain.Transaction{
		{
			UserID:                "user-123",
			AccountID:             "account-NEW-after-relink", // NEW account after re-link
			LinkedItemID:          "item-NEW-after-relink",    // NEW linked item after re-link
			UserAccountID:         &userAccountID,             // Stable user_account_id
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID, // Same external ID (Plaid transaction_id is stable)
			Amount:                -50.00,
			Date:                  time.Now(),
			Name:                  "Starbucks",
		},
	}

	// Build arguments for 32 fields per transaction (Issue #1203 added override columns + review state)
	args := make([]interface{}, 33) // #1578: pending_transaction_id_blind_idx is the 33rd
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}

	// The query should use ON CONFLICT DO UPDATE (not DO NOTHING) and RETURNING xmax
	// Issue #1213: RETURNING id, (xmax::text::bigint = 0) AS inserted
	rows := pgxmock.NewRows([]string{"id", "inserted", "external_transaction_id_blind_idx"}).
		AddRow("test-do-update-id", false, nil) // conflict-updated row: inserted=false
	mock.ExpectQuery("ON CONFLICT.*DO UPDATE").
		WithArgs(args...).
		WillReturnRows(rows)

	_, err = repo.CreateBatch(ctx, transactions)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestTransactionRepository_CreateBatch_PersistsOverrideAndReview verifies that a
// rule-applied override + review state is written on initial insert (Issue #1203,
// the former #TODO-createbatch-overrides bug).
func TestTransactionRepository_CreateBatch_PersistsOverrideAndReview(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	externalID := "plaid_tx_rule_1"
	kind := "pfc"
	primaryOverride := "FOOD_AND_DRINK"
	reviewedAt := time.Now()
	transactions := []domain.Transaction{
		{
			UserID:                "user-123",
			AccountID:             "account-123",
			LinkedItemID:          "item-123",
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID,
			Amount:                -12.00,
			Date:                  time.Now(),
			Name:                  "Coffee",
			CategoryOverrideKind:  &kind,
			PFCPrimaryOverride:    &primaryOverride,
			CategorySourceField:   domain.CategorySourceRule,
			Reviewed:              true,
			ReviewedAt:            &reviewedAt,
		},
	}

	// The INSERT must include the new override + review columns.
	// Issue #1213: RETURNING id, (xmax::text::bigint = 0) AS inserted
	args := make([]interface{}, 33) // #1578: pending_transaction_id_blind_idx is the 33rd
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}
	rows := pgxmock.NewRows([]string{"id", "inserted", "external_transaction_id_blind_idx"}).
		AddRow("test-override-id", true, nil)
	mock.ExpectQuery("category_override_kind, pfc_primary_override, pfc_detailed_override, custom_category_override_id,[\\s]+category_source, reviewed, reviewed_at").
		WithArgs(args...).
		WillReturnRows(rows)

	_, err = repo.CreateBatch(ctx, transactions)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestTransactionRepository_CreateBatch_ReturnsInsertedIDs verifies that CreateBatch
// returns (map[string]bool, error) where the map identifies genuinely inserted rows.
// Issue #1213: This enables callers to gate analytics/match-count side-effects on
// freshly inserted rows only, avoiding over-counting when ON CONFLICT DO UPDATE
// preserves existing category/review fields on a conflicting row.
func TestTransactionRepository_CreateBatch_ReturnsInsertedIDs(t *testing.T) {
	mockDB, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockDB.Close()

	repo := NewTransactionRepository(mockDB, newTestFieldEncryptor(t))
	ctx := context.Background()

	enc := newTestFieldEncryptor(t)
	freshID := uuid.New().String()
	conflictExistingID := uuid.New().String() // the id returned for the conflict row is the EXISTING row's id
	// #1586 gap G7: the conflict row is MINTED with a different id from the one already stored,
	// which is what a re-delivery looks like. Pre-assigning the stored id here (as this test used
	// to) made the write-back a no-op and hid the defect.
	conflictMintedID := uuid.New().String()
	externalID1 := "plaid_tx_fresh"
	externalID2 := "plaid_tx_conflict"

	transactions := []domain.Transaction{
		{
			ID:                    freshID,
			UserID:                "user-123",
			AccountID:             "account-123",
			LinkedItemID:          "item-123",
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID1,
			Amount:                -50.00,
			Date:                  time.Now(),
			Name:                  "Fresh Transaction",
		},
		{
			ID:                    conflictMintedID, // on conflict RETURNING returns the EXISTING row's id instead
			UserID:                "user-123",
			AccountID:             "account-123",
			LinkedItemID:          "item-123",
			Source:                domain.SourcePlaid,
			ExternalTransactionID: &externalID2,
			Amount:                -25.00,
			Date:                  time.Now(),
			Name:                  "Conflict Transaction",
		},
	}

	// RETURNING id, (xmax::text::bigint = 0) AS inserted
	// Fresh insert: our pre-assigned id, xmax=0 (inserted=true)
	// Conflict update: existing row id, xmax!=0 (inserted=false)
	blindFresh := enc.BlindIndex(externalID1)
	blindConflict := enc.BlindIndex(externalID2)
	rows := pgxmock.NewRows([]string{"id", "inserted", "external_transaction_id_blind_idx"}).
		AddRow(freshID, true, &blindFresh).
		AddRow(conflictExistingID, false, &blindConflict)

	args := make([]interface{}, 66) // 33 fields * 2 rows (#1578 added pending_transaction_id_blind_idx)
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}
	mockDB.ExpectQuery("INSERT INTO transactions").
		WithArgs(args...).
		WillReturnRows(rows)

	insertedIDs, err := repo.CreateBatch(ctx, transactions)

	require.NoError(t, err)
	assert.True(t, insertedIDs[freshID], "fresh insert should be in insertedIDs")
	assert.False(t, insertedIDs[conflictExistingID], "conflict-preserved row should NOT be in insertedIDs")
	assert.False(t, insertedIDs[conflictMintedID], "nor under the id the caller minted for it")

	// #1586 gap G7: the two returned facts are DIFFERENT questions and must stay so. insertedIDs
	// answers "is this row new?" and deliberately excludes the conflict row, because
	// EmitBatchSideEffects fires rule analytics off it and a re-delivery must not re-fire them. The
	// slice answers "which row did I write?", and for the conflict row that is the STORED id — the
	// only thing the caller can safely hand to a follow-up statement.
	assert.Equal(t, freshID, transactions[0].ID, "a fresh insert keeps the id it was written with")
	assert.Equal(t, conflictExistingID, transactions[1].ID,
		"a conflict row must come back carrying the PERSISTED id, not the minted one")
	require.NoError(t, mockDB.ExpectationsWereMet())
}

// TestTransactionRepository_GetByUserAccountID tests retrieval by stable user_account_id
// Issue #171: Allows querying transactions across re-links using stable reference
func TestTransactionRepository_GetByUserAccountID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	userAccountID := "user-account-uuid-123"
	now := time.Now()

	externalID1 := "tx-1"
	externalID2 := "tx-2"

	// Build rows matching the SELECT query column order in GetByUserAccountID
	// Note: category_source must be raw string (not *string) for sql.NullString scanning
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "account_id", "linked_item_id", "user_account_id",
		"source", "external_transaction_id", "external_metadata",
		"amount", "date", "authorized_date", "name", "merchant_name", "merchant_id",
		"pfc_primary", "pfc_detailed", "pfc_confidence_level", "pfc_category_icon_url", "payment_channel", "pending", "user_notes",
		"created_at", "updated_at", "tags", "tags_source",
		"category_override_kind", "pfc_primary_override", "pfc_detailed_override",
		"custom_category_override_id", "category_source", "category_override_date",
		"effective_pfc_primary", "effective_pfc_detailed", "effective_category_name",
		"reviewed", "reviewed_at",
		"deleted_at", "deleted_reason", "deleted_by",
	}).
		AddRow(
			"tx-uuid-1", userID, "account-old", "item-old", &userAccountID,
			"plaid", &externalID1, nil,
			-50.00, now, nil, "Starbucks", ptrStr("Starbucks Corp"), nil,
			nil, nil, nil, nil, nil, false, nil,
			now, now, []byte(`[]`), []byte(`[]`),
			nil, nil, nil, nil, "plaid", nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		).
		AddRow(
			"tx-uuid-2", userID, "account-new", "item-new", &userAccountID,
			"plaid", &externalID2, nil,
			-25.00, now, nil, "Coffee", nil, nil,
			nil, nil, nil, nil, nil, false, nil,
			now, now, []byte(`[]`), []byte(`[]`),
			nil, nil, nil, nil, "plaid", nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	mock.ExpectQuery("SELECT.*FROM transactions.*WHERE user_id.*AND user_account_id").
		WithArgs(userID, userAccountID).
		WillReturnRows(rows)

	// This method should exist for stable queries
	transactions, err := repo.GetByUserAccountID(ctx, userID, userAccountID)
	require.NoError(t, err)
	assert.Len(t, transactions, 2)
	// Both transactions have the same user_account_id despite different account_ids
	assert.Equal(t, "account-old", transactions[0].AccountID)
	assert.Equal(t, "account-new", transactions[1].AccountID)
	assert.Equal(t, userAccountID, *transactions[0].UserAccountID)
	assert.Equal(t, userAccountID, *transactions[1].UserAccountID)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// Helper function to create string pointer
func ptrStr(s string) *string {
	return &s
}

// =============================================================================
// Issue #1078: GetTransactionsForBudgetCategory Tests
//
// Two queries per call: count+sum first (via QueryRow), then paginated SELECT
// (via Query). The WHERE clause is a single inline string, so .+ wildcards
// work safely after pgxmock whitespace normalization. Count regex always
// includes COUNT(*) to prevent FIFO ambiguity with the paginated SELECT.
//
// The Or test pins "OR tags" vs the And test's "AND tags" — a missing outer
// paren in ((<catClause>) OR tags ?| $N) would silently rebind precedence
// and OR the date filter with tags instead of the compound expression.
// =============================================================================

// expectGetTxForBudgetCategoryQueries registers the FIFO count + paginated SELECT
// expectations for GetTransactionsForBudgetCategory. Returns the rows builders
// so the caller can AddRow before invoking the function under test.
func expectGetTxForBudgetCategoryQueries(
	mock pgxmock.PgxPoolIface,
	countSQLPattern, selectSQLPattern string,
	countArgs, selectArgs []interface{},
) (*pgxmock.Rows, *pgxmock.Rows) {
	countRows := pgxmock.NewRows([]string{"count", "sum"})
	selectRows := pgxmock.NewRows(transactionSelectColumns)
	mock.ExpectQuery(countSQLPattern).WithArgs(countArgs...).WillReturnRows(countRows)
	mock.ExpectQuery(selectSQLPattern).WithArgs(selectArgs...).WillReturnRows(selectRows)
	return countRows, selectRows
}

// testTxRow returns a complete transaction row for test purposes.
// All fields match transactionSelectColumns order.
func testTxRow(id, userID, accountID, itemID, name string, amount float64, now time.Time) []interface{} {
	return []interface{}{
		id, userID, accountID, itemID,
		"plaid", nil, nil,
		amount, now, nil, name, nil, nil,
		nil, nil, nil, nil,
		nil, false, nil, now, now,
		nil, nil,
		nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		false, nil, // reviewed, reviewed_at
		nil, nil, nil, // #1569 deleted_at, deleted_reason, deleted_by
	}
}

func TestTransactionRepository_GetTransactionsForBudgetCategory_CategoryOnly_PFC(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		MatchLogic:   domain.MatchLogicCategoryOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	limit, offset := 20, 0
	now := time.Now()

	countRows, selectRows := expectGetTxForBudgetCategoryQueries(
		mock,
		`SELECT COUNT\(\*\).+effective_pfc_primary = \$4`,
		`SELECT (.+) FROM transactions WHERE.+effective_pfc_primary = \$4.+LIMIT \$5 OFFSET \$6`,
		[]interface{}{userID, startDate, endDate, pfcPrimary},
		[]interface{}{userID, startDate, endDate, pfcPrimary, limit, offset},
	)
	countRows.AddRow(2, 50.00)
	selectRows.
		AddRow(testTxRow("tx-1", userID, "account-1", "item-1", "Coffee", 25.00, now)...).
		AddRow(testTxRow("tx-2", userID, "account-1", "item-1", "Restaurant", 25.00, now)...)

	transactions, totalCount, totalAmount, err := repo.GetTransactionsForBudgetCategory(ctx, userID, budgetCat, startDate, endDate, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 2)
	assert.Equal(t, 2, totalCount)
	assert.Equal(t, 50.00, totalAmount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionRepository_GetTransactionsForBudgetCategory_CategoryOnly_EmptyMatchLogic(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		MatchLogic:   "", // empty string defaults to category_only
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	limit, offset := 20, 0
	now := time.Now()

	countRows, selectRows := expectGetTxForBudgetCategoryQueries(
		mock,
		`SELECT COUNT\(\*\).+effective_pfc_primary = \$4`,
		`SELECT (.+) FROM transactions WHERE.+effective_pfc_primary = \$4.+LIMIT \$5 OFFSET \$6`,
		[]interface{}{userID, startDate, endDate, pfcPrimary},
		[]interface{}{userID, startDate, endDate, pfcPrimary, limit, offset},
	)
	countRows.AddRow(1, 12.00)
	selectRows.AddRow(testTxRow("tx-1", userID, "account-1", "item-1", "Lunch", 12.00, now)...)

	transactions, totalCount, totalAmount, err := repo.GetTransactionsForBudgetCategory(ctx, userID, budgetCat, startDate, endDate, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, 1, totalCount)
	assert.Equal(t, 12.00, totalAmount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionRepository_GetTransactionsForBudgetCategory_CategoryOnly_HierarchicalPFC(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	pfcDetailed := "FOOD_AND_DRINK_COFFEE"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		PFCDetailed:  &pfcDetailed,
		MatchLogic:   domain.MatchLogicCategoryOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	limit, offset := 20, 0
	now := time.Now()

	countRows, selectRows := expectGetTxForBudgetCategoryQueries(
		mock,
		`SELECT COUNT\(\*\).+effective_pfc_primary = \$4.+effective_pfc_detailed = \$5`,
		`SELECT (.+) FROM transactions WHERE.+effective_pfc_primary = \$4.+effective_pfc_detailed = \$5.+LIMIT \$6 OFFSET \$7`,
		[]interface{}{userID, startDate, endDate, pfcPrimary, pfcDetailed},
		[]interface{}{userID, startDate, endDate, pfcPrimary, pfcDetailed, limit, offset},
	)
	countRows.AddRow(1, 8.50)
	selectRows.AddRow(testTxRow("tx-1", userID, "account-1", "item-1", "Starbucks", 8.50, now)...)

	transactions, totalCount, totalAmount, err := repo.GetTransactionsForBudgetCategory(ctx, userID, budgetCat, startDate, endDate, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, 1, totalCount)
	assert.Equal(t, 8.50, totalAmount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionRepository_GetTransactionsForBudgetCategory_CategoryOnly_Custom(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	customCategoryID := "550e8400-e29b-41d4-a716-446655440000"
	budgetCat := &domain.BudgetCategory{
		CategoryKind:     "custom",
		CustomCategoryID: &customCategoryID,
		MatchLogic:       domain.MatchLogicCategoryOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	limit, offset := 20, 0
	now := time.Now()

	countRows, selectRows := expectGetTxForBudgetCategoryQueries(
		mock,
		`SELECT COUNT\(\*\).+custom_category_override_id IN \(SELECT id FROM custom_categories WHERE user_id = \$1 AND \(id::text = \$4 OR parent_id::text = \$4\)\)`,
		`SELECT (.+) FROM transactions WHERE.+custom_category_override_id IN \(SELECT id FROM custom_categories WHERE user_id = \$1 AND \(id::text = \$4 OR parent_id::text = \$4\)\).+LIMIT \$5 OFFSET \$6`,
		[]interface{}{userID, startDate, endDate, customCategoryID},
		[]interface{}{userID, startDate, endDate, customCategoryID, limit, offset},
	)
	countRows.AddRow(3, 120.00)
	selectRows.
		AddRow(testTxRow("tx-1", userID, "account-1", "item-1", "Groceries", 40.00, now)...).
		AddRow(testTxRow("tx-2", userID, "account-1", "item-1", "Produce", 40.00, now)...).
		AddRow(testTxRow("tx-3", userID, "account-1", "item-1", "Bakery", 40.00, now)...)

	transactions, totalCount, totalAmount, err := repo.GetTransactionsForBudgetCategory(ctx, userID, budgetCat, startDate, endDate, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 3)
	assert.Equal(t, 3, totalCount)
	assert.Equal(t, 120.00, totalAmount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionRepository_GetTransactionsForBudgetCategory_TagOnly(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		Tags:         []string{"essential", "groceries"},
		MatchLogic:   domain.MatchLogicTagOnly,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	limit, offset := 20, 0
	now := time.Now()

	countRows, selectRows := expectGetTxForBudgetCategoryQueries(
		mock,
		`SELECT COUNT\(\*\).+tags \?\| \$4`,
		`SELECT (.+) FROM transactions WHERE.+tags \?\| \$4.+LIMIT \$5 OFFSET \$6`,
		[]interface{}{userID, startDate, endDate, pgxmock.AnyArg()},
		[]interface{}{userID, startDate, endDate, pgxmock.AnyArg(), limit, offset},
	)
	countRows.AddRow(2, 75.00)
	selectRows.
		AddRow(testTxRow("tx-1", userID, "account-1", "item-1", "Supermarket", 35.00, now)...).
		AddRow(testTxRow("tx-2", userID, "account-1", "item-1", "Pharmacy", 40.00, now)...)

	transactions, totalCount, totalAmount, err := repo.GetTransactionsForBudgetCategory(ctx, userID, budgetCat, startDate, endDate, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 2)
	assert.Equal(t, 2, totalCount)
	assert.Equal(t, 75.00, totalAmount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionRepository_GetTransactionsForBudgetCategory_And_PFC(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "FOOD_AND_DRINK"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		Tags:         []string{"essential"},
		MatchLogic:   domain.MatchLogicAnd,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	limit, offset := 20, 0
	now := time.Now()

	countRows, selectRows := expectGetTxForBudgetCategoryQueries(
		mock,
		`SELECT COUNT\(\*\).+effective_pfc_primary = \$4.+AND tags \?\| \$5`,
		`SELECT (.+) FROM transactions WHERE.+effective_pfc_primary = \$4.+AND tags \?\| \$5.+LIMIT \$6 OFFSET \$7`,
		[]interface{}{userID, startDate, endDate, pfcPrimary, pgxmock.AnyArg()},
		[]interface{}{userID, startDate, endDate, pfcPrimary, pgxmock.AnyArg(), limit, offset},
	)
	countRows.AddRow(1, 22.50)
	selectRows.AddRow(testTxRow("tx-1", userID, "account-1", "item-1", "Tagged Food", 22.50, now)...)

	transactions, totalCount, totalAmount, err := repo.GetTransactionsForBudgetCategory(ctx, userID, budgetCat, startDate, endDate, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 1)
	assert.Equal(t, 1, totalCount)
	assert.Equal(t, 22.50, totalAmount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionRepository_GetTransactionsForBudgetCategory_Or_PFC(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	pfcPrimary := "ENTERTAINMENT"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		Tags:         []string{"streaming"},
		MatchLogic:   domain.MatchLogicOr,
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	limit, offset := 20, 0
	now := time.Now()

	countRows, selectRows := expectGetTxForBudgetCategoryQueries(
		mock,
		`SELECT COUNT\(\*\).+effective_pfc_primary = \$4.+ OR tags \?\| \$5\)`,
		`SELECT (.+) FROM transactions WHERE.+effective_pfc_primary = \$4.+ OR tags \?\| \$5\).+LIMIT \$6 OFFSET \$7`,
		[]interface{}{userID, startDate, endDate, pfcPrimary, pgxmock.AnyArg()},
		[]interface{}{userID, startDate, endDate, pfcPrimary, pgxmock.AnyArg(), limit, offset},
	)
	countRows.AddRow(2, 30.00)
	selectRows.
		AddRow(testTxRow("tx-1", userID, "account-1", "item-1", "Netflix", 15.00, now)...).
		AddRow(testTxRow("tx-2", userID, "account-1", "item-1", "Spotify", 15.00, now)...)

	transactions, totalCount, totalAmount, err := repo.GetTransactionsForBudgetCategory(ctx, userID, budgetCat, startDate, endDate, limit, offset)
	assert.NoError(t, err)
	assert.Len(t, transactions, 2)
	assert.Equal(t, 2, totalCount)
	assert.Equal(t, 30.00, totalAmount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionRepository_GetTransactionsForBudgetCategory_NilBudgetCategory_NoSQL(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	transactions, totalCount, totalAmount, err := repo.GetTransactionsForBudgetCategory(
		ctx, "user-123", nil, startDate, endDate, 20, 0)
	assert.NoError(t, err)
	assert.Equal(t, []domain.Transaction{}, transactions)
	assert.Equal(t, 0, totalCount)
	assert.Equal(t, 0.0, totalAmount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionRepository_GetTransactionsForBudgetCategory_UnsupportedMatchLogic_ReturnsError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	pfcPrimary := "FOOD_AND_DRINK"
	budgetCat := &domain.BudgetCategory{
		CategoryKind: "pfc",
		PFCPrimary:   &pfcPrimary,
		MatchLogic:   domain.MatchLogic("weird"),
	}
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	transactions, totalCount, totalAmount, err := repo.GetTransactionsForBudgetCategory(
		ctx, "user-123", budgetCat, startDate, endDate, 20, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported match logic")
	assert.Empty(t, transactions)
	assert.Equal(t, 0, totalCount)
	assert.Equal(t, 0.0, totalAmount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestTransactionRepository_GetByUserIDWithFilters_UserAccountID verifies that
// providing UserAccountID in TransactionFilters generates a WHERE clause that
// filters by transactions.user_account_id — the stable logical account identifier
// that survives re-links (issue #1109).
func TestTransactionRepository_GetByUserIDWithFilters_UserAccountID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	userAccountID := "ua-abc-789"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		UserAccountID: &userAccountID,
	}

	extID := "plaid_tx_1"
	now := time.Now()

	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, "account-old-1", "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		).
		AddRow(
			"tx-2", userID, "account-new-2", "item-1",
			"plaid", &extID, nil,
			-75.00, now, nil, "Transaction 2", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// The WHERE clause must contain user_account_id filter.
	// Both rows have different account_ids but the same user_account_id,
	// simulating transactions from two chain members of the same logical account.
	mock.ExpectQuery(`SELECT (.+) FROM transactions t WHERE t\.user_id(.+)AND t\.user_account_id`).
		WithArgs(userID, userAccountID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	require.NoError(t, err)
	assert.Len(t, transactions, 2)
	// Verify both chain members are returned despite different account_ids
	assert.Equal(t, "account-old-1", transactions[0].AccountID)
	assert.Equal(t, "account-new-2", transactions[1].AccountID)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestBuildOrderBy tests the buildOrderBy helper for all supported sort combinations.
func TestBuildOrderBy(t *testing.T) {
	tests := []struct {
		name     string
		filters  domain.TransactionFilters
		wantExpr string
	}{
		{
			name:     "default (empty SortBy/SortDir) -> date DESC",
			filters:  domain.TransactionFilters{},
			wantExpr: "ORDER BY COALESCE(t.authorized_date, t.date) DESC, t.id ASC",
		},
		{
			name:     "date asc",
			filters:  domain.TransactionFilters{SortBy: "date", SortDir: "asc"},
			wantExpr: "ORDER BY COALESCE(t.authorized_date, t.date) ASC, t.id ASC",
		},
		{
			name:     "date desc",
			filters:  domain.TransactionFilters{SortBy: "date", SortDir: "desc"},
			wantExpr: "ORDER BY COALESCE(t.authorized_date, t.date) DESC, t.id ASC",
		},
		{
			name:     "merchant asc",
			filters:  domain.TransactionFilters{SortBy: "merchant", SortDir: "asc"},
			wantExpr: "ORDER BY " + effectiveMerchantNameExpr + " ASC, t.id ASC",
		},
		{
			name:     "merchant desc",
			filters:  domain.TransactionFilters{SortBy: "merchant", SortDir: "desc"},
			wantExpr: "ORDER BY " + effectiveMerchantNameExpr + " DESC, t.id ASC",
		},
		{
			name:     "amount asc",
			filters:  domain.TransactionFilters{SortBy: "amount", SortDir: "asc"},
			wantExpr: "ORDER BY t.amount ASC, t.id ASC",
		},
		{
			name:     "amount desc",
			filters:  domain.TransactionFilters{SortBy: "amount", SortDir: "desc"},
			wantExpr: "ORDER BY t.amount DESC, t.id ASC",
		},
		{
			name:     "category asc",
			filters:  domain.TransactionFilters{SortBy: "category", SortDir: "asc"},
			wantExpr: "ORDER BY COALESCE(t.effective_category_name, t.effective_pfc_primary, t.pfc_primary) ASC, t.id ASC",
		},
		{
			name:     "category desc",
			filters:  domain.TransactionFilters{SortBy: "category", SortDir: "desc"},
			wantExpr: "ORDER BY COALESCE(t.effective_category_name, t.effective_pfc_primary, t.pfc_primary) DESC, t.id ASC",
		},
		{
			name:     "unknown SortBy falls back to date DESC",
			filters:  domain.TransactionFilters{SortBy: "hax", SortDir: "desc"},
			wantExpr: "ORDER BY COALESCE(t.authorized_date, t.date) DESC, t.id ASC",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildOrderBy(tc.filters)
			assert.Equal(t, tc.wantExpr, got)
		})
	}
}

// TestTransactionRepository_GetByUserIDWithFilters_SortBy tests that buildOrderBy is
// applied to the SQL emitted by GetByUserIDWithFilters.
func TestTransactionRepository_GetByUserIDWithFilters_SortBy(t *testing.T) {
	tests := []struct {
		name        string
		filters     domain.TransactionFilters
		orderByFrag string // substring that must appear in the query
	}{
		{
			name:        "default -> date DESC",
			filters:     domain.TransactionFilters{},
			orderByFrag: "ORDER BY COALESCE(t.authorized_date, t.date) DESC, t.id ASC",
		},
		{
			name:        "date asc",
			filters:     domain.TransactionFilters{SortBy: "date", SortDir: "asc"},
			orderByFrag: "ORDER BY COALESCE(t.authorized_date, t.date) ASC, t.id ASC",
		},
		{
			name:        "merchant desc",
			filters:     domain.TransactionFilters{SortBy: "merchant", SortDir: "desc"},
			orderByFrag: "ORDER BY " + effectiveMerchantNameExpr + " DESC, t.id ASC",
		},
		{
			name:        "amount asc",
			filters:     domain.TransactionFilters{SortBy: "amount", SortDir: "asc"},
			orderByFrag: "ORDER BY t.amount ASC, t.id ASC",
		},
		{
			name:        "category desc",
			filters:     domain.TransactionFilters{SortBy: "category", SortDir: "desc"},
			orderByFrag: "ORDER BY COALESCE(t.effective_category_name, t.effective_pfc_primary, t.pfc_primary) DESC, t.id ASC",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
			ctx := context.Background()

			userID := "user-sort-test"
			limit := 10
			offset := 0

			rows := pgxmock.NewRows(transactionSelectColumns) // empty result set is fine

			// pgxmock matches queries as regexps; escape the ORDER BY fragment so
			// special chars like '(' and ')' are treated literally.
			// For merchant sort the query also has LEFT JOINs, so we use a looser pattern.
			pattern := "SELECT .+ FROM transactions t.+" + regexp.QuoteMeta(tc.orderByFrag)
			mock.ExpectQuery(pattern).
				WithArgs(pgxmock.AnyArg(), limit, offset).
				WillReturnRows(rows)

			_, err = repo.GetByUserIDWithFilters(ctx, userID, tc.filters, limit, offset)
			assert.NoError(t, err)

			err = mock.ExpectationsWereMet()
			assert.NoError(t, err, "query did not contain expected ORDER BY clause")
		})
	}
}

// makeKeysetRow returns a pgxmock row with minimal values for GetByUserIDKeyset tests.
// The id must be a valid UUID string.
func makeKeysetRow(id, userID string, amount float64, txDate time.Time, merchantName *string) []interface{} {
	return []interface{}{
		id, userID, "account-1", "item-1",
		"plaid", nil, nil,
		amount, txDate, nil, "Test Merchant", merchantName, nil,
		nil, nil, nil, nil,
		nil, false, nil, txDate, txDate,
		nil, nil,
		nil, nil, nil, nil, nil, nil,
		nil, nil, nil,
		false, nil, // reviewed, reviewed_at
		nil, nil, nil, // #1569 deleted_at, deleted_reason, deleted_by
	}
}

// TestGetByUserIDKeyset_Date_Desc_FirstPage verifies that the first page (cursor=nil)
// for date/desc sort emits no keyset predicate and uses LIMIT limit+1.
func TestGetByUserIDKeyset_Date_Desc_FirstPage(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-keyset-1"
	limit := 2
	filters := domain.TransactionFilters{SortBy: "date", SortDir: "desc"}

	now := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	txID1 := "00000000-0000-0000-0000-000000000001"
	txID2 := "00000000-0000-0000-0000-000000000002"

	// Return exactly limit rows (no next page).
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(makeKeysetRow(txID1, userID, -10.00, now, nil)...).
		AddRow(makeKeysetRow(txID2, userID, -20.00, now, nil)...)

	// First page: no keyset predicate, ORDER BY date DESC, LIMIT = limit+1 = 3.
	pattern := "SELECT .+ FROM transactions t WHERE t\\.user_id .+" +
		regexp.QuoteMeta("ORDER BY COALESCE(t.authorized_date, t.date) DESC, t.id ASC") +
		`.+LIMIT`
	// The first page now also issues a whole-set aggregate ahead of the page query (#1626
	// Phase 2). Registered here because pgxmock is ordered and exhaustive; the totals it
	// returns are asserted in feed_totals_test.go, not here.
	expectFeedTotalsAggregate(mock, 0, 0, userID)

	mock.ExpectQuery(pattern).
		WithArgs(userID, limit+1).
		WillReturnRows(rows)

	txs, nextCursor, _, err := repo.GetByUserIDKeyset(ctx, userID, filters, nil, limit)
	require.NoError(t, err)
	assert.Len(t, txs, 2, "should return 2 rows (no next page)")
	assert.Nil(t, nextCursor, "cursor should be nil when fewer than limit+1 rows returned")

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByUserIDKeyset_Date_Desc_SecondPage verifies that a non-nil cursor injects
// the correct keyset predicate for date/desc sort.
func TestGetByUserIDKeyset_Date_Desc_SecondPage(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-keyset-2"
	limit := 2
	filters := domain.TransactionFilters{SortBy: "date", SortDir: "desc"}

	now := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	txID1 := "00000000-0000-0000-0000-000000000011"
	txID2 := "00000000-0000-0000-0000-000000000012"
	txID3 := "00000000-0000-0000-0000-000000000013"

	// Cursor from prior page.
	cursorDate := "2025-06-15"
	cursorID, _ := uuid.Parse("00000000-0000-0000-0000-000000000010")
	cursor := &domain.TransactionCursor{
		V: 1, K: "date", D: "desc",
		SV: &cursorDate,
		ID: cursorID,
	}

	// Return limit+1 rows to signal a next page exists.
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(makeKeysetRow(txID1, userID, -10.00, now, nil)...).
		AddRow(makeKeysetRow(txID2, userID, -20.00, now, nil)...).
		AddRow(makeKeysetRow(txID3, userID, -30.00, now, nil)...)

	// Second page: keyset predicate must appear, ORDER BY date DESC, LIMIT = limit+1.
	pattern := "SELECT .+ FROM transactions t WHERE t\\.user_id .+" +
		regexp.QuoteMeta("COALESCE(t.authorized_date, t.date)::date") +
		`.+` + regexp.QuoteMeta("ORDER BY COALESCE(t.authorized_date, t.date) DESC, t.id ASC") +
		`.+LIMIT`
	mock.ExpectQuery(pattern).
		WithArgs(userID, cursorDate, cursorID.String(), limit+1).
		WillReturnRows(rows)

	txs, nextCursor, _, err := repo.GetByUserIDKeyset(ctx, userID, filters, cursor, limit)
	require.NoError(t, err)
	assert.Len(t, txs, limit, "should return exactly limit rows when limit+1 fetched")
	require.NotNil(t, nextCursor, "cursor should be non-nil when limit+1 rows returned")
	assert.Equal(t, "date", nextCursor.K)
	assert.Equal(t, "desc", nextCursor.D)
	require.NotNil(t, nextCursor.SV)

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByUserIDKeyset_Merchant_Asc_FirstPage verifies merchant/asc first page.
// Merchant sort uses effectiveMerchantNameExpr + LEFT JOINs and scans an extra column.
func TestGetByUserIDKeyset_Merchant_Asc_FirstPage(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-keyset-3"
	limit := 5
	filters := domain.TransactionFilters{SortBy: "merchant", SortDir: "asc"}

	now := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	merchant := "Acme Corp"
	txID1 := "00000000-0000-0000-0000-000000000021"

	// Merchant sort selects an extra column (effective_merchant_sort_name).
	merchantCols := append(transactionSelectColumns, "effective_merchant_sort_name")
	rows := pgxmock.NewRows(merchantCols).
		AddRow(append(makeKeysetRow(txID1, userID, -15.00, now, &merchant), &merchant)...)

	// Merchant sort includes LEFT JOINs and effectiveMerchantNameExpr in ORDER BY.
	pattern := `SELECT .+ FROM transactions t.+LEFT JOIN merchant_name_overrides.+` +
		regexp.QuoteMeta("ORDER BY "+effectiveMerchantNameExpr+" ASC, t.id ASC") +
		`.+LIMIT`
	// The first page now also issues a whole-set aggregate ahead of the page query (#1626
	// Phase 2). Registered here because pgxmock is ordered and exhaustive; the totals it
	// returns are asserted in feed_totals_test.go, not here.
	expectFeedTotalsAggregate(mock, 0, 0, userID)

	mock.ExpectQuery(pattern).
		WithArgs(userID, limit+1).
		WillReturnRows(rows)

	txs, nextCursor, _, err := repo.GetByUserIDKeyset(ctx, userID, filters, nil, limit)
	require.NoError(t, err)
	assert.Len(t, txs, 1)
	assert.Nil(t, nextCursor)

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByUserIDKeyset_Amount_Desc_SecondPage verifies amount/desc second page cursor predicate.
func TestGetByUserIDKeyset_Amount_Desc_SecondPage(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-keyset-4"
	limit := 3
	filters := domain.TransactionFilters{SortBy: "amount", SortDir: "desc"}

	now := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	txID1 := "00000000-0000-0000-0000-000000000031"
	txID2 := "00000000-0000-0000-0000-000000000032"
	txID3 := "00000000-0000-0000-0000-000000000033"
	txID4 := "00000000-0000-0000-0000-000000000034"

	cursorSV := "-50"
	cursorID, _ := uuid.Parse("00000000-0000-0000-0000-000000000030")
	cursor := &domain.TransactionCursor{
		V: 1, K: "amount", D: "desc",
		SV: &cursorSV,
		ID: cursorID,
	}

	// Return limit+1 rows to trigger next cursor.
	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(makeKeysetRow(txID1, userID, -60.00, now, nil)...).
		AddRow(makeKeysetRow(txID2, userID, -70.00, now, nil)...).
		AddRow(makeKeysetRow(txID3, userID, -80.00, now, nil)...).
		AddRow(makeKeysetRow(txID4, userID, -90.00, now, nil)...)

	pattern := "SELECT .+ FROM transactions t WHERE t\\.user_id .+" +
		regexp.QuoteMeta("t.amount") +
		`.+` + regexp.QuoteMeta("ORDER BY t.amount DESC, t.id ASC") +
		`.+LIMIT`
	mock.ExpectQuery(pattern).
		WithArgs(userID, cursorSV, cursorID.String(), limit+1).
		WillReturnRows(rows)

	txs, nextCursor, _, err := repo.GetByUserIDKeyset(ctx, userID, filters, cursor, limit)
	require.NoError(t, err)
	assert.Len(t, txs, limit)
	require.NotNil(t, nextCursor)
	assert.Equal(t, "amount", nextCursor.K)
	assert.Equal(t, "desc", nextCursor.D)

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByUserIDKeyset_Category_Asc_FirstPage verifies category/asc first page.
func TestGetByUserIDKeyset_Category_Asc_FirstPage(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-keyset-5"
	limit := 4
	filters := domain.TransactionFilters{SortBy: "category", SortDir: "asc"}

	now := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	txID1 := "00000000-0000-0000-0000-000000000041"
	txID2 := "00000000-0000-0000-0000-000000000042"

	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(makeKeysetRow(txID1, userID, -5.00, now, nil)...).
		AddRow(makeKeysetRow(txID2, userID, -10.00, now, nil)...)

	pattern := "SELECT .+ FROM transactions t WHERE t\\.user_id .+" +
		regexp.QuoteMeta("ORDER BY COALESCE(t.effective_category_name, t.effective_pfc_primary, t.pfc_primary) ASC, t.id ASC") +
		`.+LIMIT`
	// The first page now also issues a whole-set aggregate ahead of the page query (#1626
	// Phase 2). Registered here because pgxmock is ordered and exhaustive; the totals it
	// returns are asserted in feed_totals_test.go, not here.
	expectFeedTotalsAggregate(mock, 0, 0, userID)

	mock.ExpectQuery(pattern).
		WithArgs(userID, limit+1).
		WillReturnRows(rows)

	txs, nextCursor, _, err := repo.GetByUserIDKeyset(ctx, userID, filters, nil, limit)
	require.NoError(t, err)
	assert.Len(t, txs, 2)
	assert.Nil(t, nextCursor, "fewer than limit+1 rows means no next page")

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByUserIDKeyset_MidSessionInsertExcluded verifies Issue #1128 AC #2:
// a row inserted between page fetches (with a newer date) does not appear on
// subsequent pages because the keyset predicate anchors to (date < cursorDate
// OR (date = cursorDate AND id > cursorID)). Simulated via pgxmock.
func TestGetByUserIDKeyset_MidSessionInsertExcluded(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-keyset-mid-insert"
	limit := 2
	filters := domain.TransactionFilters{SortBy: "date", SortDir: "desc"}

	// Page 1: rows A and B (B is the last row → provides cursor).
	dateA := time.Date(2025, 6, 20, 0, 0, 0, 0, time.UTC)
	dateB := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)
	txIDA := "00000000-0000-0000-0000-000000000080"
	txIDB := "00000000-0000-0000-0000-000000000081"
	txIDC := "00000000-0000-0000-0000-000000000082"
	txIDD := "00000000-0000-0000-0000-000000000083"

	// Return limit+1 (3) rows so the repo signals a next page.
	rowsPage1 := pgxmock.NewRows(transactionSelectColumns).
		AddRow(makeKeysetRow(txIDA, userID, -10.00, dateA, nil)...).
		AddRow(makeKeysetRow(txIDB, userID, -20.00, dateB, nil)...).
		AddRow(makeKeysetRow(txIDC, userID, -5.00, dateB, nil)...) // sentinel row (limit+1)

	// First page: no keyset predicate.
	patternPage1 := `SELECT .+ FROM transactions t WHERE t\.user_id .+` +
		regexp.QuoteMeta("ORDER BY COALESCE(t.authorized_date, t.date) DESC, t.id ASC") +
		`.+LIMIT`
	// The first page now also issues a whole-set aggregate ahead of the page query (#1626
	// Phase 2). Registered here because pgxmock is ordered and exhaustive; the totals it
	// returns are asserted in feed_totals_test.go, not here.
	expectFeedTotalsAggregate(mock, 0, 0, userID)

	mock.ExpectQuery(patternPage1).
		WithArgs(userID, limit+1).
		WillReturnRows(rowsPage1)

	txsP1, cursorP1, _, err := repo.GetByUserIDKeyset(ctx, userID, filters, nil, limit)
	require.NoError(t, err)
	assert.Len(t, txsP1, limit, "page 1 returns exactly limit rows")
	require.NotNil(t, cursorP1, "cursor must be present when limit+1 rows were fetched")

	// At this point, imagine a new transaction X is inserted with dateX > dateA
	// (i.e. a future sync). The cursor from B anchors to (dateB, idB), so X
	// must NOT appear in page 2.
	// The keyset predicate for page 2 (date desc): date < cursorDate OR (date = cursorDate AND id > cursorID).
	// X has date > dateA > dateB, so date < cursorDate is false and date = cursorDate is false → X excluded ✓.

	cursorDate := *cursorP1.SV // "2025-06-15" (B's date)
	cursorUUID := cursorP1.ID

	// Page 2 returns C and D (not A, not B, not the hypothetical X).
	rowsPage2 := pgxmock.NewRows(transactionSelectColumns).
		AddRow(makeKeysetRow(txIDC, userID, -5.00, dateB, nil)...).
		AddRow(makeKeysetRow(txIDD, userID, -30.00, dateB, nil)...)

	// Second page: keyset predicate must be present with cursor values.
	patternPage2 := `SELECT .+ FROM transactions t WHERE t\.user_id .+` +
		regexp.QuoteMeta("COALESCE(t.authorized_date, t.date)::date") +
		`.+` + regexp.QuoteMeta("ORDER BY COALESCE(t.authorized_date, t.date) DESC, t.id ASC") +
		`.+LIMIT`
	mock.ExpectQuery(patternPage2).
		WithArgs(userID, cursorDate, cursorUUID.String(), limit+1).
		WillReturnRows(rowsPage2)

	txsP2, _, _, err := repo.GetByUserIDKeyset(ctx, userID, filters, cursorP1, limit)
	require.NoError(t, err)
	assert.Len(t, txsP2, limit, "page 2 returns exactly limit rows")

	// Confirm: none of page 2's rows duplicate page 1 rows.
	p1IDs := map[string]bool{txsP1[0].ID: true, txsP1[1].ID: true}
	for _, tx := range txsP2 {
		assert.False(t, p1IDs[tx.ID], "page 2 must not contain page 1 rows (no duplicate on mid-session insert)")
	}

	// And page 1 must not contain X (X's ID not in p1IDs would be trivially true,
	// but the meaningful assertion is the WHERE predicate shape verified by pgxmock
	// matching patternPage2 — if the cursor args are wrong, ExpectationsWereMet fails).
	require.NoError(t, mock.ExpectationsWereMet(), "keyset predicate must use cursor (date, id) args that exclude mid-session inserts")
}

// TestBuildWhereClause_MerchantID verifies that a non-nil MerchantID filter produces
// the correct SQL condition and argument in the WHERE clause.
func TestBuildWhereClause_MerchantID(t *testing.T) {
	merchantID := "123e4567-e89b-12d3-a456-426614174000"
	userID := "user-abc"

	filters := domain.TransactionFilters{
		MerchantID: &merchantID,
	}

	whereClause, args := buildWhereClause(userID, filters)

	// The WHERE clause must contain a merchant_id equality condition.
	assert.Contains(t, whereClause, "merchant_id = $", "WHERE clause must include merchant_id condition")

	// args: $1=userID, $2=merchantID
	require.Len(t, args, 2, "expected exactly 2 args: user_id and merchant_id")
	assert.Equal(t, userID, args[0])
	assert.Equal(t, merchantID, args[1])
}

// TestTransactionRepository_GetByUserIDWithFilters_MerchantID verifies that the
// MerchantID filter is applied correctly by GetByUserIDWithFilters (Issue #1163).
func TestTransactionRepository_GetByUserIDWithFilters_MerchantID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-123"
	merchantID := "123e4567-e89b-12d3-a456-426614174000"
	limit := 10
	offset := 0
	filters := domain.TransactionFilters{
		MerchantID: &merchantID,
	}

	extID := "plaid_tx_1"
	now := time.Now()

	rows := pgxmock.NewRows(transactionSelectColumns).
		AddRow(
			"tx-1", userID, "account-1", "item-1",
			"plaid", &extID, nil,
			-50.00, now, nil, "Transaction 1", nil, nil,
			nil, nil, nil, nil,
			nil, false, nil, now, now,
			nil, nil,
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil,
			false, nil,
			nil, nil, nil,
		)

	// WHERE clause must include merchant_id filter.
	mock.ExpectQuery(`SELECT (.+) FROM transactions t WHERE t\.user_id(.+)AND t\.merchant_id`).
		WithArgs(userID, merchantID, limit, offset).
		WillReturnRows(rows)

	transactions, err := repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	require.NoError(t, err)
	assert.Len(t, transactions, 1)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestBuildWhereClause_PFCDetailedsOnly verifies that a PFCDetaileds-only filter
// produces effective_pfc_detailed IN (...) in the WHERE clause.
func TestBuildWhereClause_PFCDetailedsOnly(t *testing.T) {
	userID := "user-abc"
	filters := domain.TransactionFilters{
		PFCDetaileds: []string{"FOOD_AND_DRINK_FAST_FOOD"},
	}

	whereClause, args := buildWhereClause(userID, filters)

	// Must contain effective_pfc_detailed IN condition.
	assert.Contains(t, whereClause, "effective_pfc_detailed IN (", "WHERE clause must include effective_pfc_detailed condition")

	// Must NOT contain effective_pfc_primary IN when no primaries set.
	assert.NotContains(t, whereClause, "effective_pfc_primary IN (", "WHERE clause must not include primary condition when only detaileds set")

	// args: $1=userID, $2=detailed value
	require.Len(t, args, 2, "expected exactly 2 args: user_id and the detailed value")
	assert.Equal(t, userID, args[0])
	assert.Equal(t, "FOOD_AND_DRINK_FAST_FOOD", args[1])
}

// TestBuildWhereClause_PFCPrimariesAndDetailedsCombined verifies that when both
// PFCPrimaries and PFCDetaileds are set, the WHERE clause uses combined OR logic.
func TestBuildWhereClause_PFCPrimariesAndDetailedsCombined(t *testing.T) {
	userID := "user-abc"
	filters := domain.TransactionFilters{
		PFCPrimaries: []string{"FOOD_AND_DRINK"},
		PFCDetaileds: []string{"TRANSPORTATION_RIDE_SHARE"},
	}

	whereClause, args := buildWhereClause(userID, filters)

	// Must contain both column conditions joined by OR inside parentheses.
	assert.Contains(t, whereClause, "effective_pfc_primary IN (", "WHERE clause must include primary condition")
	assert.Contains(t, whereClause, "effective_pfc_detailed IN (", "WHERE clause must include detailed condition")
	assert.Contains(t, whereClause, " OR ", "WHERE clause must use OR logic between primary and detailed")

	// args: $1=userID, $2=primary value, $3=detailed value
	require.Len(t, args, 3, "expected exactly 3 args: user_id, primary value, detailed value")
	assert.Equal(t, userID, args[0])
	assert.Equal(t, "FOOD_AND_DRINK", args[1])
	assert.Equal(t, "TRANSPORTATION_RIDE_SHARE", args[2])
}

// TestBuildWhereClause_CategorySources verifies that a CategorySources filter produces
// category_source IN (...) in the WHERE clause (Issue #1210).
func TestBuildWhereClause_CategorySources(t *testing.T) {
	userID := "user-abc"
	filters := domain.TransactionFilters{
		CategorySources: []string{"plaid", "rule"},
	}

	whereClause, args := buildWhereClause(userID, filters)

	assert.Contains(t, whereClause, "category_source IN (", "WHERE clause must include category_source IN condition")

	// args: $1=userID, $2=plaid, $3=rule
	require.Len(t, args, 3, "expected exactly 3 args: user_id and the two source values")
	assert.Equal(t, userID, args[0])
	assert.Equal(t, "plaid", args[1])
	assert.Equal(t, "rule", args[2])
}

// TestBuildWhereClause_CategorySourcesEmpty verifies that an empty CategorySources filter
// produces no additional WHERE clause condition.
func TestBuildWhereClause_CategorySourcesEmpty(t *testing.T) {
	userID := "user-abc"
	filters := domain.TransactionFilters{
		CategorySources: []string{},
	}

	whereClause, args := buildWhereClause(userID, filters)

	assert.NotContains(t, whereClause, "category_source IN (", "WHERE clause must not include category_source when filter is empty")

	// args: $1=userID only
	require.Len(t, args, 1, "expected only user_id arg")
	assert.Equal(t, userID, args[0])
}

// TestBuildWhereClause_PFCPrimariesOnly_Unchanged is a regression test verifying that
// providing only PFCPrimaries (no detaileds) still produces effective_pfc_primary IN (...)
// without any effective_pfc_detailed condition.
func TestBuildWhereClause_PFCPrimariesOnly_Unchanged(t *testing.T) {
	userID := "user-abc"
	filters := domain.TransactionFilters{
		PFCPrimaries: []string{"FOOD_AND_DRINK"},
	}

	whereClause, args := buildWhereClause(userID, filters)

	// Must contain primary condition.
	assert.Contains(t, whereClause, "effective_pfc_primary IN (", "WHERE clause must include primary condition")

	// Must NOT contain detailed condition.
	assert.NotContains(t, whereClause, "effective_pfc_detailed IN (", "WHERE clause must not include detailed condition when only primaries set")

	// args: $1=userID, $2=primary value
	require.Len(t, args, 2, "expected exactly 2 args: user_id and the primary value")
	assert.Equal(t, userID, args[0])
	assert.Equal(t, "FOOD_AND_DRINK", args[1])
}

// TestGetReviewSummary_NoRows verifies that GetReviewSummary returns a zeroed response when
// the CTE returns no rows (new user with zero transactions).
// NOTE: pgxmock cannot validate the FILTER/EXTRACT/json_agg CTE against the real schema —
// see transaction_review_summary_integration_test.go for that.
func TestGetReviewSummary_NoRows(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	mock.ExpectQuery(".*").WithArgs(pgxmock.AnyArg()).WillReturnError(pgx.ErrNoRows)

	summary, err := repo.GetReviewSummary(ctx, "user-x", domain.ReviewSummaryFilters{})
	require.NoError(t, err, "ErrNoRows must be treated as zero summary, not an error")
	require.NotNil(t, summary)
	assert.Equal(t, int64(0), summary.UnreviewedCount)
	assert.Empty(t, summary.TopUnreviewedCategories)
}

// TestGetReviewSummary_WithRows verifies that GetReviewSummary correctly scans a non-empty result.
func TestGetReviewSummary_WithRows(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	avgSec := 1800.0
	topJSON := `[{"category_key":"FOOD_AND_DRINK","display_name":"Coffee","count":3}]`

	rows := pgxmock.NewRows([]string{
		"unreviewed_count", "reviewed_by_rule_count", "reviewed_by_manual_count",
		"reviewed_by_plaid_count", "avg_ingestion_to_review_sec", "top_unreviewed_categories",
	}).AddRow(int64(5), int64(10), int64(3), int64(2), &avgSec, []byte(topJSON))

	mock.ExpectQuery(".*").WithArgs(pgxmock.AnyArg()).WillReturnRows(rows)

	summary, err := repo.GetReviewSummary(ctx, "user-y", domain.ReviewSummaryFilters{})
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, int64(5), summary.UnreviewedCount)
	assert.Equal(t, int64(10), summary.ReviewedByRuleCount)
	assert.Equal(t, int64(3), summary.ReviewedByManualCount)
	assert.Equal(t, int64(2), summary.ReviewedByPlaidCount)
	require.NotNil(t, summary.AvgIngestionToReviewSec)
	assert.InDelta(t, 1800.0, *summary.AvgIngestionToReviewSec, 0.001)
	require.Len(t, summary.TopUnreviewedCategories, 1)
	assert.Equal(t, "FOOD_AND_DRINK", summary.TopUnreviewedCategories[0].CategoryKey)
}

// TestBulkUpdateCategory_Success verifies that BulkUpdateCategory issues one UPDATE and
// returns the row count reported by pgx (Issue #1210).
// NOTE: pgxmock cannot validate the effective_category_name CASE expression or the
// ANY($::uuid[]) cast against the real schema — see transaction_bulk_category_integration_test.go.
func TestBulkUpdateCategory_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-111"
	pfcPrimary := "FOOD_AND_DRINK"
	ids := []string{uuid.New().String(), uuid.New().String()}

	mock.ExpectBegin()
	mock.ExpectExec("FOR UPDATE").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE transactions")).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(2)))
	mock.ExpectCommit()

	n, err := repo.BulkUpdateCategory(ctx, userID, ids, ptrStr("pfc"), &pfcPrimary, nil, nil, domain.CategorySourceManual)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBulkUpdateCategory_EmptyIDs verifies that BulkUpdateCategory returns 0 and skips
// the DB call when the id list is empty.
func TestBulkUpdateCategory_EmptyIDs(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	n, err := repo.BulkUpdateCategory(ctx, "user-x", []string{}, nil, nil, nil, nil, domain.CategorySourceManual)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
	// No DB expectations set; ensures no query was issued.
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestBulkUpdateCategory_PartialOwnership verifies that rows affected < requested is
// correctly returned (unowned IDs ignored by the WHERE user_id=$ clause).
func TestBulkUpdateCategory_PartialOwnership(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-222"
	ids := []string{uuid.New().String(), uuid.New().String(), uuid.New().String()} // 3 requested

	mock.ExpectBegin()
	mock.ExpectExec("FOR UPDATE").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE transactions")).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(1))) // only 1 owned
	mock.ExpectCommit()

	n, err := repo.BulkUpdateCategory(ctx, userID, ids, nil, nil, nil, nil, domain.CategorySourceManual)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "should reflect only the owned rows that were updated")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// =============================================================================
// Issue #1348: Effective merchant name in sort/search (Task 1 — TDD: failing first)
// =============================================================================

// TestBuildOrderBy_MerchantSort verifies merchant sort uses effectiveMerchantNameExpr.
func TestBuildOrderBy_MerchantSort(t *testing.T) {
	filters := domain.TransactionFilters{SortBy: "merchant", SortDir: "desc"}
	got := buildOrderBy(filters)
	assert.Contains(t, got, effectiveMerchantNameExpr)
	assert.Contains(t, got, "DESC")
	assert.Contains(t, got, "t.id ASC")
}

// TestBuildKeysetCursor_Merchant_UsesEFF verifies cursor SV = scanned EFF, not MerchantName.
func TestBuildKeysetCursor_Merchant_UsesEFF(t *testing.T) {
	rawName := "Raw Merchant"
	effName := "Override Merchant"
	tx := domain.Transaction{
		ID:           "00000000-0000-0000-0000-000000000001",
		Name:         "Fallback Name",
		MerchantName: &rawName,
	}
	cursor, err := buildKeysetCursor(tx, "merchant", "desc", &effName)
	require.NoError(t, err)
	require.NotNil(t, cursor.SV)
	assert.Equal(t, "Override Merchant", *cursor.SV)
}

// TestBuildKeysetCursor_Merchant_FallsBackToName verifies cursor SV falls back to tx.Name when effMN nil.
func TestBuildKeysetCursor_Merchant_FallsBackToName(t *testing.T) {
	tx := domain.Transaction{
		ID:   "00000000-0000-0000-0000-000000000001",
		Name: "Fallback Name",
	}
	cursor, err := buildKeysetCursor(tx, "merchant", "desc", nil)
	require.NoError(t, err)
	require.NotNil(t, cursor.SV)
	assert.Equal(t, "Fallback Name", *cursor.SV)
}

// TestGetByUserIDWithFilters_MerchantSort_HasJoins verifies merchant joins added for merchant sort.
func TestGetByUserIDWithFilters_MerchantSort_HasJoins(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-merchant-sort-joins"
	limit := 10
	offset := 0

	rows := pgxmock.NewRows(transactionSelectColumns)

	// Pattern must contain "merchant_name_overrides" (from merchantSortJoins)
	pattern := `SELECT .+ FROM transactions t.+LEFT JOIN merchant_name_overrides.+`
	mock.ExpectQuery(pattern).
		WithArgs(userID, limit, offset).
		WillReturnRows(rows)

	filters := domain.TransactionFilters{SortBy: "merchant", SortDir: "desc"}
	_, err = repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByUserIDWithFilters_DateSort_NoSearch_NoJoins verifies no merchant joins for plain date sort.
func TestGetByUserIDWithFilters_DateSort_NoSearch_NoJoins(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-date-sort-no-joins"
	limit := 10
	offset := 0

	rows := pgxmock.NewRows(transactionSelectColumns)

	// Pattern: FROM transactions t WHERE t.user_id (no joins between FROM and WHERE)
	mock.ExpectQuery(`SELECT .+ FROM transactions t WHERE t\.user_id`).
		WithArgs(userID, limit, offset).
		WillReturnRows(rows)

	filters := domain.TransactionFilters{SortBy: "date", SortDir: "desc"}
	_, err = repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByUserIDWithFilters_Search_HasJoins verifies merchant joins added when search active.
func TestGetByUserIDWithFilters_Search_HasJoins(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-search-joins"
	limit := 10
	offset := 0
	search := "coffee"

	rows := pgxmock.NewRows(transactionSelectColumns)

	pattern := `SELECT .+ FROM transactions t.+LEFT JOIN merchant_name_overrides.+`
	mock.ExpectQuery(pattern).
		WithArgs(userID, "%"+search+"%", limit, offset).
		WillReturnRows(rows)

	filters := domain.TransactionFilters{SortBy: "date", Search: &search}
	_, err = repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByUserIDWithFilters_Search_ThreeILIKE verifies search uses 3 ILIKE terms:
// t.name, t.merchant_name, and effectiveMerchantNameExpr.
func TestGetByUserIDWithFilters_Search_ThreeILIKE(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-search-three-ilike"
	limit := 10
	offset := 0
	search := "acme"

	rows := pgxmock.NewRows(transactionSelectColumns)

	// All three ILIKE conditions must appear in the query.
	pattern := `SELECT .+ FROM transactions t.+` +
		regexp.QuoteMeta("t.name ILIKE") + `.+` +
		regexp.QuoteMeta("t.merchant_name ILIKE") + `.+` +
		regexp.QuoteMeta(effectiveMerchantNameExpr+" ILIKE")
	mock.ExpectQuery(pattern).
		WithArgs(userID, "%"+search+"%", limit, offset).
		WillReturnRows(rows)

	filters := domain.TransactionFilters{SortBy: "date", Search: &search}
	_, err = repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	assert.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByUserIDKeyset_Merchant_Desc_Predicate verifies keyset predicate for merchant/desc:
// uses effectiveMerchantNameExpr with no IS NULL branch.
func TestGetByUserIDKeyset_Merchant_Desc_Predicate(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewTransactionRepository(mock, newTestFieldEncryptor(t))
	ctx := context.Background()

	userID := "user-keyset-merchant-desc"
	limit := 2
	filters := domain.TransactionFilters{SortBy: "merchant", SortDir: "desc"}

	now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	txID1 := "00000000-0000-0000-0000-000000000051"
	txID2 := "00000000-0000-0000-0000-000000000052"
	txID3 := "00000000-0000-0000-0000-000000000053"

	merchant := "Zebra Inc"
	cursorSV := "Zebra Inc"
	cursorID, _ := uuid.Parse("00000000-0000-0000-0000-000000000050")
	cursor := &domain.TransactionCursor{
		V: 1, K: "merchant", D: "desc",
		SV: &cursorSV,
		ID: cursorID,
	}

	merchantCols := append(transactionSelectColumns, "effective_merchant_sort_name")
	rows := pgxmock.NewRows(merchantCols).
		AddRow(append(makeKeysetRow(txID1, userID, -10.00, now, &merchant), &merchant)...).
		AddRow(append(makeKeysetRow(txID2, userID, -20.00, now, &merchant), &merchant)...).
		AddRow(append(makeKeysetRow(txID3, userID, -30.00, now, &merchant), &merchant)...)

	// Predicate must use EFF < $N (no IS NULL branch).
	// Pattern checks the keyset predicate shape; exact $N placeholders vary.
	effExpr := effectiveMerchantNameExpr
	pattern := "SELECT .+ FROM transactions t.+LEFT JOIN merchant_name_overrides.+" +
		regexp.QuoteMeta("(("+effExpr+" < $") + `\d+\) OR \(` + regexp.QuoteMeta(effExpr+" = $") + `\d+ AND t\.id > \$\d+\)\)`
	mock.ExpectQuery(pattern).
		WithArgs(userID, cursorSV, cursorID.String(), limit+1).
		WillReturnRows(rows)

	txs, nextCursor, _, err := repo.GetByUserIDKeyset(ctx, userID, filters, cursor, limit)
	require.NoError(t, err)
	assert.Len(t, txs, limit)
	require.NotNil(t, nextCursor)
	assert.Equal(t, "merchant", nextCursor.K)

	require.NoError(t, mock.ExpectationsWereMet())
}
