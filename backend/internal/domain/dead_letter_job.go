// Package domain contains core business entities for BoostedBudget.
package domain

import (
	"encoding/json"
	"time"
)

// DeadLetterJob represents a job that has exhausted all retry attempts.
// These jobs are stored for manual review and potential retry.
type DeadLetterJob struct {
	ID            int64           `json:"id"`
	OriginalJobID int64           `json:"original_job_id"`
	Kind          string          `json:"kind"`
	Queue         string          `json:"queue"`
	Args          json.RawMessage `json:"args"`
	Errors        json.RawMessage `json:"errors"` // Array of error messages from each attempt
	MaxAttempts   int             `json:"max_attempts"`
	CreatedAt     time.Time       `json:"created_at"`
	FailedAt      time.Time       `json:"failed_at"`
	UserID        *string         `json:"user_id,omitempty"`   // User who owns the failed job
	Notified      bool            `json:"notified"`            // Whether user has been notified
	RetriedAt     *time.Time      `json:"retried_at,omitempty"`
	RetryJobID    *int64          `json:"retry_job_id,omitempty"`
	Notes         *string         `json:"notes,omitempty"` // Admin notes
}

// JobError represents a single error from a job attempt.
type JobError struct {
	Attempt   int       `json:"attempt"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// DeadLetterJobFilters contains filter parameters for listing dead letter jobs.
type DeadLetterJobFilters struct {
	Kind      *string    `json:"kind,omitempty"`
	UserID    *string    `json:"user_id,omitempty"`
	StartDate *time.Time `json:"start_date,omitempty"`
	EndDate   *time.Time `json:"end_date,omitempty"`
	Retried   *bool      `json:"retried,omitempty"`
	SortBy    string     `json:"sort_by,omitempty"`  // validated enum: failed_at|kind|retried_at
	SortDir   string     `json:"sort_dir,omitempty"` // validated enum: asc|desc
}

// DeadLetterJobListResponse is the paginated response for listing dead letter jobs.
type DeadLetterJobListResponse struct {
	Jobs       []*DeadLetterJob `json:"jobs"`
	Total      int              `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	TotalPages int              `json:"total_pages"`
	HasMore    bool             `json:"has_more"`
}

// DeadLetterRetryResponse is returned after a retry attempt.
type DeadLetterRetryResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	NewJobID     *int64 `json:"new_job_id,omitempty"`
	DeadLetterID int64  `json:"dead_letter_id"`
}

// DeadLetterStatsResponse contains aggregate statistics about dead letter jobs.
type DeadLetterStatsResponse struct {
	Total       int64            `json:"total"`
	Unnotified  int64            `json:"unnotified"`
	Retried     int64            `json:"retried"`
	Last24Hours int64            `json:"last_24_hours"`
	ByKind      map[string]int64 `json:"by_kind"`
}

// MaskedDeadLetterJob is the same as DeadLetterJob but with Args and Errors redacted
// to prevent exposing sensitive data in list/detail views.
type MaskedDeadLetterJob struct {
	ID            int64           `json:"id"`
	OriginalJobID int64           `json:"original_job_id"`
	Kind          string          `json:"kind"`
	Queue         string          `json:"queue"`
	Args          json.RawMessage `json:"args"`
	Errors        json.RawMessage `json:"errors"`
	MaxAttempts   int             `json:"max_attempts"`
	CreatedAt     time.Time       `json:"created_at"`
	FailedAt      time.Time       `json:"failed_at"`
	UserID        *string         `json:"user_id,omitempty"`
	Notified      bool            `json:"notified"`
	RetriedAt     *time.Time      `json:"retried_at,omitempty"`
	RetryJobID    *int64          `json:"retry_job_id,omitempty"`
	Notes         *string         `json:"notes,omitempty"`
}

// DeadLetterBulkDeleteRequest is the request body for bulk-deleting dead letter jobs.
type DeadLetterBulkDeleteRequest struct {
	Kind   *string `json:"kind,omitempty"`
	UserID *string `json:"user_id,omitempty"`
	// StartDate/EndDate carry format:"date" so the published contract stops advertising a
	// bare string here. The format is necessary but NOT sufficient: "0001-01-01" satisfies
	// it and still selects every row, which is why the handler also bounds the row count.
	StartDate *string `json:"start_date,omitempty" format:"date" doc:"Inclusive lower bound on failed_at (YYYY-MM-DD)"`
	EndDate   *string `json:"end_date,omitempty" format:"date" doc:"Inclusive upper bound on failed_at (YYYY-MM-DD)"`
	Retried   *bool   `json:"retried,omitempty"`
	// ExpectedCount confirms a deletion larger than MaxUnconfirmedDLQBulkDelete. It is a
	// confirmation token, not a limit: the value must equal the count the server itself
	// computed for these exact filters, so it can only be supplied after the server has
	// disclosed that count in a 409. It cannot be used to raise the ceiling.
	ExpectedCount *int64 `json:"expected_count,omitempty" minimum:"0" doc:"Exact number of rows the caller expects to delete; required when the match exceeds the unconfirmed ceiling"`
}

// DeadLetterBulkDeleteResponse is returned after a bulk delete operation.
type DeadLetterBulkDeleteResponse struct {
	Deleted int64 `json:"deleted"`
}
