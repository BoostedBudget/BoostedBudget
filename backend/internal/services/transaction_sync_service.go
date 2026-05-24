package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	// 1. Get plaintext access token (decrypted by repository layer on read)
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
	logger                    *slog.Logger
	webhookRepo               repository.WebhookRepository
	linkedItemRepo            repository.LinkedItemRepository
	transactionRepo           repository.TransactionRepository
	accountRepo               repository.AccountRepository          // Issue #261: For account mapping (Plaid account_id -> internal UUID)
	profileRepo               repository.ProfileRepository          // For fetching user subscription tier (historical depth)
	balanceSnapshotRepo       repository.BalanceSnapshotRepository  // Issue #353: For storing balance snapshots
	merchantRepo              repository.MerchantRepo               // Issue #1163: Batch-upsert merchants before CreateBatch
	plaidService              PlaidService
	subscriptionService       *SubscriptionService          // For tier-based historical depth limit
	ruleApplicationService    RuleApplicationService        // Issue #346: Apply categorization rules to new transactions
	budgetSyncService         BudgetSyncService             // Issue #494: Sync budget amounts after transaction sync
	transactionMonitorService TransactionMonitorService     // Issue #36: Auto-detect completed transfer suggestions
}

// NewTransactionSyncService creates a new TransactionSyncService instance
func NewTransactionSyncService(
	webhookRepo repository.WebhookRepository,
	linkedItemRepo repository.LinkedItemRepository,
	transactionRepo repository.TransactionRepository,
	accountRepo repository.AccountRepository,
	profileRepo repository.ProfileRepository,
	balanceSnapshotRepo repository.BalanceSnapshotRepository,
	plaidService PlaidService,
	subscriptionService *SubscriptionService,
	ruleApplicationService RuleApplicationService,
	budgetSyncService BudgetSyncService,               // Issue #494: Sync budget amounts
	transactionMonitorService TransactionMonitorService, // Issue #36: Auto-detect completed transfers
	merchantRepo repository.MerchantRepo,              // Issue #1163: Merchant batch-upsert
	logger *slog.Logger,
) TransactionSyncService {
	if logger == nil {
		logger = slog.Default()
	}
	return &transactionSyncService{
		logger:                    logger,
		webhookRepo:               webhookRepo,
		linkedItemRepo:            linkedItemRepo,
		transactionRepo:           transactionRepo,
		accountRepo:               accountRepo,
		profileRepo:               profileRepo,
		balanceSnapshotRepo:       balanceSnapshotRepo,
		merchantRepo:              merchantRepo,
		plaidService:              plaidService,
		subscriptionService:       subscriptionService,
		ruleApplicationService:    ruleApplicationService,
		budgetSyncService:         budgetSyncService,
		transactionMonitorService: transactionMonitorService,
	}
}

