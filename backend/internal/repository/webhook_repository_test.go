package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TDD: Phase 3 - Repository Layer Tests
// Issue #202: Plaid Webhook Integration
// Following RED-GREEN-REFACTOR: Write tests first, then implement

func TestWebhookEventCreate_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()

	// Test webhook event
	payload := json.RawMessage(`{"webhook_type":"TRANSACTIONS","webhook_code":"DEFAULT_UPDATE"}`)
	signature := "test-jwt-signature"
	webhookCode := "DEFAULT_UPDATE"
	itemID := "test-item-123"
	newTxCount := 5

	event := &domain.WebhookEvent{
		WebhookType:     "TRANSACTIONS",
		WebhookCode:     &webhookCode,
		ItemID:          &itemID,
		NewTransactions: &newTxCount,
		Payload:         payload,
		Signature:       &signature,
		SignatureValid:  true,
		Processed:       false,
	}

	// Mock the INSERT query
	mock.ExpectQuery(`INSERT INTO webhook_events`).
		WithArgs(
			"TRANSACTIONS",
			&webhookCode,
			&itemID,
			sqlmock.AnyArg(), // error_code
			&newTxCount,
			sqlmock.AnyArg(), // removed_transactions
			payload,
			&signature,
			true,
			false,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow("test-id-123", time.Now(), time.Now()))

	// Execute
	err = repo.Create(ctx, event)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, "test-id-123", event.ID)
	assert.NotZero(t, event.CreatedAt)
	assert.NotZero(t, event.UpdatedAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookEventCreate_DatabaseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()

	payload := json.RawMessage(`{"webhook_type":"TRANSACTIONS"}`)
	event := &domain.WebhookEvent{
		WebhookType: "TRANSACTIONS",
		Payload:     payload,
	}

	// Mock database error
	mock.ExpectQuery(`INSERT INTO webhook_events`).
		WillReturnError(assert.AnError)

	// Execute
	err = repo.Create(ctx, event)

	// Assert
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookEventMarkProcessed_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()
	webhookID := "test-id-123"

	// Mock the UPDATE query (expects timestamp + id)
	mock.ExpectExec(`UPDATE webhook_events SET processed`).
		WithArgs(sqlmock.AnyArg(), webhookID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Execute
	err = repo.MarkProcessed(ctx, webhookID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookEventMarkProcessed_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()
	webhookID := "nonexistent-id"

	// Mock no rows affected (expects timestamp + id)
	mock.ExpectExec(`UPDATE webhook_events SET processed`).
		WithArgs(sqlmock.AnyArg(), webhookID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Execute
	err = repo.MarkProcessed(ctx, webhookID)

	// Assert - should not error, just no update
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookEventGetUnprocessed_Multiple(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()

	// Mock SELECT query
	rows := sqlmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"created_at", "updated_at",
	}).
		AddRow(
			"id-1", "TRANSACTIONS", "DEFAULT_UPDATE", "item-1", nil,
			5, nil, json.RawMessage(`{}`),
			"sig-1", true, false, nil,
			time.Now(), time.Now(),
		).
		AddRow(
			"id-2", "ITEM", "ERROR", "item-2", "ITEM_LOGIN_REQUIRED",
			nil, nil, json.RawMessage(`{}`),
			"sig-2", true, false, nil,
			time.Now(), time.Now(),
		)

	mock.ExpectQuery(`SELECT .+ FROM webhook_events WHERE processed = false`).
		WillReturnRows(rows)

	// Execute
	events, err := repo.GetUnprocessed(ctx)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, "TRANSACTIONS", events[0].WebhookType)
	assert.Equal(t, "ITEM", events[1].WebhookType)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookEventGetUnprocessed_Empty(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()

	// Mock empty result
	rows := sqlmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"created_at", "updated_at",
	})

	mock.ExpectQuery(`SELECT .+ FROM webhook_events WHERE processed = false`).
		WillReturnRows(rows)

	// Execute
	events, err := repo.GetUnprocessed(ctx)

	// Assert
	assert.NoError(t, err)
	assert.Empty(t, events)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookEventGetByItemID_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()
	itemID := "test-item-123"
	limit := 10

	// Mock SELECT query with item_id filter
	rows := sqlmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"created_at", "updated_at",
	}).
		AddRow(
			"id-1", "TRANSACTIONS", "DEFAULT_UPDATE", itemID, nil,
			3, nil, json.RawMessage(`{}`),
			"sig-1", true, true, timePtr(time.Now()),
			time.Now(), time.Now(),
		)

	mock.ExpectQuery(`SELECT .+ FROM webhook_events WHERE item_id`).
		WithArgs(itemID, limit).
		WillReturnRows(rows)

	// Execute
	events, err := repo.GetByItemID(ctx, itemID, limit)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, itemID, *events[0].ItemID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookEventGetByItemID_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()
	itemID := "nonexistent-item"
	limit := 10

	// Mock empty result
	rows := sqlmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"created_at", "updated_at",
	})

	mock.ExpectQuery(`SELECT .+ FROM webhook_events WHERE item_id`).
		WithArgs(itemID, limit).
		WillReturnRows(rows)

	// Execute
	events, err := repo.GetByItemID(ctx, itemID, limit)

	// Assert
	assert.NoError(t, err)
	assert.Empty(t, events)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Helper function to create time pointer
