package domain

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
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
//   - "manual":             user sets current_amount directly (default for pre-#1625 goals)
//   - "contributions":      SUM of confirmed savings_goal_contributions (default for new goals)
//   - "balance":            mirrors Account.CurrentBalance (dedicated 1:1 account)
//   - "account_percentage": Account.CurrentBalance × account_percentage (shared account, multi-bucket)
type SavingsGoal struct {
	ID            string  `json:"id" db:"id"`
	UserID        string  `json:"user_id" db:"user_id"`
	Name          string  `json:"name" db:"name"`
	Description   *string `json:"description,omitempty" db:"description"`
	TargetAmount  float64 `json:"target_amount" db:"target_amount"`
	CurrentAmount float64 `json:"current_amount" db:"current_amount"` // Computed per ProgressBasis; stored for fast reads
	CurrencyCode  string  `json:"currency_code" db:"currency_code"`
	AccountID     string  `json:"account_id" db:"account_id"`                     // Required linked account
	UserAccountID *string `json:"user_account_id,omitempty" db:"user_account_id"` // Stable reference surviving re-links (Issue #1109)
	// Deadline stays *time.Time — NOT serialized — since savings_goals.deadline is a Postgres
	// DATE column and Deadline is still a SQL bind param/scan target; DeadlineDisplay is what
	// actually reaches clients (#1631 Batch F2, same sibling-display pattern as
	// domain/activity_item.go). *string, not string: an empty string is a PRESENT key, and NULL
	// must stay an omitted one.
	Deadline        *time.Time `json:"-" db:"deadline"`
	DeadlineDisplay *string    `json:"deadline,omitempty" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this goal's deadline; omitted when no deadline is set"`
	IsCompleted     bool       `json:"is_completed" db:"is_completed"`
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

// PopulateDateDisplay fills DeadlineDisplay from Deadline (#1631 Batch F2). Idempotent, so calling
// it again after mutating Deadline (e.g. in UpdateGoal) is the correct way to keep the two in
// sync — there is no dependency tracking; DeadlineDisplay is only as fresh as the last call.
func (sg *SavingsGoal) PopulateDateDisplay() {
	sg.DeadlineDisplay = formatNullableDateOnly(sg.Deadline)
}

// Validate checks that the SavingsGoal has all required fields and valid values.
func (sg *SavingsGoal) Validate() error {
	if sg.UserID == "" {
		return fmt.Errorf("%w: user_id is required", ErrInvalidSavingsGoal)
	}

	if sg.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidSavingsGoal)
	}

	// ⚠ #1625 final review I4: RUNES, not bytes. Huma's maxLength on the request structs counts
	// runes (JSON Schema semantics), so `len()` here made the published schema and the validator
	// disagree: a 60-character CJK name is 180 bytes, passes the schema Huma advertises, then is
	// rejected by a message that says "100 characters" while counting something else.
	if utf8.RuneCountInString(sg.Name) > 100 {
		return fmt.Errorf("%w: name must be 100 characters or less", ErrInvalidSavingsGoal)
	}

	if sg.TargetAmount <= 0 {
		return fmt.Errorf("%w: target_amount must be positive", ErrInvalidSavingsGoal)
	}

	if sg.CurrentAmount < 0 {
		return fmt.Errorf("%w: current_amount cannot be negative", ErrInvalidSavingsGoal)
	}

	if sg.CurrencyCode == "" {
		return fmt.Errorf("%w: currency_code is required", ErrInvalidSavingsGoal)
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
			return fmt.Errorf("%w: account_percentage must only be set when progress_basis is account_percentage", ErrInvalidSavingsGoal)
		}
	case ProgressBasisBalance:
		if sg.AccountPercentage != nil {
			return fmt.Errorf("%w: account_percentage must only be set when progress_basis is account_percentage", ErrInvalidSavingsGoal)
		}
		if sg.AccountID == "" {
			return fmt.Errorf("%w: account_id is required when progress_basis is balance", ErrInvalidSavingsGoal)
		}
	case ProgressBasisAccountPercentage:
		if sg.AccountPercentage == nil {
			return fmt.Errorf("%w: account_percentage is required when progress_basis is account_percentage", ErrInvalidSavingsGoal)
		}
		if *sg.AccountPercentage <= 0 || *sg.AccountPercentage > 1 {
			return fmt.Errorf("%w: account_percentage must be between 0 and 1 (exclusive 0, inclusive 1)", ErrInvalidSavingsGoal)
		}
		if sg.AccountID == "" {
			return fmt.Errorf("%w: account_id is required when progress_basis is account_percentage", ErrInvalidSavingsGoal)
		}
	default:
		return fmt.Errorf("%w: progress_basis must be one of: manual, contributions, balance, account_percentage", ErrInvalidSavingsGoal)
	}

	// account_id is required for all bases (savings goals always link to an account).
	if sg.AccountID == "" {
		return fmt.Errorf("%w: account_id is required", ErrInvalidSavingsGoal)
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
	// ArchivedAt marks a row as belonging to a CLOSED contributions period — set when the goal
	// left, or re-entered, the contributions progress_basis (#1625, migration 000190).
	//
	// The row is retained and still listed, but SumConfirmedByGoal and SumConfirmedByGoalInPeriod
	// exclude it, so a basis round-trip cannot count it alongside the synthetic seed that replaced
	// it. This field is what keeps the LEDGER and the TOTAL honest with each other: ListByGoal
	// deliberately still returns archived rows (retention: they are history, not rubbish), so
	// without an explicit marker the listed contributions would visibly fail to add up to the
	// goal's progress, with nothing on screen explaining why.
	ArchivedAt *time.Time `json:"archived_at,omitempty" db:"archived_at" doc:"Set when this contribution belongs to a closed contributions period. Such rows are retained and listed but do NOT count toward the goal's current_amount."`
}