// ProcessWebhook processes a single TRANSACTIONS webhook event
func (s *transactionSyncService) ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
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
	s.logger.Info("webhook processed and stored",
		"webhook_id", webhook.ID,
		"added", len(result.Added),
		"modified", len(result.Modified),
		"removed", len(result.Removed),
	)

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
	// Step 1: Get initial cursor (empty string for first sync)
	cursor := ""
	if linkedItem.Cursor != nil {
		cursor = *linkedItem.Cursor
	}

	// Step 2: Determine historical depth for first sync
	// Only applies on FIRST sync (empty cursor). Re-syncs use cursor-based incremental sync.
	var daysRequested int
	if cursor == "" {
		// Look up user to determine their tier-based historical depth limit
		user, err := s.profileRepo.GetUserByID(ctx, linkedItem.UserID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user for historical depth: %w", err)
		}
		daysHistorical, err := s.subscriptionService.GetHistoricalDepthLimit(ctx, user)
		if err != nil {
			return nil, fmt.Errorf("failed to get historical depth limit: %w", err)
		}
		daysRequested = daysHistorical
		s.logger.Info("first sync, requesting historical transactions", "item_id", linkedItem.ID, "days_requested", daysRequested)
	} else {
		// Re-sync (cursor-based) - no historical depth needed
		daysRequested = 0
		s.logger.Info("re-sync using cursor", "item_id", linkedItem.ID)
	}

	// Step 3: Get plaintext access token (populated by repo layer on read)
	accessToken := linkedItem.AccessToken

	// Step 4: Initialize result aggregation
	result := &domain.TransactionSyncResult{
		Added:    []plaid.Transaction{},
		Modified: []plaid.Transaction{},
		Removed:  []plaid.RemovedTransaction{},
	}

	// Step 5: Pagination loop - continue until has_more=false
	hasMore := true
	pageCount := 0

	for hasMore {
		pageCount++

		// Call Plaid API for single page (with tier-based historical depth)
		resp, err := s.plaidService.TransactionsSync(ctx, accessToken, cursor, daysRequested)
		if err != nil {
			// Issue #171: Check for invalid cursor error and auto-reset
			// Check if error is already a PlaidSyncError, otherwise classify it
			var plaidErr *domain.PlaidSyncError
			if existingPlaidErr, ok := err.(*domain.PlaidSyncError); ok {
				plaidErr = existingPlaidErr
			} else {
				plaidErr = domain.ClassifyPlaidError(err)
			}
			if plaidErr.ErrorType == domain.PlaidErrorInvalidCursor {
				// Reset cursor to empty string for full re-sync
				s.logger.Warn("invalid cursor detected, resetting for full re-sync", "item_id", linkedItem.ID)
				if resetErr := s.linkedItemRepo.UpdateCursor(ctx, linkedItem.ID, ""); resetErr != nil {
					return result, fmt.Errorf("failed to reset cursor after INVALID_CURSOR: %w", resetErr)
				}
				// Return error to trigger retry with empty cursor
				return result, fmt.Errorf("cursor reset, will retry full sync: %w", err)
			}
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

		s.logger.Info("synced page",
			"page", pageCount,
			"item_id", linkedItem.ID,
			"added", len(resp.Added),
			"modified", len(resp.Modified),
			"removed", len(resp.Removed),
			"has_more", hasMore,
		)
	}

	// All pages fetched successfully
	result.NewCursor = cursor
	s.logger.Info("sync complete",
		"item_id", linkedItem.ID,
		"total_added", len(result.Added),
		"total_modified", len(result.Modified),
		"total_removed", len(result.Removed),
		"pages", pageCount,
	)

	// ========================================================================
	// Issue #261: Transaction Storage Logic
	// Store Added/Modified transactions, delete Removed transactions
	// ========================================================================

	// Step 5: Build account maps for transaction mapping (O(1) lookups)
	// Issue #171: Also builds userAccountMap for stable UserAccountID reference
	accountMap, userAccountMap, err := s.buildAccountMaps(ctx, linkedItem.ID)
	if err != nil {
		// Return successful sync result but log storage error
		// Webhook can retry storage on next sync
		s.logger.Error("failed to build account maps", "item_id", linkedItem.ID, "error", err)
		return result, fmt.Errorf("failed to build account maps: %w", err)
	}

	// Step 6: Process Added transactions (create batch)
	// Issue #171: Pass userAccountMap for stable UserAccountID propagation
	if len(result.Added) > 0 {
		if err := s.processAddedTransactions(ctx, result.Added, linkedItem, accountMap, userAccountMap); err != nil {
			s.logger.Error("failed to process added transactions", "item_id", linkedItem.ID, "error", err)
			return result, fmt.Errorf("failed to process added transactions: %w", err)
		}
	}

	// Step 7: Process Modified transactions (update with user_notes preservation)
	// Issue #171: Pass userAccountMap for stable UserAccountID propagation
	if len(result.Modified) > 0 {
		if err := s.processModifiedTransactions(ctx, result.Modified, linkedItem, accountMap, userAccountMap); err != nil {
			s.logger.Error("failed to process modified transactions", "item_id", linkedItem.ID, "error", err)
			return result, fmt.Errorf("failed to process modified transactions: %w", err)
		}
	}

	// Step 8: Process Removed transactions (hard delete)
	if len(result.Removed) > 0 {
		if err := s.processRemovedTransactions(ctx, result.Removed, linkedItem); err != nil {
			s.logger.Error("failed to process removed transactions", "item_id", linkedItem.ID, "error", err)
			return result, fmt.Errorf("failed to process removed transactions: %w", err)
		}
	}

	s.logger.Info("transaction storage complete", "item_id", linkedItem.ID)

	// ========================================================================
	// Issue #353: Create Balance Snapshots
	// Capture current account balances after transaction sync completes
	// ========================================================================

	// Create balance snapshots for all accounts (non-blocking)
	if err := s.createBalanceSnapshots(ctx, linkedItem, accessToken); err != nil {
		s.logger.Warn("failed to create balance snapshots", "item_id", linkedItem.ID, "error", err)
		// Don't fail the entire sync - snapshots are non-critical
	}

	// Issue #494: Sync budget amounts (non-blocking)
	if s.budgetSyncService != nil {
		if err := s.budgetSyncService.RecalculateBudgetAmounts(ctx, linkedItem.UserID); err != nil {
			s.logger.Warn("failed to sync budget amounts", "user_id", linkedItem.UserID, "error", err)
		}
	}

	return result, nil
}

