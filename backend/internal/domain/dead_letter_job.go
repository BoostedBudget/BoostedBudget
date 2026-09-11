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
	UserID        *string         `json:"user_id,omitempty"` // User who owns the failed job
	Notified      bool            `json:"notified"`          // Whether user has been notified
	RetriedAt     *time.Time      `json:"retried_at,omitempty"`
	RetryJobID    *int64          `json:"retry_job_id,omitempty"`
	Notes         *string         `json:"notes,omitempty"` // Admin notes
}

// JobError represents a single error from a job attempt.
type JobError struct {
	Attempt   int       `json:"attempt"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`

	// Everything below was added by #1631 and is OPTIONAL in both directions.
	//
	// `dead_letter_jobs.errors` is an existing JSON column with production rows already in it,
	// written in the old {attempt, message, timestamp} shape. Those rows decode with these
	// fields simply absent, and `omitempty` keeps a row written for a non-Postgres failure
	// byte-identical to what the old code produced. `message` above is never rewritten or
	// dropped — it stays the flattened one-line form every existing consumer reads.

	// Chain is the wrapped error chain, outermost boundary first, with each layer's own
	// contribution only (the wrapped error's text is trimmed off the tail). It exists because
	// the flattened `message` concatenates every layer into one line, hiding WHICH boundary
	// failed. Absent when the error had no wrapping to record.
	Chain []string `json:"chain,omitempty"`

	// PgError is everything Postgres actually sent, when a *pgconn.PgError is anywhere in the
	// chain. Found with errors.As, never by string-matching the message.
	PgError *PgErrorContext `json:"pg_error,omitempty"`
}

// PgErrorContext is the structured payload of a *pgconn.PgError.
//
// ⚠ PII: Detail, Hint and Message can all embed ROW VALUES. For a 23505 the Detail reads
// `Key (account_id, security_id)=(<uuid>, <uuid>) already exists.` — harmless — but the same
// mechanism on a unique-email constraint prints `Key (email)=(someone@example.com) already
// exists.` Anything rendering these three fields must carry `data-ph-mask`, and they must NEVER
// be interpolated into an `aria-label`/`title`/`alt`/`placeholder`, which session replay records
// verbatim and cannot mask. Code, ConstraintName, TableName, SchemaName, ColumnName, Severity and
// Routine are schema/identifier metadata, not row data, and stay legible.
type PgErrorContext struct {
	Code           string `json:"code,omitempty"` // SQLSTATE, e.g. "23505"
	Severity       string `json:"severity,omitempty"`
	Message        string `json:"message,omitempty"`
	Detail         string `json:"detail,omitempty"`
	Hint           string `json:"hint,omitempty"`
	ConstraintName string `json:"constraint_name,omitempty"`
	TableName      string `json:"table_name,omitempty"`
	SchemaName     string `json:"schema_name,omitempty"`
	ColumnName     string `json:"column_name,omitempty"`
	Routine        string `json:"routine,omitempty"`
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

// MaskedDeadLetterJob was DELETED (round 16, batch EC). It claimed "Args and Errors redacted",
// which nothing implemented: it had no constructor, no conversion, and no reference outside its
// own declaration, while `DeadLetterJob` had 44. It documented a contract the code does not
// honour, and reviewers on this branch cited it as evidence that DLQ args were safe.
//
// The masking that actually runs is services.MaskJobArgs / services.MaskJobErrors, and it is
// PARTIAL by design: MaskJobArgs redacts a fixed 7-entry denylist of TOP-LEVEL string keys with
// no recursion, so real args keep `user_id`, `clerk_user_id`, `linked_item_id`, `item_id` and a
// free-text `reason`. It was deliberately left as-is rather than given recursion: recursion
// would not redact any of those five (they are not on the denylist), so it would deepen the
// false impression of completeness without removing a single field that leaks today. The
// admin-facing exposure is closed where it mattered — DeadLetterDetailModal now carries
// data-ph-mask on both the server-masked and the revealed raw args blocks, so session replay
// no longer transmits either. Widening the denylist is a separate, deliberate decision.

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
