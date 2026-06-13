package domain

import (
	"errors"
	"time"
)

// ProgressBasis constants define how a savings goal's current_amount is derived (Issue #1300).
const (
	ProgressBasisManual            = "manual"
	ProgressBasisContributions     = "contributions"
	ProgressBasisBalance           = "balance"
	ProgressBasisAccountPercentage = "account_percentage"
)

// SavingsGoal represents a user-defined savings goal linked to an account.
// Savings goals are FREE for all subscription tiers (Issue #27).
// current_amount is computed per progress_basis (Issue #1300):
//   - "manual":             user sets current_amount directly (default for existing goals)
//   - "contributions":      SUM of confirmed savings_goal_contributions (default for new goals)
//   - "balance":            mirrors Account.CurrentBalance (dedicated 1:1 account)
//   - "account_percentage": Account.CurrentBalance × account_percentage (shared account, multi-bucket)
type SavingsGoal struct {
	ID            string     `json:"id" db:"id"`
	UserID        string     `json:"user_id" db:"user_id"`
	Name          string     `json:"name" db:"name"`
	Description   *string    `json:"description,omitempty" db:"description"`
	TargetAmount  float64    `json:"target_amount" db:"target_amount"`
	CurrentAmount float64    `json:"current_amount" db:"current_amount"` // Computed per ProgressBasis; stored for fast reads
	CurrencyCode  string     `json:"currency_code" db:"currency_code"`
	AccountID     string     `json:"account_id" db:"account_id"` // Required linked account
	UserAccountID *string    `json:"user_account_id,omitempty" db:"user_account_id"` // Stable reference surviving re-links (Issue #1109)
	Deadline      *time.Time `json:"deadline,omitempty" db:"deadline"`
	IsCompleted   bool       `json:"is_completed" db:"is_completed"`
	// CompletedAt is sticky once set — never cleared by a later computed drop (Issue #1300 Codex C5).
	CompletedAt     *time.Time `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`

	// Progress tracking configuration (Issue #1300).
	// ProgressBasis determines how current_amount is derived.
	ProgressBasis string `json:"progress_basis" db:"progress_basis"`
	// AccountPercentage is required (0 < x <= 1) when ProgressBasis is "account_percentage".
	AccountPercentage *float64 `json:"account_percentage,omitempty" db:"account_percentage"`
	// LastCalculatedAt records when current_amount was last recomputed.
	LastCalculatedAt *time.Time `json:"last_calculated_at,omitempty" db:"last_calculated_at"`
	// BalanceAsOf records the account's last_synced_at at the time of last recompute
	// (for balance/account_percentage bases — freshness signal for API/MCP consumers).
	BalanceAsOf *time.Time `json:"balance_as_of,omitempty" db:"balance_as_of"`

	// Associated data (populated on demand, not stored in DB)
	Account *Account `json:"account,omitempty" db:"-"`
}

// Validate checks that the SavingsGoal has all required fields and valid values.
func (sg *SavingsGoal) Validate() error {
	if sg.UserID == "" {
		return errors.New("user_id is required")
	}

	if sg.Name == "" {
		return errors.New("name is required")
	}

	if len(sg.Name) > 100 {
		return errors.New("name must be 100 characters or less")
	}

	if sg.TargetAmount <= 0 {
		return errors.New("target_amount must be positive")
	}

	if sg.CurrentAmount < 0 {
		return errors.New("current_amount cannot be negative")
	}

	if sg.CurrencyCode == "" {
		return errors.New("currency_code is required")
	}

	// Validate progress_basis and related fields (Issue #1300).
	basis := sg.ProgressBasis
	if basis == "" {
		basis = ProgressBasisManual // zero-value treated as manual for backward compat
	}
	switch basis {
	case ProgressBasisManual, ProgressBasisContributions:
		// account_percentage must not be set for these bases
		if sg.AccountPercentage != nil {
			return errors.New("account_percentage must only be set when progress_basis is account_percentage")
		}
	case ProgressBasisBalance:
		if sg.AccountPercentage != nil {
			return errors.New("account_percentage must only be set when progress_basis is account_percentage")
		}
		if sg.AccountID == "" {
			return errors.New("account_id is required when progress_basis is balance")
		}
	case ProgressBasisAccountPercentage:
		if sg.AccountPercentage == nil {
			return errors.New("account_percentage is required when progress_basis is account_percentage")
		}
		if *sg.AccountPercentage <= 0 || *sg.AccountPercentage > 1 {
			return errors.New("account_percentage must be between 0 and 1 (exclusive 0, inclusive 1)")
		}
		if sg.AccountID == "" {
			return errors.New("account_id is required when progress_basis is account_percentage")
		}
	default:
		return errors.New("progress_basis must be one of: manual, contributions, balance, account_percentage")
	}

	// account_id is required for all bases (savings goals always link to an account).
	if sg.AccountID == "" {
		return errors.New("account_id is required")
	}

	return nil
}

// validProgressBases is the set of allowed progress_basis values.
var validProgressBases = map[string]bool{
	ProgressBasisManual:            true,
	ProgressBasisContributions:     true,
	ProgressBasisBalance:           true,
	ProgressBasisAccountPercentage: true,
}