// ============================================================================
// Issue #261: Transaction Storage Logic - Helper Methods
// ============================================================================

// buildAccountMaps builds both accountMap and userAccountMap for transaction mapping.
// Issue #171: userAccountMap provides stable UserAccountID that survives re-links.
// Returns:
//   - accountMap: Plaid account_id -> Internal account UUID
//   - userAccountMap: Plaid account_id -> UserAccount UUID (stable reference)
func (s *transactionSyncService) buildAccountMaps(ctx context.Context, linkedItemID string) (map[string]string, map[string]string, error) {
	// Load all accounts for this linked item
	accounts, err := s.accountRepo.GetByLinkedItemID(ctx, linkedItemID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load accounts for linked item %s: %w", linkedItemID, err)
	}

	// Build maps: Plaid account_id -> Internal IDs
	accountMap := make(map[string]string, len(accounts))
	userAccountMap := make(map[string]string, len(accounts))

	for _, account := range accounts {
		accountMap[account.PlaidAccountID] = account.ID
		// Issue #171: Also map to UserAccountID for stable reference
		if account.UserAccountID != nil {
			userAccountMap[account.PlaidAccountID] = *account.UserAccountID
		}
	}

	return accountMap, userAccountMap, nil
}

// mapPlaidTransaction converts a Plaid transaction to domain.Transaction.
// Maps account IDs using accountMap and userAccountMap for stable references.
// Returns error if account is not found in map or date parsing fails.
func (s *transactionSyncService) mapPlaidTransaction(
	ctx context.Context,
	plaidTxn plaid.Transaction,
	linkedItemID string,
	userID string,
	accountMap map[string]string,
	userAccountMap map[string]string,
) (*domain.Transaction, error) {
	internalAccountID, found := accountMap[plaidTxn.AccountId]
	if !found {
		return nil, fmt.Errorf("account not found for plaid_account_id %s", plaidTxn.AccountId)
	}

	var userAccountID *string
	if len(userAccountMap) > 0 {
		if uaID, found := userAccountMap[plaidTxn.AccountId]; found {
			userAccountID = &uaID
		}
	}

	tx, err := MapPlaidTransactionBase(plaidTxn)
	if err != nil {
		return nil, fmt.Errorf("failed to map plaid transaction %s: %w", plaidTxn.GetTransactionId(), err)
	}

	externalID := plaidTxn.GetTransactionId()
	tx.UserID = userID
	tx.AccountID = internalAccountID
	tx.LinkedItemID = linkedItemID
	tx.UserAccountID = userAccountID
	tx.ExternalTransactionID = &externalID

	return tx, nil
}

