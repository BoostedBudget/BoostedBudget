package workers

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TransactionSyncProvider defines the interface for syncing transactions from any provider.
// This abstraction allows support for multiple providers (Plaid, Teller, Finicity, etc.)
// without changing the orchestrator or scheduler logic.
//
// Current implementations:
//   - PlaidSyncProvider (this file, using Plaid TransactionsSync API)
//
// Future implementations:
//   - TellerSyncProvider
//   - FinicitySyncProvider
//   - YodleeSyncProvider
//   - ManualSyncProvider (for user-entered transactions)
type TransactionSyncProvider interface {
	// GetProviderName returns the source identifier for this provider
	// (e.g., "plaid", "teller", "manual")
	GetProviderName() domain.TransactionSource

	// SyncForLinkedItem performs an incremental sync for a single linked item.
	// Returns the number of transactions synced (added + modified + removed).
	// Updates the cursor in the linked_items table for incremental sync.
	// Emits transaction events for all changes.
	//
	// For Plaid: Uses TransactionsSync endpoint with cursor-based pagination
	// For other providers: Implementation varies based on provider API
	SyncForLinkedItem(ctx context.Context, linkedItemID string) (int, error)

	// SupportsIncrementalSync indicates whether this provider supports
	// cursor-based incremental syncing (true) or requires full re-sync (false).
	//
	// Plaid: true (uses cursor from TransactionsSync)
	// Manual: false (no external sync needed)
	SupportsIncrementalSync() bool
}
