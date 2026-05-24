package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
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

	// ListWebhookEvents lists webhook events with optional filters and pagination
	// Returns events, total count, and error
	ListWebhookEvents(ctx context.Context, filters domain.WebhookEventFilters, limit, offset int) ([]domain.PaddleWebhookEvent, int, error)

	// GetWebhookStats returns aggregate webhook statistics for a time window
	// timeRangeHours=0 means all time
	GetWebhookStats(ctx context.Context, timeRangeHours int) (*domain.WebhookStatsResponse, error)

	// ResetProcessingStatus resets a webhook's processing state so it can be replayed
	// Returns ErrPaddleWebhookNotFound if no webhook with that id exists
	ResetProcessingStatus(ctx context.Context, id string) error
}

// webhookOrderByEntry specifies a SQL column name for webhook sort ordering.
type webhookOrderByEntry struct {
	col string
}

// webhooksOrderByAllowlist maps frontend sort_by values to safe SQL column names.
// No NULLS LAST needed because none of these columns are nullable.
var webhooksOrderByAllowlist = map[string]webhookOrderByEntry{
	"created_at": {"created_at"},
	"attempts":   {"processing_attempts"},
	"status":     {"processed"},
}

// paddleWebhookRepository implements PaddleWebhookRepository using PostgreSQL
type paddleWebhookRepository struct {
	db  DB
	enc *appcrypto.FieldEncryptor
}

// NewPaddleWebhookRepository creates a new PaddleWebhookRepository instance
func NewPaddleWebhookRepository(db DB, enc *appcrypto.FieldEncryptor) PaddleWebhookRepository {
	return &paddleWebhookRepository{db: db, enc: enc}
}

