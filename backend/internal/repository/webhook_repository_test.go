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