// processAddedTransactions stores newly added transactions in the database.
// Maps Plaid transactions to domain.Transaction and calls CreateBatch.
// Issue #171: Also sets UserAccountID from userAccountMap for stable reference.
//
// Graceful error handling:
//   - Skips transactions with unmapped accounts (logs warning)
//   - Continues processing even if some transactions fail mapping
//   - Returns error only if database CreateBatch fails
//
// Deduplication: CreateBatch uses ON CONFLICT DO UPDATE, preserving user enrichments.
func (s *transactionSyncService) processAddedTransactions(
	ctx context.Context,
	added []plaid.Transaction,
	linkedItem *domain.LinkedItem,
	accountMap map[string]string,
	userAccountMap map[string]string,
) error {
	if len(added) == 0 {
		return nil
	}

	// Map Plaid transactions to domain.Transaction
	transactions := make([]domain.Transaction, 0, len(added))
	for _, plaidTxn := range added {
		domainTxn, err := s.mapPlaidTransaction(ctx, plaidTxn, linkedItem.ID, linkedItem.UserID, accountMap, userAccountMap)
		if err != nil {
			s.logger.Warn("skipping transaction", "transaction_id", plaidTxn.TransactionId, "error", err)
			continue
		}
		transactions = append(transactions, *domainTxn)
	}

	// Skip DB call if all transactions were skipped
	if len(transactions) == 0 {
		s.logger.Warn("all added transactions skipped due to mapping errors", "count", len(added))
		return nil
	}

	// Issue #346: Apply categorization rules to transactions before storing
	// This applies user-defined rules to automatically categorize and tag transactions.
	// Rules are applied in-place, modifying the transaction slice.
	if s.ruleApplicationService != nil {
		if err := s.ruleApplicationService.ApplyRulesToBatch(ctx, transactions, linkedItem.UserID); err != nil {
			// Log but don't fail - rule application is non-critical
			s.logger.Warn("failed to apply categorization rules", "error", err)
		}
	}

	// Issue #1163: Batch-upsert merchants and stamp merchant_id (one DB round-trip, best-effort)
	if s.merchantRepo != nil {
		nameSet := make(map[string]struct{})
		for _, txn := range transactions {
			if txn.MerchantName != nil && *txn.MerchantName != "" {
				nameSet[*txn.MerchantName] = struct{}{}
			}
		}
		if len(nameSet) > 0 {
			names := make([]string, 0, len(nameSet))
			for name := range nameSet {
				names = append(names, name)
			}
			merchantIDs, err := s.merchantRepo.GetOrCreateMerchants(ctx, names)
			if err != nil {
				// Non-blocking: merchant_id is best-effort, sync must not fail
				s.logger.Warn("failed to upsert merchants, continuing without merchant_id", "err", err)
			} else {
				for i := range transactions {
					if transactions[i].MerchantName != nil {
						if id, ok := merchantIDs[*transactions[i].MerchantName]; ok {
							transactions[i].MerchantID = &id
						}
					}
				}
			}
		}
	}

	// Store in database (ON CONFLICT DO NOTHING handles deduplication)
	if err := s.transactionRepo.CreateBatch(ctx, transactions); err != nil {
		return fmt.Errorf("failed to create batch of %d transactions: %w", len(transactions), err)
	}

	s.logger.Info("stored added transactions", "stored", len(transactions), "skipped", len(added)-len(transactions))

	// Issue #36: Monitor for transfer suggestion matches (non-blocking)
	// Auto-accept pending suggestions that match newly synced transactions.
	if s.transactionMonitorService != nil {
		// Convert to pointer slice for monitor service
		txnPtrs := make([]*domain.Transaction, len(transactions))
		for i := range transactions {
			txnPtrs[i] = &transactions[i]
		}
		if err := s.transactionMonitorService.MonitorForMatches(ctx, linkedItem.UserID, txnPtrs); err != nil {
			// Log but don't fail - transaction monitoring is non-critical
			s.logger.Warn("failed to monitor for transfer suggestion matches", "error", err)
		}
	}

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
	userAccountMap map[string]string,
) error {
	if len(modified) == 0 {
		return nil
	}

	successCount := 0
	skippedCount := 0

	// Issue #1163: Batch-upsert merchants and stamp merchant_id (one DB round-trip, best-effort)
	// Mirrors processAddedTransactions. If upsert fails, Update proceeds with merchant_id = nil.
	var merchantIDs map[string]string
	if s.merchantRepo != nil {
		nameSet := make(map[string]struct{})
		for _, plaidTxn := range modified {
			if mn := plaidTxn.MerchantName.Get(); mn != nil && *mn != "" {
				nameSet[*mn] = struct{}{}
			}
		}
		if len(nameSet) > 0 {
			names := make([]string, 0, len(nameSet))
			for name := range nameSet {
				names = append(names, name)
			}
			ids, err := s.merchantRepo.GetOrCreateMerchants(ctx, names)
			if err != nil {
				// Non-blocking: merchant_id is best-effort, sync must not fail
				s.logger.Warn("failed to upsert merchants, continuing without merchant_id", "err", err)
			} else {
				merchantIDs = ids
			}
		}
	}

	for _, plaidTxn := range modified {
		// Map Plaid transaction to domain.Transaction (new data from Plaid)
		newTxn, err := s.mapPlaidTransaction(ctx, plaidTxn, linkedItem.ID, linkedItem.UserID, accountMap, userAccountMap)
		if err != nil {
			s.logger.Warn("skipping modified transaction", "transaction_id", plaidTxn.TransactionId, "error", err)
			skippedCount++
			continue
		}

		// Fetch existing transaction to preserve user_notes
		existingTxn, err := s.transactionRepo.GetByExternalID(ctx, domain.SourcePlaid, plaidTxn.TransactionId, linkedItem.UserID)
		if err != nil {
			s.logger.Warn("modified transaction not found in database", "transaction_id", plaidTxn.TransactionId, "error", err)
			skippedCount++
			continue
		}

		// CRITICAL: Preserve user_notes from existing transaction
		newTxn.UserNotes = existingTxn.UserNotes

		// Copy ID from existing transaction (required for Update)
		newTxn.ID = existingTxn.ID

		// Issue #1163: Stamp merchant_id from the pre-computed map (best-effort)
		if newTxn.MerchantName != nil && merchantIDs != nil {
			if id, ok := merchantIDs[*newTxn.MerchantName]; ok {
				newTxn.MerchantID = &id
			}
		}

		// Update transaction with new data but preserved user_notes
		if err := s.transactionRepo.Update(ctx, newTxn); err != nil {
			return fmt.Errorf("failed to update transaction %s: %w", plaidTxn.TransactionId, err)
		}

		successCount++
	}

	s.logger.Info("updated modified transactions", "updated", successCount, "skipped", skippedCount)
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
			s.logger.Warn("removed transaction has nil transaction_id, skipping")
			skippedCount++
			continue
		}

		// Delete transaction (hard delete)
		err := s.transactionRepo.DeleteByExternalID(ctx, domain.SourcePlaid, *removedTxn.TransactionId, linkedItem.UserID)
		if err != nil {
			// Log warning but continue (idempotent - may already be deleted)
			s.logger.Warn("failed to delete transaction (may already be deleted)", "transaction_id", *removedTxn.TransactionId, "error", err)
			// Count as skipped rather than failure for idempotency
			skippedCount++
			continue
		}

		successCount++
	}

	s.logger.Info("deleted removed transactions", "deleted", successCount, "skipped", skippedCount)
	return nil
}

