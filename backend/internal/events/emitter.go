package events

import (
	"context"
	"sync"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Event type constants
const (
	EventTypeAdded    = "added"
	EventTypeModified = "modified"
	EventTypeRemoved  = "removed"
)

// TransactionEvent represents a transaction change event.
// Used to notify subscribers when transactions are added, modified, or removed.
type TransactionEvent struct {
	Type        string                 // Event type: "added", "modified", or "removed"
	Transaction *domain.Transaction    // The transaction that changed
	UserID      string                 // User who owns the transaction
	Source      domain.TransactionSource // Provider source (plaid, manual, etc.)
	Timestamp   time.Time              // When the event occurred
}

// EventHandler is a function that processes transaction events.
// Returns an error if event handling fails.
type EventHandler func(event TransactionEvent) error

// EventEmitter provides event emission and subscription capabilities.
// This is an in-memory implementation suitable for single-instance deployments.
// For distributed systems, this can be replaced with a message queue (RabbitMQ, Kafka, etc.)
type EventEmitter interface {
	// Emit sends an event to all registered subscribers
	Emit(ctx context.Context, event TransactionEvent) error

	// Subscribe registers a handler to receive events
	Subscribe(handler EventHandler)
}

// eventEmitter implements EventEmitter with in-memory pub/sub
type eventEmitter struct {
	handlers []EventHandler
	mu       sync.RWMutex // Protects handlers slice
}

// NewEventEmitter creates a new in-memory event emitter
func NewEventEmitter() EventEmitter {
	return &eventEmitter{
		handlers: make([]EventHandler, 0),
	}
}

// Emit sends an event to all registered subscribers.
// Handlers are called synchronously in the order they were registered.
// If context is cancelled, returns immediately without calling remaining handlers.
func (e *eventEmitter) Emit(ctx context.Context, event TransactionEvent) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check context cancellation before processing
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Call all registered handlers
	for _, handler := range e.handlers {
		// Check context cancellation before each handler
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Call handler (errors are logged but don't stop other handlers)
		if err := handler(event); err != nil {
			// In production, would log this error
			// For now, continue to next handler
			continue
		}
	}

	return nil
}

// Subscribe registers an event handler.
// Thread-safe - can be called concurrently.
func (e *eventEmitter) Subscribe(handler EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.handlers = append(e.handlers, handler)
}
