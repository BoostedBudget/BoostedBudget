package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

var (
	// ErrWebhookNotFound is returned when a webhook event is not found in the database
	ErrWebhookNotFound = errors.New("webhook not found")
)

// WebhookRepository defines the interface for webhook event data access.
// Stores all incoming Plaid webhooks for audit, debugging, and GDPR compliance.
//
// Issue #202: Plaid Webhook Integration
// Issue #256: Transaction Sync Service - Added Update and GetUnprocessedByType
// Issue #573: River Queue - Added GetByID for async job processing
type WebhookRepository interface {
	// Create stores a new webhook event in the database
	Create(ctx context.Context, event *domain.WebhookEvent) error

	// GetByID retrieves a webhook event by its ID (Issue #573)
	// Used by River workers to fetch webhook details for async processing
	// Returns domain.ErrNotFound if webhook doesn't exist
	GetByID(ctx context.Context, id string) (*domain.WebhookEvent, error)

	// MarkProcessed marks a webhook event as processed
	// Used after successful processing to prevent double-processing
	MarkProcessed(ctx context.Context, id string) error

	// GetUnprocessed retrieves all unprocessed webhook events
	// Ordered by created_at ASC (oldest first) for manual replay
	GetUnprocessed(ctx context.Context) ([]domain.WebhookEvent, error)

	// GetByItemID retrieves webhook events for a specific Plaid item
	// Ordered by created_at DESC (newest first) with limit
	// Used for debugging and viewing webhook history per item
	GetByItemID(ctx context.Context, itemID string, limit int) ([]domain.WebhookEvent, error)

	// Update updates webhook processing metadata (Issue #256)
	// Used to track processing status, retries, and errors
	// Updates: processed, processing_started_at, processing_completed_at,
	//          processing_error, retry_count, next_retry_at
	Update(ctx context.Context, event *domain.WebhookEvent) error

	// GetUnprocessedByType retrieves unprocessed webhooks of specific type (Issue #256)
	// Filters:
	//   - processed = false
	//   - webhook_type = webhookType
	//   - next_retry_at IS NULL OR next_retry_at <= NOW() (respects retry schedule)
	// Orders: created_at ASC (oldest first for FIFO processing)
	// Used by WebhookProcessor to fetch TRANSACTIONS webhooks
	GetUnprocessedByType(ctx context.Context, webhookType string, limit int) ([]domain.WebhookEvent, error)
}

// webhookRepository implements WebhookRepository using PostgreSQL
type webhookRepository struct {
	db DB
}

// NewWebhookRepository creates a new WebhookRepository instance
func NewWebhookRepository(db DB) WebhookRepository {
	return &webhookRepository{db: db}
}

// Create stores a new webhook event in the database
func (r *webhookRepository) Create(ctx context.Context, event *domain.WebhookEvent) error {
	// Generate UUID if not already set
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	query := `
		INSERT INTO webhook_events (
			webhook_type, webhook_code, item_id, error_code,
			new_transactions, removed_transactions, payload,
			signature, signature_valid, processed
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query,
		event.WebhookType,
		event.WebhookCode,
		event.ItemID,
		event.ErrorCode,
		event.NewTransactions,
		event.RemovedTransactions,
		event.Payload,
		event.Signature,
		event.SignatureValid,
		event.Processed,
	).Scan(&event.ID, &event.CreatedAt, &event.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create webhook event: %w", err)
	}

	return nil
}

// GetByID retrieves a webhook event by its ID
// Used by River workers to fetch webhook details for async processing
func (r *webhookRepository) GetByID(ctx context.Context, id string) (*domain.WebhookEvent, error) {
	query := `
		SELECT id, webhook_type, webhook_code, item_id, error_code,
		       new_transactions, removed_transactions, payload,
		       signature, signature_valid, processed, processed_at,
		       processing_started_at, processing_completed_at, processing_error,
		       retry_count, next_retry_at,
		       created_at, updated_at
		FROM webhook_events
		WHERE id = $1
	`

	var event domain.WebhookEvent
	err := r.db.QueryRow(ctx, query, id).Scan(
		&event.ID,
		&event.WebhookType,
		&event.WebhookCode,
		&event.ItemID,
		&event.ErrorCode,
		&event.NewTransactions,
		&event.RemovedTransactions,
		&event.Payload,
		&event.Signature,
		&event.SignatureValid,
		&event.Processed,
		&event.ProcessedAt,
		&event.ProcessingStartedAt,
		&event.ProcessingCompletedAt,
		&event.ProcessingError,
		&event.RetryCount,
		&event.NextRetryAt,
		&event.CreatedAt,
		&event.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWebhookNotFound
		}
		return nil, fmt.Errorf("failed to get webhook event by id: %w", err)
	}

	return &event, nil
}

// MarkProcessed marks a webhook event as processed
func (r *webhookRepository) MarkProcessed(ctx context.Context, id string) error {
	query := `
		UPDATE webhook_events
		SET processed = true, processed_at = $1
		WHERE id = $2
	`

	now := time.Now()
	_, err := r.db.Exec(ctx, query, now, id)
	if err != nil {
		return fmt.Errorf("failed to mark webhook as processed: %w", err)
	}

	return nil
}

// GetUnprocessed retrieves all unprocessed webhook events
func (r *webhookRepository) GetUnprocessed(ctx context.Context) ([]domain.WebhookEvent, error) {
	query := `
		SELECT id, webhook_type, webhook_code, item_id, error_code,
		       new_transactions, removed_transactions, payload,
		       signature, signature_valid, processed, processed_at,
		       created_at, updated_at
		FROM webhook_events
		WHERE processed = false
		ORDER BY created_at ASC
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get unprocessed webhooks: %w", err)
	}
	defer rows.Close()

	var events []domain.WebhookEvent
	for rows.Next() {
		var event domain.WebhookEvent
		err := rows.Scan(
			&event.ID,
			&event.WebhookType,
			&event.WebhookCode,
			&event.ItemID,
			&event.ErrorCode,
			&event.NewTransactions,
			&event.RemovedTransactions,
			&event.Payload,
			&event.Signature,
			&event.SignatureValid,
			&event.Processed,
			&event.ProcessedAt,
			&event.CreatedAt,
			&event.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan webhook event: %w", err)
		}
		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating webhook events: %w", err)
	}

	return events, nil
}

