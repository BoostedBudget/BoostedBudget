package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/plaid/plaid-go/v23/plaid"
)

// TransactionSyncService handles synchronizing transactions from Plaid
// via the TransactionsSync API endpoint.
//
// Issue #256: Transaction Sync Service
// Processes TRANSACTIONS webhooks by:
// 1. Fetching unprocessed webhooks from webhook_events table
// 2. Calling Plaid TransactionsSync API with cursor-based pagination
// 3. Storing Added/Modified transactions, deleting Removed transactions
// 4. Updating cursor after successful sync
// 5. Handling retries with exponential backoff on errors
type TransactionSyncService interface {
	// ProcessWebhook processes a single TRANSACTIONS webhook event
	// Coordinates the sync operation:
	// - Validates webhook has item_id
	// - Fetches linked item (with access token)
	// - Calls SyncTransactions for Plaid API integration
	// - Updates webhook status (processed or error with retry)
	ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error

	// SyncTransactions fetches transactions from Plaid using TransactionsSync API.
	// Handles cursor-based pagination until has_more=false, aggregating all results.
	//
	// CRITICAL: Updates cursor after EACH page (not at end) to enable resumability.
	// If sync fails on page N, cursor is already at page N-1, so retry continues from there.
	//
	// Process:
	// 1. Decrypt access token using EncryptionService
	// 2. Loop: call PlaidService.TransactionsSync with cursor
	// 3. Aggregate Added/Modified/Removed from each page
	// 4. Update cursor in database IMMEDIATELY after each page
	// 5. Continue until has_more=false
	//
	// Returns aggregated results from ALL pages fetched. On error mid-pagination,
	// returns partial results with error (cursor already updated for resumability).
	//
	// Issue #260: Pagination is resumable on mid-sync errors
	SyncTransactions(
		ctx context.Context,
		linkedItem *domain.LinkedItem,
	) (*domain.TransactionSyncResult, error)
}

// transactionSyncService implements TransactionSyncService
type transactionSyncService struct {
	webhookRepo       repository.WebhookRepository
	linkedItemRepo    repository.LinkedItemRepository
	transactionRepo   repository.TransactionRepository
	accountRepo       repository.AccountRepository // Issue #261: For account mapping (Plaid account_id -> internal UUID)
	plaidService      PlaidService
	encryptionService EncryptionService
}

// NewTransactionSyncService creates a new TransactionSyncService instance
func NewTransactionSyncService(
	webhookRepo repository.WebhookRepository,
	linkedItemRepo repository.LinkedItemRepository,
	transactionRepo repository.TransactionRepository,
	accountRepo repository.AccountRepository,
	plaidService PlaidService,
	encryptionService EncryptionService,
) TransactionSyncService {
	return &transactionSyncService{
		webhookRepo:       webhookRepo,
		linkedItemRepo:    linkedItemRepo,
		transactionRepo:   transactionRepo,
		accountRepo:       accountRepo,
		plaidService:      plaidService,
		encryptionService: encryptionService,
	}
}

