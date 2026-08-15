package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
	"github.com/plaid/plaid-go/v23/plaid"
)

const (
	investmentTxnPageSize      = 500
	investmentTxnLookbackYears = 2
	investmentTxnOverlapDays   = 1
)

// InvestmentSyncService syncs investment holdings and transactions from Plaid's
// /investments/holdings/get and /investments/transactions/get endpoints.
//
// Holdings use a full-replace strategy (no cursor/delta). Transactions use
// offset-based pagination with an investments_last_synced_date for incremental sync.
//
// Issue #659: Plaid Investments — Holdings, Securities, Investment Transactions
type InvestmentSyncService interface {
	// ProcessWebhook routes HOLDINGS and INVESTMENTS_TRANSACTIONS webhooks.
	ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error

	// SyncHoldings fetches the complete holdings snapshot from Plaid and atomically
	// replaces all holdings for each account belonging to the linked item.
	SyncHoldings(ctx context.Context, linkedItem *domain.LinkedItem) (*domain.HoldingSyncResult, error)

	// SyncInvestmentTransactions fetches investment transactions using offset pagination.
	// Uses investments_last_synced_date - 1 day as the incremental start date.
	// Updates investments_last_synced_date only on full success.
	SyncInvestmentTransactions(ctx context.Context, linkedItem *domain.LinkedItem) (*domain.InvestmentTransactionSyncResult, error)

	// SetAnalytics sets the PostHog analytics client (setter injection to avoid circular dependencies).
	SetAnalytics(a phclient.Analytics)

	// SetCacheInvalidation wires cache generation bumping (setter injection, matching
	// TransactionSyncService's convention). Issue #1531 Task 10b-2a-ii: GetNetWorthTrend is a
	// cached read that folds in holdings/investment-transaction data, so a Plaid HOLDINGS or
	// INVESTMENTS_TRANSACTIONS webhook — routed through ProcessWebhook, which calls SyncHoldings/
	// SyncInvestmentTransactions below — must bump the synced user's cache generation once those
	// writes commit. Optional: nil is a safe no-op (every call site is nil-guarded).
	SetCacheInvalidation(gens *cache.Generations)
}

type investmentSyncService struct {
	logger                *slog.Logger
	analytics             phclient.Analytics
	webhookRepo           repository.WebhookRepository
	linkedItemRepo        repository.LinkedItemRepository
	accountRepo           repository.AccountRepository
	securityRepo          repository.SecurityRepository
	holdingRepo           repository.HoldingRepository
	investmentTxnRepo     repository.InvestmentTransactionRepository
	portfolioSnapshotRepo repository.PortfolioSnapshotRepository
	plaidService          PlaidService
	cacheGens             *cache.Generations
}

// SetAnalytics sets the PostHog analytics client.
func (s *investmentSyncService) SetAnalytics(a phclient.Analytics) {
	s.analytics = a
}

// SetCacheInvalidation wires cache generation bumping. See the interface method's doc comment.
func (s *investmentSyncService) SetCacheInvalidation(gens *cache.Generations) {
	s.cacheGens = gens
}

// bumpCacheForUser invalidates userID's cached analytics (e.g. GetNetWorthTrend) after a
// SyncHoldings/SyncInvestmentTransactions call has committed at least one write. Void by
// design — cache.Generations.Bump itself logs+counts its own Redis failures — matching every
// other generation bump in this codebase.
//
// Issue #1531 Blocker 2: detaches from ctx's own cancellation/deadline the same way
// transaction_sync_service.go's bumpCacheForSync and the HTTP mutation middleware do — ctx here
// is the River job's context (via ProcessWebhook), and a job timeout/worker-shutdown cancellation
// landing after the sync's writes have committed but before this bump runs must not cancel a
// bump for a write that already durably happened.
func (s *investmentSyncService) bumpCacheForUser(ctx context.Context, userID string) {
	if s.cacheGens == nil {
		return
	}
	bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	defer cancel()
	s.cacheGens.Bump(bumpCtx, cache.ScopeUser, userID, "investment_sync")
}

