package domain

import (
	"errors"
	"time"
)

// BalanceSnapshot represents a point-in-time snapshot of an account's balance.
// Captured from Plaid webhooks (INITIAL_UPDATE, DEFAULT_UPDATE) to track historical balances.
// Supports multiple snapshots per day for high-frequency syncs.
//
// Issue #171: UserAccountID provides stable reference across Plaid re-links.
// When querying balance history, use UserAccountID to get history across all account versions.
type BalanceSnapshot struct {
	ID               string    `json:"id" db:"id"`
	AccountID        string    `json:"account_id" db:"account_id"`
	UserAccountID    *string   `json:"user_account_id,omitempty" db:"user_account_id"` // Stable reference (Issue #171)
	UserID           string    `json:"user_id" db:"user_id"`
	CurrentBalance   *float64  `json:"current_balance,omitempty" db:"current_balance"`
	AvailableBalance *float64  `json:"available_balance,omitempty" db:"available_balance"`
	CurrencyCode     string    `json:"currency_code" db:"currency_code"` // ISO 4217 currency code
	SnapshotAt       time.Time `json:"snapshot_at" db:"snapshot_at"`     // Timestamp when this snapshot was captured
	Source           string    `json:"source" db:"source"`               // plaid_webhook, migration, manual
	CreatedAt        time.Time `json:"created_at" db:"created_at"`       // When this record was created
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
	if bs.SnapshotAt.IsZero() {
		return errors.New("snapshot_at is required")
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
	// UntypedBalance is the part of TotalBalance that belongs to NO account type, carried
	// explicitly because net-worth history is reconstructed from ByType alone and this part used
	// to vanish (#1626 Phase 3 item 5).
	//
	// It is nonzero only for an ORPHANED balance snapshot whose chain row is gone as well —
	// accounts row hard-deleted (account_id ON DELETE SET NULL) AND user_accounts row hard-deleted
	// (user_account_id likewise), leaving nothing that records what kind of account it was. The
	// aggregated reads recover a type from user_accounts.identity_type whenever the chain row
	// survives, so this is the residue of a residue — but a day whose ONLY rows were such
	// snapshots reported net worth of exactly zero beside a correct balance, and that day is on
	// somebody's chart.
	//
	// ⚠ AN EXPLICIT FIELD, not TotalBalance minus the ByType sum. That subtraction is only valid
	// while every producer keeps the two consistent, and a test fixture in this repo already did
	// not (dashboard_service_test.go's TestGetNetWorthHistory_LiabilitiesSignedCorrectly states a
	// TotalBalance that is not its ByType sum). An invariant a caller can violate by accident is
	// not an invariant.
	UntypedBalance float64 `json:"untyped_balance,omitempty"`
}
