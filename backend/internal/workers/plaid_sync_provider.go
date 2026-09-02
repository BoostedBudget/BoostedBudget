package workers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/events"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/plaid/plaid-go/v23/plaid"
)

// PlaidClient defines the interface for Plaid API operations needed for transaction sync.
// This allows for mocking in tests.
type PlaidClient interface {
	TransactionsSync(ctx context.Context, accessToken string, cursor *string) (*plaid.TransactionsSyncResponse, error)
}

// ⚠ THIS PROVIDER IS NOT WIRED INTO PRODUCTION, and a guard added here does NOT protect the running
// system. Read this before "fixing" anything in this file (#1537 round 9).
//
// NewPlaidSyncProvider has no non-test caller, and neither does the layer above it:
// NewTransactionSyncOrchestrator (orchestrator.go) is never constructed outside tests either. The
// whole TransactionSyncProvider abstraction is deliberate scaffolding for the multi-provider future
// provider.go describes (Teller, Finicity, Yodlee, manual entry) — kept on purpose, unlike the
// genuinely accidental dead code #1565 deleted.
//
// The transaction sync that ACTUALLY RUNS is services/transactionSyncService.SyncTransactions,
// reached from PlaidSyncWorker (queue/jobs/plaid_sync.go) and wired in cmd/server/main.go.
//
// This matters because it has already gone wrong once: round 4 of this branch found that a blank
// Plaid cursor must never be persisted (it forces a full historical re-sync), added the guard HERE,
// and left the live path unguarded for five more rounds. The two implementations are similar enough
// to be mistaken for each other and are not kept in sync by anything. If you change a sync
// invariant, change it in the service first, and check whether it belongs here at all.

// plaidSyncProvider implements TransactionSyncProvider for Plaid
type plaidSyncProvider struct {
	linkedItemRepo repository.LinkedItemRepository
	txnRepo        repository.TransactionRepository
	plaidClient    PlaidClient
	eventEmitter   events.EventEmitter
	analytics      phclient.Analytics
}

// NewPlaidSyncProvider creates a new Plaid transaction sync provider
func NewPlaidSyncProvider(
	linkedItemRepo repository.LinkedItemRepository,
	txnRepo repository.TransactionRepository,
	plaidClient PlaidClient,
	eventEmitter events.EventEmitter,
) TransactionSyncProvider {
	return &plaidSyncProvider{
		linkedItemRepo: linkedItemRepo,
		txnRepo:        txnRepo,
		plaidClient:    plaidClient,
		eventEmitter:   eventEmitter,
	}
}

// SetAnalytics sets the analytics client for capturing sync events.
func (p *plaidSyncProvider) SetAnalytics(analytics phclient.Analytics) {
	p.analytics = analytics
}

// GetProviderName returns the source identifier for Plaid
func (p *plaidSyncProvider) GetProviderName() domain.TransactionSource {
	return domain.SourcePlaid
}

// SupportsIncrementalSync returns true because Plaid supports cursor-based incremental sync
func (p *plaidSyncProvider) SupportsIncrementalSync() bool {
	return true
}

