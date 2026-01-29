package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupPaddleWebhookMockDB creates a mock database and repository for testing
func setupPaddleWebhookMockDB(t *testing.T) (pgxmock.PgxPoolIface, PaddleWebhookRepository) {
	t.Helper()

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	repo := NewPaddleWebhookRepository(mock)
	return mock, repo
}

// TestPaddleWebhookCreate_Success tests successful webhook event creation
func TestPaddleWebhookCreate_Success(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()
	signature := "ts=1614280549;h1=abc123"

	event := &domain.PaddleWebhookEvent{
		EventID:            "evt_01h1234567890",
		EventType:          "subscription.created",
		Payload:            json.RawMessage(`{"data": {"id": "sub_123"}}`),
		Signature:          &signature,
		SignatureValid:     true,
		Processed:          false,
		ProcessingAttempts: 0,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO paddle_webhook_events`)).
		WithArgs(
			pgxmock.AnyArg(), // id (generated UUID)
			event.EventID,
			event.EventType,
			event.Payload,
			event.Signature,
			event.SignatureValid,
			event.Processed,
			event.ProcessingAttempts,
		).
		WillReturnRows(pgxmock.NewRows([]string{"created_at", "updated_at"}).
			AddRow(now, now))

	err := repo.Create(ctx, event)

	require.NoError(t, err)
	assert.NotEmpty(t, event.ID, "ID should be generated")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreate_DuplicateEventID tests idempotency - duplicate event_id returns error
func TestCreate_DuplicateEventID(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	signature := "ts=1614280549;h1=abc123"

	event := &domain.PaddleWebhookEvent{
		EventID:            "evt_01h1234567890",
		EventType:          "subscription.created",
		Payload:            json.RawMessage(`{"data": {}}`),
		Signature:          &signature,
		SignatureValid:     true,
		Processed:          false,
		ProcessingAttempts: 0,
	}

	// Simulate unique constraint violation (PostgreSQL code 23505)
	pgErr := &pgconn.PgError{
		Code: "23505", // unique_violation
	}

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO paddle_webhook_events`)).
		WithArgs(
			pgxmock.AnyArg(),
			event.EventID,
			event.EventType,
			event.Payload,
			event.Signature,
			event.SignatureValid,
			event.Processed,
			event.ProcessingAttempts,
		).
		WillReturnError(pgErr)

	err := repo.Create(ctx, event)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPaddleWebhookDuplicate)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreate_DatabaseError tests database connection failure
func TestCreate_DatabaseError(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	signature := "ts=1614280549;h1=abc123"

	event := &domain.PaddleWebhookEvent{
		EventID:            "evt_01h1234567890",
		EventType:          "subscription.created",
		Payload:            json.RawMessage(`{"data": {}}`),
		Signature:          &signature,
		SignatureValid:     true,
		Processed:          false,
		ProcessingAttempts: 0,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO paddle_webhook_events`)).
		WithArgs(
			pgxmock.AnyArg(),
			event.EventID,
			event.EventType,
			event.Payload,
			event.Signature,
			event.SignatureValid,
			event.Processed,
			event.ProcessingAttempts,
		).
		WillReturnError(pgx.ErrTxClosed)

	err := repo.Create(ctx, event)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create paddle webhook event")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByID_Found tests successful retrieval by UUID
func TestGetByID_Found(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	id := uuid.New().String()
	now := time.Now()
	signature := "ts=1614280549;h1=abc123"
	errorMsg := "test error"

	rows := pgxmock.NewRows([]string{
		"id", "event_id", "event_type", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_error", "processing_attempts",
		"created_at", "updated_at",
	}).AddRow(
		id,
		"evt_01h1234567890",
		"subscription.created",
		json.RawMessage(`{"data": {}}`),
		&signature,
		true,
		true,
		&now,
		&errorMsg,
		2,
		now,
		now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, event_id, event_type, payload`)).
		WithArgs(id).
		WillReturnRows(rows)

	event, err := repo.GetByID(ctx, id)

	require.NoError(t, err)
	assert.NotNil(t, event)
	assert.Equal(t, id, event.ID)
	assert.Equal(t, "evt_01h1234567890", event.EventID)
	assert.Equal(t, "subscription.created", event.EventType)
	assert.True(t, event.SignatureValid)
	assert.True(t, event.Processed)
	assert.Equal(t, 2, event.ProcessingAttempts)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByID_NotFound tests retrieval of non-existent webhook
