package domain

import (
	"errors"
	"time"
)

// BalanceSnapshot represents a daily snapshot of an account's balance.
// Captured from Plaid webhooks (INITIAL_UPDATE, DEFAULT_UPDATE) to track historical balances.
type BalanceSnapshot struct {
	ID               string    `json:"id" db:"id"`
	AccountID        string    `json:"account_id" db:"account_id"`
	UserID           string    `json:"user_id" db:"user_id"`
	CurrentBalance   *float64  `json:"current_balance,omitempty" db:"current_balance"`
	AvailableBalance *float64  `json:"available_balance,omitempty" db:"available_balance"`
	CurrencyCode     string    `json:"currency_code" db:"currency_code"` // ISO 4217 currency code
	SnapshotDate     time.Time `json:"snapshot_date" db:"snapshot_date"` // The date this balance represents
	Source           string    `json:"source" db:"source"`               // plaid_webhook, migration, manual
	CreatedAt        time.Time `json:"created_at" db:"created_at"`       // When this snapshot was created
}

// BalanceSnapshotSource constants for the source field
const (
	SnapshotSourcePlaidWebhook = "plaid_webhook"
	SnapshotSourceMigration    = "migration"
	SnapshotSourceManual       = "manual"
)

// Validate checks that the BalanceSnapshot has all required fields
func (bs *BalanceSnapshot) Validate() error {
	if bs.UserID == "" {
		return errors.New("user_id is required")
	}
	if bs.AccountID == "" {
		return errors.New("account_id is required")
	}
	if bs.CurrencyCode == "" {
		return errors.New("currency_code is required")
	}
	if bs.SnapshotDate.IsZero() {
		return errors.New("snapshot_date is required")
	}
	if bs.Source == "" {
		return errors.New("source is required")
	}

	// Validate source
	validSources := map[string]bool{
		SnapshotSourcePlaidWebhook: true,
		SnapshotSourceMigration:    true,
		SnapshotSourceManual:       true,
	}
	if !validSources[bs.Source] {
		return errors.New("invalid source: must be one of plaid_webhook, migration, manual")
	}

	// At least one balance should be present
	if bs.CurrentBalance == nil && bs.AvailableBalance == nil {
		return errors.New("at least one of current_balance or available_balance is required")
	}

	return nil
}

// BalanceHistoryPoint represents an aggregated balance data point for charts.
// Used by the dashboard to display balance over time.
type BalanceHistoryPoint struct {
	Date         time.Time          `json:"date"`
	TotalBalance float64            `json:"total_balance"`
	ByType       map[string]float64 `json:"by_type,omitempty"` // Optional breakdown by account type
}