// ProcessWebhook processes a single TRANSACTIONS webhook event
func (s *transactionSyncService) ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	// Validate webhook is not nil
	if webhook == nil {
		return errors.New("webhook event is nil")
	}

	// Validate webhook has item_id (required for TRANSACTIONS webhooks)
	if webhook.ItemID == nil || *webhook.ItemID == "" {
		// Mark webhook with error
		errorMsg := "webhook missing item_id - cannot process TRANSACTIONS webhook"
		webhook.ProcessingError = &errorMsg
		_ = s.webhookRepo.Update(ctx, webhook)
		return errors.New(errorMsg)
	}

	// Mark processing started
	now := time.Now()
	webhook.ProcessingStartedAt = &now
	if err := s.webhookRepo.Update(ctx, webhook); err != nil {
		return fmt.Errorf("failed to mark webhook as processing: %w", err)
	}

	// Get linked item by item_id
	linkedItem, err := s.linkedItemRepo.GetByItemID(ctx, *webhook.ItemID)
	if err != nil {
		// Mark webhook with error and don't retry (non-retryable error)
		errorMsg := fmt.Sprintf("linked item not found for item_id %s: %v", *webhook.ItemID, err)
		webhook.ProcessingError = &errorMsg
		webhook.RetryCount++
		_ = s.webhookRepo.Update(ctx, webhook)
		return fmt.Errorf("failed to get linked item: %w", err)
	}

	// Call SyncTransactions to fetch and process transactions from Plaid
	// This handles: decryption, pagination, cursor updates per page
	result, err := s.SyncTransactions(ctx, linkedItem)
	if err != nil {
		// Classify error to determine retry strategy
		plaidErr := domain.ClassifyPlaidError(err)

		errorMsg := plaidErr.Error()
		webhook.ProcessingError = &errorMsg

		// Permanent errors (e.g., INVALID_ACCESS_TOKEN) should NOT be retried
		// Temporary/RateLimit errors should be retried with exponential backoff
		if plaidErr.ErrorType != domain.PlaidErrorPermanent {
			webhook.RetryCount++
		}

		_ = s.webhookRepo.Update(ctx, webhook)
		return fmt.Errorf("sync transactions failed: %w", err)
	}

	// Log transaction counts for observability
	// Note: Storage handled in SyncTransactions (issue #261)
	log.Printf("Webhook %s processed and stored: %d added, %d modified, %d removed transactions",
		webhook.ID, len(result.Added), len(result.Modified), len(result.Removed))

	// Mark webhook as processed successfully
	completedAt := time.Now()
	webhook.Processed = true
	webhook.ProcessingCompletedAt = &completedAt
	if err := s.webhookRepo.Update(ctx, webhook); err != nil {
		return fmt.Errorf("failed to mark webhook as processed: %w", err)
	}

	return nil
}