// Validate checks SavingsGoalContribution required fields and invariants.
func (c *SavingsGoalContribution) Validate() error {
	if c.SavingsGoalID == "" {
		return fmt.Errorf("%w: savings_goal_id is required", ErrInvalidSavingsGoal)
	}

	if c.Amount <= 0 {
		return fmt.Errorf("%w: amount must be positive", ErrInvalidSavingsGoal)
	}

	validSources := map[string]bool{
		"manual":             true,
		"confirmed_transfer": true,
		"boost_match":        true,
	}
	if !validSources[c.Source] {
		return fmt.Errorf("%w: source must be one of: manual, confirmed_transfer, boost_match", ErrInvalidSavingsGoal)
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
		return fmt.Errorf("%w: status must be one of: proposed, confirmed, rejected", ErrInvalidSavingsGoal)
	}

	return nil
}

// LogContributionRequest is the body for POST /savings-goals/{goalID}/contributions.
// Creates a manually entered, confirmed contribution (D6 source-a / M20 / Issue #1300).
type LogContributionRequest struct {
	// Amount is the contribution amount (must be positive).
	Amount float64 `json:"amount" required:"true"`
	// OccurredOn is the date the contribution occurred (YYYY-MM-DD).
	// #1625: `format:"date"` is enforced by Huma at parse time. Without it a malformed date
	// (e.g. "09/08/2026") passed both Huma AND Validate() and only failed at the Postgres DATE
	// column, surfacing as a bare 500 instead of a 422 naming the field.
	OccurredOn string `json:"occurred_on" required:"true" format:"date" example:"2026-09-08"`
	// TransactionID optionally links to an existing transaction (must belong to the user).
	TransactionID *string `json:"transaction_id,omitempty" format:"uuid"`

	// #1625: `notes` is GONE from this request. It was accepted by the schema, never read by the
	// handler, and has no column on savings_goal_contributions — the API advertised storage that
	// does not exist and silently discarded whatever was sent. Removing the field is the honest
	// half of this ticket's theme; persisting notes properly needs a migration and a column, and
	// is a feature, not this bugfix. No data is lost, because none was ever stored.
}

