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
	AccountID     string     `json:"account_id" db:"account_id"`                     // Required linked account
	UserAccountID *string    `json:"user_account_id,omitempty" db:"user_account_id"` // Stable reference surviving re-links (Issue #1109)
	Deadline      *time.Time `json:"deadline,omitempty" db:"deadline"`
	IsCompleted   bool       `json:"is_completed" db:"is_completed"`
	// CompletedAt is sticky once set — never cleared by a later computed drop (Issue #1300 Codex C5).
	CompletedAt *time.Time `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`

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

	// DeletedAt is the tombstone marker (#1569 Task 8). NULL on every live goal, and every
	// ordinary read carries `deleted_at IS NULL` — enforced mechanically by
	// TestNoConfigReadForgetsTheTombstoneFilter. It is serialised (omitempty, so live goals are
	// unchanged on the wire) for exactly one consumer: the deleted-goals listing added in Task 12,
	// which has to tell the user WHEN they deleted a goal before offering to destroy it for good.
	DeletedAt *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`

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

// SavingsGoalContribution is an explicit, confirmed contribution toward a savings goal (Issue #1300).
// Only "confirmed" contributions count toward goal progress when basis = "contributions".
// Contributions are split-capable: one transaction may be allocated across multiple goals
// via the partial unique constraint (transaction_id, savings_goal_id).
type SavingsGoalContribution struct {
	ID            string `json:"id" db:"id"`
	SavingsGoalID string `json:"savings_goal_id" db:"savings_goal_id"`
	// TransactionID is nil for manually entered contributions.
	TransactionID *string `json:"transaction_id,omitempty" db:"transaction_id"`
	// Amount is always positive (stored as ABS of the original transaction magnitude).
	Amount float64 `json:"amount" db:"amount"`
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

	// R4-4: include 'rejected' — sticky tombstone status added by migration 000099 (R3-7).
	// Before this fix, Validate() rejected 'rejected' rows, creating an inconsistency with
	// the migration CHECK constraint and the /reject API endpoint.
	validStatuses := map[string]bool{
		"proposed":  true,
		"confirmed": true,
		"rejected":  true,
	}
	if !validStatuses[c.Status] {
		return errors.New("status must be one of: proposed, confirmed, rejected")
	}

	return nil
}

// LogContributionRequest is the body for POST /savings-goals/{goalID}/contributions.
// Creates a manually entered, confirmed contribution (D6 source-a / M20 / Issue #1300).
type LogContributionRequest struct {
	// Amount is the contribution amount (must be positive).
	Amount float64 `json:"amount" required:"true"`
	// OccurredOn is the date the contribution occurred (YYYY-MM-DD).
	OccurredOn string `json:"occurred_on" required:"true"`
	// TransactionID optionally links to an existing transaction (must belong to the user).
	TransactionID *string `json:"transaction_id,omitempty"`
	// Notes is an optional user note (stored in source field as "manual").
	Notes *string `json:"notes,omitempty"`
}

// Validate checks LogContributionRequest fields.
func (r *LogContributionRequest) Validate() error {
	if r.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	if r.OccurredOn == "" {
		return errors.New("occurred_on is required")
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

// =============================================================================
// Issue #1300 / Codex C11 — Savings progress summary (MCP-callable analytics)
// Explicit basis-aware fields prevent LLMs from misreading mixed-basis amounts.
// =============================================================================

// Freshness constants for balance-based progress bases.
const (
	FreshnessNotApplicable = "not_applicable" // manual / contributions
	FreshnessFresh         = "fresh"          // balance_as_of within 24 h
	FreshnessStale         = "stale"          // balance_as_of older than 24 h
	FreshnessUnknown       = "unknown"        // balance_as_of not yet recorded
	// FreshnessBasisAccountDisconnected marks a balance/account_percentage-basis goal whose
	// linked account is no longer active (status replaced, expired, or removed — Issue #1569).
	// The backend stops updating that account's current_balance once it leaves 'active', so
	// recompute freezes current_amount rather than reporting a stale figure as if it were live.
	// Distinct from FreshnessStale, which means "the balance IS still being updated, just not
	// recently" — a disconnected account's balance will NEVER update again.
	FreshnessBasisAccountDisconnected = "basis_account_disconnected"
)

// ComputedFrom constants label the progress source for MCP consumers.
const (
	ComputedFromManual            = "manual"
	ComputedFromConfirmedContribs = "confirmed_contributions"
	ComputedFromAccountBalance    = "account_balance"
	ComputedFromAccountPercentage = "account_percentage"
)

// SavingsGoalProgressItem contains basis-aware progress details for a single goal.
// Designed for LLM/MCP consumption — all semantics are explicit (Codex C11).
type SavingsGoalProgressItem struct {
	GoalID    string `json:"goal_id"`
	GoalName  string `json:"goal_name"`
	AccountID string `json:"account_id,omitempty"`
	// Amounts
	CurrentAmount float64 `json:"current_amount"`
	TargetAmount  float64 `json:"target_amount"`
	// Percent 0–100 (clamped)
	PercentComplete float64 `json:"percent_complete"`
	// Basis semantics — consumers MUST use these to interpret current_amount correctly
	ProgressBasis string `json:"progress_basis"` // manual|contributions|balance|account_percentage
	ComputedFrom  string `json:"computed_from"`  // human-readable, see ComputedFrom* constants
	// Always returned — even for non-contributions bases — so consumers can compare approaches
	ConfirmedContributionsTotal float64 `json:"confirmed_contributions_total"`
	// Balance-only fields (non-nil when basis = balance or account_percentage)
	LinkedBalance *float64   `json:"linked_balance,omitempty"`
	BalanceAsOf   *time.Time `json:"balance_as_of,omitempty"`
	// Freshness is enumerated so orval emits a TypeScript union rather than a bare `string`
	// (Issue #1569 Task 13). Without the enum the frontend has to hand-duplicate the literal
	// "basis_account_disconnected", and a rename here would leave tsc and both test suites
	// green while the UI silently stopped rendering the explanation — exactly the silent-drift
	// class this flag was surfaced to prevent. Keep in sync with the Freshness* constants above.
	Freshness         string   `json:"freshness" enum:"fresh,stale,unknown,not_applicable,basis_account_disconnected"`
	AccountPercentage *float64 `json:"account_percentage,omitempty"` // non-nil when basis=account_percentage
	// Completion
	CompletedAt *time.Time `json:"completed_at,omitempty"` // sticky; nil until first completion
	Deadline    *time.Time `json:"deadline,omitempty"`
}

// SavingsProgressSummary wraps per-goal progress with portfolio-level totals.
type SavingsProgressSummary struct {
	Goals []SavingsGoalProgressItem `json:"goals"`
	// Portfolio totals (sum over all active goals)
	TotalSaved             float64 `json:"total_saved"`
	TotalTarget            float64 `json:"total_target"`
	OverallPercentComplete float64 `json:"overall_percent_complete"` // 0–100
	GoalCount              int     `json:"goal_count"`
	CompletedGoalCount     int     `json:"completed_goal_count"`
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
	// ProgressBasis controls how current_amount is derived (Issue #1300).
	// Defaults to "manual" if empty. Valid: manual|contributions|balance|account_percentage.
	ProgressBasis string `json:"progress_basis,omitempty"`
	// AccountPercentage is required (0 < x ≤ 1) when ProgressBasis is "account_percentage".
	AccountPercentage *float64 `json:"account_percentage,omitempty"`
}

// Validate checks progress-basis discriminator rules for the create request.
// Basic field validation (required, max-length, gt=0) is still enforced via struct tags
// on the domain.SavingsGoal constructed from this request; this method only covers
// the cross-field basis rules that tags cannot express.
func (r *CreateSavingsGoalRequest) Validate() error {
	basis := r.ProgressBasis
	if basis == "" {
		basis = ProgressBasisManual
	}
	return validateProgressBasisRequest(basis, r.AccountPercentage, r.AccountID)
}

// UpdateSavingsGoalRequest represents the request to update a savings goal.
type UpdateSavingsGoalRequest struct {
	Name          *string    `json:"name,omitempty" validate:"omitempty,max=100"`
	Description   *string    `json:"description,omitempty"`
	TargetAmount  *float64   `json:"target_amount,omitempty" validate:"omitempty,gt=0"`
	CurrentAmount *float64   `json:"current_amount,omitempty" validate:"omitempty,gte=0"`
	AccountID     *string    `json:"account_id,omitempty" validate:"omitempty,uuid"`
	Deadline      *time.Time `json:"deadline,omitempty"`
	// ProgressBasis changes the derivation mode for current_amount (Issue #1300).
	// When nil, the existing basis is preserved.
	ProgressBasis *string `json:"progress_basis,omitempty"`
	// AccountPercentage is required (0 < x ≤ 1) when ProgressBasis is "account_percentage".
	AccountPercentage *float64 `json:"account_percentage,omitempty"`
}

// Validate checks progress-basis discriminator rules for the update request.
// Only applies cross-field checks when ProgressBasis is explicitly set.
func (r *UpdateSavingsGoalRequest) Validate() error {
	if r.ProgressBasis == nil {
		// No basis change — nothing cross-field to validate at DTO level.
		return nil
	}
	basis := *r.ProgressBasis
	accountID := ""
	if r.AccountID != nil {
		accountID = *r.AccountID
	}
	// accountID may still be "" here (not changing account) — the full discriminator
	// check happens in SavingsGoal.Validate() after the service applies all field updates.
	// Here we only reject obviously invalid basis values and the pct discriminator.
	return validateProgressBasisRequest(basis, r.AccountPercentage, accountID)
}

// validateProgressBasisRequest is the shared cross-field validator for progress_basis
// used by both create and update request DTOs.
func validateProgressBasisRequest(basis string, accountPercentage *float64, accountID string) error {
	switch basis {
	case ProgressBasisManual, ProgressBasisContributions:
		if accountPercentage != nil {
			return errors.New("account_percentage must only be set when progress_basis is account_percentage")
		}
	case ProgressBasisBalance:
		if accountPercentage != nil {
			return errors.New("account_percentage must only be set when progress_basis is account_percentage")
		}
	case ProgressBasisAccountPercentage:
		if accountPercentage == nil {
			return errors.New("account_percentage is required when progress_basis is account_percentage")
		}
		if *accountPercentage <= 0 || *accountPercentage > 1 {
			return errors.New("account_percentage must be between 0 and 1 (exclusive 0, inclusive 1)")
		}
	default:
		return errors.New("progress_basis must be one of: manual, contributions, balance, account_percentage")
	}
	return nil
}

// SavingsInflowTransaction is a lightweight projection returned by
// GetInflowTransactionsForAccount — only the fields needed by the savings progress
// service are populated (Issue #1300, plan section 1D).
// Magnitude is ABS(amount) — always positive (Codex C9 / #1303 sign-inversion guard).
type SavingsInflowTransaction struct {
	ID        string  `json:"id"`
	Magnitude float64 `json:"magnitude"` // ABS(amount) — positive magnitude
	Date      string  `json:"date"`      // YYYY-MM-DD
}

// SavingsGoal-related errors.
var (
	ErrSavingsGoalNotFound      = errors.New("savings goal not found")
	ErrSavingsGoalAlreadyExists = errors.New("savings goal with this name already exists")

	// ErrSavingsGoalNotDeleted is returned by the purge path when the goal is still live.
	// Purging is the second half of a two-step: DELETE /savings-goals/{id} tombstones the goal and
	// keeps every contribution; POST /savings-goals/{id}/purge is what actually destroys them, and
	// it refuses on a goal the user has not already deleted. Same precondition shape as
	// repository.ErrAccountNotDisconnected on the account purge (Issue #1569, Task 6). Issue #1569.
	ErrSavingsGoalNotDeleted = errors.New("savings goal must be deleted before it can be purged")

	// ErrContributionNotFound is returned when a savings_goal_contribution is not found.
	ErrContributionNotFound = errors.New("savings goal contribution not found")
	// ErrContributionOverAllocated is returned when adding a contribution would exceed
	// the transaction's total amount (Codex C2 — allocation-cap enforcement).
	ErrContributionOverAllocated = errors.New("contribution amount exceeds transaction's remaining capacity")
	// ErrContributionAlreadyConfirmed is returned when attempting to confirm an already-confirmed row.
	ErrContributionAlreadyConfirmed = errors.New("contribution is already confirmed")
	// ErrContributionBasisMismatch is returned when a manual contribution is submitted for a goal
	// whose progress_basis is not "contributions" (Codex C8 — intake gating, R3-3).
	ErrContributionBasisMismatch = errors.New("manual contributions require progress_basis='contributions'")
	// ErrContributionNotProposed is returned when attempting to reject a contribution that is
	// not in 'proposed' status (e.g., already confirmed or already rejected). (R3-7)
	ErrContributionNotProposed = errors.New("only proposed contributions can be rejected")
)
