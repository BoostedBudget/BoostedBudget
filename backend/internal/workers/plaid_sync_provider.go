package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/events"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/plaid/plaid-go/v23/plaid"
)

// PlaidClient defines the interface for Plaid API operations needed for transaction sync.
// This allows for mocking in tests.
type PlaidClient interface {
	TransactionsSync(ctx context.Context, accessToken string, cursor *string) (*plaid.TransactionsSyncResponse, error)
}

// plaidSyncProvider implements TransactionSyncProvider for Plaid
type plaidSyncProvider struct {
	linkedItemRepo repository.LinkedItemRepository
	txnRepo        repository.TransactionRepository
	plaidClient    PlaidClient
	eventEmitter   events.EventEmitter
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
func (p *plaidSyncProvider) SyncForLinkedItem(ctx context.Context, linkedItemID string) (int, error) {
	// Fetch linked item
	linkedItem, err := p.linkedItemRepo.GetByID(ctx, linkedItemID)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch linked item: %w", err)
	}

	// Track total transactions processed
	totalCount := 0

	// Start sync with existing cursor (nil for initial sync)
	cursor := linkedItem.Cursor

	// Loop to handle pagination
	for {
		// Call Plaid TransactionsSync API
		syncResp, err := p.plaidClient.TransactionsSync(ctx, linkedItem.AccessTokenEncrypted, cursor)
		if err != nil {
			return totalCount, fmt.Errorf("plaid transactions sync failed: %w", err)
		}

		// Process added transactions
		if len(syncResp.Added) > 0 {
			added, err := p.processAdded(ctx, linkedItem, syncResp.Added)
			if err != nil {
				return totalCount, fmt.Errorf("failed to process added transactions: %w", err)
			}
			totalCount += added
		}

		// Process modified transactions
		if len(syncResp.Modified) > 0 {
			modified, err := p.processModified(ctx, linkedItem, syncResp.Modified)
			if err != nil {
				return totalCount, fmt.Errorf("failed to process modified transactions: %w", err)
			}
			totalCount += modified
		}

		// Process removed transactions
		if len(syncResp.Removed) > 0 {
			removed, err := p.processRemoved(ctx, linkedItem, syncResp.Removed)
			if err != nil {
				return totalCount, fmt.Errorf("failed to process removed transactions: %w", err)
			}
			totalCount += removed
		}

		// Update cursor for next page/sync
		nextCursor := syncResp.NextCursor
		cursor = &nextCursor

		// Break if no more pages
		if !syncResp.HasMore {
			break
		}
	}

	// Update cursor in database for next incremental sync
	if cursor != nil {
		if err := p.linkedItemRepo.UpdateCursor(ctx, linkedItemID, *cursor); err != nil {
			return totalCount, fmt.Errorf("failed to update cursor: %w", err)
		}
	}

	return totalCount, nil
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

	// Batch insert into database
	if err := p.txnRepo.CreateBatch(ctx, domainTxns); err != nil {
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
			// Log error but don't fail sync
			// In production, would use structured logging
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
			// Log error but don't fail sync
		}
	}

	return len(plaidTxns), nil
}

// processRemoved handles removed transactions from Plaid
func (p *plaidSyncProvider) processRemoved(ctx context.Context, linkedItem *domain.LinkedItem, plaidTxns []plaid.RemovedTransaction) (int, error) {
	for _, plaidTxn := range plaidTxns {
		externalID := plaidTxn.GetTransactionId()

		// Delete from database
		if err := p.txnRepo.DeleteByExternalID(ctx, domain.SourcePlaid, externalID, linkedItem.UserID); err != nil {
			return 0, fmt.Errorf("failed to delete transaction: %w", err)
		}

		// Emit event (with minimal transaction data since it's deleted)
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
			// Log error but don't fail sync
		}
	}

	return len(plaidTxns), nil
}

// convertPlaidTransaction converts a Plaid transaction to domain.Transaction
func (p *plaidSyncProvider) convertPlaidTransaction(linkedItem *domain.LinkedItem, plaidTxn plaid.Transaction) (*domain.Transaction, error) {
	// Parse date
	date, err := time.Parse("2006-01-02", plaidTxn.Date)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction date: %w", err)
	}

	// Parse authorized_date if present
	var authorizedDate *time.Time
	if plaidTxn.AuthorizedDate.Get() != nil {
		ad, err := time.Parse("2006-01-02", *plaidTxn.AuthorizedDate.Get())
		if err == nil {
			authorizedDate = &ad
		}
	}

	// Serialize external metadata as JSON
	externalMetadata, err := p.buildExternalMetadata(plaidTxn)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize external metadata: %w", err)
	}

	// Serialize category array as JSON
	var categoryJSON *string
	if len(plaidTxn.Category) > 0 {
		catBytes, _ := json.Marshal(plaidTxn.Category)
		catStr := string(catBytes)
		categoryJSON = &catStr
	}

	// Serialize personal finance category as JSON
	var pfCategoryJSON *string
	if plaidTxn.PersonalFinanceCategory.IsSet() {
		pfBytes, _ := json.Marshal(plaidTxn.PersonalFinanceCategory.Get())
		pfStr := string(pfBytes)
		pfCategoryJSON = &pfStr
	}

	// Get payment channel
	var paymentChannel *string
	if plaidTxn.PaymentChannel != "" {
		paymentChannel = &plaidTxn.PaymentChannel
	}

	// Get merchant name
	var merchantName *string
	if plaidTxn.MerchantName.Get() != nil {
		merchantName = plaidTxn.MerchantName.Get()
	}

	externalID := plaidTxn.GetTransactionId()

	return &domain.Transaction{
		UserID:                  linkedItem.UserID,
		AccountID:               plaidTxn.AccountId, // This should be mapped from Plaid account ID to our account UUID
		LinkedItemID:            linkedItem.ID,
		Source:                  domain.SourcePlaid,
		ExternalTransactionID:   &externalID,
		ExternalMetadata:        externalMetadata,
		Amount:                  plaidTxn.Amount,
		Date:                    date,
		AuthorizedDate:          authorizedDate,
		Name:                    plaidTxn.Name,
		MerchantName:            merchantName,
		Category:                categoryJSON,
		PersonalFinanceCategory: pfCategoryJSON,
		PaymentChannel:          paymentChannel,
		Pending:                 plaidTxn.GetPending(),
		UserNotes:               nil,
	}, nil
}

// buildExternalMetadata creates JSON metadata from Plaid-specific fields
func (p *plaidSyncProvider) buildExternalMetadata(plaidTxn plaid.Transaction) (*string, error) {
	metadata := make(map[string]interface{})

	// Include Plaid-specific fields that don't have direct domain equivalents
	if plaidTxn.Website.Get() != nil {
		metadata["website"] = *plaidTxn.Website.Get()
	}

	// Location is a struct, check if it has meaningful data
	location := plaidTxn.Location
	if location.City.Get() != nil || location.Region.Get() != nil {
		metadata["location"] = location
	}

	// PaymentMeta is a struct, check if it has meaningful data
	paymentMeta := plaidTxn.PaymentMeta
	if paymentMeta.ReferenceNumber.Get() != nil || paymentMeta.PpdId.Get() != nil {
		metadata["payment_meta"] = paymentMeta
	}

	// Only serialize if we have data
	if len(metadata) == 0 {
		return nil, nil
	}

	bytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	metadataStr := string(bytes)
	return &metadataStr, nil
}
