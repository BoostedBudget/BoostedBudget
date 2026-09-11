// Package river provides the River implementation of the queue interfaces.
package river

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// DeadLetterHandler handles jobs that have exhausted all retry attempts.
// It moves failed jobs to a dead letter table for manual review and
// optionally notifies users about failures that affect them.
type DeadLetterHandler struct {
	repo      *repository.DeadLetterJobRepository
	logger    *slog.Logger
	analytics phclient.Analytics
}

// DeadLetterHandlerDeps contains dependencies for DeadLetterHandler.
type DeadLetterHandlerDeps struct {
	DB     *pgxpool.Pool
	Logger *slog.Logger
}

// NewDeadLetterHandler creates a new dead letter handler.
func NewDeadLetterHandler(deps DeadLetterHandlerDeps) *DeadLetterHandler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &DeadLetterHandler{
		repo:   repository.NewDeadLetterJobRepository(deps.DB),
		logger: logger,
	}
}

// SetAnalytics injects the analytics client for capturing dead letter events.
func (h *DeadLetterHandler) SetAnalytics(analytics phclient.Analytics) {
	h.analytics = analytics
}

// HandleError is called by River when a job has exhausted all retry attempts.
// It stores the job in the dead letter queue for manual review.
func (h *DeadLetterHandler) HandleError(ctx context.Context, job *rivertype.JobRow, err error) *river.ErrorHandlerResult {
	// Only handle jobs that have exhausted all attempts
	if job.Attempt < job.MaxAttempts {
		return nil // Let River handle the retry
	}

	h.logger.Info("job exhausted all retries, moving to dead letter queue",
		"job_id", job.ID,
		"kind", job.Kind,
		"queue", job.Queue,
		"attempt", job.Attempt,
		"max_attempts", job.MaxAttempts,
		"error", err.Error(),
	)

	// Build error history from job errors
	errors := buildErrorHistory(job, err)
	errorsJSON, _ := json.Marshal(errors)

	// Extract user ID from job args if possible
	userID := repository.UserIDExtractor(job.Kind, job.EncodedArgs)

	// Create dead letter job record
	deadLetterJob := &domain.DeadLetterJob{
		OriginalJobID: job.ID,
		Kind:          job.Kind,
		Queue:         job.Queue,
		Args:          job.EncodedArgs,
		Errors:        errorsJSON,
		MaxAttempts:   job.MaxAttempts,
		CreatedAt:     job.CreatedAt,
		FailedAt:      time.Now(),
		UserID:        userID,
		Notified:      false,
	}

	if err := h.repo.Create(ctx, deadLetterJob); err != nil {
		h.logger.Error("failed to create dead letter job record",
			"job_id", job.ID,
			"kind", job.Kind,
			"error", err.Error(),
		)
		// Don't return error - let the job fail naturally
		// The dead letter insert failure shouldn't affect job completion
	} else {
		h.logger.Info("job moved to dead letter queue",
			"job_id", job.ID,
			"dead_letter_id", deadLetterJob.ID,
			"user_id", userID,
		)
		if h.analytics != nil {
			h.analytics.Capture("system", phclient.EventDeadLetterCreated, map[string]interface{}{
				"job_kind": job.Kind,
				"queue":    job.Queue,
			})
		}
	}

	return nil
}

// buildErrorHistory extracts error history from job errors and current error.
func buildErrorHistory(job *rivertype.JobRow, currentErr error) []domain.JobError {
	var errors []domain.JobError

	// Add historical errors from job
	for _, attemptErr := range job.Errors {
		errors = append(errors, domain.JobError{
			Attempt:   attemptErr.Attempt,
			Message:   attemptErr.Error,
			Timestamp: attemptErr.At,
		})
	}

	// Add current error, enriched with the structure the error value actually carries (#1631).
	//
	// Only this attempt can be enriched: River hands historical attempts back as plain strings
	// (rivertype.AttemptError.Error), so the error VALUES for earlier attempts are gone by the
	// time this runs and there is nothing to run errors.As against. That is acceptable because
	// the final attempt is the one an operator triages, and the gap is why the fields are
	// optional rather than required.
	current := domain.JobError{
		Attempt:   job.Attempt,
		Message:   currentErr.Error(),
		Timestamp: time.Now(),
	}
	current.Chain = errorChain(currentErr)
	current.PgError = pgErrorContext(currentErr)
	errors = append(errors, current)

	return errors
}

// ErrorHandlerFunc returns a River ErrorHandler function that can be used
// with River's Config.ErrorHandler.
func (h *DeadLetterHandler) ErrorHandlerFunc() river.ErrorHandler {
	return &riverErrorHandler{handler: h}
}

// riverErrorHandler adapts DeadLetterHandler to River's ErrorHandler interface.
type riverErrorHandler struct {
	handler *DeadLetterHandler
}

// HandleError implements river.ErrorHandler.
func (e *riverErrorHandler) HandleError(ctx context.Context, job *rivertype.JobRow, err error) *river.ErrorHandlerResult {
	return e.handler.HandleError(ctx, job, err)
}

// HandlePanic implements river.ErrorHandler.
func (e *riverErrorHandler) HandlePanic(ctx context.Context, job *rivertype.JobRow, panicVal any, trace string) *river.ErrorHandlerResult {
	e.handler.logger.Error("job panicked",
		"job_id", job.ID,
		"kind", job.Kind,
		"panic", panicVal,
		"trace", trace,
	)
	// Treat panics like errors - if exhausted, move to dead letter
	if job.Attempt >= job.MaxAttempts {
		return e.handler.HandleError(ctx, job, &panicError{val: panicVal, trace: trace})
	}
	return nil
}

// panicError wraps a panic value as an error.
type panicError struct {
	val   any
	trace string
}

func (e *panicError) Error() string {
	return "panic: " + e.trace
}