// SyncForLinkedItem performs incremental transaction sync for a Plaid linked item.
//
// Process:
//  1. Fetch linked item from database (contains access token and cursor)
//  2. Call Plaid TransactionsSync API with cursor (null for initial sync)
//  3. Process added transactions: batch insert into database
//  4. Process modified transactions: update existing records
//  5. Process removed transactions: delete from database
//  6. Emit events for all changes
//  7. Update cursor for next sync
//  8. Handle pagination if has_more = true
//
// Returns the total number of transactions processed (added + modified + removed).
func (p *plaidSyncProvider) SyncForLinkedItem(ctx context.Context, linkedItemID string) (n int, err error) {
	// Fetch linked item
	linkedItem, fetchErr := p.linkedItemRepo.GetByID(ctx, linkedItemID)
	if fetchErr != nil {
		return 0, fmt.Errorf("failed to fetch linked item: %w", fetchErr)
	}

	defer func() {
		if p.analytics == nil {
			return
		}
		if err != nil {
			p.analytics.Capture(linkedItem.UserID, phclient.EventSyncFailed, nil)
		} else {
			p.analytics.Capture(linkedItem.UserID, phclient.EventSyncCompleted, map[string]interface{}{
				"linked_item_id":    linkedItemID,
				"transaction_count": n,
			})
			p.analytics.Capture(linkedItem.UserID, phclient.EventAccountSynced, map[string]interface{}{
				"account_id": linkedItemID,
			})
		}
	}()

	// Start sync with existing cursor (nil for initial sync)
	cursor := linkedItem.Cursor

	// Loop to handle pagination
	for {
		// Call Plaid TransactionsSync API
		syncResp, syncErr := p.plaidClient.TransactionsSync(ctx, linkedItem.AccessTokenEncrypted, cursor)
		if syncErr != nil {
			return n, fmt.Errorf("plaid transactions sync failed: %w", syncErr)
		}

		// Process added transactions
		if len(syncResp.Added) > 0 {
			added, addErr := p.processAdded(ctx, linkedItem, syncResp.Added)
			if addErr != nil {
				return n, fmt.Errorf("failed to process added transactions: %w", addErr)
			}
			n += added
		}

		// Process modified transactions
		if len(syncResp.Modified) > 0 {
			modified, modErr := p.processModified(ctx, linkedItem, syncResp.Modified)
			if modErr != nil {
				return n, fmt.Errorf("failed to process modified transactions: %w", modErr)
			}
			n += modified
		}

		// Process removed transactions
		if len(syncResp.Removed) > 0 {
			removed, remErr := p.processRemoved(ctx, linkedItem, syncResp.Removed)
			if remErr != nil {
				return n, fmt.Errorf("failed to process removed transactions: %w", remErr)
			}
			n += removed
		}

		// Update cursor for next page/sync
		nextCursor := syncResp.NextCursor
		cursor = &nextCursor

		// Break if no more pages
		if !syncResp.HasMore {
			break
		}
	}

	// Update cursor in database for next incremental sync.
	//
	// cursor is non-nil by construction: the pagination loop above always runs at
	// least one iteration and unconditionally assigns cursor = &nextCursor before
	// it can break, and every other exit from the loop is a return. The previous
	// `if cursor != nil` guard was therefore dead code (staticcheck SA4031).
	//
	// It IS guarded on non-empty, which is a different and load-bearing check. Plaid treats an
	// absent-or-empty cursor as "start from the beginning of history", so persisting "" turns the next
	// INCREMENTAL sync into a full re-sync and pushes the entire history back through CreateBatch.
	// The stored cursor is the only thing making this incremental, so overwriting a good one with a
	// blank is strictly destructive.
	//
	// The dead nil-check that staticcheck removed looked like it was doing this — "don't persist a
	// cursor we never got" — but `cursor != nil` tested a pointer that could not be nil and never
	// looked at the string. So the protection its comment implied did not exist until now.
	// Pinned by TestPlaidSyncProvider_EmptyNextCursor_DoesNotOverwriteStoredCursor.
	if *cursor != "" {
		if cursorErr := p.linkedItemRepo.UpdateCursor(ctx, linkedItemID, *cursor); cursorErr != nil {
			return n, fmt.Errorf("failed to update cursor: %w", cursorErr)
		}
	}

	return n, nil
}

// processAdded handles added transactions from Plaid
func (p *plaidSyncProvider) processAdded(ctx context.Context, linkedItem *domain.LinkedItem, plaidTxns []plaid.Transaction) (int, error) {
	// Convert Plaid transactions to domain transactions
	domainTxns := make([]domain.Transaction, 0, len(plaidTxns))

	for _, plaidTxn := range plaidTxns {
		domainTxn, err := p.convertPlaidTransaction(linkedItem, plaidTxn)
		if err != nil {
			return 0, fmt.Errorf("failed to convert transaction: %w", err)
		}
		domainTxns = append(domainTxns, *domainTxn)
	}

	// Batch insert into database.
	// TODO: gate EventTypeAdded emission on insertedIDs to avoid duplicate events
	// on conflict-updated rows (same class as Issue #1213; tracked as follow-up).
	if _, err := p.txnRepo.CreateBatch(ctx, domainTxns); err != nil {
		return 0, fmt.Errorf("failed to batch create transactions: %w", err)
	}

	// Emit events for each added transaction
	for _, txn := range domainTxns {
		event := events.TransactionEvent{
			Type:        events.EventTypeAdded,
			Transaction: &txn,
			UserID:      linkedItem.UserID,
			Source:      domain.SourcePlaid,
			Timestamp:   time.Now(),
		}
		if err := p.eventEmitter.Emit(ctx, event); err != nil {
			// Emit failures are non-fatal: the transactions are already persisted,
			// so a dropped event must not roll back or abort the sync. Log and continue.
			slog.ErrorContext(ctx, "failed to emit transaction event",
				"error", err,
				"event_type", events.EventTypeAdded,
				"linked_item_id", linkedItem.ID,
			)
		}
	}

	return len(domainTxns), nil
}

