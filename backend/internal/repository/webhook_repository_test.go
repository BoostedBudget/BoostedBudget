package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TDD: Phase 3 - Repository Layer Tests
// Issue #202: Plaid Webhook Integration
// Following RED-GREEN-REFACTOR: Write tests first, then implement

func TestWebhookEventCreate_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
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

	// Mock the INSERT query — payload and item_id are encrypted, blind index is added
	mock.ExpectQuery(`INSERT INTO webhook_events`).
		WithArgs(
			"TRANSACTIONS",
			&webhookCode,
			pgxmock.AnyArg(), // encrypted item_id
			pgxmock.AnyArg(), // item_id_blind_idx
			pgxmock.AnyArg(), // error_code
			&newTxCount,
			pgxmock.AnyArg(), // removed_transactions
			pgxmock.AnyArg(), // encrypted payload
			&signature,
			true,
			false,
		).
		WillReturnRows(pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).
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
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
	ctx := context.Background()

	payload := json.RawMessage(`{"webhook_type":"TRANSACTIONS"}`)
	event := &domain.WebhookEvent{
		WebhookType: "TRANSACTIONS",
		Payload:     payload,
	}

	// Mock database error - 11 args now (added item_id_blind_idx)
	args := make([]interface{}, 11)
	for i := range args {
		args[i] = pgxmock.AnyArg()
	}
	mock.ExpectQuery(`INSERT INTO webhook_events`).
		WithArgs(args...).
		WillReturnError(assert.AnError)

	// Execute
	err = repo.Create(ctx, event)

	// Assert
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookEventMarkProcessed_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
	ctx := context.Background()
	webhookID := "test-id-123"

	// Mock the UPDATE query (expects timestamp + id)
	mock.ExpectExec(`UPDATE webhook_events SET processed`).
		WithArgs(pgxmock.AnyArg(), webhookID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err = repo.MarkProcessed(ctx, webhookID)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookEventMarkProcessed_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
	ctx := context.Background()
	webhookID := "nonexistent-id"

	// Mock no rows affected (expects timestamp + id)
	mock.ExpectExec(`UPDATE webhook_events SET processed`).
		WithArgs(pgxmock.AnyArg(), webhookID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	// Execute
	err = repo.MarkProcessed(ctx, webhookID)

	// Assert - should not error, just no update
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookEventGetUnprocessed_Multiple(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
	ctx := context.Background()

	// Use pointers for nullable columns (pgxmock v4 requirement)
	webhookCode1 := "DEFAULT_UPDATE"
	webhookCode2 := "ERROR"
	itemID1 := "ENC$TENC[item-1]"
	itemID2 := "ENC$TENC[item-2]"
	errorCode := "ITEM_LOGIN_REQUIRED"
	newTransactions := 5
	signature1 := "sig-1"
	signature2 := "sig-2"
	now := time.Now()

	// Mock SELECT query — payload column is now TEXT (encrypted string)
	rows := pgxmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"created_at", "updated_at",
	}).
		AddRow(
			"id-1", "TRANSACTIONS", &webhookCode1, &itemID1, nil,
			&newTransactions, nil, "ENC$TENC[{}]",
			&signature1, true, false, nil,
			now, now,
		).
		AddRow(
			"id-2", "ITEM", &webhookCode2, &itemID2, &errorCode,
			nil, nil, "ENC$TENC[{}]",
			&signature2, true, false, nil,
			now, now,
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
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
	ctx := context.Background()

	// Mock empty result
	rows := pgxmock.NewRows([]string{
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
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
	ctx := context.Background()
	itemID := "test-item-123"
	limit := 10

	// Use pointers for nullable columns (pgxmock v4 requirement)
	webhookCode := "DEFAULT_UPDATE"
	newTransactions := 3
	signature := "sig-1"
	now := time.Now()
	// DB stores encrypted item_id; the repo will decrypt it back to "test-item-123"
	encItemID := "ENC$TENC[" + itemID + "]"

	// Mock SELECT query — WHERE clause now uses item_id_blind_idx, not item_id
	rows := pgxmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"created_at", "updated_at",
	}).
		AddRow(
			"id-1", "TRANSACTIONS", &webhookCode, &encItemID, nil,
			&newTransactions, nil, "ENC$TENC[{}]",
			&signature, true, true, &now,
			now, now,
		)

	mock.ExpectQuery(`SELECT .+ FROM webhook_events WHERE item_id_blind_idx`).
		WithArgs(pgxmock.AnyArg(), limit). // blind index value + limit
		WillReturnRows(rows)

	// Execute
	events, err := repo.GetByItemID(ctx, itemID, limit)

	// Assert — repo decrypts, so ItemID should be the original plaintext
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, itemID, *events[0].ItemID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookEventGetByItemID_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
	ctx := context.Background()
	itemID := "nonexistent-item"
	limit := 10

	// Mock empty result
	rows := pgxmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"created_at", "updated_at",
	})

	mock.ExpectQuery(`SELECT .+ FROM webhook_events WHERE item_id_blind_idx`).
		WithArgs(pgxmock.AnyArg(), limit). // blind index value + limit
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
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
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
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Execute
	err = repo.Update(ctx, event)

	// Assert
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookRepository_Update_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
	ctx := context.Background()

	event := &domain.WebhookEvent{
		ID:        "nonexistent-id",
		Processed: true,
	}

	// Mock UPDATE with no rows affected
	mock.ExpectExec(`UPDATE webhook_events SET`).
		WithArgs(
			event.Processed,
			pgxmock.AnyArg(), // processing_started_at
			pgxmock.AnyArg(), // processing_completed_at
			pgxmock.AnyArg(), // processing_error
			event.RetryCount,
			pgxmock.AnyArg(), // next_retry_at
			event.ID,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	// Execute
	err = repo.Update(ctx, event)

	// Assert - should not error even if no rows updated
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWebhookRepository_GetUnprocessedByType_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
	ctx := context.Background()

	// Use pointers for nullable columns (pgxmock v4 requirement)
	webhookCode1 := "DEFAULT_UPDATE"
	webhookCode2 := "INITIAL_UPDATE"
	itemID1 := "ENC$TENC[item-1]"
	itemID2 := "ENC$TENC[item-2]"
	newTx1 := 5
	newTx2 := 10
	signature1 := "sig-1"
	signature2 := "sig-2"
	now := time.Now()

	// Mock SELECT query filtered by webhook_type — payload is now TEXT (encrypted string)
	rows := pgxmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_started_at", "processing_completed_at", "processing_error",
		"retry_count", "next_retry_at",
		"created_at", "updated_at",
	}).
		AddRow(
			"id-1", "TRANSACTIONS", &webhookCode1, &itemID1, nil,
			&newTx1, nil, "ENC$TENC[{}]",
			&signature1, true, false, nil,
			nil, nil, nil, 0, nil,
			now, now,
		).
		AddRow(
			"id-2", "TRANSACTIONS", &webhookCode2, &itemID2, nil,
			&newTx2, nil, "ENC$TENC[{}]",
			&signature2, true, false, nil,
			nil, nil, nil, 0, nil,
			now, now,
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
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
	ctx := context.Background()

	// Use pointers for nullable columns (pgxmock v4 requirement)
	webhookCode := "DEFAULT_UPDATE"
	itemID := "ENC$TENC[item-1]"
	newTransactions := 5
	signature := "sig-1"
	now := time.Now()

	// Mock result with only TRANSACTIONS webhooks (filtering works)
	rows := pgxmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_started_at", "processing_completed_at", "processing_error",
		"retry_count", "next_retry_at",
		"created_at", "updated_at",
	}).
		AddRow(
			"id-1", "TRANSACTIONS", &webhookCode, &itemID, nil,
			&newTransactions, nil, "ENC$TENC[{}]",
			&signature, true, false, nil,
			nil, nil, nil, 0, nil,
			now, now,
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
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
	ctx := context.Background()

	// Mock empty result (webhooks exist but next_retry_at is in future)
	rows := pgxmock.NewRows([]string{
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
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	enc := newTestFieldEncryptor(t)
	repo := NewWebhookRepository(mock, enc)
	ctx := context.Background()

	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)

	// Use pointers for nullable columns (pgxmock v4 requirement)
	webhookCode := "DEFAULT_UPDATE"
	itemID1 := "ENC$TENC[item-1]"
	itemID2 := "ENC$TENC[item-2]"
	newTx1 := 5
	newTx2 := 3
	signature1 := "sig-1"
	signature2 := "sig-2"

	// Mock result ordered by created_at ASC (oldest first)
	rows := pgxmock.NewRows([]string{
		"id", "webhook_type", "webhook_code", "item_id", "error_code",
		"new_transactions", "removed_transactions", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_started_at", "processing_completed_at", "processing_error",
		"retry_count", "next_retry_at",
		"created_at", "updated_at",
	}).
		AddRow(
			"id-older", "TRANSACTIONS", &webhookCode, &itemID1, nil,
			&newTx1, nil, "ENC$TENC[{}]",
			&signature1, true, false, nil,
			nil, nil, nil, 0, nil,
			older, older,
		).
		AddRow(
			"id-newer", "TRANSACTIONS", &webhookCode, &itemID2, nil,
			&newTx2, nil, "ENC$TENC[{}]",
			&signature2, true, false, nil,
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