// GetByItemID retrieves webhook events for a specific Plaid item
func (r *webhookRepository) GetByItemID(ctx context.Context, itemID string, limit int) ([]domain.WebhookEvent, error) {
	query := `
		SELECT id, webhook_type, webhook_code, item_id, error_code,
		       new_transactions, removed_transactions, payload,
		       signature, signature_valid, processed, processed_at,
		       created_at, updated_at
		FROM webhook_events
		WHERE item_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := r.db.Query(ctx, query, itemID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get webhooks by item_id: %w", err)
	}
	defer rows.Close()

	var events []domain.WebhookEvent
	for rows.Next() {
		var event domain.WebhookEvent
		err := rows.Scan(
			&event.ID,
			&event.WebhookType,
			&event.WebhookCode,
			&event.ItemID,
			&event.ErrorCode,
			&event.NewTransactions,
			&event.RemovedTransactions,
			&event.Payload,
			&event.Signature,
			&event.SignatureValid,
			&event.Processed,
			&event.ProcessedAt,
			&event.CreatedAt,
			&event.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan webhook event: %w", err)
		}
		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating webhook events: %w", err)
	}

	return events, nil
}

// ============================================================================
// Issue #256: Transaction Sync Service - Webhook Processing Methods
// ============================================================================

// Update updates webhook processing metadata
// Used to track processing attempts, retries, and errors
func (r *webhookRepository) Update(ctx context.Context, event *domain.WebhookEvent) error {
	query := `
		UPDATE webhook_events SET
			processed = $1,
			processing_started_at = $2,
			processing_completed_at = $3,
			processing_error = $4,
			retry_count = $5,
			next_retry_at = $6,
			updated_at = NOW()
		WHERE id = $7
	`

	_, err := r.db.Exec(ctx, query,
		event.Processed,
		event.ProcessingStartedAt,
		event.ProcessingCompletedAt,
		event.ProcessingError,
		event.RetryCount,
		event.NextRetryAt,
		event.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update webhook event: %w", err)
	}

	return nil
}

// GetUnprocessedByType retrieves unprocessed webhooks of a specific type
// Filters by:
//   - processed = false
//   - webhook_type = webhookType
//   - (next_retry_at IS NULL OR next_retry_at <= NOW()) - respects retry schedule
//
// Orders by: created_at ASC (oldest first for FIFO processing)
// Used by WebhookProcessor to fetch TRANSACTIONS webhooks ready for processing
func (r *webhookRepository) GetUnprocessedByType(ctx context.Context, webhookType string, limit int) ([]domain.WebhookEvent, error) {
	query := `
		SELECT id, webhook_type, webhook_code, item_id, error_code,
		       new_transactions, removed_transactions, payload,
		       signature, signature_valid, processed, processed_at,
		       processing_started_at, processing_completed_at, processing_error,
		       retry_count, next_retry_at,
		       created_at, updated_at
		FROM webhook_events
		WHERE processed = false
		  AND webhook_type = $1
		  AND (next_retry_at IS NULL OR next_retry_at <= NOW())
		ORDER BY created_at ASC
		LIMIT $2
	`

	rows, err := r.db.Query(ctx, query, webhookType, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get unprocessed webhooks by type: %w", err)
	}
	defer rows.Close()

	var events []domain.WebhookEvent
	for rows.Next() {
		var event domain.WebhookEvent
		err := rows.Scan(
			&event.ID,
			&event.WebhookType,
			&event.WebhookCode,
			&event.ItemID,
			&event.ErrorCode,
			&event.NewTransactions,
			&event.RemovedTransactions,
			&event.Payload,
			&event.Signature,
			&event.SignatureValid,
			&event.Processed,
			&event.ProcessedAt,
			&event.ProcessingStartedAt,
			&event.ProcessingCompletedAt,
			&event.ProcessingError,
			&event.RetryCount,
			&event.NextRetryAt,
			&event.CreatedAt,
			&event.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan webhook event: %w", err)
		}
		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating webhook events: %w", err)
	}

	return events, nil
}