// NewInvestmentSyncService creates a new InvestmentSyncService.
func NewInvestmentSyncService(
	webhookRepo repository.WebhookRepository,
	linkedItemRepo repository.LinkedItemRepository,
	accountRepo repository.AccountRepository,
	securityRepo repository.SecurityRepository,
	holdingRepo repository.HoldingRepository,
	investmentTxnRepo repository.InvestmentTransactionRepository,
	portfolioSnapshotRepo repository.PortfolioSnapshotRepository,
	plaidService PlaidService,
	logger *slog.Logger,
) InvestmentSyncService {
	if logger == nil {
		logger = slog.Default()
	}
	return &investmentSyncService{
		logger:                logger,
		webhookRepo:           webhookRepo,
		linkedItemRepo:        linkedItemRepo,
		accountRepo:           accountRepo,
		securityRepo:          securityRepo,
		holdingRepo:           holdingRepo,
		investmentTxnRepo:     investmentTxnRepo,
		portfolioSnapshotRepo: portfolioSnapshotRepo,
		plaidService:          plaidService,
	}
}

// ProcessWebhook routes HOLDINGS and INVESTMENTS_TRANSACTIONS webhooks to the
// appropriate sync function, following the same pattern as TransactionSyncService.
func (s *investmentSyncService) ProcessWebhook(ctx context.Context, webhook *domain.WebhookEvent) error {
	if webhook == nil {
		return errors.New("webhook event is nil")
	}
	if webhook.ItemID == nil || *webhook.ItemID == "" {
		errorMsg := "webhook missing item_id — cannot process investment webhook"
		webhook.ProcessingError = &errorMsg
		if updateErr := s.webhookRepo.Update(ctx, webhook); updateErr != nil {
			s.logger.Error("failed to update webhook processing state", "webhook_id", webhook.ID, "error", updateErr)
		}
		return errors.New(errorMsg)
	}

	now := time.Now()
	webhook.ProcessingStartedAt = &now
	if err := s.webhookRepo.Update(ctx, webhook); err != nil {
		return fmt.Errorf("failed to mark webhook as processing: %w", err)
	}

	linkedItem, err := s.linkedItemRepo.GetByItemID(ctx, *webhook.ItemID)
	if err != nil {
		errorMsg := fmt.Sprintf("linked item not found for item_id %s: %v", *webhook.ItemID, err)
		webhook.ProcessingError = &errorMsg
		if updateErr := s.webhookRepo.Update(ctx, webhook); updateErr != nil {
			s.logger.Error("failed to update webhook processing state", "webhook_id", webhook.ID, "error", updateErr)
		}
		return fmt.Errorf("failed to get linked item: %w", err)
	}

	switch webhook.WebhookType {
	case "HOLDINGS":
		var holdingResult *domain.HoldingSyncResult
		holdingResult, err = s.SyncHoldings(ctx, linkedItem)
		if err == nil && holdingResult != nil && len(holdingResult.Errors) > 0 {
			// Partial failure: some accounts skipped due to unmapped securities.
			// Existing holdings are preserved but not updated — trigger retry.
			err = fmt.Errorf("holdings sync partial failure: %v", holdingResult.Errors)
		}
	case "INVESTMENTS_TRANSACTIONS":
		_, err = s.SyncInvestmentTransactions(ctx, linkedItem)
	default:
		return fmt.Errorf("unsupported investment webhook type: %s", webhook.WebhookType)
	}

	if err != nil {
		plaidErr := domain.ClassifyPlaidError(err)
		errorMsg := plaidErr.Error()
		webhook.ProcessingError = &errorMsg
		if plaidErr.ErrorType != domain.PlaidErrorPermanent {
			webhook.RetryCount++
		}
		if updateErr := s.webhookRepo.Update(ctx, webhook); updateErr != nil {
			s.logger.Error("failed to update webhook processing state", "webhook_id", webhook.ID, "error", updateErr)
		}
		if s.analytics != nil {
			s.analytics.Capture(linkedItem.UserID, phclient.EventInvestmentSyncFailed, map[string]interface{}{
				"error_type":   fmt.Sprintf("%d", plaidErr.ErrorType),
				"webhook_type": webhook.WebhookType,
			})
		}
		return fmt.Errorf("investment sync failed: %w", err)
	}

	completedAt := time.Now()
	webhook.Processed = true
	webhook.ProcessingCompletedAt = &completedAt
	if err := s.webhookRepo.Update(ctx, webhook); err != nil {
		return fmt.Errorf("failed to mark webhook as processed: %w", err)
	}

	return nil
}

