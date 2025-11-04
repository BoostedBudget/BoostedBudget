package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	// ErrSyncAuditNotFound is returned when a sync audit record is not found
	ErrSyncAuditNotFound = errors.New("sync audit not found")
)

// SyncAuditRepository defines the interface for sync audit data access.
// Used to enforce monthly transaction sync limits (e.g., free tier: 3 accounts per month).
//
// Key Design:
// - Track by plaid_account_id (not account_id) to prevent delete-reconnect abuse
// - UNIQUE constraint on (user_id, plaid_account_id, sync_month) enables re-sync without incrementing counter
// - account_id can be NULL after account deletion (audit trail persists)
type SyncAuditRepository interface {
	// CountSyncsThisMonth returns the number of unique accounts synced in a given month.
	// Used to enforce monthly sync limits (e.g., free tier: 3 accounts per month).
	//
	// Example: CountSyncsThisMonth(ctx, "user123", "2025-11") returns 3
	// if user has synced 3 different plaid_account_ids in November 2025.
	CountSyncsThisMonth(ctx context.Context, userID, month string) (int, error)

	// HasSyncedThisMonth checks if a specific account has already synced in a given month.
	// Used to determine if re-sync is allowed (doesn't count against limit).
	//
	// Returns true if the account has synced this month, false otherwise.
	HasSyncedThisMonth(ctx context.Context, userID, plaidAccountID, month string) (bool, error)

	// RecordSync records a sync operation for an account.
	// Uses UPSERT (INSERT ... ON CONFLICT DO UPDATE) to handle re-syncs:
	// - First sync: Creates new record
	// - Re-sync: Updates last_sync_at and increments transaction_count
	//
	// The UNIQUE constraint on (user_id, plaid_account_id, sync_month) ensures
	// the same account can only have one record per month per user.
	RecordSync(ctx context.Context, audit *domain.SyncAudit) error

	// GetSyncStats returns sync statistics for a user in a given month.
	// Used to display to users: "You've synced 2 of 3 accounts this month"
	GetSyncStats(ctx context.Context, userID, month string) (*domain.SyncStats, error)
}

// syncAuditRepository implements SyncAuditRepository using PostgreSQL
type syncAuditRepository struct {
	db *sql.DB
}

// NewSyncAuditRepository creates a new SyncAuditRepository instance
func NewSyncAuditRepository(db *sql.DB) SyncAuditRepository {
	return &syncAuditRepository{db: db}
}

// CountSyncsThisMonth returns the number of unique accounts synced in a given month
func (r *syncAuditRepository) CountSyncsThisMonth(ctx context.Context, userID, month string) (int, error) {
	query := `
		SELECT COUNT(DISTINCT plaid_account_id)
		FROM account_sync_audit
		WHERE user_id = $1 AND sync_month = $2
	`

	var count int
	err := r.db.QueryRowContext(ctx, query, userID, month).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// HasSyncedThisMonth checks if a specific account has already synced in a given month
func (r *syncAuditRepository) HasSyncedThisMonth(ctx context.Context, userID, plaidAccountID, month string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM account_sync_audit
			WHERE user_id = $1 AND plaid_account_id = $2 AND sync_month = $3
		)
	`

	var exists bool
	err := r.db.QueryRowContext(ctx, query, userID, plaidAccountID, month).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

// RecordSync records a sync operation for an account (UPSERT behavior)
func (r *syncAuditRepository) RecordSync(ctx context.Context, audit *domain.SyncAudit) error {
	// Generate UUID if not already set
	if audit.ID == "" {
		audit.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	audit.CreatedAt = now
	audit.UpdatedAt = now

	// UPSERT query: INSERT new record, or UPDATE on conflict
	// Conflict on UNIQUE(user_id, plaid_account_id, sync_month)
	query := `
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

	_, err := r.db.ExecContext(ctx, query,
		audit.ID,
		audit.UserID,
		audit.AccountID,
		audit.PlaidAccountID,
		audit.SyncMonth,
		audit.FirstSyncAt,
		audit.LastSyncAt,
		audit.TierAtSync,
		audit.DaysHistorical,
		audit.TransactionCount,
		audit.CreatedAt,
		audit.UpdatedAt,
	)

	if err != nil {
		return err
	}

	return nil
}

// GetSyncStats returns sync statistics for a user in a given month
func (r *syncAuditRepository) GetSyncStats(ctx context.Context, userID, month string) (*domain.SyncStats, error) {
	query := `
		SELECT
			COUNT(DISTINCT plaid_account_id) as accounts_synced,
			COALESCE(SUM(transaction_count), 0) as total_transactions
		FROM account_sync_audit
		WHERE user_id = $1 AND sync_month = $2
	`

	stats := &domain.SyncStats{
		UserID: userID,
		Month:  month,
	}

	var totalTransactions int64
	err := r.db.QueryRowContext(ctx, query, userID, month).Scan(
		&stats.AccountsSynced,
		&totalTransactions,
	)

	if err != nil {
		return nil, err
	}

	stats.TotalTransactions = int(totalTransactions)

	return stats, nil
}