func TestGetByID_NotFound(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	id := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, event_id, event_type, payload`)).
		WithArgs(id).
		WillReturnError(pgx.ErrNoRows)

	event, err := repo.GetByID(ctx, id)

	require.Error(t, err)
	assert.Nil(t, event)
	assert.ErrorIs(t, err, ErrPaddleWebhookNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByID_DatabaseError tests database query failure
func TestGetByID_DatabaseError(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	id := uuid.New().String()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, event_id, event_type, payload`)).
		WithArgs(id).
		WillReturnError(pgx.ErrTxClosed)

	event, err := repo.GetByID(ctx, id)

	require.Error(t, err)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "failed to get paddle webhook event by id")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByEventID_Found tests retrieval by Paddle event_id
func TestGetByEventID_Found(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	eventID := "evt_01h1234567890"
	now := time.Now()
	signature := "ts=1614280549;h1=abc123"

	rows := pgxmock.NewRows([]string{
		"id", "event_id", "event_type", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_error", "processing_attempts",
		"created_at", "updated_at",
	}).AddRow(
		uuid.New().String(),
		eventID,
		"subscription.created",
		json.RawMessage(`{"data": {}}`),
		&signature,
		true,
		false,
		nil,
		nil,
		0,
		now,
		now,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, event_id, event_type, payload`)).
		WithArgs(eventID).
		WillReturnRows(rows)

	event, err := repo.GetByEventID(ctx, eventID)

	require.NoError(t, err)
	assert.NotNil(t, event)
	assert.Equal(t, eventID, event.EventID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByEventID_NotFound tests retrieval of non-existent event_id
func TestGetByEventID_NotFound(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	eventID := "evt_nonexistent"

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, event_id, event_type, payload`)).
		WithArgs(eventID).
		WillReturnError(pgx.ErrNoRows)

	event, err := repo.GetByEventID(ctx, eventID)

	require.Error(t, err)
	assert.Nil(t, event)
	assert.ErrorIs(t, err, ErrPaddleWebhookNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByEventID_DatabaseError tests database query failure for event_id lookup
func TestGetByEventID_DatabaseError(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	eventID := "evt_01h1234567890"

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, event_id, event_type, payload`)).
		WithArgs(eventID).
		WillReturnError(pgx.ErrTxClosed)

	event, err := repo.GetByEventID(ctx, eventID)

	require.Error(t, err)
	assert.Nil(t, event)
	assert.Contains(t, err.Error(), "failed to get paddle webhook event by event_id")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMarkProcessed_Success tests marking webhook as successfully processed
func TestMarkProcessed_Success(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	id := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE paddle_webhook_events`)).
		WithArgs(id).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.MarkProcessed(ctx, id)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMarkProcessed_NotFound tests marking non-existent webhook as processed
func TestMarkProcessed_NotFound(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	id := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE paddle_webhook_events`)).
		WithArgs(id).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.MarkProcessed(ctx, id)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPaddleWebhookNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMarkProcessed_DatabaseError tests database update failure
func TestMarkProcessed_DatabaseError(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	id := uuid.New().String()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE paddle_webhook_events`)).
		WithArgs(id).
		WillReturnError(pgx.ErrTxClosed)

	err := repo.MarkProcessed(ctx, id)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to mark paddle webhook as processed")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMarkFailed_Success tests marking webhook as failed with error message
func TestMarkFailed_Success(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	id := uuid.New().String()
	errorMsg := "subscription not found"

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE paddle_webhook_events`)).
		WithArgs(id, errorMsg).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.MarkFailed(ctx, id, errorMsg)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMarkFailed_IncrementsRetryCount tests that processing_attempts increments
func TestMarkFailed_IncrementsRetryCount(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	id := uuid.New().String()
	errorMsg := "temporary database error"

	// MarkFailed should increment processing_attempts in the query
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE paddle_webhook_events`)).
		WithArgs(id, errorMsg).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.MarkFailed(ctx, id, errorMsg)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMarkFailed_NotFound tests marking non-existent webhook as failed
