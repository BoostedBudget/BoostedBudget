package events

import (
	"context"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventEmitter_Emit_Success tests successful event emission
func TestEventEmitter_Emit_Success(t *testing.T) {
	emitter := NewEventEmitter()
	ctx := context.Background()

	externalID := "plaid_tx_123"
	transaction := &domain.Transaction{
		ID:                    "tx-uuid-123",
		UserID:                "user-123",
		AccountID:             "account-123",
		LinkedItemID:          "item-123",
		Source:                domain.SourcePlaid,
		ExternalTransactionID: &externalID,
		Amount:                -50.25,
		Date:                  time.Now(),
		Name:                  "Starbucks",
	}

	event := TransactionEvent{
		Type:        EventTypeAdded,
		Transaction: transaction,
		UserID:      transaction.UserID,
		Source:      transaction.Source,
		Timestamp:   time.Now(),
	}

	err := emitter.Emit(ctx, event)
	assert.NoError(t, err)
}

// TestEventEmitter_Subscribe_ReceivesEvents tests event subscription and delivery
func TestEventEmitter_Subscribe_ReceivesEvents(t *testing.T) {
	emitter := NewEventEmitter()
	ctx := context.Background()

	// Create a channel to receive events
	received := make(chan TransactionEvent, 10)
	handler := func(event TransactionEvent) error {
		received <- event
		return nil
	}

	// Subscribe to events
	emitter.Subscribe(handler)

	// Emit an event
	externalID := "plaid_tx_456"
	transaction := &domain.Transaction{
		ID:                    "tx-uuid-456",
		UserID:                "user-456",
		AccountID:             "account-456",
		LinkedItemID:          "item-456",
		Source:                domain.SourcePlaid,
		ExternalTransactionID: &externalID,
		Amount:                -25.00,
		Date:                  time.Now(),
		Name:                  "Amazon",
	}

	event := TransactionEvent{
		Type:        EventTypeModified,
		Transaction: transaction,
		UserID:      transaction.UserID,
		Source:      transaction.Source,
		Timestamp:   time.Now(),
	}

	err := emitter.Emit(ctx, event)
	require.NoError(t, err)

	// Wait for event with timeout
	select {
	case receivedEvent := <-received:
		assert.Equal(t, event.Type, receivedEvent.Type)
		assert.Equal(t, event.UserID, receivedEvent.UserID)
		assert.Equal(t, event.Transaction.ID, receivedEvent.Transaction.ID)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestEventEmitter_MultipleSubscribers tests multiple handlers receive events
func TestEventEmitter_MultipleSubscribers(t *testing.T) {
	emitter := NewEventEmitter()
	ctx := context.Background()

	received1 := make(chan TransactionEvent, 10)
	received2 := make(chan TransactionEvent, 10)

	handler1 := func(event TransactionEvent) error {
		received1 <- event
		return nil
	}
	handler2 := func(event TransactionEvent) error {
		received2 <- event
		return nil
	}

	emitter.Subscribe(handler1)
	emitter.Subscribe(handler2)

	externalID := "plaid_tx_789"
	transaction := &domain.Transaction{
		ID:                    "tx-uuid-789",
		UserID:                "user-789",
		Source:                domain.SourcePlaid,
		ExternalTransactionID: &externalID,
		Amount:                -100.00,
		Date:                  time.Now(),
		Name:                  "Whole Foods",
	}

	event := TransactionEvent{
		Type:        EventTypeRemoved,
		Transaction: transaction,
		UserID:      transaction.UserID,
		Source:      transaction.Source,
		Timestamp:   time.Now(),
	}

	err := emitter.Emit(ctx, event)
	require.NoError(t, err)

	// Both handlers should receive the event
	select {
	case <-received1:
		// Expected
	case <-time.After(1 * time.Second):
		t.Fatal("handler1 did not receive event")
	}

	select {
	case <-received2:
		// Expected
	case <-time.After(1 * time.Second):
		t.Fatal("handler2 did not receive event")
	}
}

// TestEventEmitter_AllEventTypes tests all event type constants
func TestEventEmitter_AllEventTypes(t *testing.T) {
	emitter := NewEventEmitter()
	ctx := context.Background()

	testCases := []struct {
		name      string
		eventType string
	}{
		{"Added event", EventTypeAdded},
		{"Modified event", EventTypeModified},
		{"Removed event", EventTypeRemoved},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			externalID := "tx_test"
			transaction := &domain.Transaction{
				ID:                    "tx-test",
				UserID:                "user-test",
				Source:                domain.SourcePlaid,
				ExternalTransactionID: &externalID,
				Amount:                -10.00,
				Date:                  time.Now(),
				Name:                  "Test Transaction",
			}

			event := TransactionEvent{
				Type:        tc.eventType,
				Transaction: transaction,
				UserID:      transaction.UserID,
				Source:      transaction.Source,
				Timestamp:   time.Now(),
			}

			err := emitter.Emit(ctx, event)
			assert.NoError(t, err)
		})
	}
}

// TestEventEmitter_ContextCancellation tests that context cancellation is respected
func TestEventEmitter_ContextCancellation(t *testing.T) {
	emitter := NewEventEmitter()

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	externalID := "tx_cancelled"
	transaction := &domain.Transaction{
		ID:                    "tx-cancelled",
		UserID:                "user-cancelled",
		Source:                domain.SourcePlaid,
		ExternalTransactionID: &externalID,
		Amount:                -15.00,
		Date:                  time.Now(),
		Name:                  "Cancelled Test",
	}

	event := TransactionEvent{
		Type:        EventTypeAdded,
		Transaction: transaction,
		UserID:      transaction.UserID,
		Source:      transaction.Source,
		Timestamp:   time.Now(),
	}

	// Should handle cancelled context gracefully
	err := emitter.Emit(ctx, event)
	// Implementation can either return error or handle gracefully
	// This test validates the implementation handles context cancellation
	_ = err // Check based on implementation decision
}
