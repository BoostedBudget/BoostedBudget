package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSyncAuditMockDB creates a mock database and sqlmock instance for sync audit repository testing
func setupSyncAuditMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, SyncAuditRepository) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	repo := NewSyncAuditRepository(db)
	return db, mock, repo
}

// TestSyncAuditRepository_CountSyncsThisMonth_Success tests counting unique accounts synced in a month
func TestSyncAuditRepository_CountSyncsThisMonth_Success(t *testing.T) {
	db, mock, repo := setupSyncAuditMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	syncMonth := "2025-11"

	// Expect query to count distinct plaid_account_id for user in month
	expectedQuery := `
		SELECT COUNT(DISTINCT plaid_account_id)
		FROM account_sync_audit
		WHERE user_id = $1 AND sync_month = $2
	`
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID, syncMonth).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	// Execute
	count, err := repo.CountSyncsThisMonth(ctx, userID, syncMonth)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 3, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSyncAuditRepository_CountSyncsThisMonth_ReturnsZero tests zero count when no syncs
func TestSyncAuditRepository_CountSyncsThisMonth_ReturnsZero(t *testing.T) {
	db, mock, repo := setupSyncAuditMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	syncMonth := "2025-11"

	expectedQuery := `
		SELECT COUNT(DISTINCT plaid_account_id)
		FROM account_sync_audit
		WHERE user_id = $1 AND sync_month = $2
	`
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID, syncMonth).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// Execute
	count, err := repo.CountSyncsThisMonth(ctx, userID, syncMonth)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSyncAuditRepository_CountSyncsThisMonth_DatabaseError tests error handling
func TestSyncAuditRepository_CountSyncsThisMonth_DatabaseError(t *testing.T) {
	db, mock, repo := setupSyncAuditMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	syncMonth := "2025-11"

	expectedQuery := `
		SELECT COUNT(DISTINCT plaid_account_id)
		FROM account_sync_audit
		WHERE user_id = $1 AND sync_month = $2
	`
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID, syncMonth).
		WillReturnError(sql.ErrConnDone)

	// Execute
	count, err := repo.CountSyncsThisMonth(ctx, userID, syncMonth)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, 0, count)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSyncAuditRepository_HasSyncedThisMonth_True tests when account has synced this month
func TestSyncAuditRepository_HasSyncedThisMonth_True(t *testing.T) {
	db, mock, repo := setupSyncAuditMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	plaidAccountID := "plaid-acc-xyz123"
	syncMonth := "2025-11"

	expectedQuery := `
		SELECT EXISTS(
			SELECT 1 FROM account_sync_audit
			WHERE user_id = $1 AND plaid_account_id = $2 AND sync_month = $3
		)
	`
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID, plaidAccountID, syncMonth).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	// Execute
	exists, err := repo.HasSyncedThisMonth(ctx, userID, plaidAccountID, syncMonth)

	// Assert
	assert.NoError(t, err)
	assert.True(t, exists)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSyncAuditRepository_HasSyncedThisMonth_False tests when account has NOT synced this month
func TestSyncAuditRepository_HasSyncedThisMonth_False(t *testing.T) {
	db, mock, repo := setupSyncAuditMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	plaidAccountID := "plaid-acc-xyz123"
	syncMonth := "2025-11"

	expectedQuery := `
		SELECT EXISTS(
			SELECT 1 FROM account_sync_audit
			WHERE user_id = $1 AND plaid_account_id = $2 AND sync_month = $3
		)
	`
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID, plaidAccountID, syncMonth).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	// Execute
	exists, err := repo.HasSyncedThisMonth(ctx, userID, plaidAccountID, syncMonth)

	// Assert
	assert.NoError(t, err)
	assert.False(t, exists)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSyncAuditRepository_RecordSync_FirstTime tests recording first sync of an account