// SyncHoldings fetches the complete holdings snapshot from Plaid and atomically
// replaces all holdings per account. Since Plaid returns a full snapshot (no delta),
// we delete existing holdings and insert fresh ones inside a single DB transaction.
func (s *investmentSyncService) SyncHoldings(ctx context.Context, linkedItem *domain.LinkedItem) (*domain.HoldingSyncResult, error) {
	// Issue #1531 Task 10b-2a-ii: bump the user's cache generation once, on the way out,
	// whenever at least one account's holdings were actually replaced below — regardless of
	// whether a LATER account in the loop fails. See transaction_sync_service.go's identical
	// defer for why "any commit, then bump" is required rather than bumping only on full success.
	var cacheBumpNeeded bool
	defer func() {
		if cacheBumpNeeded {
			s.bumpCacheForUser(ctx, linkedItem.UserID)
		}
	}()

	accessToken := linkedItem.AccessToken

	resp, err := s.plaidService.InvestmentsHoldingsGet(ctx, accessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch holdings from Plaid: %w", err)
	}

	// Upsert securities and build plaid_security_id → internal UUID map
	domainSecurities := mapPlaidSecuritiesToDomain(resp.Securities)
	securityMap, err := s.securityRepo.UpsertBatch(ctx, domainSecurities)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert securities: %w", err)
	}

	// Build plaid_account_id → internal account ID and user_account_id maps from our DB.
	// userAccountMap is used to set user_account_id on each holding at insert time
	// so the stable chain-expansion key is populated (Issue #1109).
	accounts, err := s.accountRepo.GetByLinkedItemID(ctx, linkedItem.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts for linked item: %w", err)
	}
	accountMap := make(map[string]string, len(accounts))
	holdingUserAccountMap := make(map[string]*string, len(accounts))
	for _, a := range accounts {
		accountMap[a.PlaidAccountID] = a.ID
		holdingUserAccountMap[a.PlaidAccountID] = a.UserAccountID
	}

	// Group Plaid holdings by plaid_account_id for efficient lookup
	holdingsByAccount := make(map[string][]plaid.Holding, len(accounts))
	for _, h := range resp.Holdings {
		holdingsByAccount[h.AccountId] = append(holdingsByAccount[h.AccountId], h)
	}

	result := &domain.HoldingSyncResult{
		SecuritiesUpserted: len(domainSecurities),
	}

	// For each internal account, replace holdings atomically
	for plaidAccountID, internalAccountID := range accountMap {
		plaidHoldings := holdingsByAccount[plaidAccountID] // empty if account was liquidated

		domainHoldings := make([]domain.Holding, 0, len(plaidHoldings))
		skippedAny := false
		for _, ph := range plaidHoldings {
			internalSecurityID, ok := securityMap[ph.SecurityId]
			if !ok {
				s.logger.Warn("security not in map, skipping account holdings replace",
					"plaid_security_id", ph.SecurityId,
					"plaid_account_id", plaidAccountID,
				)
				skippedAny = true
				break
			}

			h := domain.Holding{
				UserID:        linkedItem.UserID,
				AccountID:     internalAccountID,
				UserAccountID: holdingUserAccountMap[plaidAccountID], // stable chain-expansion key (Issue #1109)
				SecurityID:    internalSecurityID,
				Quantity:      ph.Quantity,
			}

			instValue := ph.InstitutionValue
			h.InstitutionValue = &instValue
			instPrice := ph.InstitutionPrice
			h.InstitutionPrice = &instPrice

			if cb := ph.CostBasis.Get(); cb != nil {
				val := *cb
				h.CostBasis = &val
			}

			domainHoldings = append(domainHoldings, h)
		}

		if skippedAny {
			result.Errors = append(result.Errors, fmt.Sprintf("account %s: skipped replace due to unmapped security", plaidAccountID))
			continue
		}

		if err := s.holdingRepo.ReplaceForAccount(ctx, internalAccountID, linkedItem.UserID, domainHoldings); err != nil {
			return result, fmt.Errorf("failed to replace holdings for account %s: %w", internalAccountID, err)
		}
		cacheBumpNeeded = true

		result.HoldingsReplaced += len(domainHoldings)
		result.AccountsProcessed++
	}

	s.logger.Info("holdings sync complete",
		"item_id", linkedItem.ID,
		"securities_upserted", result.SecuritiesUpserted,
		"holdings_replaced", result.HoldingsReplaced,
		"accounts_processed", result.AccountsProcessed,
	)

	if s.analytics != nil {
		s.analytics.Capture(linkedItem.UserID, phclient.EventInvestmentHoldingsSynced, map[string]interface{}{
			"securities_upserted": result.SecuritiesUpserted,
			"holdings_replaced":   result.HoldingsReplaced,
			"accounts_processed":  result.AccountsProcessed,
		})
	}

	// Issue #138: Upsert portfolio snapshot from committed holdings state.
	// Skip when accounts were skipped (partial sync must not become a day-change baseline).
	// Each per-item webhook recomputes the full-user aggregate via aggregate-at-write SQL,
	// so concurrent multi-institution syncs converge to the correct total.
	if len(result.Errors) == 0 && s.portfolioSnapshotRepo != nil {
		snapshotDate := time.Now().UTC().Truncate(24 * time.Hour)
		if _, snapErr := s.portfolioSnapshotRepo.Upsert(ctx, linkedItem.UserID, snapshotDate); snapErr != nil {
			s.logger.Warn("portfolio snapshot upsert failed (non-fatal)",
				"user_id", linkedItem.UserID,
				"error", snapErr,
			)
		}
	}

	return result, nil
}