// createBalanceSnapshots fetches current account balances from Plaid and stores snapshots.
// Issue #353: Balance Tracking Dashboard - Capture balance snapshots during webhook processing
// Issue #364: Migrated to timestamp-based storage for high-frequency Premium+ syncs
//
// Called after transaction sync completes to capture the current state of all account balances.
// Uses 5-minute deduplication window to prevent excessive snapshots while allowing
// multiple snapshots per day for high-frequency sync tiers.
//
// Error Handling:
//   - Logs warnings but doesn't fail the entire sync
//   - Balance snapshots are non-critical for transaction sync
//   - Failed snapshots will be retried on next webhook
func (s *transactionSyncService) createBalanceSnapshots(
	ctx context.Context,
	linkedItem *domain.LinkedItem,
	accessToken string,
) error {
	// Skip if balance snapshot repository is not configured (backward compatibility for tests)
	if s.balanceSnapshotRepo == nil {
		return nil
	}

	// Step 1: Call Plaid /accounts/get to fetch current balances
	client := s.plaidService.GetClient()
	accountsReq := plaid.NewAccountsGetRequest(accessToken)
	accountsResp, _, err := client.PlaidApi.AccountsGet(ctx).AccountsGetRequest(*accountsReq).Execute()
	if err != nil {
		return fmt.Errorf("failed to fetch accounts from Plaid: %w", err)
	}

	plaidAccounts := accountsResp.GetAccounts()
	if len(plaidAccounts) == 0 {
		s.logger.Info("no accounts returned from Plaid, skipping balance snapshots", "item_id", linkedItem.ID)
		return nil
	}

	// Step 2: Get internal account IDs (map Plaid account_id -> internal UUID)
	internalAccounts, err := s.accountRepo.GetByLinkedItemID(ctx, linkedItem.ID)
	if err != nil {
		return fmt.Errorf("failed to get internal accounts: %w", err)
	}

	// Build map: plaid_account_id -> internal account
	accountMap := make(map[string]domain.Account)
	for _, account := range internalAccounts {
		accountMap[account.PlaidAccountID] = account
	}

	// Step 3: Create balance snapshots for each account
	now := time.Now().UTC()
	deduplicationWindow := 5 * time.Minute
	successCount, skippedCount, dedupedCount := 0, 0, 0

	for _, plaidAccount := range plaidAccounts {
		plaidAccountID := plaidAccount.GetAccountId()

		// Find matching internal account
		internalAccount, exists := accountMap[plaidAccountID]
		if !exists {
			s.logger.Warn("Plaid account not found in internal accounts, skipping snapshot", "plaid_account_id", plaidAccountID)
			skippedCount++
			continue
		}

		// Check for recent snapshot (5-minute deduplication window)
		latestSnapshot, err := s.balanceSnapshotRepo.GetLatestByAccountID(ctx, internalAccount.ID)
		if err != nil {
			s.logger.Warn("failed to check latest snapshot for account", "plaid_account_id", plaidAccountID, "error", err)
		} else if latestSnapshot != nil && now.Sub(latestSnapshot.SnapshotAt) < deduplicationWindow {
			s.logger.Debug("skipping snapshot, recent snapshot exists",
				"plaid_account_id", plaidAccountID,
				"age", now.Sub(latestSnapshot.SnapshotAt).Round(time.Second),
			)
			dedupedCount++
			continue
		}

		// Extract balance from Plaid response
		balances, hasBalances := plaidAccount.GetBalancesOk()
		if !hasBalances || balances == nil {
			s.logger.Warn("no balance data for account, skipping snapshot", "plaid_account_id", plaidAccountID)
			skippedCount++
			continue
		}

		// Create and validate balance snapshot
		snapshot := s.createBalanceSnapshot(internalAccount, linkedItem, balances, now, plaidAccountID)
		if snapshot == nil {
			skippedCount++
			continue
		}

		// Store snapshot
		if err := s.balanceSnapshotRepo.Create(ctx, snapshot); err != nil {
			s.logger.Warn("failed to create balance snapshot", "plaid_account_id", plaidAccountID, "error", err)
			skippedCount++
			continue
		}

		successCount++
	}

	s.logger.Info("created balance snapshots",
		"item_id", linkedItem.ID,
		"created", successCount,
		"skipped", skippedCount,
		"deduped", dedupedCount,
	)
	return nil
}