// SyncTransactions fetches all transaction pages from Plaid using cursor-based pagination.
// Issue #260: Plaid TransactionsSync API Integration - core pagination logic
func (s *transactionSyncService) SyncTransactions(
	ctx context.Context,
	linkedItem *domain.LinkedItem,
) (*domain.TransactionSyncResult, error) {
	// Step 1: Decrypt access token
	accessToken, err := s.encryptionService.Decrypt(linkedItem.AccessTokenEncrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt access token: %w", err)
	}

	// Step 2: Initialize result aggregation
	result := &domain.TransactionSyncResult{
		Added:    []plaid.Transaction{},
		Modified: []plaid.Transaction{},
		Removed:  []plaid.RemovedTransaction{},
	}

	// Step 3: Get initial cursor (empty string for first sync)
	cursor := ""
	if linkedItem.Cursor != nil {
		cursor = *linkedItem.Cursor
	}

	// Step 4: Pagination loop - continue until has_more=false
	hasMore := true
	pageCount := 0

	for hasMore {
		pageCount++

		// Call Plaid API for single page
		resp, err := s.plaidService.TransactionsSync(ctx, accessToken, cursor)
		if err != nil {
			// Return partial results with error
			// Cursor already updated for pages that succeeded
			return result, fmt.Errorf("sync failed on page %d: %w", pageCount, err)
		}

		// Aggregate results from this page
		result.Added = append(result.Added, resp.Added...)
		result.Modified = append(result.Modified, resp.Modified...)
		result.Removed = append(result.Removed, resp.Removed...)

		// CRITICAL: Update cursor IMMEDIATELY for resumability
		// If next page fails, we can resume from this cursor
		newCursor := resp.GetNextCursor()
		err = s.linkedItemRepo.UpdateCursor(ctx, linkedItem.ID, newCursor)
		if err != nil {
			// Cursor update failed - return partial results with error
			return result, fmt.Errorf("failed to update cursor on page %d: %w", pageCount, err)
		}
		cursor = newCursor

		// Check if more pages exist
		hasMore = resp.GetHasMore()

		log.Printf("Synced page %d for item %s: %d added, %d modified, %d removed (hasMore=%v)",
			pageCount, linkedItem.ID, len(resp.Added), len(resp.Modified), len(resp.Removed), hasMore)
	}

	// All pages fetched successfully
	result.NewCursor = cursor
	log.Printf("Sync complete for item %s: total %d added, %d modified, %d removed across %d pages",
		linkedItem.ID, len(result.Added), len(result.Modified), len(result.Removed), pageCount)

	// ========================================================================
	// Issue #261: Transaction Storage Logic
	// Store Added/Modified transactions, delete Removed transactions
	// ========================================================================

	// Step 5: Build account map for transaction mapping (O(1) lookups)
	accountMap, err := s.buildAccountMap(ctx, linkedItem.ID)
	if err != nil {
		// Return successful sync result but log storage error
		// Webhook can retry storage on next sync
		log.Printf("ERROR: Failed to build account map for item %s: %v", linkedItem.ID, err)
		return result, fmt.Errorf("failed to build account map: %w", err)
	}

	// Step 6: Process Added transactions (create batch)
	if len(result.Added) > 0 {
		if err := s.processAddedTransactions(ctx, result.Added, linkedItem, accountMap); err != nil {
			log.Printf("ERROR: Failed to process added transactions for item %s: %v", linkedItem.ID, err)
			return result, fmt.Errorf("failed to process added transactions: %w", err)
		}
	}

	// Step 7: Process Modified transactions (update with user_notes preservation)
	if len(result.Modified) > 0 {
		if err := s.processModifiedTransactions(ctx, result.Modified, linkedItem, accountMap); err != nil {
			log.Printf("ERROR: Failed to process modified transactions for item %s: %v", linkedItem.ID, err)
			return result, fmt.Errorf("failed to process modified transactions: %w", err)
		}
	}

	// Step 8: Process Removed transactions (hard delete)
	if len(result.Removed) > 0 {
		if err := s.processRemovedTransactions(ctx, result.Removed, linkedItem); err != nil {
			log.Printf("ERROR: Failed to process removed transactions for item %s: %v", linkedItem.ID, err)
			return result, fmt.Errorf("failed to process removed transactions: %w", err)
		}
	}

	log.Printf("Transaction storage complete for item %s", linkedItem.ID)

	return result, nil
}

// ============================================================================
// Issue #261: Transaction Storage Logic - Helper Methods
// ============================================================================

// buildAccountMap loads all accounts for a linked item and creates a map
// from Plaid account_id to internal account UUID for efficient lookups.
//
// This allows O(1) account mapping when processing transactions, avoiding
// N database queries for N transactions.
//
// Returns:
//   - map[plaidAccountID]internalAccountUUID
//   - error if accounts cannot be loaded
func (s *transactionSyncService) buildAccountMap(ctx context.Context, linkedItemID string) (map[string]string, error) {
	// Load all accounts for this linked item
	accounts, err := s.accountRepo.GetByLinkedItemID(ctx, linkedItemID)
	if err != nil {
		return nil, fmt.Errorf("failed to load accounts for linked item %s: %w", linkedItemID, err)
	}

	// Build map: Plaid account_id -> Internal account UUID
	accountMap := make(map[string]string, len(accounts))
	for _, account := range accounts {
		accountMap[account.PlaidAccountID] = account.ID
	}

	return accountMap, nil
}