// Create stores a new Paddle webhook event in the database
func (r *paddleWebhookRepository) Create(ctx context.Context, event *domain.PaddleWebhookEvent) error {
	// Generate UUID if not already set
	if event.ID == "" {
		event.ID = uuid.New().String()
	}

	encPayload, err := r.enc.EncryptJSON(event.Payload)
	if err != nil {
		return fmt.Errorf("encrypt paddle webhook payload: %w", err)
	}

	query := `
		INSERT INTO paddle_webhook_events (
			id, event_id, event_type, payload,
			signature, signature_valid, processed,
			processing_attempts
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at
	`

	err = r.db.QueryRow(ctx, query,
		event.ID,
		event.EventID,
		event.EventType,
		encPayload,
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

const paddleWebhookSelectQuery = `
	SELECT id, event_id, event_type, payload,
	       signature, signature_valid, processed, processed_at,
	       processing_error, processing_attempts,
	       created_at, updated_at
	FROM paddle_webhook_events`

// GetByID retrieves a Paddle webhook event by its UUID
func (r *paddleWebhookRepository) GetByID(ctx context.Context, id string) (*domain.PaddleWebhookEvent, error) {
	return r.scanSingleWebhook(ctx, paddleWebhookSelectQuery+" WHERE id = $1", id, "by id")
}

// GetByEventID retrieves a Paddle webhook event by Paddle's event_id
func (r *paddleWebhookRepository) GetByEventID(ctx context.Context, eventID string) (*domain.PaddleWebhookEvent, error) {
	return r.scanSingleWebhook(ctx, paddleWebhookSelectQuery+" WHERE event_id = $1", eventID, "by event_id")
}

// scanSingleWebhook is a helper that scans a single webhook from a query row
func (r *paddleWebhookRepository) scanSingleWebhook(ctx context.Context, query string, arg interface{}, context string) (*domain.PaddleWebhookEvent, error) {
	var event domain.PaddleWebhookEvent
	var rawPayload string
	err := r.db.QueryRow(ctx, query, arg).Scan(
		&event.ID,
		&event.EventID,
		&event.EventType,
		&rawPayload,
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
		return nil, fmt.Errorf("failed to get paddle webhook event %s: %w", context, err)
	}

	event.Payload, err = r.enc.DecryptJSON(rawPayload)
	if err != nil {
		return nil, fmt.Errorf("decrypt paddle webhook payload: %w", err)
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
		var rawPayload string
		err := rows.Scan(
			&event.ID,
			&event.EventID,
			&event.EventType,
			&rawPayload,
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
		event.Payload, err = r.enc.DecryptJSON(rawPayload)
		if err != nil {
			return nil, fmt.Errorf("decrypt paddle webhook payload: %w", err)
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

// ListWebhookEvents lists webhook events with optional filters and pagination
func (r *paddleWebhookRepository) ListWebhookEvents(ctx context.Context, filters domain.WebhookEventFilters, limit, offset int) ([]domain.PaddleWebhookEvent, int, error) {
	conditions := []string{}
	args := []interface{}{}
	argIdx := 1

	if filters.EventType != nil {
		conditions = append(conditions, fmt.Sprintf("event_type = $%d", argIdx))
		args = append(args, *filters.EventType)
		argIdx++
	}
	if filters.Processed != nil {
		conditions = append(conditions, fmt.Sprintf("processed = $%d", argIdx))
		args = append(args, *filters.Processed)
		argIdx++
	}
	if filters.StartDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, *filters.StartDate)
		argIdx++
	}
	if filters.EndDate != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, *filters.EndDate)
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM paddle_webhook_events %s", whereClause)
	var total int
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count paddle webhook events: %w", err)
	}

	entry := webhooksOrderByAllowlist[filters.SortBy]
	if entry.col == "" {
		entry = webhookOrderByEntry{"created_at"} // fallback
	}
	orderDir := strings.ToUpper(filters.SortDir)
	if orderDir != "ASC" && orderDir != "DESC" {
		orderDir = "DESC"
	}

	listQuery := fmt.Sprintf(
		`SELECT id, event_id, event_type, payload, signature, signature_valid, processed, processed_at, processing_error, processing_attempts, created_at, updated_at FROM paddle_webhook_events %s ORDER BY %s %s LIMIT $%d OFFSET $%d`,
		whereClause, entry.col, orderDir, argIdx, argIdx+1,
	)
	listArgs := append(args, limit, offset)

	rows, err := r.db.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list paddle webhook events: %w", err)
	}

	events, err := r.scanPaddleWebhookEvents(rows)
	if err != nil {
		return nil, 0, err
	}

	return events, total, nil
}

// GetWebhookStats returns aggregate webhook statistics for a time window
func (r *paddleWebhookRepository) GetWebhookStats(ctx context.Context, timeRangeHours int) (*domain.WebhookStatsResponse, error) {
	timeClause := ""
	if timeRangeHours > 0 {
		timeClause = fmt.Sprintf("WHERE created_at > NOW() - INTERVAL '%d hours'", timeRangeHours)
	}

	statsQuery := fmt.Sprintf(`
		SELECT
		    COUNT(*) as total,
		    COUNT(*) FILTER (WHERE processed = true) as processed,
		    COUNT(*) FILTER (WHERE processing_error IS NOT NULL) as failed,
		    COUNT(*) FILTER (WHERE processed = false) as unprocessed,
		    COALESCE(AVG(EXTRACT(epoch FROM (processed_at - created_at)) * 1000), 0)::BIGINT as avg_processing_time_ms
		FROM paddle_webhook_events %s
	`, timeClause)

	stats := &domain.WebhookStatsResponse{TimeRangeHours: timeRangeHours}
	err := r.db.QueryRow(ctx, statsQuery).Scan(
		&stats.Total,
		&stats.Processed,
		&stats.Failed,
		&stats.Unprocessed,
		&stats.AvgProcessingTimeMs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get paddle webhook stats: %w", err)
	}

	breakdownQuery := fmt.Sprintf(`
		SELECT
		    event_type,
		    COUNT(*) as count,
		    CASE WHEN COUNT(*) > 0 THEN
		        (COUNT(*) FILTER (WHERE processed = true)::float / COUNT(*)::float) * 100
		    ELSE 0 END as success_rate
		FROM paddle_webhook_events %s
		GROUP BY event_type
		ORDER BY count DESC
	`, timeClause)

	rows, err := r.db.Query(ctx, breakdownQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to get paddle webhook breakdown: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item domain.WebhookEventTypeStats
		if err := rows.Scan(&item.EventType, &item.Count, &item.SuccessRate); err != nil {
			return nil, fmt.Errorf("failed to scan webhook breakdown row: %w", err)
		}
		stats.EventTypeBreakdown = append(stats.EventTypeBreakdown, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating webhook breakdown: %w", err)
	}

	return stats, nil
}

// ResetProcessingStatus resets a webhook's processing state so it can be replayed
func (r *paddleWebhookRepository) ResetProcessingStatus(ctx context.Context, id string) error {
	query := `
		UPDATE paddle_webhook_events
		SET processed = false, processed_at = NULL, processing_error = NULL, updated_at = NOW()
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to reset paddle webhook processing status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrPaddleWebhookNotFound
	}

	return nil
}