func timePtr(t time.Time) *time.Time {
	return &t
}

// ============================================================================
// TDD RED PHASE: Issue #256 - Transaction Sync Service
// New tests for webhook processing metadata
// These tests will FAIL until GREEN phase implementation
// ============================================================================

func TestWebhookRepository_Update_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()

	// Create webhook event with processing metadata
	startedAt := time.Now()
	completedAt := time.Now().Add(5 * time.Second)
	nextRetryAt := time.Now().Add(1 * time.Minute)
	processingError := "test error message"

	event := &domain.WebhookEvent{
		ID:                    "test-id-123",
		Processed:             true,
		ProcessingStartedAt:   &startedAt,
		ProcessingCompletedAt: &completedAt,
		ProcessingError:       &processingError,
		RetryCount:            2,
		NextRetryAt:           &nextRetryAt,
	}

	// Mock UPDATE query with all processing fields
	mock.ExpectExec(`UPDATE webhook_events SET`).
		WithArgs(
			event.Processed,
			event.ProcessingStartedAt,
			event.ProcessingCompletedAt,
			event.ProcessingError,
			event.RetryCount,
			event.NextRetryAt,
			event.ID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Execute
	err = repo.Update(ctx, event)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookRepository_Update_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()

	event := &domain.WebhookEvent{
		ID:        "nonexistent-id",
		Processed: true,
	}

	// Mock UPDATE with no rows affected
	mock.ExpectExec(`UPDATE webhook_events SET`).
		WithArgs(
			event.Processed,
			sqlmock.AnyArg(), // processing_started_at
			sqlmock.AnyArg(), // processing_completed_at
			sqlmock.AnyArg(), // processing_error
			event.RetryCount,
			sqlmock.AnyArg(), // next_retry_at
			event.ID,
		).
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Execute
	err = repo.Update(ctx, event)

	// Assert - should not error even if no rows updated
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookRepository_GetUnprocessedByType_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()

	// Mock SELECT query filtered by webhook_type
	rows := sqlmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_started_at", "processing_completed_at", "processing_error",
		"retry_count", "next_retry_at",
		"created_at", "updated_at",
	}).
		AddRow(
			"id-1", "TRANSACTIONS", "DEFAULT_UPDATE", "item-1", nil,
			5, nil, json.RawMessage(`{}`),
			"sig-1", true, false, nil,
			nil, nil, nil, 0, nil,
			time.Now(), time.Now(),
		).
		AddRow(
			"id-2", "TRANSACTIONS", "INITIAL_UPDATE", "item-2", nil,
			10, nil, json.RawMessage(`{}`),
			"sig-2", true, false, nil,
			nil, nil, nil, 0, nil,
			time.Now(), time.Now(),
		)

	// Expect query with webhook_type filter and retry filter
	// Note: NOW() is a SQL function, not a parameter
	mock.ExpectQuery(`SELECT .+ FROM webhook_events WHERE processed = false AND webhook_type = (.+) AND \(next_retry_at IS NULL OR next_retry_at <=`).
		WithArgs("TRANSACTIONS", 10).
		WillReturnRows(rows)

	// Execute
	events, err := repo.GetUnprocessedByType(ctx, "TRANSACTIONS", 10)

	// Assert
	assert.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, "TRANSACTIONS", events[0].WebhookType)
	assert.Equal(t, "TRANSACTIONS", events[1].WebhookType)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookRepository_GetUnprocessedByType_OnlyTransactions(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()

	// Mock result with only TRANSACTIONS webhooks (filtering works)
	rows := sqlmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_started_at", "processing_completed_at", "processing_error",
		"retry_count", "next_retry_at",
		"created_at", "updated_at",
	}).
		AddRow(
			"id-1", "TRANSACTIONS", "DEFAULT_UPDATE", "item-1", nil,
			5, nil, json.RawMessage(`{}`),
			"sig-1", true, false, nil,
			nil, nil, nil, 0, nil,
			time.Now(), time.Now(),
		)

	mock.ExpectQuery(`SELECT .+ FROM webhook_events WHERE processed = false AND webhook_type`).
		WithArgs("TRANSACTIONS", 10).
		WillReturnRows(rows)

	// Execute
	events, err := repo.GetUnprocessedByType(ctx, "TRANSACTIONS", 10)

	// Assert - should only return TRANSACTIONS, not ITEM or other types
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	for _, event := range events {
		assert.Equal(t, "TRANSACTIONS", event.WebhookType, "Should only return TRANSACTIONS webhooks")
	}
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookRepository_GetUnprocessedByType_RespectsRetryTime(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()

	// Mock empty result (webhooks exist but next_retry_at is in future)
	rows := sqlmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_started_at", "processing_completed_at", "processing_error",
		"retry_count", "next_retry_at",
		"created_at", "updated_at",
	})

	// Query should filter WHERE next_retry_at IS NULL OR next_retry_at <= NOW()
	// Note: NOW() is a SQL function, not a parameter
	mock.ExpectQuery(`SELECT .+ FROM webhook_events WHERE processed = false AND webhook_type = (.+) AND \(next_retry_at IS NULL OR next_retry_at <=`).
		WithArgs("TRANSACTIONS", 10).
		WillReturnRows(rows)

	// Execute
	events, err := repo.GetUnprocessedByType(ctx, "TRANSACTIONS", 10)

	// Assert - should return empty if all webhooks have future retry times
	assert.NoError(t, err)
	assert.Empty(t, events, "Should not return webhooks with future retry times")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookRepository_GetUnprocessedByType_OrdersByCreatedAt(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewWebhookRepository(db)
	ctx := context.Background()

	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)

	// Mock result ordered by created_at ASC (oldest first)
	rows := sqlmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_started_at", "processing_completed_at", "processing_error",
		"retry_count", "next_retry_at",
		"created_at", "updated_at",
	}).
		AddRow(
			"id-older", "TRANSACTIONS", "DEFAULT_UPDATE", "item-1", nil,
			5, nil, json.RawMessage(`{}`),
			"sig-1", true, false, nil,
			nil, nil, nil, 0, nil,
			older, older,
		).
		AddRow(
			"id-newer", "TRANSACTIONS", "DEFAULT_UPDATE", "item-2", nil,
			3, nil, json.RawMessage(`{}`),
			"sig-2", true, false, nil,
			nil, nil, nil, 0, nil,
			newer, newer,
		)

	// Expect ORDER BY created_at ASC
	mock.ExpectQuery(`SELECT .+ FROM webhook_events .+ ORDER BY created_at ASC`).
		WithArgs("TRANSACTIONS", 10).
		WillReturnRows(rows)

	// Execute
	events, err := repo.GetUnprocessedByType(ctx, "TRANSACTIONS", 10)

	// Assert - first event should be the older one (FIFO processing)
	assert.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, "id-older", events[0].ID, "Should return oldest webhook first (FIFO)")
	assert.Equal(t, "id-newer", events[1].ID, "Should return newer webhook second")
	assert.True(t, events[0].CreatedAt.Before(events[1].CreatedAt), "Events should be ordered oldest first")
	assert.NoError(t, mock.ExpectationsWereMet())
}
