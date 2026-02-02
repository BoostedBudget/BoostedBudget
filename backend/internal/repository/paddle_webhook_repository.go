package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// pgx.Rows is the interface type from pgx package

var (
	// ErrPaddleWebhookNotFound is returned when a webhook event is not found in the database
	ErrPaddleWebhookNotFound = errors.New("paddle webhook not found")

	// ErrPaddleWebhookDuplicate is returned when attempting to create a webhook with duplicate event_id
	ErrPaddleWebhookDuplicate = errors.New("paddle webhook with this event_id already exists")
)

// PaddleWebhookRepository defines the interface for Paddle webhook event data access.
// Stores all incoming Paddle webhooks for audit, debugging, and idempotency.
//
// Issue #45, #46: Paddle Payment Integration
type PaddleWebhookRepository interface {
	// Create stores a new Paddle webhook event in the database
	// Returns ErrPaddleWebhookDuplicate if event_id already exists (idempotency)
	Create(ctx context.Context, event *domain.PaddleWebhookEvent) error

	// GetByID retrieves a Paddle webhook event by its UUID
	// Used by River workers to fetch webhook details for async processing
	// Returns ErrPaddleWebhookNotFound if webhook doesn't exist
	GetByID(ctx context.Context, id string) (*domain.PaddleWebhookEvent, error)

	// GetByEventID retrieves a Paddle webhook event by Paddle's event_id
	// Used for duplicate detection and idempotency checks
	// Returns ErrPaddleWebhookNotFound if webhook doesn't exist
	GetByEventID(ctx context.Context, eventID string) (*domain.PaddleWebhookEvent, error)

	// MarkProcessed marks a webhook event as successfully processed
	// Sets processed=true and processed_at=now()
	// Used after successful processing to prevent reprocessing
	MarkProcessed(ctx context.Context, id string) error

	// MarkFailed marks a webhook event as failed with error message
	// Increments processing_attempts and stores error for debugging
	// Used when webhook processing fails for retry logic
	MarkFailed(ctx context.Context, id string, errorMsg string) error

	// GetUnprocessed retrieves all unprocessed webhook events
	// Ordered by created_at ASC (oldest first) for FIFO processing
	// Used for manual replay and retry processing
	GetUnprocessed(ctx context.Context, limit int) ([]domain.PaddleWebhookEvent, error)

	// GetUnprocessedByType retrieves unprocessed webhooks of a specific event type
	// Filters: processed = false AND event_type = eventType
	// Ordered by created_at ASC (oldest first)
	// Used to process specific webhook types in batch
	GetUnprocessedByType(ctx context.Context, eventType string, limit int) ([]domain.PaddleWebhookEvent, error)

	// Update updates a webhook event's processing status
	// Used to track processing progress, attempts, and errors
	Update(ctx context.Context, event *domain.PaddleWebhookEvent) error

	// GetRecentWebhooks retrieves recent webhook events for admin dashboard
	// Returns the most recent N webhooks ordered by created_at DESC
	// Used for monitoring webhook delivery and processing
	GetRecentWebhooks(ctx context.Context, limit int) ([]domain.PaddleWebhookEvent, error)
}

// paddleWebhookRepository implements PaddleWebhookRepository using PostgreSQL
type paddleWebhookRepository struct {
	db DB
}

// NewPaddleWebhookRepository creates a new PaddleWebhookRepository instance
func NewPaddleWebhookRepository(db DB) PaddleWebhookRepository {
	return &paddleWebhookRepository{db: db}
}

// Create stores a new Paddle webhook event in the database
func (r *paddleWebhookRepository) Create(ctx context.Context, event *domain.PaddleWebhookEvent) error {
	// Generate UUID if not already set
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	query := `
		INSERT INTO paddle_webhook_events (
			id, event_id, event_type, payload,
			signature, signature_valid, processed,
			processing_attempts
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query,
		event.ID,
		event.EventID,
		event.EventType,
		event.Payload,
		event.Signature,
		event.SignatureValid,
		event.Processed,
		event.ProcessingAttempts,
	).Scan(&event.CreatedAt, &event.UpdatedAt)

	if err != nil {
		// Check for unique constraint violation on event_id
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			return ErrPaddleWebhookDuplicate
		}
		return fmt.Errorf("failed to create paddle webhook event: %w", err)
	}

	return nil
}

// GetByID retrieves a Paddle webhook event by its UUID
func (r *paddleWebhookRepository) GetByID(ctx context.Context, id string) (*domain.PaddleWebhookEvent, error) {
	query := `
		SELECT id, event_id, event_type, payload,
		       signature, signature_valid, processed, processed_at,
		       processing_error, processing_attempts,
		       created_at, updated_at
		FROM paddle_webhook_events
		WHERE id = $1
	`

	var event domain.PaddleWebhookEvent
	err := r.db.QueryRow(ctx, query, id).Scan(
		&event.ID,
		&event.EventID,
		&event.EventType,
		&event.Payload,
		&event.Signature,
		&event.SignatureValid,
		&event.Processed,
		&event.ProcessedAt,
		&event.ProcessingError,
		&event.ProcessingAttempts,
		&event.CreatedAt,
		&event.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaddleWebhookNotFound
		}
		return nil, fmt.Errorf("failed to get paddle webhook event by id: %w", err)
	}

	return &event, nil
}

// GetByEventID retrieves a Paddle webhook event by Paddle's event_id
func (r *paddleWebhookRepository) GetByEventID(ctx context.Context, eventID string) (*domain.PaddleWebhookEvent, error) {
	query := `
		SELECT id, event_id, event_type, payload,
		       signature, signature_valid, processed, processed_at,
		       processing_error, processing_attempts,
		       created_at, updated_at
		FROM paddle_webhook_events
		WHERE event_id = $1
	`

	var event domain.PaddleWebhookEvent
	err := r.db.QueryRow(ctx, query, eventID).Scan(
		&event.ID,
		&event.EventID,
		&event.EventType,
		&event.Payload,
		&event.Signature,
		&event.SignatureValid,
		&event.Processed,
		&event.ProcessedAt,
		&event.ProcessingError,
		&event.ProcessingAttempts,
		&event.CreatedAt,
		&event.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaddleWebhookNotFound
		}
		return nil, fmt.Errorf("failed to get paddle webhook event by event_id: %w", err)
	}

	return &event, nil
}

// MarkProcessed marks a webhook event as successfully processed
func (r *paddleWebhookRepository) MarkProcessed(ctx context.Context, id string) error {
	query := `
		UPDATE paddle_webhook_events
		SET processed = true,
		    processed_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to mark paddle webhook as processed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrPaddleWebhookNotFound
	}

	return nil
}