// createBalanceSnapshot builds a BalanceSnapshot from Plaid balance data.
// Returns nil if the snapshot is invalid (missing both current and available balance).
func (s *transactionSyncService) createBalanceSnapshot(
	account domain.Account,
	linkedItem *domain.LinkedItem,
	balances *plaid.AccountBalance,
	timestamp time.Time,
	plaidAccountID string,
) *domain.BalanceSnapshot {
	snapshot := &domain.BalanceSnapshot{
		AccountID:     account.ID,
		UserAccountID: account.UserAccountID,
		UserID:        linkedItem.UserID,
		SnapshotAt:    timestamp,
		Source:        domain.SnapshotSourcePlaidWebhook,
		CurrencyCode:  "USD",
	}

	// Set current balance if available
	if current, ok := balances.GetCurrentOk(); ok && current != nil {
		currentFloat := float64(*current)
		snapshot.CurrentBalance = &currentFloat
	}

	// Set available balance if available
	if available, ok := balances.GetAvailableOk(); ok && available != nil {
		availableFloat := float64(*available)
		snapshot.AvailableBalance = &availableFloat
	}

	// Set currency code if available
	if currencyCode, ok := balances.GetIsoCurrencyCodeOk(); ok && currencyCode != nil {
		snapshot.CurrencyCode = *currencyCode
	}

	// Validate snapshot has at least one balance
	if snapshot.CurrentBalance == nil && snapshot.AvailableBalance == nil {
		s.logger.Warn("account has neither current nor available balance, skipping snapshot", "plaid_account_id", plaidAccountID)
		return nil
	}

	return snapshot
}