// mapPlaidTransaction converts a Plaid transaction to domain.Transaction.
// Performs account ID mapping using the provided accountMap.
//
// Returns error if:
//   - Account not found in accountMap
//   - Date parsing fails
//   - JSON serialization fails
func (s *transactionSyncService) mapPlaidTransaction(
	ctx context.Context,
	plaidTxn plaid.Transaction,
	linkedItemID string,
	userID string,
	accountMap map[string]string,
) (*domain.Transaction, error) {
	// Map Plaid account_id to internal account UUID
	internalAccountID, found := accountMap[plaidTxn.AccountId]
	if !found {
		return nil, fmt.Errorf("account not found for plaid_account_id %s", plaidTxn.AccountId)
	}

	// Parse transaction date (required)
	date, err := time.Parse("2006-01-02", plaidTxn.Date)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction date %s: %w", plaidTxn.Date, err)
	}

	// Parse authorized_date (optional)
	var authorizedDate *time.Time
	if plaidTxn.AuthorizedDate.Get() != nil {
		ad, err := time.Parse("2006-01-02", *plaidTxn.AuthorizedDate.Get())
		if err == nil { // Ignore parse errors for optional field
			authorizedDate = &ad
		}
	}

	// Serialize external metadata (Plaid-specific fields)
	externalMetadata, err := s.buildExternalMetadata(plaidTxn)
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

	// Extract optional fields
	var paymentChannel *string
	if plaidTxn.PaymentChannel != "" {
		paymentChannel = &plaidTxn.PaymentChannel
	}

	var merchantName *string
	if plaidTxn.MerchantName.Get() != nil {
		merchantName = plaidTxn.MerchantName.Get()
	}

	externalID := plaidTxn.GetTransactionId()

	return &domain.Transaction{
		UserID:                  userID,
		AccountID:               internalAccountID, // Correctly mapped!
		LinkedItemID:            linkedItemID,
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
		UserNotes:               nil, // Always nil for new/modified transactions from Plaid
	}, nil
}

