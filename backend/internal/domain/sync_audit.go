package domain

import (
	"errors"
	"time"
)

// SyncAudit tracks which accounts have synced transaction data in which month.
// Used to enforce monthly sync limits (e.g., free tier: 3 accounts per month).
//
// Key Design Points:
// - Track by plaid_account_id (not account_id) to prevent "delete and reconnect" abuse
// - UNIQUE constraint on (user_id, plaid_account_id, sync_month) enables re-sync without incrementing counter
// - account_id can be NULL after account deletion (audit trail persists)
type SyncAudit struct {
	ID               string     `json:"id" db:"id"`
	UserID           string     `json:"user_id" db:"user_id"`
	AccountID        *string    `json:"account_id,omitempty" db:"account_id"` // Nullable after account deletion
	PlaidAccountID   string     `json:"plaid_account_id" db:"plaid_account_id"` // Immutable tracking key
	SyncMonth        string     `json:"sync_month" db:"sync_month"`           // YYYY-MM format (e.g., "2025-11")
	FirstSyncAt      time.Time  `json:"first_sync_at" db:"first_sync_at"`     // When account first synced this month
	LastSyncAt       time.Time  `json:"last_sync_at" db:"last_sync_at"`       // Most recent sync this month
	TierAtSync       string     `json:"tier_at_sync" db:"tier_at_sync"`       // User's tier when synced (free, premium, etc.)
	DaysHistorical   int        `json:"days_historical" db:"days_historical"` // How many days back synced (30, 365, 730)
	TransactionCount int        `json:"transaction_count" db:"transaction_count"` // Total transactions synced this month (cumulative)
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

// Validate checks that the SyncAudit has all required fields
func (s *SyncAudit) Validate() error {
	if s.UserID == "" {
		return errors.New("user_id is required")
	}
	if s.PlaidAccountID == "" {
		return errors.New("plaid_account_id is required")
	}
	if s.SyncMonth == "" {
		return errors.New("sync_month is required")
	}

	// Validate sync_month format (YYYY-MM)
	if len(s.SyncMonth) != 7 || s.SyncMonth[4] != '-' {
		return errors.New("sync_month must be in YYYY-MM format (e.g., '2025-11')")
	}

	if s.TierAtSync == "" {
		return errors.New("tier_at_sync is required")
	}

	if s.DaysHistorical < 0 {
		return errors.New("days_historical cannot be negative")
	}

	if s.TransactionCount < 0 {
		return errors.New("transaction_count cannot be negative")
	}

	return nil
}

// SyncStats represents sync usage statistics for a user in a given month.
// Used to display to users and enforce monthly sync limits.
type SyncStats struct {
	UserID              string    `json:"user_id"`
	Month               string    `json:"month"`                // YYYY-MM format
	AccountsSynced      int       `json:"accounts_synced"`      // Number of unique accounts synced this month
	AccountsLimit       int       `json:"accounts_limit"`       // User's monthly sync limit based on tier
	TotalTransactions   int       `json:"total_transactions"`   // Total transactions synced this month (all accounts combined)
	CanSyncMore         bool      `json:"can_sync_more"`        // Whether user can sync more accounts this month
	ResetDate           time.Time `json:"reset_date"`           // When the counter resets (first day of next month)
}

// CalculateResetDate calculates the first day of the next month (when counter resets)
func (s *SyncStats) CalculateResetDate() time.Time {
	// Parse YYYY-MM format
	year := 0
	month := 0
	_, err := time.Parse("2006-01", s.Month)
	if err == nil {
		t, _ := time.Parse("2006-01", s.Month)
		year = t.Year()
		month = int(t.Month())
	}

	// Next month
	if month == 12 {
		return time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(year, time.Month(month+1), 1, 0, 0, 0, 0, time.UTC)
}
