// Package domain contains domain models for rollover history tracking.
// Issue #36: Rule Executors - Envelope budgeting rollover tracking.
package domain

import (
	"errors"
	"time"
)

// =============================================================================
// Rollover Status
// =============================================================================

// RolloverStatus represents the status of a rollover.
type RolloverStatus string

// Supported rollover statuses (matches database CHECK constraint).
const (
	RolloverStatusPending  RolloverStatus = "pending"
	RolloverStatusApplied  RolloverStatus = "applied"
	RolloverStatusRejected RolloverStatus = "rejected"
)

// validRolloverStatuses is a set of all valid rollover statuses.
var validRolloverStatuses = map[RolloverStatus]bool{
	RolloverStatusPending:  true,
	RolloverStatusApplied:  true,
	RolloverStatusRejected: true,
}

// IsValid checks if the rollover status is valid.
func (rs RolloverStatus) IsValid() bool {
	return validRolloverStatuses[rs]
}

// =============================================================================
// Rollover Type
// =============================================================================

// RolloverType represents the type of rollover calculation.
type RolloverType string

// Supported rollover types (matches database CHECK constraint).
const (
	RolloverTypeFull       RolloverType = "full"       // Roll over entire remaining amount
	RolloverTypeCapped     RolloverType = "capped"     // Roll over up to a maximum amount
	RolloverTypePercentage RolloverType = "percentage" // Roll over a percentage of remaining
)

// validRolloverTypes is a set of all valid rollover types.
var validRolloverTypes = map[RolloverType]bool{
	RolloverTypeFull:       true,
	RolloverTypeCapped:     true,
	RolloverTypePercentage: true,
}

// IsValid checks if the rollover type is valid.
func (rt RolloverType) IsValid() bool {
	return validRolloverTypes[rt]
}

// =============================================================================
// RolloverHistory
// =============================================================================

// RolloverHistory tracks envelope budgeting category rollovers.
// Records when unspent funds are rolled over from one budget category to another
// or back to unallocated funds. This provides audit trail and history for users.
type RolloverHistory struct {
	ID                     string         `json:"id" db:"id"`
	UserID                 string         `json:"user_id" db:"user_id"`
	BudgetID               string         `json:"budget_id" db:"budget_id"`
	RuleID                 *string        `json:"rule_id,omitempty" db:"rule_id"`
	RuleExecutionID        *string        `json:"rule_execution_id,omitempty" db:"rule_execution_id"`
	SourceBudgetCategoryID string         `json:"source_budget_category_id" db:"source_budget_category_id"`
	TargetBudgetCategoryID *string        `json:"target_budget_category_id,omitempty" db:"target_budget_category_id"` // nil = to unallocated funds
	Amount                 float64        `json:"amount" db:"amount"`
	RolloverType           RolloverType   `json:"rollover_type" db:"rollover_type"`
	BudgetPeriodStart      time.Time      `json:"budget_period_start" db:"budget_period_start"`
	BudgetPeriodEnd        time.Time      `json:"budget_period_end" db:"budget_period_end"`
	Status                 RolloverStatus `json:"status" db:"status"`
	AppliedAt              *time.Time     `json:"applied_at,omitempty" db:"applied_at"`
	CreatedAt              time.Time      `json:"created_at" db:"created_at"`
}

// Validate checks that the RolloverHistory has all required fields and valid values.
func (rh *RolloverHistory) Validate() error {
	if rh.UserID == "" {
		return errors.New("user_id is required")
	}

	if rh.BudgetID == "" {
		return errors.New("budget_id is required")
	}

	if rh.SourceBudgetCategoryID == "" {
		return errors.New("source_budget_category_id is required")
	}

	if rh.Amount <= 0 {
		return errors.New("amount must be positive")
	}

	if !rh.RolloverType.IsValid() {
		return errors.New("invalid rollover_type: must be full, capped, or percentage")
	}

	if !rh.Status.IsValid() {
		return errors.New("invalid status: must be pending, applied, or rejected")
	}

	if rh.BudgetPeriodStart.IsZero() {
		return errors.New("budget_period_start is required")
	}

	if rh.BudgetPeriodEnd.IsZero() {
		return errors.New("budget_period_end is required")
	}

	if !rh.BudgetPeriodEnd.After(rh.BudgetPeriodStart) {
		return errors.New("budget_period_end must be after budget_period_start")
	}

	return nil
}

// MarkApplied marks the rollover as applied with timestamp.
func (rh *RolloverHistory) MarkApplied() {
	now := time.Now()
	rh.Status = RolloverStatusApplied
	rh.AppliedAt = &now
}

// MarkRejected marks the rollover as rejected.
func (rh *RolloverHistory) MarkRejected() {
	rh.Status = RolloverStatusRejected
}

// IsToUnallocated returns true if the rollover goes to unallocated funds.
func (rh *RolloverHistory) IsToUnallocated() bool {
	return rh.TargetBudgetCategoryID == nil
}

// Rollover-related errors.
var (
	ErrRolloverHistoryNotFound = errors.New("rollover history not found")
)