// SyncInvestmentTransactions fetches investment transactions using offset-based pagination.
// On incremental sync, startDate = investments_last_synced_date - 1 day (safety overlap).
// On first sync, uses a 2-year lookback. Updates investments_last_synced_date only on full success.
func (s *investmentSyncService) SyncInvestmentTransactions(ctx context.Context, linkedItem *domain.LinkedItem) (*domain.InvestmentTransactionSyncResult, error) {
	// Issue #1531 Task 10b-2a-ii: same "any commit, then bump" defer as SyncHoldings above —
	// a page that commits transactions before a LATER page fails must still bump.
	var cacheBumpNeeded bool
	defer func() {
		if cacheBumpNeeded {
			s.bumpCacheForUser(ctx, linkedItem.UserID)
		}
	}()

	accessToken := linkedItem.AccessToken

	now := time.Now().UTC()
	endDate := now.Format("2006-01-02") // Today in YYYY-MM-DD format

	var startDate string
	if linkedItem.InvestmentsLastSyncedDate != nil {
		// Incremental sync: overlap by 1 day so we don't miss transactions posted
		// after the previous sync's cutoff (unique constraint ensures idempotency).
		startDate = linkedItem.InvestmentsLastSyncedDate.AddDate(0, 0, -investmentTxnOverlapDays).Format("2006-01-02")
		s.logger.Info("incremental investment transaction sync",
			"item_id", linkedItem.ID,
			"start_date", startDate,
		)
	} else {
		// First sync: 2-year lookback for full history
		startDate = now.AddDate(-investmentTxnLookbackYears, 0, 0).Format("2006-01-02")
		s.logger.Info("first investment transaction sync, using lookback",
			"item_id", linkedItem.ID,
			"start_date", startDate,
			"lookback_years", investmentTxnLookbackYears,
		)
	}

	// Build account maps for transaction → account ID and user_account_id resolution.
	// userAccountMap carries the stable user_account_id (survives Plaid re-links)
	// so investment transactions written here have the chain-expansion key set on
	// insert (mirrors the banking UpsertBatch approach — Issue #1109).
	accounts, err := s.accountRepo.GetByLinkedItemID(ctx, linkedItem.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts for linked item: %w", err)
	}
	accountMap := make(map[string]string, len(accounts))
	userAccountMap := make(map[string]*string, len(accounts))
	for _, a := range accounts {
		accountMap[a.PlaidAccountID] = a.ID
		userAccountMap[a.PlaidAccountID] = a.UserAccountID
	}

	result := &domain.InvestmentTransactionSyncResult{}

	var offset int32
	const pageSize int32 = investmentTxnPageSize

	for {
		resp, err := s.plaidService.InvestmentsTransactionsGet(ctx, accessToken, startDate, endDate, offset, pageSize)
		if err != nil {
			// Return partial results — caller can retry; date NOT updated so next retry covers same range
			return result, fmt.Errorf("failed to fetch investment transactions at offset %d: %w", offset, err)
		}

		result.PagesProcessed++

		// Upsert securities for this page, building plaid_security_id → UUID map
		securityMap := make(map[string]string)
		if len(resp.Securities) > 0 {
			domainSecurities := mapPlaidSecuritiesToDomain(resp.Securities)
			securityMap, err = s.securityRepo.UpsertBatch(ctx, domainSecurities)
			if err != nil {
				return result, fmt.Errorf("failed to upsert securities on page %d: %w", result.PagesProcessed, err)
			}
			result.SecuritiesUpserted += len(securityMap)
		}

		// Map and upsert investment transactions for this page
		domainTxns := mapPlaidInvestmentTransactions(resp.InvestmentTransactions, linkedItem.UserID, accountMap, userAccountMap, securityMap, s.logger)
		if len(domainTxns) > 0 {
			inserted, err := s.investmentTxnRepo.UpsertBatch(ctx, domainTxns)
			if err != nil {
				return result, fmt.Errorf("failed to upsert transactions on page %d: %w", result.PagesProcessed, err)
			}
			cacheBumpNeeded = true
			result.TransactionsInserted += inserted
			result.TransactionsSkipped += len(domainTxns) - inserted
		}

		s.logger.Info("processed investment transactions page",
			"page", result.PagesProcessed,
			"item_id", linkedItem.ID,
			"txns_on_page", len(resp.InvestmentTransactions),
			"total", resp.TotalInvestmentTransactions,
		)

		// Advance offset and check pagination termination
		offset += int32(len(resp.InvestmentTransactions))
		if offset >= resp.TotalInvestmentTransactions || len(resp.InvestmentTransactions) == 0 {
			break
		}
	}

	// Update investments_last_synced_date ONLY on full success (all pages processed)
	// Truncate now to midnight UTC (endDate already in this format)
	today := now.Truncate(24 * time.Hour)
	if err := s.linkedItemRepo.UpdateInvestmentsLastSyncedDate(ctx, linkedItem.ID, today); err != nil {
		// Non-fatal: data is stored; log warning and continue
		s.logger.Warn("failed to update investments_last_synced_date",
			"item_id", linkedItem.ID,
			"error", err,
		)
	}

	s.logger.Info("investment transactions sync complete",
		"item_id", linkedItem.ID,
		"pages", result.PagesProcessed,
		"inserted", result.TransactionsInserted,
		"skipped", result.TransactionsSkipped,
		"securities_upserted", result.SecuritiesUpserted,
	)

	if s.analytics != nil {
		s.analytics.Capture(linkedItem.UserID, phclient.EventInvestmentTransactionsSynced, map[string]interface{}{
			"pages_processed":       result.PagesProcessed,
			"transactions_inserted": result.TransactionsInserted,
			"transactions_skipped":  result.TransactionsSkipped,
		})
	}

	return result, nil
}

