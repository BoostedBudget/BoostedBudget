package domain

import (
	"errors"
	"time"
)

// SavingsGoal represents a user-defined savings goal linked to an account.
// Savings goals are FREE for all subscription tiers (Issue #27).
// Current amount is automatically synced from linked account balance (Issue #452).
type SavingsGoal struct {
	ID            string     `json:"id" db:"id"`
	UserID        string     `json:"user_id" db:"user_id"`
	Name          string     `json:"name" db:"name"`
	Description   *string    `json:"description,omitempty" db:"description"`
	TargetAmount  float64    `json:"target_amount" db:"target_amount"`
	CurrentAmount float64    `json:"current_amount" db:"current_amount"` // Synced from account balance
	CurrencyCode  string     `json:"currency_code" db:"currency_code"`
	AccountID     string     `json:"account_id" db:"account_id"` // Required linked account
	Deadline      *time.Time `json:"deadline,omitempty" db:"deadline"`
	IsCompleted   bool       `json:"is_completed" db:"is_completed"`
	CompletedAt   *time.Time `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`

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

	if sg.AccountID == "" {
		return errors.New("account_id is required")
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