func TestSyncAuditRepository_RecordSync_FirstTime(t *testing.T) {
	db, mock, repo := setupSyncAuditMockDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()
	accountID := uuid.New().String()

	audit := &domain.SyncAudit{
		ID:               uuid.New().String(),
		UserID:           uuid.New().String(),
		AccountID:        &accountID,
		PlaidAccountID:   "plaid-acc-xyz123",
		SyncMonth:        "2025-11",
		FirstSyncAt:      now,
		LastSyncAt:       now,
		TierAtSync:       "free",
		DaysHistorical:   30,
		TransactionCount: 150,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	// Expect UPSERT query (INSERT with ON CONFLICT DO UPDATE)
	expectedQuery := `
		INSERT INTO account_sync_audit (
			id, user_id, account_id, plaid_account_id, sync_month,
			first_sync_at, last_sync_at, tier_at_sync, days_historical,
			transaction_count, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (user_id, plaid_account_id, sync_month)
		DO UPDATE SET
			last_sync_at = EXCLUDED.last_sync_at,
			transaction_count = account_sync_audit.transaction_count + EXCLUDED.transaction_count,
			updated_at = EXCLUDED.updated_at
	`
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(
			audit.ID,
			audit.UserID,
			audit.AccountID,
			audit.PlaidAccountID,
			audit.SyncMonth,
			sqlmock.AnyArg(), // first_sync_at
			sqlmock.AnyArg(), // last_sync_at
			audit.TierAtSync,
			audit.DaysHistorical,
			audit.TransactionCount,
			sqlmock.AnyArg(), // created_at
			sqlmock.AnyArg(), // updated_at
		).WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute
	err := repo.RecordSync(ctx, audit)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSyncAuditRepository_RecordSync_ReSync tests re-syncing same account (upsert behavior)
func TestSyncAuditRepository_RecordSync_ReSync(t *testing.T) {
	db, mock, repo := setupSyncAuditMockDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()
	accountID := uuid.New().String()

	audit := &domain.SyncAudit{
		ID:               uuid.New().String(),
		UserID:           uuid.New().String(),
		AccountID:        &accountID,
		PlaidAccountID:   "plaid-acc-xyz123",
		SyncMonth:        "2025-11",
		FirstSyncAt:      now,
		LastSyncAt:       now,
		TierAtSync:       "free",
		DaysHistorical:   30,
		TransactionCount: 25, // New transactions since last sync
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	expectedQuery := `
		INSERT INTO account_sync_audit (
			id, user_id, account_id, plaid_account_id, sync_month,
			first_sync_at, last_sync_at, tier_at_sync, days_historical,
			transaction_count, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (user_id, plaid_account_id, sync_month)
		DO UPDATE SET
			last_sync_at = EXCLUDED.last_sync_at,
			transaction_count = account_sync_audit.transaction_count + EXCLUDED.transaction_count,
			updated_at = EXCLUDED.updated_at
	`
	// On conflict, DO UPDATE is triggered (existing record updated)
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(
			audit.ID,
			audit.UserID,
			audit.AccountID,
			audit.PlaidAccountID,
			audit.SyncMonth,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			audit.TierAtSync,
			audit.DaysHistorical,
			audit.TransactionCount,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).WillReturnResult(sqlmock.NewResult(1, 1)) // 1 row affected (updated)

	// Execute
	err := repo.RecordSync(ctx, audit)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSyncAuditRepository_RecordSync_DifferentMonth tests syncing in different month creates new record
func TestSyncAuditRepository_RecordSync_DifferentMonth(t *testing.T) {
	db, mock, repo := setupSyncAuditMockDB(t)
	defer db.Close()

	ctx := context.Background()
	now := time.Now()
	accountID := uuid.New().String()

	audit := &domain.SyncAudit{
		ID:               uuid.New().String(),
		UserID:           uuid.New().String(),
		AccountID:        &accountID,
		PlaidAccountID:   "plaid-acc-xyz123",
		SyncMonth:        "2025-12", // Different month from previous sync (assumed)
		FirstSyncAt:      now,
		LastSyncAt:       now,
		TierAtSync:       "free",
		DaysHistorical:   30,
		TransactionCount: 100,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	expectedQuery := `
		INSERT INTO account_sync_audit (
			id, user_id, account_id, plaid_account_id, sync_month,
			first_sync_at, last_sync_at, tier_at_sync, days_historical,
			transaction_count, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (user_id, plaid_account_id, sync_month)
		DO UPDATE SET
			last_sync_at = EXCLUDED.last_sync_at,
			transaction_count = account_sync_audit.transaction_count + EXCLUDED.transaction_count,
			updated_at = EXCLUDED.updated_at
	`
	// No conflict because sync_month is different (new record inserted)
	mock.ExpectExec(regexp.QuoteMeta(expectedQuery)).
		WithArgs(
			audit.ID,
			audit.UserID,
			audit.AccountID,
			audit.PlaidAccountID,
			audit.SyncMonth,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			audit.TierAtSync,
			audit.DaysHistorical,
			audit.TransactionCount,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute
	err := repo.RecordSync(ctx, audit)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSyncAuditRepository_GetSyncStats_Success tests retrieving sync statistics
func TestSyncAuditRepository_GetSyncStats_Success(t *testing.T) {
	db, mock, repo := setupSyncAuditMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	syncMonth := "2025-11"

	expectedQuery := `
		SELECT
			COUNT(DISTINCT plaid_account_id) as accounts_synced,
			COALESCE(SUM(transaction_count), 0) as total_transactions
		FROM account_sync_audit
		WHERE user_id = $1 AND sync_month = $2
	`
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID, syncMonth).
		WillReturnRows(sqlmock.NewRows([]string{"accounts_synced", "total_transactions"}).
			AddRow(3, 450))

	// Execute
	stats, err := repo.GetSyncStats(ctx, userID, syncMonth)

	// Assert
	assert.NoError(t, err)
	require.NotNil(t, stats)
	assert.Equal(t, 3, stats.AccountsSynced)
	assert.Equal(t, 450, stats.TotalTransactions)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSyncAuditRepository_GetSyncStats_NoSyncs tests stats when no syncs exist
func TestSyncAuditRepository_GetSyncStats_NoSyncs(t *testing.T) {
	db, mock, repo := setupSyncAuditMockDB(t)
	defer db.Close()

	ctx := context.Background()
	userID := uuid.New().String()
	syncMonth := "2025-11"

	expectedQuery := `
		SELECT
			COUNT(DISTINCT plaid_account_id) as accounts_synced,
			COALESCE(SUM(transaction_count), 0) as total_transactions
		FROM account_sync_audit
		WHERE user_id = $1 AND sync_month = $2
	`
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(userID, syncMonth).
		WillReturnRows(sqlmock.NewRows([]string{"accounts_synced", "total_transactions"}).
			AddRow(0, 0))

	// Execute
	stats, err := repo.GetSyncStats(ctx, userID, syncMonth)

	// Assert
	assert.NoError(t, err)
	require.NotNil(t, stats)
	assert.Equal(t, 0, stats.AccountsSynced)
	assert.Equal(t, 0, stats.TotalTransactions)
	assert.NoError(t, mock.ExpectationsWereMet())
}