// MarkFailed marks a webhook event as failed with error message
func (r *paddleWebhookRepository) MarkFailed(ctx context.Context, id string, errorMsg string) error {
	query := `
		UPDATE paddle_webhook_events
		SET processing_error = $2,
		    processing_attempts = processing_attempts + 1,
		    updated_at = NOW()
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id, errorMsg)
	if err != nil {
		return fmt.Errorf("failed to mark paddle webhook as failed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrPaddleWebhookNotFound
	}

	return nil
}

// scanPaddleWebhookEvents scans rows into PaddleWebhookEvent slice
func (r *paddleWebhookRepository) scanPaddleWebhookEvents(rows pgx.Rows) ([]domain.PaddleWebhookEvent, error) {
	defer rows.Close()

	var events []domain.PaddleWebhookEvent
	for rows.Next() {
		var event domain.PaddleWebhookEvent
		err := rows.Scan(
			&event.ID,
			&event.EventID,
			&event.EventType,
			&event.Payload,
			&event.Signature,
			&event.SignatureValid,
			&event.Processed,
			&event.ProcessedAt,
			&event.ProcessingError,
			&event.ProcessingAttempts,
			&event.CreatedAt,
			&event.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan paddle webhook event: %w", err)
		}
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating paddle webhook events: %w", err)
	}
	return events, nil
}

// GetUnprocessed retrieves all unprocessed webhook events
func (r *paddleWebhookRepository) GetUnprocessed(ctx context.Context, limit int) ([]domain.PaddleWebhookEvent, error) {
	query := `
		SELECT id, event_id, event_type, payload,
		       signature, signature_valid, processed, processed_at,
		       processing_error, processing_attempts,
		       created_at, updated_at
		FROM paddle_webhook_events
		WHERE processed = false
		ORDER BY created_at ASC
		LIMIT $1
	`

	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get unprocessed paddle webhooks: %w", err)
	}

	return r.scanPaddleWebhookEvents(rows)
}

// GetUnprocessedByType retrieves unprocessed webhooks of a specific event type
func (r *paddleWebhookRepository) GetUnprocessedByType(ctx context.Context, eventType string, limit int) ([]domain.PaddleWebhookEvent, error) {
	query := `
		SELECT id, event_id, event_type, payload,
		       signature, signature_valid, processed, processed_at,
		       processing_error, processing_attempts,
		       created_at, updated_at
		FROM paddle_webhook_events
		WHERE processed = false AND event_type = $1
		ORDER BY created_at ASC
		LIMIT $2
	`

	rows, err := r.db.Query(ctx, query, eventType, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get unprocessed paddle webhooks by type: %w", err)
	}

	return r.scanPaddleWebhookEvents(rows)
}

// Update updates a webhook event's processing status
func (r *paddleWebhookRepository) Update(ctx context.Context, event *domain.PaddleWebhookEvent) error {
	query := `
		UPDATE paddle_webhook_events
		SET processed = $2,
		    processed_at = $3,
		    processing_error = $4,
		    processing_attempts = $5,
		    updated_at = NOW()
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query,
		event.ID,
		event.Processed,
		event.ProcessedAt,
		event.ProcessingError,
		event.ProcessingAttempts,
	)

	if err != nil {
		return fmt.Errorf("failed to update paddle webhook event: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrPaddleWebhookNotFound
	}

	return nil
}

// GetRecentWebhooks retrieves recent webhook events for admin dashboard
func (r *paddleWebhookRepository) GetRecentWebhooks(ctx context.Context, limit int) ([]domain.PaddleWebhookEvent, error) {
	query := `
		SELECT id, event_id, event_type, payload,
		       signature, signature_valid, processed, processed_at,
		       processing_error, processing_attempts,
		       created_at, updated_at
		FROM paddle_webhook_events
		ORDER BY created_at DESC
		LIMIT $1
	`

	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent paddle webhooks: %w", err)
	}

	return r.scanPaddleWebhookEvents(rows)
}