// buildExternalMetadata creates JSON metadata from Plaid-specific fields
// that don't have direct domain equivalents.
func (s *transactionSyncService) buildExternalMetadata(plaidTxn plaid.Transaction) (*string, error) {
	metadata := make(map[string]interface{})

	// Include Plaid-specific fields
	if plaidTxn.Website.Get() != nil {
		metadata["website"] = *plaidTxn.Website.Get()
	}

	// Location data
	location := plaidTxn.Location
	if location.City.Get() != nil || location.Region.Get() != nil {
		metadata["location"] = location
	}

	// Payment metadata
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

// processAddedTransactions stores newly added transactions in the database.
// Maps Plaid transactions to domain.Transaction and calls CreateBatch.
//
// Graceful error handling:
//   - Skips transactions with unmapped accounts (logs warning)
//   - Continues processing even if some transactions fail mapping
//   - Returns error only if database CreateBatch fails
//
// Deduplication: CreateBatch uses ON CONFLICT DO NOTHING, so duplicates are ignored.
func (s *transactionSyncService) processAddedTransactions(
	ctx context.Context,
	added []plaid.Transaction,
	linkedItem *domain.LinkedItem,
	accountMap map[string]string,
) error {
	// Optimization: Skip DB call if no transactions
	if len(added) == 0 {
		return nil
	}

	// Map Plaid transactions to domain.Transaction
	transactions := make([]domain.Transaction, 0, len(added))
	for _, plaidTxn := range added {
		domainTxn, err := s.mapPlaidTransaction(ctx, plaidTxn, linkedItem.ID, linkedItem.UserID, accountMap)
		if err != nil {
			// Log warning but continue processing other transactions
			log.Printf("Warning: Skipping transaction %s: %v", plaidTxn.TransactionId, err)
			continue
		}
		transactions = append(transactions, *domainTxn)
	}

	// Skip DB call if all transactions were skipped
	if len(transactions) == 0 {
		log.Printf("Warning: All %d added transactions skipped due to mapping errors", len(added))
		return nil
	}

	// Store in database (ON CONFLICT DO NOTHING handles deduplication)
	if err := s.transactionRepo.CreateBatch(ctx, transactions); err != nil {
		return fmt.Errorf("failed to create batch of %d transactions: %w", len(transactions), err)
	}

	log.Printf("Successfully stored %d added transactions (skipped %d)", len(transactions), len(added)-len(transactions))
	return nil
}

// processModifiedTransactions updates existing transactions in the database.
// CRITICAL: Preserves user_notes field from existing transaction.
//
// Process:
//   1. Map Plaid transaction to domain.Transaction
//   2. Fetch existing transaction by external_transaction_id
//   3. Preserve user_notes from existing transaction
//   4. Call Update with new data + preserved user_notes + existing ID
//
// Graceful error handling:
//   - Skips transactions with unmapped accounts (logs warning)
//   - Skips transactions not found in database (logs warning)
//   - Continues processing even if some transactions fail
//   - Returns error only if Update call fails
func (s *transactionSyncService) processModifiedTransactions(
	ctx context.Context,
	modified []plaid.Transaction,
	linkedItem *domain.LinkedItem,
	accountMap map[string]string,
) error {
	// Optimization: Skip processing if no transactions
	if len(modified) == 0 {
		return nil
	}

	successCount := 0
	skippedCount := 0

	for _, plaidTxn := range modified {
		// Map Plaid transaction to domain.Transaction (new data from Plaid)
		newTxn, err := s.mapPlaidTransaction(ctx, plaidTxn, linkedItem.ID, linkedItem.UserID, accountMap)
		if err != nil {
			log.Printf("Warning: Skipping modified transaction %s: %v", plaidTxn.TransactionId, err)
			skippedCount++
			continue
		}

		// Fetch existing transaction to preserve user_notes
		existingTxn, err := s.transactionRepo.GetByExternalID(ctx, domain.SourcePlaid, plaidTxn.TransactionId, linkedItem.UserID)
		if err != nil {
			log.Printf("Warning: Modified transaction %s not found in database (may not be synced yet): %v", plaidTxn.TransactionId, err)
			skippedCount++
			continue
		}

		// CRITICAL: Preserve user_notes from existing transaction
		newTxn.UserNotes = existingTxn.UserNotes

		// Copy ID from existing transaction (required for Update)
		newTxn.ID = existingTxn.ID

		// Update transaction with new data but preserved user_notes
		if err := s.transactionRepo.Update(ctx, newTxn); err != nil {
			return fmt.Errorf("failed to update transaction %s: %w", plaidTxn.TransactionId, err)
		}

		successCount++
	}

	log.Printf("Successfully updated %d modified transactions (skipped %d)", successCount, skippedCount)
	return nil
}

// processRemovedTransactions deletes transactions that Plaid reports as removed.
// Uses hard delete (no soft-delete/deleted_at column exists).
//
// Idempotent: If transaction already deleted, logs warning but continues.
//
// Graceful error handling:
//   - Skips transactions with nil transaction_id (logs warning)
//   - Continues processing even if delete fails for some transactions
//   - Treats "not found" errors as success (idempotent)
func (s *transactionSyncService) processRemovedTransactions(
	ctx context.Context,
	removed []plaid.RemovedTransaction,
	linkedItem *domain.LinkedItem,
) error {
	// Optimization: Skip processing if no transactions
	if len(removed) == 0 {
		return nil
	}

	successCount := 0
	skippedCount := 0

	for _, removedTxn := range removed {
		// Skip transactions with nil ID
		if removedTxn.TransactionId == nil {
			log.Printf("Warning: Removed transaction has nil transaction_id, skipping")
			skippedCount++
			continue
		}

		// Delete transaction (hard delete)
		err := s.transactionRepo.DeleteByExternalID(ctx, domain.SourcePlaid, *removedTxn.TransactionId, linkedItem.UserID)
		if err != nil {
			// Log warning but continue (idempotent - may already be deleted)
			log.Printf("Warning: Failed to delete transaction %s (may already be deleted): %v", *removedTxn.TransactionId, err)
			// Count as skipped rather than failure for idempotency
			skippedCount++
			continue
		}

		successCount++
	}

	log.Printf("Successfully deleted %d removed transactions (skipped %d)", successCount, skippedCount)
	return nil
}
