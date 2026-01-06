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