// SavingsGoalContribution is an explicit, confirmed contribution toward a savings goal (Issue #1300).
// Only "confirmed" contributions count toward goal progress when basis = "contributions".
// Contributions are split-capable: one transaction may be allocated across multiple goals
// via the partial unique constraint (transaction_id, savings_goal_id).
type SavingsGoalContribution struct {
	ID            string  `json:"id" db:"id"`
	SavingsGoalID string  `json:"savings_goal_id" db:"savings_goal_id"`
	// TransactionID is nil for manually entered contributions.
	TransactionID *string `json:"transaction_id,omitempty" db:"transaction_id"`
	// Amount is always positive (stored as ABS of the original transaction magnitude).
	Amount     float64 `json:"amount" db:"amount"`
	// Source records the contribution origin: manual (user-entered), confirmed_transfer
	// (user confirmed a detected TRANSFER_IN), or boost_match (auto-confirmed via
	// TransferSuggestion.MatchedTransactionID).
	Source string `json:"source" db:"source"`
	// Status: only "confirmed" contributions count toward progress.
	Status string `json:"status" db:"status"`
	// ManagedByReconciliation distinguishes auto-derived rows (reconciler owns: may re-derive/delete)
	// from user-entered rows (reconciler must never touch) — Issue #1300 Codex C3.
	ManagedByReconciliation bool `json:"managed_by_reconciliation" db:"managed_by_reconciliation"`
	// ConfirmedByUser records whether a human explicitly confirmed this contribution.
	ConfirmedByUser bool      `json:"confirmed_by_user" db:"confirmed_by_user"`
	OccurredOn      string    `json:"occurred_on" db:"occurred_on"` // date string YYYY-MM-DD
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// Validate checks SavingsGoalContribution required fields and invariants.
func (c *SavingsGoalContribution) Validate() error {
	if c.SavingsGoalID == "" {
		return errors.New("savings_goal_id is required")
	}

	if c.Amount <= 0 {
		return errors.New("amount must be positive")
	}

	validSources := map[string]bool{
		"manual":             true,
		"confirmed_transfer": true,
		"boost_match":        true,
	}
	if !validSources[c.Source] {
		return errors.New("source must be one of: manual, confirmed_transfer, boost_match")
	}

	validStatuses := map[string]bool{
		"proposed":  true,
		"confirmed": true,
	}
	if !validStatuses[c.Status] {
		return errors.New("status must be one of: proposed, confirmed")
	}

	return nil
}

// Progress returns the progress percentage toward the goal (0-100+).
func (sg *SavingsGoal) Progress() float64 {
	if sg.TargetAmount == 0 {
		return 0
	}
	return (sg.CurrentAmount / sg.TargetAmount) * 100
}

// RemainingAmount returns the amount still needed to reach the goal.
func (sg *SavingsGoal) RemainingAmount() float64 {
	remaining := sg.TargetAmount - sg.CurrentAmount
	if remaining < 0 {
		return 0
	}
	return remaining
}

// IsOnTrack returns true if the goal is on track to be completed by deadline.
// Returns true if no deadline is set or if the goal is completed.
func (sg *SavingsGoal) IsOnTrack() bool {
	if sg.Deadline == nil || sg.IsCompleted {
		return true
	}

	now := time.Now()
	if now.After(*sg.Deadline) {
		// Past deadline - only on track if target reached
		return sg.CurrentAmount >= sg.TargetAmount
	}

	// Calculate expected progress based on time elapsed since creation
	totalDays := sg.Deadline.Sub(sg.CreatedAt).Hours() / 24
	elapsedDays := now.Sub(sg.CreatedAt).Hours() / 24

	if totalDays <= 0 {
		return true
	}

	expectedProgress := (elapsedDays / totalDays) * 100
	actualProgress := sg.Progress()

	return actualProgress >= expectedProgress
}

// DaysUntilDeadline returns the number of days until the deadline.
// Returns -1 if no deadline is set.
// Returns 0 if the deadline has passed.
func (sg *SavingsGoal) DaysUntilDeadline() int {
	if sg.Deadline == nil {
		return -1
	}
	days := int(time.Until(*sg.Deadline).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// IsAchieved returns true if the current amount meets or exceeds the target.
func (sg *SavingsGoal) IsAchieved() bool {
	return sg.CurrentAmount >= sg.TargetAmount
}

// CreateSavingsGoalRequest represents the request to create a new savings goal.
type CreateSavingsGoalRequest struct {
	Name          string     `json:"name" validate:"required,max=100"`
	Description   *string    `json:"description,omitempty"`
	TargetAmount  float64    `json:"target_amount" validate:"required,gt=0"`
	CurrentAmount float64    `json:"current_amount,omitempty" validate:"gte=0"`
	CurrencyCode  string     `json:"currency_code,omitempty"`
	AccountID     string     `json:"account_id" validate:"required,uuid"`
	Deadline      *time.Time `json:"deadline,omitempty"`
}

// UpdateSavingsGoalRequest represents the request to update a savings goal.
type UpdateSavingsGoalRequest struct {
	Name          *string    `json:"name,omitempty" validate:"omitempty,max=100"`
	Description   *string    `json:"description,omitempty"`
	TargetAmount  *float64   `json:"target_amount,omitempty" validate:"omitempty,gt=0"`
	CurrentAmount *float64   `json:"current_amount,omitempty" validate:"omitempty,gte=0"`
	AccountID     *string    `json:"account_id,omitempty" validate:"omitempty,uuid"`
	Deadline      *time.Time `json:"deadline,omitempty"`
}

// SavingsGoal-related errors.
var (
	ErrSavingsGoalNotFound      = errors.New("savings goal not found")
	ErrSavingsGoalAlreadyExists = errors.New("savings goal with this name already exists")
)