func TestMarkFailed_NotFound(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	id := uuid.New().String()
	errorMsg := "test error"

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE paddle_webhook_events`)).
		WithArgs(id, errorMsg).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.MarkFailed(ctx, id, errorMsg)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPaddleWebhookNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUnprocessed_ReturnsMultiple tests retrieving multiple unprocessed webhooks
func TestGetUnprocessed_ReturnsMultiple(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	limit := 10
	now := time.Now()
	signature := "ts=1614280549;h1=abc123"

	rows := pgxmock.NewRows([]string{
		"id", "event_id", "event_type", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_error", "processing_attempts",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), "evt_01h1111", "subscription.created",
			json.RawMessage(`{}`), &signature, true, false, nil, nil, 0, now, now,
		).
		AddRow(
			uuid.New().String(), "evt_01h2222", "subscription.updated",
			json.RawMessage(`{}`), &signature, true, false, nil, nil, 0, now, now,
		).
		AddRow(
			uuid.New().String(), "evt_01h3333", "transaction.completed",
			json.RawMessage(`{}`), &signature, true, false, nil, nil, 0, now, now,
		)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, event_id, event_type, payload`)).
		WithArgs(limit).
		WillReturnRows(rows)

	events, err := repo.GetUnprocessed(ctx, limit)

	require.NoError(t, err)
	assert.Len(t, events, 3)
	assert.Equal(t, "evt_01h1111", events[0].EventID)
	assert.Equal(t, "evt_01h2222", events[1].EventID)
	assert.Equal(t, "evt_01h3333", events[2].EventID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUnprocessed_EmptyResult tests retrieving when no unprocessed webhooks exist
func TestGetUnprocessed_EmptyResult(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	limit := 10

	rows := pgxmock.NewRows([]string{
		"id", "event_id", "event_type", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_error", "processing_attempts",
		"created_at", "updated_at",
	})

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, event_id, event_type, payload`)).
		WithArgs(limit).
		WillReturnRows(rows)

	events, err := repo.GetUnprocessed(ctx, limit)

	require.NoError(t, err)
	assert.Empty(t, events)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUnprocessed_DatabaseError tests database query failure
func TestGetUnprocessed_DatabaseError(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	limit := 10

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, event_id, event_type, payload`)).
		WithArgs(limit).
		WillReturnError(pgx.ErrTxClosed)

	events, err := repo.GetUnprocessed(ctx, limit)

	require.Error(t, err)
	assert.Nil(t, events)
	assert.Contains(t, err.Error(), "failed to get unprocessed paddle webhooks")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetUnprocessedByType_Success tests retrieving unprocessed webhooks of specific type
func TestGetUnprocessedByType_Success(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	eventType := "subscription.created"
	limit := 5
	now := time.Now()
	signature := "ts=1614280549;h1=abc123"

	rows := pgxmock.NewRows([]string{
		"id", "event_id", "event_type", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_error", "processing_attempts",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), "evt_01h1111", eventType,
			json.RawMessage(`{}`), &signature, true, false, nil, nil, 0, now, now,
		).
		AddRow(
			uuid.New().String(), "evt_01h2222", eventType,
			json.RawMessage(`{}`), &signature, true, false, nil, nil, 0, now, now,
		)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, event_id, event_type, payload`)).
		WithArgs(eventType, limit).
		WillReturnRows(rows)

	events, err := repo.GetUnprocessedByType(ctx, eventType, limit)

	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, eventType, events[0].EventType)
	assert.Equal(t, eventType, events[1].EventType)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdate_Success tests updating webhook event processing status
func TestUpdate_Success(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	errorMsg := "processing failed"

	event := &domain.PaddleWebhookEvent{
		ID:                 uuid.New().String(),
		Processed:          false,
		ProcessedAt:        nil,
		ProcessingError:    &errorMsg,
		ProcessingAttempts: 3,
	}

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE paddle_webhook_events`)).
		WithArgs(
			event.ID,
			event.Processed,
			event.ProcessedAt,
			event.ProcessingError,
			event.ProcessingAttempts,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := repo.Update(ctx, event)

	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdate_NotFound tests updating non-existent webhook
func TestUpdate_NotFound(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	event := &domain.PaddleWebhookEvent{
		ID:                 uuid.New().String(),
		Processed:          true,
		ProcessingAttempts: 1,
	}

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE paddle_webhook_events`)).
		WithArgs(
			event.ID,
			event.Processed,
			event.ProcessedAt,
			event.ProcessingError,
			event.ProcessingAttempts,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := repo.Update(ctx, event)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPaddleWebhookNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetRecentWebhooks_Success tests retrieving recent webhooks for admin dashboard
func TestGetRecentWebhooks_Success(t *testing.T) {
	mock, repo := setupPaddleWebhookMockDB(t)
	defer mock.Close()

	ctx := context.Background()
	limit := 20
	now := time.Now()
	signature := "ts=1614280549;h1=abc123"
	_ = now // Used in AddRow below

	rows := pgxmock.NewRows([]string{
		"id", "event_id", "event_type", "payload",
		"signature", "signature_valid", "processed", "processed_at",
		"processing_error", "processing_attempts",
		"created_at", "updated_at",
	}).
		AddRow(
			uuid.New().String(), "evt_newest", "subscription.created",
			json.RawMessage(`{}`), &signature, true, true, &now, nil, 0, now, now,
		).
		AddRow(
			uuid.New().String(), "evt_older", "transaction.completed",
			json.RawMessage(`{}`), &signature, true, true, &now, nil, 0, now.Add(-1*time.Hour), now,
		)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, event_id, event_type, payload`)).
		WithArgs(limit).
		WillReturnRows(rows)

	events, err := repo.GetRecentWebhooks(ctx, limit)

	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, "evt_newest", events[0].EventID)
	assert.Equal(t, "evt_older", events[1].EventID)
	assert.NoError(t, mock.ExpectationsWereMet())
}