// ============================================================================
// Plaid → Domain Mapping Helpers
// ============================================================================

// mapPlaidSecuritiesToDomain converts Plaid SDK security objects to domain.Security.
func mapPlaidSecuritiesToDomain(plaidSecs []plaid.Security) []domain.Security {
	result := make([]domain.Security, 0, len(plaidSecs))
	for _, ps := range plaidSecs {
		sec := domain.Security{
			PlaidSecurityID: ps.SecurityId,
		}
		if n := ps.Name.Get(); n != nil {
			sec.Name = *n
		}
		sec.TickerSymbol = ps.TickerSymbol.Get()
		sec.Type = ps.Type.Get()
		sec.ISIN = ps.Isin.Get()
		sec.CUSIP = ps.Cusip.Get()
		sec.IsCashEquivalent = ps.IsCashEquivalent.Get()
		sec.ClosePrice = ps.ClosePrice.Get()
		sec.ClosePriceAsOf = ps.ClosePriceAsOf.Get()
		result = append(result, sec)
	}
	return result
}

// mapPlaidInvestmentTransactions converts Plaid investment transactions to domain types,
// resolving account IDs, user_account_ids, and security IDs using the provided maps.
// user_account_id is the stable chain-expansion key (Issue #1109): setting it here on
// insert ensures pre-relink rows remain visible after a Plaid re-link.
// Transactions for unmapped accounts are silently skipped (logged as warning).
func mapPlaidInvestmentTransactions(
	plaidTxns []plaid.InvestmentTransaction,
	userID string,
	accountMap map[string]string,
	userAccountMap map[string]*string,
	securityMap map[string]string,
	logger *slog.Logger,
) []domain.InvestmentTransaction {
	result := make([]domain.InvestmentTransaction, 0, len(plaidTxns))

	for _, pt := range plaidTxns {
		internalAccountID, ok := accountMap[pt.AccountId]
		if !ok {
			if logger != nil {
				logger.Warn("no internal account for investment transaction, skipping",
					"plaid_account_id", pt.AccountId,
				)
			}
			continue
		}

		date, err := time.Parse("2006-01-02", pt.Date)
		if err != nil {
			if logger != nil {
				logger.Warn("failed to parse investment transaction date, skipping",
					"investment_transaction_id", pt.InvestmentTransactionId,
					"date", pt.Date,
					"error", err,
				)
			}
			continue
		}

		txn := domain.InvestmentTransaction{
			UserID:                       userID,
			AccountID:                    internalAccountID,
			UserAccountID:                userAccountMap[pt.AccountId], // stable chain-expansion key (Issue #1109)
			PlaidInvestmentTransactionID: pt.InvestmentTransactionId,
			Type:                         string(pt.Type),
			Name:                         pt.Name,
			Amount:                       pt.Amount,
			Date:                         date,
		}

		// Resolve security ID (nil for cash transactions)
		if plaidSecID := pt.SecurityId.Get(); plaidSecID != nil && *plaidSecID != "" {
			if securityMap != nil {
				if internalSecID, ok := securityMap[*plaidSecID]; ok {
					txn.SecurityID = &internalSecID
					txn.PlaidSecurityID = *plaidSecID
				}
			}
		}

		// Optional fields
		if subtype := string(pt.Subtype); subtype != "" {
			txn.Subtype = &subtype
		}
		txn.Quantity = &pt.Quantity
		txn.Price = &pt.Price
		txn.Fees = pt.Fees.Get()

		result = append(result, txn)
	}

	return result
}