// processModified handles modified transactions from Plaid
func (p *plaidSyncProvider) processModified(ctx context.Context, linkedItem *domain.LinkedItem, plaidTxns []plaid.Transaction) (int, error) {
	for _, plaidTxn := range plaidTxns {
		domainTxn, err := p.convertPlaidTransaction(linkedItem, plaidTxn)
		if err != nil {
			return 0, fmt.Errorf("failed to convert transaction: %w", err)
		}

		// Lookup existing transaction to get its ID
		existingTxn, err := p.txnRepo.GetByExternalID(ctx, domain.SourcePlaid, *domainTxn.ExternalTransactionID, linkedItem.UserID)
		if err != nil {
			return 0, fmt.Errorf("failed to find existing transaction: %w", err)
		}
		domainTxn.ID = existingTxn.ID

		// Update existing transaction
		if err := p.txnRepo.Update(ctx, domainTxn); err != nil {
			return 0, fmt.Errorf("failed to update transaction: %w", err)
		}

		// Emit event
		event := events.TransactionEvent{
			Type:        events.EventTypeModified,
			Transaction: domainTxn,
			UserID:      linkedItem.UserID,
			Source:      domain.SourcePlaid,
			Timestamp:   time.Now(),
		}
		if err := p.eventEmitter.Emit(ctx, event); err != nil {
			// Non-fatal: the update is already committed. Log and continue.
			slog.ErrorContext(ctx, "failed to emit transaction event",
				"error", err,
				"event_type", events.EventTypeModified,
				"linked_item_id", linkedItem.ID,
			)
		}
	}

	return len(plaidTxns), nil
}

// processRemoved TOMBSTONES the transactions Plaid reports as removed. Issue #1569.
//
// This provider is not wired into production (see the banner at the top of this file), so the
// change that matters is in services/transactionSyncService.processRemovedTransactions. It is
// mirrored here because the two paths were CONVERGENT — both hard-deleted — and leaving one of
// them able to destroy user-authored work would silently un-fix #1569 the day the multi-provider
// abstraction is wired up. The banner's own warning is that these two are similar enough to be
// mistaken for each other and are kept in sync by nothing.
//
// ErrTransactionNotFound is tolerated for the same reason the service tolerates it: a removal for
// a row that was never stored, or a re-delivery of one already tombstoned, is the idempotent case,
// not a failure. Every other error still aborts the page.
func (p *plaidSyncProvider) processRemoved(ctx context.Context, linkedItem *domain.LinkedItem, plaidTxns []plaid.RemovedTransaction) (int, error) {
	for _, plaidTxn := range plaidTxns {
		externalID := plaidTxn.GetTransactionId()

		_, err := p.txnRepo.TombstoneByExternalID(
			ctx, domain.SourcePlaid, externalID, linkedItem.UserID, domain.TxnDeletedByPlaidSync)
		if err != nil && !errors.Is(err, repository.ErrTransactionNotFound) {
			return 0, fmt.Errorf("failed to tombstone transaction: %w", err)
		}

		// Emit event (the row is retained but no longer live, so the payload stays minimal)
		event := events.TransactionEvent{
			Type: events.EventTypeRemoved,
			Transaction: &domain.Transaction{
				UserID:                linkedItem.UserID,
				Source:                domain.SourcePlaid,
				ExternalTransactionID: &externalID,
			},
			UserID:    linkedItem.UserID,
			Source:    domain.SourcePlaid,
			Timestamp: time.Now(),
		}
		if err := p.eventEmitter.Emit(ctx, event); err != nil {
			// Non-fatal: the tombstone is already committed. Log and continue.
			slog.ErrorContext(ctx, "failed to emit transaction event",
				"error", err,
				"event_type", events.EventTypeRemoved,
				"linked_item_id", linkedItem.ID,
			)
		}
	}

	return len(plaidTxns), nil
}

// convertPlaidTransaction converts a Plaid transaction to domain.Transaction
func (p *plaidSyncProvider) convertPlaidTransaction(linkedItem *domain.LinkedItem, plaidTxn plaid.Transaction) (*domain.Transaction, error) {
	tx, err := services.MapPlaidTransactionBase(plaidTxn)
	if err != nil {
		return nil, fmt.Errorf("failed to map plaid transaction %s: %w", plaidTxn.GetTransactionId(), err)
	}

	externalID := plaidTxn.GetTransactionId()
	tx.UserID = linkedItem.UserID
	tx.AccountID = plaidTxn.AccountId
	tx.LinkedItemID = linkedItem.ID
	tx.ExternalTransactionID = &externalID

	return tx, nil
}