// Validate checks LogContributionRequest fields.
func (r *LogContributionRequest) Validate() error {
	if r.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	if r.OccurredOn == "" {
		return errors.New("occurred_on is required")
	}
	// Belt-and-braces alongside the Huma `format:"date"` tag: any non-HTTP caller (a worker, a
	// test, a future internal path) still gets a 422-shaped error rather than a Postgres 500.
	if _, err := time.Parse("2006-01-02", r.OccurredOn); err != nil {
		return errors.New("occurred_on must be a YYYY-MM-DD date")
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
	// Deadline stays *time.Time — NOT serialized — mirroring SavingsGoal.Deadline above;
	// DeadlineDisplay is what actually reaches clients (#1631 Batch F2). This item isn't itself a
	// DB scan target (it's assembled in savings_progress_service.go from an already-populated
	// SavingsGoal), so DeadlineDisplay is set directly from goal.DeadlineDisplay at construction
	// rather than via its own PopulateDateDisplay call.
	Deadline        *time.Time `json:"-"`
	DeadlineDisplay *string    `json:"deadline,omitempty" format:"date" doc:"UTC calendar day (YYYY-MM-DD) this goal's deadline; omitted when no deadline is set"`
}

// SavingsProgressSummary wraps per-goal progress with portfolio-level totals.
type SavingsProgressSummary struct {
	Goals []SavingsGoalProgressItem `json:"goals"`
	// Portfolio totals (sum over all active goals)
	TotalSaved             float64 `json:"total_saved"`
	TotalTarget            float64 `json:"total_target"`
	OverallPercentComplete float64 `json:"overall_percent_complete"` // 0–100
	// GoalCount is the number of ACTIVE goals — the same universe as Goals, TotalSaved and
	// TotalTarget. It is NOT the user's total number of goals. GoalCount + CompletedGoalCount is,
	// for a user with no tombstoned goals.
	GoalCount int `json:"goal_count"`
	// CompletedGoalCount is the number of COMPLETED goals, counted OUTSIDE the active universe
	// above (Issue #1569 Task 20). It was previously derived by counting IsCompleted within that
	// universe, whose query filters `is_completed = false`, so it was structurally always 0 — a
	// required field of the JSON contract answering "how many goals have I completed?" with a
	// confident wrong zero.
	CompletedGoalCount int `json:"completed_goal_count"`
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
	// Name and TargetAmount carry Huma-native tags (not go-playground `validate:` syntax, which
	// Huma never reads — see the package-level note above CreateSavingsGoalRequest's sibling
	// domain.SavingsGoal.Validate() sentinel doc). This makes both constraints visible in the
	// published OpenAPI schema, so an MCP agent can learn "positive, <=100 chars" without first
	// submitting a bad value and reading the 4xx (#1625, second half).
	// minLength is load-bearing: Validate() rejects an empty name, and without it the published
	// schema said "1-100 characters" while accepting "" (#1625 final review I4).
	Name         string  `json:"name" required:"true" minLength:"1" maxLength:"100" doc:"Goal name (1-100 characters, counted in Unicode code points)."`
	Description  *string `json:"description,omitempty"`
	TargetAmount float64 `json:"target_amount" required:"true" exclusiveMinimum:"0" doc:"Target amount to save. Must be positive."`
	// CurrentAmount seeds the goal's progress. Under "manual" it is the value, and stays so.
	//
	// Under "contributions" it is IGNORED: a goal created this instant has no contributions, so
	// the row is written with zero regardless of what was sent.
	//
	// Under "balance" / "account_percentage" it is honoured at insert and then overwritten by
	// the first recompute — but only if a progress service is wired. The MCP create path builds
	// a transaction-local service with none, so there the seed PERSISTS until the next sync.
	// (An earlier version of this comment claimed every non-manual basis recomputed the value
	// away "immediately", which was not true of that path.)
	CurrentAmount float64 `json:"current_amount,omitempty" validate:"gte=0"`
	CurrencyCode  string  `json:"currency_code,omitempty"`
	AccountID     string  `json:"account_id" validate:"required,uuid"`
	// Deadline accepts EITHER a bare "YYYY-MM-DD" date-only string OR a full RFC3339 timestamp
	// (#1631 Batch F2) — today's frontend sends the RFC3339 form
	// (`new Date(dateOnly).toISOString()` in CreateSavingsGoalDialog.tsx), so that form must keep
	// working. Deliberately no `format:"..."` tag: see domain/flexible_date.go and the identical
	// note on CreateBudgetRequest.StartDate for why one would defeat this type's whole purpose.
	Deadline *FlexibleDate `json:"deadline,omitempty" doc:"Goal deadline. Accepts a bare YYYY-MM-DD date or a full RFC3339 timestamp."`
	// ProgressBasis controls how current_amount is derived (Issue #1300).
	// Defaults to "contributions" for NEW goals when empty (#1625 — the documented #1300 intent,
	// which the code had never implemented; it defaulted to "manual", so contributions could not
	// be logged against a brand-new goal). Valid: manual|contributions|balance|account_percentage.
	ProgressBasis string `json:"progress_basis,omitempty" enum:"manual,contributions,balance,account_percentage" doc:"How current_amount is derived. ⚠ BREAKING (#1625): omitting this now means \"contributions\", NOT \"manual\". A goal created without it derives progress from its contribution ledger, and a later PUT setting current_amount directly is refused with a 400. Pass \"manual\" explicitly if you intend to manage current_amount yourself."`
	// AccountPercentage is required (0 < x ≤ 1) when ProgressBasis is "account_percentage".
	AccountPercentage *float64 `json:"account_percentage,omitempty"`
}

// UpdateSavingsGoalRequest represents the request to update a savings goal.
type UpdateSavingsGoalRequest struct {
	// Name and TargetAmount carry Huma-native tags (see CreateSavingsGoalRequest above for why).
	Name         *string  `json:"name,omitempty" minLength:"1" maxLength:"100" doc:"Goal name (1-100 characters, counted in Unicode code points). Omit to leave unchanged; an empty string is refused, not treated as omitted."`
	Description  *string  `json:"description,omitempty"`
	TargetAmount *float64 `json:"target_amount,omitempty" exclusiveMinimum:"0" doc:"Target amount to save. Must be positive."`
	// CurrentAmount may ONLY be set when the goal's progress_basis is "manual" — every other
	// basis derives this value, and writing it by hand puts the row out of step with the source
	// it claims to track. Sending it on a derived-basis goal is a 400 (#1625).
	//
	// ⚠ The description below is not decoration: it is what an MCP agent reads. Both reviewers
	// flagged that making this a 400 without saying so in the published schema left the only
	// documentation an agent has steering it straight into the error — which is the same
	// undiscoverable-contract failure this whole ticket is about.
	CurrentAmount *float64 `json:"current_amount,omitempty" validate:"omitempty,gte=0" doc:"Only settable when progress_basis is 'manual'. Contributions-based goals derive this from their contribution ledger (POST /savings-goals/{goalID}/contributions); balance-based goals track their account. Sending this for any non-manual basis returns 400."`
	AccountID     *string  `json:"account_id,omitempty" validate:"omitempty,uuid"`
	// Deadline: same FlexibleDate acceptance (bare date OR RFC3339) as CreateSavingsGoalRequest
	// above, and the same reason for omitting a `format` tag — see there.
	Deadline *FlexibleDate `json:"deadline,omitempty" doc:"Goal deadline. Accepts a bare YYYY-MM-DD date or a full RFC3339 timestamp."`
	// ProgressBasis changes the derivation mode for current_amount (Issue #1300).
	// When nil, the existing basis is preserved.
	ProgressBasis *string `json:"progress_basis,omitempty"`
	// AccountPercentage is required (0 < x ≤ 1) when ProgressBasis is "account_percentage".
	AccountPercentage *float64 `json:"account_percentage,omitempty"`
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
	// ErrInvalidSavingsGoal is the field-validation sentinel for SavingsGoal / SavingsGoalContribution
	// (#1625, this ticket's headline defect surviving in a second domain: cluster A gave
	// categorization rules ErrInvalidRule + a mapper case; savings goals had no equivalent, so a
	// negative target_amount, an over-long name, or an invalid progress_basis/account_percentage
	// combination reached the mapper's default branch as a bare, unwrapped error and became a
	// 500 + Sentry event instead of a 4xx. Every Validate() failure in this file now wraps this
	// sentinel so internal/huma/errors/mapper.go can classify it; see the mapper case comment for
	// the ordering rationale mirrored from ErrInvalidRule/ErrInvalidCondition.
	ErrInvalidSavingsGoal = errors.New("invalid savings goal")

	// ErrInvalidGoalUpdate is a client mistake in an update request — mapped to 400, never 500.
	// #1625: introduced for "current_amount on a derived basis", which used to be written
	// silently and left the row claiming progress the ledger did not have.
	ErrInvalidGoalUpdate = errors.New("invalid savings goal update")

	ErrSavingsGoalNotFound      = errors.New("savings goal not found")
	ErrSavingsGoalAlreadyExists = errors.New("savings goal with this name already exists")

	// ErrSavingsGoalNotDeleted is returned by the purge path when the goal is still live.
	// Purging is the second half of a two-step: DELETE /savings-goals/{id} tombstones the goal and
	// keeps every contribution; POST /savings-goals/{id}/purge is what actually destroys them, and
	// it refuses on a goal the user has not already deleted. Same precondition shape as
	// repository.ErrAccountNotDisconnected on the account purge (Issue #1569, Task 6). Issue #1569.
	ErrSavingsGoalNotDeleted = errors.New("savings goal must be deleted before it can be purged")

	// ErrSavingsGoalBasisConflict is returned when a write that was computed for one
	// progress_basis reaches a row that is now on a DIFFERENT one — the goal moved underneath the
	// request. #1625: two distinct callers raise it, for the same reason.
	//
	//  1. UpdateWithBasisGuardInTx — a basis switch whose observed prior basis (read back under
	//     SELECT … FOR UPDATE) is not the one the caller planned the switch from. Two concurrent
	//     manual→contributions requests would otherwise BOTH archive and BOTH seed a synthetic
	//     carry-over contribution, and because a seed carries no transaction_id it sits outside
	//     the partial unique index — nothing else stops the second, and the goal reads double.
	//  2. UpdateCurrentAmount — a recompute that summed the ledger for basis X trying to land on
	//     a row that is now on basis Y. The value is right for a basis the row no longer has.
	//
	// It is a PRECONDITION failure, not a fault: the honest answer is 409 and "read it again",
	// which is why it must never surface as a 500 (see internal/huma/errors/mapper.go).
	ErrSavingsGoalBasisConflict = errors.New("savings goal progress_basis changed concurrently")

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
	// ErrContributionAlreadyRejected is returned when attempting to confirm a contribution that
	// is a rejected sticky tombstone (R3-7). Rejection is a deliberate user decision that must
	// not be reversible via confirm — see #1625 Task 9.
	ErrContributionAlreadyRejected = errors.New("contribution was already rejected and cannot be confirmed")
)
