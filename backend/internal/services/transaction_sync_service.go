package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/merchant"
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
// 3. Storing Added/Modified transactions, tombstoning Removed transactions
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
	// CRITICAL (#1570): the cursor is persisted ONCE, after the transaction-storage steps commit —
	// never per page. Advancing per page was a data-loss window, because Plaid does not re-deliver
	// rows the stored cursor has moved past and the worker discards the result on error.
	//
	// Process:
	// 1. Get plaintext access token (decrypted by repository layer on read)
	// 2. Loop: call PlaidService.TransactionsSync with cursor
	// 3. Aggregate Added/Modified/Removed from each page
	// 4. Continue until has_more=false
	// 5. Store the aggregated pages, THEN persist the furthest cursor reached
	//
	// Returns aggregated results from ALL pages fetched. On any error before the cursor write, the
	// stored cursor is left where the run started, so the next run re-fetches everything and
	// re-upserts it.
	//
	// Issue #260: Pagination is resumable on mid-sync errors. Issue #1570: resumability means
	// resuming from the last STORED position, not the last fetched one.
	SyncTransactions(
		ctx context.Context,
		linkedItem *domain.LinkedItem,
	) (*domain.TransactionSyncResult, error)

	// SetTransferCorrelationService wires in the TransferCorrelationService after construction.
	// Issue #972: Savings transfer automation — B4 wiring.
	SetTransferCorrelationService(svc TransferCorrelationService)

	// SetSavingsProgressServices wires in the savings contribution/progress services after construction.
	// All three parameters are optional (nil = skip that step). Issue #1300 G2.
	SetSavingsProgressServices(
		contribCleaner SyncContributionCleaner,
		progressUpdater SyncGoalProgressUpdater,
		categoryBackfiller SyncManagedCategoryBackfiller,
	)

	// SetSavingsDetectionServices wires in the D6 source-b proposed-contribution detection
	// services. All parameters are optional (nil = skip). Issue #1300 M18 RG1.
	SetSavingsDetectionServices(
		goalReader SyncContributionsBasisGoalReader,
		proposedUpserter SyncProposedContributionUpserter,
		inflowRepo repository.TransactionRepository,
	)

	// SetCacheInvalidation wires cache generation bumping so a successful SyncTransactions call
	// invalidates the synced user's (and, when resolvable, their household's) cached analytics.
	// Issue #1531 Task 10b-2: River jobs (PlaidSyncWorker, WebhookProcessingWorker) never went
	// through the HTTP mutation middleware, so a bank sync left every cached spending aggregate
	// serving pre-sync numbers until its namespace TTL — this closes that gap. Both parameters are
	// optional (nil = skip that bump entirely); this mirrors every other optional wiring setter in
	// this file (SetTransferCorrelationService et al.) so existing callers/tests that don't call
	// this setter are unaffected — see SyncTransactions' final bumpCacheForSync call, which is a
	// true no-op when cacheGens is nil.
	SetCacheInvalidation(gens *cache.Generations, hh SyncCacheHouseholdLookup)

	// SetAuditLogger wires the audit trail for Plaid-initiated account removals
	// (account.removed_by_provider). Issue #1569 Task 5: when a bank closes an account, Plaid stops
	// returning it and the sync soft-removes it — a user who later asks "why did this disappear?"
	// deserves an entry saying so, exactly as the user-initiated disconnect path writes one.
	// Optional (nil = skip the audit write), matching every other optional wiring setter here.
	SetAuditLogger(al AuditLogger)
}

// SyncCacheHouseholdLookup resolves the household (if any) a synced user belongs to, so
// bumpCacheForSync can also invalidate the household's cache scope. Same method shape as
// internal/huma/middleware.HouseholdLookup and internal/services.deletionService's HouseholdReader
// — redefined locally (not imported) because services must not depend on the huma layer, and
// because a narrower, purpose-scoped interface here documents exactly what this file needs.
// Satisfied structurally by *repository.householdRepository's GetHouseholdForUser.
type SyncCacheHouseholdLookup interface {
	GetHouseholdForUser(ctx context.Context, userID string) (household *domain.Household, isOwner bool, found bool, err error)
}

// =============================================================================
// Issue #1300 G2: narrow consumer-defined interfaces for savings progress wiring.
// Defined here (not in savings packages) to avoid import cycles.
// =============================================================================

// SyncContributionCleaner tombstones auto-derived contribution rows for a `removed` transaction,
// preventing phantom progress from orphaned rows (Codex C4). #1569 round 12 task 2: this is a
// tombstone (deleted_at/deleted_reason), not a hard delete — the row is retained.
type SyncContributionCleaner interface {
	DeleteManagedForTransaction(ctx context.Context, transactionID string) error
}

// SyncGoalProgressUpdater recomputes savings goal progress for a user after a sync
// so that balance/contribution-based goals reflect fresh transaction data.
type SyncGoalProgressUpdater interface {
	RecomputeUser(ctx context.Context, userID string) error
}

// SyncManagedCategoryBackfiller creates any missing managed savings categories across
// active budgets for active goals (idempotent self-healing, Codex C7).
type SyncManagedCategoryBackfiller interface {
	BackfillManagedCategories(ctx context.Context, userID string) error
}

// SyncContributionsBasisGoalReader fetches active savings goals that use
// progress_basis='contributions' so the sync can detect transfer-ins and
// create proposed contributions (M18 / D6 source-b).
type SyncContributionsBasisGoalReader interface {
	GetActiveByUserID(ctx context.Context, userID string) ([]*domain.SavingsGoal, error)
}

// SyncProposedContributionUpserter creates or updates a proposed contribution row
// linked to a detected transfer-in (M18 / D6 source-b).
type SyncProposedContributionUpserter interface {
	UpsertProposedFromTransaction(ctx context.Context, userID, goalID, transactionID string, magnitude float64) error
}

// transactionSyncService implements TransactionSyncService
type transactionSyncService struct {
	logger                    *slog.Logger
	webhookRepo               repository.WebhookRepository
	linkedItemRepo            repository.LinkedItemRepository
	transactionRepo           repository.TransactionRepository
	accountRepo               repository.AccountRepository         // Issue #261: For account mapping (Plaid account_id -> internal UUID)
	profileRepo               repository.ProfileRepository         // For fetching user subscription tier (historical depth)
	balanceSnapshotRepo       repository.BalanceSnapshotRepository // Issue #353: For storing balance snapshots
	merchantRepo              repository.MerchantRepo              // Issue #1163: Batch-upsert merchants before CreateBatch
	normalize                 func(string) string                  // Issue #957: merchant name normalizer
	plaidService              PlaidService
	subscriptionService       *SubscriptionService       // For tier-based historical depth limit
	ruleApplicationService    RuleApplicationService     // Issue #346: Apply categorization rules to new transactions
	budgetSyncService         BudgetSyncService          // Issue #494: Sync budget amounts after transaction sync
	transactionMonitorService TransactionMonitorService  // Issue #36: Auto-detect completed transfer suggestions
	transferCorrelationSvc    TransferCorrelationService // Issue #972: Correlate inflows against pending transfer suggestions
	// Issue #1300 G2: savings progress wiring (all optional; nil = skip)
	savingsContribCleaner   SyncContributionCleaner       // cleans managed contrib rows before txn removal
	savingsProgressUpdater  SyncGoalProgressUpdater       // recomputes goal progress after sync
	savingsCategoryBackfill SyncManagedCategoryBackfiller // backfills missing savings categories
	// Issue #1300 M18: proposed contribution detection (D6 source-b)
	savingsGoalReader       SyncContributionsBasisGoalReader // fetches contributions-basis goals
	savingsProposedUpserter SyncProposedContributionUpserter // upserts proposed rows from detected inflows
	savingsInflowRepo       repository.TransactionRepository // shared — GetInflowTransactionsForAccount
	// Issue #1531 Task 10b-2: cache invalidation wiring (both optional; nil = skip)
	cacheGens      *cache.Generations       // mints/bumps generation tokens; nil when cache disabled/not wired
	cacheHousehold SyncCacheHouseholdLookup // resolves a synced user's household; nil = user-scope bump only
	// Issue #1569 Task 5: records account.removed_by_provider when a bank closes an account and
	// Plaid stops returning it. Optional (nil = no audit entry, same nil-safe shape as every other
	// setter-injected dependency here); wired by SetAuditLogger.
	auditLogger AuditLogger
	analytics   phclient.ProductAnalytics
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
	budgetSyncService BudgetSyncService, // Issue #494: Sync budget amounts
	transactionMonitorService TransactionMonitorService, // Issue #36: Auto-detect completed transfers
	merchantRepo repository.MerchantRepo, // Issue #1163: Merchant batch-upsert
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
		normalize:                 merchant.Normalize,
		plaidService:              plaidService,
		subscriptionService:       subscriptionService,
		ruleApplicationService:    ruleApplicationService,
		budgetSyncService:         budgetSyncService,
		transactionMonitorService: transactionMonitorService,
	}
}

// SetTransferCorrelationService wires in the TransferCorrelationService.
// Call after NewTransactionSyncService before the server starts serving requests.
// Issue #972: Savings transfer automation — B4 wiring.
func (s *transactionSyncService) SetTransferCorrelationService(svc TransferCorrelationService) {
	s.transferCorrelationSvc = svc
}

// SetSavingsProgressServices wires in the savings progress services.
// All three parameters are optional (nil = skip that step). Issue #1300 G2.
func (s *transactionSyncService) SetSavingsProgressServices(
	contribCleaner SyncContributionCleaner,
	progressUpdater SyncGoalProgressUpdater,
	categoryBackfiller SyncManagedCategoryBackfiller,
) {
	s.savingsContribCleaner = contribCleaner
	s.savingsProgressUpdater = progressUpdater
	s.savingsCategoryBackfill = categoryBackfiller
}

// SetSavingsDetectionServices wires in D6 source-b proposed-contribution detection.
// All parameters are optional (nil = skip). Issue #1300 M18 RG1.
func (s *transactionSyncService) SetSavingsDetectionServices(
	goalReader SyncContributionsBasisGoalReader,
	proposedUpserter SyncProposedContributionUpserter,
	inflowRepo repository.TransactionRepository,
) {
	s.savingsGoalReader = goalReader
	s.savingsProposedUpserter = proposedUpserter
	s.savingsInflowRepo = inflowRepo
}

// SetCacheInvalidation wires cache generation bumping. See the interface method's doc comment
// for why this exists and why both parameters are optional. Issue #1531 Task 10b-2.
func (s *transactionSyncService) SetCacheInvalidation(gens *cache.Generations, hh SyncCacheHouseholdLookup) {
	s.cacheGens = gens
	s.cacheHousehold = hh
}

// SetAuditLogger wires the audit trail for Plaid-initiated account removals. See the interface
// method's doc comment. Issue #1569 Task 5.
func (s *transactionSyncService) SetAuditLogger(al AuditLogger) {
	s.auditLogger = al
}

func (s *transactionSyncService) SetProductAnalytics(analytics phclient.ProductAnalytics) {
	s.analytics = analytics
}

// SetTransactionSyncProductAnalytics wires the optional typed completion producer without
// widening the long-standing TransactionSyncService interface implemented by worker test doubles.
func SetTransactionSyncProductAnalytics(service TransactionSyncService, analytics phclient.ProductAnalytics) {
	if setter, ok := service.(interface {
		SetProductAnalytics(phclient.ProductAnalytics)
	}); ok {
		setter.SetProductAnalytics(analytics)
	}
}

// bumpCacheForSync invalidates userID's cached analytics (and, when resolvable, their
// household's). ⚠ CORRECTED (#1537 round 16): this used to say "after a SyncTransactions call has
// fully committed ... at the very end of the success path". That is no longer true — SyncTransactions
// now arms a deferred bump BEFORE each storage step, so this is also called after PARTIAL failures,
// which is the whole point of that change. Still called at most once per sync — never per page/batch, since a bump's cost is a single
// Redis write PER SCOPE (two when a household resolves, one otherwise — corrected round 17)
// regardless of how much data changed, and bumping per page would multiply that
// cost by transaction volume for no correctness gain (a single bump after all pages/storage have
// committed already invalidates everything written during this sync).
//
// Void by design, matching every other generation bump in this codebase (cache.Generations.Bump
// itself, the HTTP mutation middleware, the GDPR deletion purge): a bump failure must NEVER fail
// an already-committed sync. cache.Generations.Bump already logs+counts its own Redis failures;
// this method additionally logs (but does not fail on) a household LOOKUP failure, since that
// failure happens in this method, not inside Bump.
//
// This is the single call site that closes Issue #1531 Task 10b-2's stale-data gap: every
// caller of SyncTransactions (PlaidSyncWorker.Work, TransactionSyncService.ProcessWebhook — and
// therefore WebhookProcessingWorker's TRANSACTIONS route) is a thin wrapper that does not
// duplicate transaction storage logic, so bumping here, once, covers all of them without a
// second bump call anywhere else.
// bumpCacheTimeout bounds the detached post-sync bump work, matching cache_invalidation.go's
// identical constant and reasoning (Issue #1531 Blocker 2 — see that file's package doc comment).
const bumpCacheTimeout = 5 * time.Second

// syncHouseholdBumpAttempts/syncHouseholdBumpBackoff mirror cache_invalidation.go's identical
// constants — see that file's householdBumpAttempts doc comment (#1569 round 11 item 3).
const (
	syncHouseholdBumpAttempts = 2
	syncHouseholdBumpBackoff  = 100 * time.Millisecond
)

func (s *transactionSyncService) bumpCacheForSync(ctx context.Context, userID string) {
	if s.cacheGens == nil {
		return
	}

	// Issue #1531 Blocker 2: this is called from a defer at the end of SyncTransactions, using
	// the SAME ctx the whole sync ran under (a River job's context, per plaid_sync.go /
	// webhook_processing.go). A job timeout or worker shutdown-triggered cancellation landing in
	// this narrow window — AFTER the sync's DB writes have already committed, but before this
	// bump runs — would cancel the bump for a write that has already durably happened, exactly
	// the class of bug Blocker 2 closes in the HTTP mutation middleware. Detach the same way:
	// context.WithoutCancel + an explicit bounded WithTimeout (WithoutCancel strips the parent
	// deadline too), matching internal/middleware/rate_limit_fallback.go, internal/health/check.go,
	// and internal/huma/middleware/cache_invalidation.go.
	bumpCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bumpCacheTimeout)
	defer cancel()

	// #1569 round 11 item 3: household FIRST, with retry, then the user-scope bump — see
	// cache_invalidation.go's identical reordering for the full reasoning. In short: the ORIGINAL
	// "user, then household" order put the higher-blast-radius bump (another household member
	// served stale money) behind a lookup that can independently fail, and a bare Bump's own
	// silent Redis SET failure has the identical effect regardless of order — hence BOTH the
	// reorder AND the switch to BumpWithRetry for the household half.
	if s.cacheHousehold != nil {
		household, _, found, err := s.cacheHousehold.GetHouseholdForUser(bumpCtx, userID)
		if err != nil {
			// Void by design: the sync already committed; a household lookup failure must never
			// retroactively fail it. The user-scope bump below still runs regardless.
			s.logger.Error("cache invalidation: household lookup failed after transaction sync; "+
				"household-scoped cache may serve stale data until its namespace TTL",
				"user_id", userID, "error", err)
		} else if found && household != nil {
			s.cacheGens.BumpWithRetry(bumpCtx, cache.ScopeHousehold, household.ID, "plaid_sync",
				syncHouseholdBumpAttempts, syncHouseholdBumpBackoff)
		}
	}

	s.cacheGens.Bump(bumpCtx, cache.ScopeUser, userID, "plaid_sync")
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
	// This handles: decryption, pagination, storage, and the single post-storage cursor advance
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
	syncKind := financialDataSyncKind(cursor)

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
	//
	// #1570 (closed under #1569): pendingCursor is the furthest REAL cursor Plaid hands us during
	// this run. It is held here and persisted exactly once, after the storage steps below commit —
	// see the storage-before-cursor contract documented after this loop. It starts empty, meaning
	// "nothing earned yet, leave the stored cursor alone".
	pendingCursor := ""
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
			// Return partial results with error.
			//
			// #1570: the cursor has NOT been advanced for the pages that already succeeded — it is
			// written once, after storage, at the end of this function. So the worker discarding
			// `result` here loses nothing durable: the next run re-fetches from the last
			// known-good position and re-upserts.
			return result, fmt.Errorf("sync failed on page %d: %w", pageCount, err)
		}

		// Aggregate results from this page
		result.Added = append(result.Added, resp.Added...)
		result.Modified = append(result.Modified, resp.Modified...)
		result.Removed = append(result.Removed, resp.Removed...)

		// Advance the cursor IN MEMORY only (#1570). `cursor` is what the NEXT request must carry;
		// `pendingCursor` is what we will durably write, once, if and only if storage commits. This
		// used to call UpdateCursor per page for resumability, which was the data-loss window: the
		// storage steps run after this loop, so a per-page write made every failure in between
		// permanent — Plaid does not re-deliver rows the stored cursor has moved past.
		//
		// Issue #1537: a BLANK next_cursor must never become the persisted cursor. An empty stored
		// cursor sends the next run down the no-cursor branch above, which applies initial-sync
		// historical options and asks Plaid for the whole configured window again — so one blank
		// write turns every subsequent incremental sync into a full re-sync through CreateBatch.
		// Leaving pendingCursor at the last real value keeps the correct resume point.
		//
		// This guard belongs HERE and not inside UpdateCursor: the PlaidErrorInvalidCursor branch
		// above writes "" on purpose, precisely to force that full re-sync when Plaid tells us the
		// stored cursor is no longer valid.
		//
		// Round 4 of this branch added the identical check to workers/plaid_sync_provider.go, whose
		// NewPlaidSyncProvider has no non-test caller — so the fix landed in dead code while this,
		// the live path, kept the bug. Verified by TestSyncTransactions_BlankNextCursor_
		// DoesNotOverwriteStoredCursor, which failed before this guard existed.
		newCursor := resp.GetNextCursor()
		if newCursor == "" {
			// #1537 round 10: an empty next_cursor is only SAFE to ignore on the last page.
			//
			// If Plaid also says has_more=true there is no safe continuation: this loop is `for hasMore`
			// and the cursor cannot advance, so returning here is the difference between failing a job
			// and re-issuing the IDENTICAL request forever — the loop has no page cap, and its only
			// non-error exit is has_more going false. Round 9 added the blank-cursor guard without this
			// branch and so converted bad data into a potential hang, which is the worse failure. Both
			// reviewers found it independently; pinned by
			// TestSyncTransactions_BlankNextCursorWithHasMore_FailsClosedWithoutLooping, whose
			// load-bearing assertion is the Plaid CALL COUNT rather than the error.
			//
			// Plaid's contract is that has_more=true must be followed by a request carrying
			// next_cursor; an empty one means the data is not yet available. Failing closed lets the
			// job retry with the cursor we still hold, which is exactly what we want.
			if resp.GetHasMore() {
				// #1537 round 11: the round-10 version returned an error here unconditionally, which
				// LOST DATA on any multi-page sync, because UpdateCursor then ran per page while
				// storage happened after this loop and the worker discards `result` on error. #1570
				// removed the per-page write, so an error here would no longer lose rows — but
				// breaking is still strictly better than erroring: it STORES pages 1..N instead of
				// throwing them away and making the next run re-fetch all of them.
				//
				// What we do next depends on whether we got anything this run:
				if pageCount > 1 {
					// Progress made: pages 1..N are ALL kept — page N's rows were appended above,
					// BEFORE this guard. Page N handed us no cursor, so pendingCursor still holds
					// page N-1's; once storage commits we persist that, and the next sync re-fetches
					// page N and re-runs the whole post-pagination tail against it.
					//
					// #1537 T93(a): an earlier version of this comment claimed "no loss and no
					// duplicate rows". Only the first half is right, and only for TRANSACTION rows.
					// Since #1570 this note is load-bearing rather than incidental: re-delivery is
					// now the normal consequence of ANY sync failure, not just of this branch.
					// What re-fetching page N is actually idempotent in:
					//   - added: transactionRepo.CreateBatch is ON CONFLICT (source,
					//     external_transaction_id_blind_idx, user_id) DO UPDATE — re-upsert, no dupe.
					//   - modified: Update keyed on the existing row's id — same final contents, but
					//     it rewrites updated_at. Since #1569 a row the lookup cannot find is
					//     UPSERTED through CreateBatch rather than dropped, so re-delivery of a
					//     modification for a never-stored row now converges instead of losing it.
					//   - removed: TombstoneByExternalID reports ErrTransactionNotFound for an
					//     already-tombstoned row, which the caller treats as success, and its
					//     `deleted_at IS NULL` guard keeps the recorded deleted_at/deleted_by at the
					//     first removal rather than restamping them on every re-delivery.
					// What it is NOT idempotent in (all verified, all downstream of storage):
					//   - balance_snapshots: balanceSnapshotRepository.Create is a plain INSERT with
					//     NO ON CONFLICT, and snapshot_at is a fresh time.Now() per run, so a re-run
					//     more than createBalanceSnapshots' 5-minute dedup window later ADDS a row
					//     per account. Migration 000017 deliberately dropped the old
					//     UNIQUE(account_id, snapshot_date) to allow many snapshots per day, and the
					//     read paths take only the latest per account per day — so this inflates row
					//     count, not reported balances.
					//   - Plaid sync metrics: the worker's PlaidSyncTxns counters
					//     (queue/jobs/plaid_sync.go, the observability block after a successful sync)
					//     add page N's added/modified/removed a SECOND time.
					//   - boost rules: RecalculateBudgetAmounts below re-emits budget_updated, and
					//     boostExecutionService.ProcessEventTrigger has no cooldown or dedup — every
					//     matching rule re-executes, writing a new boost_executions row, bumping
					//     IncrementExecutionCount/UpdateLastSuccessful, and (for transfer-suggestion
					//     rules) creating a NEW pending suggestion with a fresh uuid. Note this is
					//     not specific to this branch — every sync run does it — but the extra run
					//     this branch causes is an extra round of it.
					//
					// Net: transactions are safe to re-deliver; nothing else on the tail is.
					// Not an error; the item is simply not fully caught up between incremental syncs.
					s.logger.Warn("plaid returned has_more=true with an empty next_cursor; stopping "+
						"pagination and keeping every page fetched this run — the cursor lags one "+
						"page, so the last is re-fetched and upserted on the next sync",
						"item_id", linkedItem.ID,
						"page", pageCount,
						"pages_kept", pageCount,
					)
					break
				}
				// No progress: the very FIRST page came back malformed, so there is nothing to keep.
				// Fail closed so the job retries and, if it persists, dead-letters visibly — returning
				// success here would make the job report "done" having synced nothing, forever, which
				// is a silent stall and worse for an operator than a loud failure.
				return result, fmt.Errorf(
					"plaid returned has_more=true with an empty next_cursor on the first page for "+
						"item %s: cannot advance and nothing was fetched", linkedItem.ID)
			}
			s.logger.Warn("plaid returned an empty next_cursor on the final page; keeping the "+
				"existing cursor rather than persisting a blank, which would force a full "+
				"historical re-sync on the next run",
				"item_id", linkedItem.ID,
				"page", pageCount,
			)
		} else {
			cursor = newCursor
			pendingCursor = newCursor
		}

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

	// Pagination finished. NOTE this is reached on TWO paths, and the difference matters: either
	// has_more went false (every page fetched), or #1537 round 11's guard broke out early because
	// Plaid promised more pages while sending no cursor to fetch them with. In the second case the
	// pages already fetched ARE stored and the cursor persisted below is consistent with them, so
	// the next sync resumes correctly — but this is NOT a complete sync, and an earlier version of
	// this comment said "All pages fetched successfully", which my own break made false.
	//
	// ── #1570 STORAGE-BEFORE-CURSOR CONTRACT (the guarantee; read before moving anything) ────────
	// Nothing has been written to linked_items.cursor yet. `pendingCursor` holds the furthest real
	// cursor Plaid returned this run, and it is persisted by the SINGLE UpdateCursor call after
	// Step 8, only once every transaction-storage step has committed. Consequences:
	//   - Any error return before that call leaves the stored cursor exactly where the run started,
	//     so the next run re-fetches every page from the last known-good position. No page can be
	//     stranded behind an advanced cursor any more.
	//   - Re-delivery is therefore the NORMAL consequence of a failed sync, not a rare edge. See
	//     the T93(a) note in the pagination loop for exactly what re-delivery is idempotent in:
	//     transaction rows are (CreateBatch upserts); balance snapshots, the worker's sync metrics
	//     and boost-rule executions are not.
	//   - If the UpdateCursor call itself fails, the rows are already durable and the next run
	//     re-delivers and re-upserts them. That is the safe direction of the two.
	//   - NOT atomic, and does not need to be: Steps 6/7/8 are three independent non-transactional
	//     writes (Modified is one Update per row). A partial failure therefore leaves some rows
	//     durable AND the cursor unmoved — the over-delivery direction, which CreateBatch/Update
	//     absorb by upserting. The guarantee is one-directional: the cursor advances only if EVERY
	//     storage step returned nil, never the reverse.
	//   - RESIDUAL, and the reason this is not "zero rows can ever be missed": the storage steps
	//     still have log-and-continue paths that skip a row and return nil, so the cursor can
	//     advance past a row that was never written. FOUR REMAIN — the enumeration below was
	//     wrong in its first #1569 revision, which claimed two and named only
	//     processAddedTransactions; processModifiedTransactions has its own pair, structurally
	//     identical, and missing them is how residual (3) survived a round:
	//       1. processAddedTransactions — a row whose Plaid account_id is not in accountMap is
	//          Warn'd and skipped (the `!found` skip). buildAccountMaps now loads ALL statuses
	//          (GetByLinkedItemIDIncludingRetained), not just 'active', so this fires only if
	//          Plaid names a plaid_account_id with no accounts row AT ALL for this
	//          linked_item_id — not merely a non-active one;
	//       2. processAddedTransactions — if that skipped every row in the page, the function
	//          returns nil early; same narrowing as (1), for the same reason;
	//       3. processModifiedTransactions — a row that is BOTH unmappable and not stored. Same
	//          narrowing as (1): there is no account to insert under and no row to update. The
	//          unmappable-but-STORED case is no longer a skip at all — the stored row supplies the
	//          identity and the edit is applied (see UNMAPPABLE ACCOUNTS on that function);
	//       4. processModifiedTransactions — MapPlaidTransactionBase rejects the payload itself
	//          (malformed provider data). Deliberately NOT an error return: re-delivery cannot fix
	//          a payload, so aborting would wedge this item's sync permanently.
	//     Every one fails toward RETAINING (an un-imported row, or a stored row left unchanged)
	//     rather than destroying, which is the safe side of #1569's governing principle.
	//     processRemovedTransactions also skips a `removed` entry whose transaction_id is nil; that
	//     is not in this list because a nil id identifies no row, so there is nothing to miss.
	//
	//     TWO OTHERS WERE CLOSED BY #1569 TASK 7 and are recorded here because this comment is
	//     where the next reader will look for them:
	//       (3) processModifiedTransactions used to DROP a modification whose row GetByExternalID
	//           could not find. It now UPSERTS it through CreateBatch, and a lookup error that is
	//           NOT ErrTransactionNotFound aborts instead of being absorbed. See the MISSING ROWS
	//           note on that function for why an upsert cannot resurrect a tombstone.
	//       (4) processRemovedTransactions used to Warn-and-continue on a genuine
	//           DeleteByExternalID database error while still returning nil, so the cursor
	//           advanced past a removal that was never applied and Plaid never re-delivered it.
	//           The hard delete is gone entirely — the path tombstones now — and a real failure
	//           returns an error, leaving the cursor where the run started. Only
	//           ErrTransactionNotFound (nothing live to tombstone) is still treated as success,
	//           because re-delivery makes that the normal case rather than an exception.
	// The whole run's pages are held in memory until storage — unchanged by #1570, since the loop
	// already accumulated every page into `result`; only the cursor write moved.
	// ─────────────────────────────────────────────────────────────────────────────────────────────
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
	// Store Added/Modified transactions, tombstone Removed transactions
	// ========================================================================

	// Issue #1531 Task 10b-2a-ii / #1537 T92: bump the cache on the way out (via defer) whenever
	// a storage step was ATTEMPTED, regardless of whether it — or any later step — failed.
	//
	// What the deferred bump guarantees, precisely: if control reached the `if len(result.X) > 0`
	// guard of ANY of the three storage steps below, the user's (and, where resolvable, their
	// household's) cache generation is ATTEMPTED on every exit path from here on, error or success.
	// "Attempted", not "guaranteed": generation.Bump swallows a failed Redis SET — it logs an error
	// and records a metric, but returns nothing, so the caller cannot know, and a household lookup failure leaves the household generation alone — so a bump can be
	// requested and not happen. This closes the code path, not the infrastructure.
	// It does NOT guarantee a bump for the non-fatal post-storage steps, which are reached only
	// after cacheBumpNeeded is already true. (This used to also except "the pagination loop's
	// UpdateCursor calls"; #1570 deleted those — the single cursor write now happens after the
	// storage steps, inside the region the defer already covers, and still touches no cached-read
	// table.)
	//
	// The flag is armed BEFORE each write is attempted, not after it succeeds (#1537 T92). It used
	// to be armed after: processModifiedTransactions commits one transactionRepo.Update PER ROW
	// and returns on the first failure, so a MODIFIED-ONLY sync (Plaid DEFAULT_UPDATE's common
	// shape, with result.Added empty and therefore no earlier step to arm the flag) that committed
	// 5 of 10 updates returned before arming it — five durable rows in `transactions`, a
	// cached-read table, left invisible to cached analytics for the full namespace TTL. Arming
	// first is unambiguously the fail-safe direction: a spurious bump costs exactly one cache
	// generation, while a missed bump serves stale financial data. ⚠ Not free, though: arming before
	// the write means a batch whose rows are all skipped (already-deleted removals, missing modified
	// rows) bumps anyway, and each bump is a fresh token plus a Redis write plus metrics — and a
	// second write for the household scope. The cost is cache misses and regeneration, not just an
	// integer. It is still the right trade; it is not a no-op.
	var cacheBumpNeeded bool
	defer func() {
		if cacheBumpNeeded {
			s.bumpCacheForSync(ctx, linkedItem.UserID)
		}
	}()

	// Step 5: Build account maps for transaction mapping (O(1) lookups)
	// Issue #171: Also builds userAccountMap for stable UserAccountID reference
	accountMap, userAccountMap, err := s.buildAccountMaps(ctx, linkedItem.ID, linkedItem.UserID)
	if err != nil {
		// #1570: this returns an ERROR (an earlier comment here claimed "successful sync result",
		// which was never true). It is also the failure that used to lose every page of a
		// multi-page sync; the cursor has not moved, so the next run re-fetches all of them.
		s.logger.Error("failed to build account maps", "item_id", linkedItem.ID, "error", err)
		return result, fmt.Errorf("failed to build account maps: %w", err)
	}

	// Step 6: Process Added transactions (create batch)
	// Issue #171: Pass userAccountMap for stable UserAccountID propagation
	if len(result.Added) > 0 {
		// #1537 T92: arm BEFORE the write, not after it succeeds — see the defer above.
		cacheBumpNeeded = true
		if err := s.processAddedTransactions(ctx, result.Added, linkedItem, accountMap, userAccountMap); err != nil {
			s.logger.Error("failed to process added transactions", "item_id", linkedItem.ID, "error", err)
			return result, fmt.Errorf("failed to process added transactions: %w", err)
		}
	}

	// Step 7: Process Modified transactions (update with user_notes preservation)
	// Issue #171: Pass userAccountMap for stable UserAccountID propagation
	if len(result.Modified) > 0 {
		// #1537 T92: arm BEFORE the write, not after it succeeds — see the defer above.
		cacheBumpNeeded = true
		if err := s.processModifiedTransactions(ctx, result.Modified, linkedItem, accountMap, userAccountMap); err != nil {
			s.logger.Error("failed to process modified transactions", "item_id", linkedItem.ID, "error", err)
			return result, fmt.Errorf("failed to process modified transactions: %w", err)
		}
	}

	// Step 8: Process Removed transactions (tombstone)
	if len(result.Removed) > 0 {
		// #1537 T92: arm BEFORE the write, not after it succeeds — see the defer above.
		cacheBumpNeeded = true
		if err := s.processRemovedTransactions(ctx, result.Removed, linkedItem); err != nil {
			s.logger.Error("failed to process removed transactions", "item_id", linkedItem.ID, "error", err)
			return result, fmt.Errorf("failed to process removed transactions: %w", err)
		}
	}

	s.logger.Info("transaction storage complete", "item_id", linkedItem.ID)

	// #1570: THE cursor advance. Every transaction-storage step above has committed, so it is now
	// safe to tell Plaid we have consumed these pages. This is deliberately placed before the
	// non-fatal tail (balance snapshots, savings, budget recalc, correlation): those steps cannot
	// fail the sync, so blocking the cursor on them would only cause pointless re-delivery.
	// pendingCursor is empty when Plaid returned no real cursor this run (see the #1537 blank-cursor
	// guard) — in that case the stored cursor is left exactly as it was.
	if pendingCursor != "" {
		if err := s.linkedItemRepo.UpdateCursor(ctx, linkedItem.ID, pendingCursor); err != nil {
			s.logger.Error("failed to advance cursor after storage", "item_id", linkedItem.ID, "error", err)
			return result, fmt.Errorf("failed to update cursor after storage: %w", err)
		}
	}

	// ========================================================================
	// Issue #353: Create Balance Snapshots
	// Capture current account balances after transaction sync completes
	// ========================================================================

	// Create balance snapshots for all accounts (non-blocking)
	// #1537 round 16: armed BEFORE the snapshot write too. createBalanceSnapshots writes
	// balance_snapshots — one of the twelve cached-read tables — and with all three
	// transaction lists empty none of the guards above has armed the flag, so a panic
	// between here and the unconditional arm below would skip invalidation entirely.
	cacheBumpNeeded = true
	if err := s.createBalanceSnapshots(ctx, linkedItem, accessToken); err != nil {
		s.logger.Warn("failed to create balance snapshots", "item_id", linkedItem.ID, "error", err)
		// Don't fail the entire sync - snapshots are non-critical
	}

	// ========================================================================
	// Issue #1300 G2 / M18 (RG1): Savings progress reconciliation (ordered BEFORE budget recalc)
	// Ordering: detect proposed contributions (D6 source-b) → RecomputeUser
	// → BackfillManagedCategories → RecalculateBudgetAmounts → CorrelateForUser.
	// All steps non-fatal.
	// ========================================================================

	// Step 8a (M18 / D6 source-b): For each active contributions-basis goal, detect recent
	// transfer-ins to the linked account and upsert proposed contribution rows.
	// Proposed rows don't count toward progress until the user confirms them.
	if s.savingsGoalReader != nil && s.savingsProposedUpserter != nil && s.savingsInflowRepo != nil {
		if err := s.detectAndUpsertProposedContributions(ctx, linkedItem.UserID); err != nil {
			s.logger.Warn("savings proposed-contribution detection failed (non-fatal)",
				"user_id", linkedItem.UserID, "error", err)
		}
	}

	// Recompute all active savings goals for this user (balance/contributions/account_percentage).
	if s.savingsProgressUpdater != nil {
		if err := s.savingsProgressUpdater.RecomputeUser(ctx, linkedItem.UserID); err != nil {
			s.logger.Warn("savings RecomputeUser failed (non-fatal)", "user_id", linkedItem.UserID, "error", err)
		}
	}

	// Backfill any missing managed savings categories in active budgets (self-healing, Codex C7).
	if s.savingsCategoryBackfill != nil {
		if err := s.savingsCategoryBackfill.BackfillManagedCategories(ctx, linkedItem.UserID); err != nil {
			s.logger.Warn("savings BackfillManagedCategories failed (non-fatal)", "user_id", linkedItem.UserID, "error", err)
		}
	}

	// Issue #494: Sync budget amounts (non-blocking)
	if s.budgetSyncService != nil {
		if err := s.budgetSyncService.RecalculateBudgetAmounts(ctx, linkedItem.UserID); err != nil {
			s.logger.Warn("failed to sync budget amounts", "user_id", linkedItem.UserID, "error", err)
		}
	}

	// Issue #972: Correlate inflows against pending transfer suggestions (non-fatal)
	if s.transferCorrelationSvc != nil {
		if err := s.transferCorrelationSvc.CorrelateForUser(ctx, linkedItem.UserID); err != nil {
			s.logger.Error("transfer correlation failed", "user_id", linkedItem.UserID, "error", err)
			// non-fatal: don't fail the sync if correlation errors
		}
	}

	// Issue #1531 Task 10b-2 / 10b-2a-ii: the deferred bump above fires here on the full-success
	// path too — set unconditionally so a fully successful sync still bumps even when Added,
	// Modified, and Removed were ALL empty (matching the original always-bump-on-success
	// behavior; a no-op sync is still worth invalidating since transfer correlation, savings
	// progress, and budget sync above may have written even with zero transaction changes).
	cacheBumpNeeded = true

	if s.analytics != nil {
		s.analytics.CaptureEvent(ctx, linkedItem.UserID, phclient.FinancialDataSyncCompletedPayload{
			SyncKind: syncKind,
		})
	}

	return result, nil
}

func financialDataSyncKind(preSyncCursor string) phclient.FinancialDataSyncCompletedSyncKind {
	if preSyncCursor == "" {
		return phclient.FinancialDataSyncCompletedSyncKindInitial
	}
	return phclient.FinancialDataSyncCompletedSyncKindRefresh
}

// ============================================================================
// Issue #261: Transaction Storage Logic - Helper Methods
// ============================================================================

// upsertMerchants deduplicates and batch-upserts merchant names, returning a map
// of merchant_name -> merchant UUID. Returns nil if upserting fails or no merchants found.
// Non-blocking: logs warnings but doesn't fail the sync.
func (s *transactionSyncService) upsertMerchants(ctx context.Context, transactions []domain.Transaction) map[string]string {
	if s.merchantRepo == nil {
		return nil
	}

	// Deduplicate merchant names, skipping whitespace-only names (G33, issue #957).
	nameSet := make(map[string]struct{})
	for _, txn := range transactions {
		if txn.MerchantName != nil && strings.TrimSpace(*txn.MerchantName) != "" {
			nameSet[*txn.MerchantName] = struct{}{}
		}
	}

	if len(nameSet) == 0 {
		return nil
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}

	merchantIDs, err := s.merchantRepo.GetOrCreateMerchantsWithNormalize(ctx, names, s.normalize)
	if err != nil {
		s.logger.Warn("failed to upsert merchants, continuing without merchant_id", "err", err)
		return nil
	}

	return merchantIDs
}

// buildAccountMaps builds both accountMap and userAccountMap for transaction mapping.
// Issue #171: userAccountMap provides stable UserAccountID that survives re-links.
// ownerUserID is the linked item's owner (Issue #1615): any account whose UserID does not
// match it is skipped and logged rather than mapped, mirroring the investment sync guard.
// Returns:
//   - accountMap: Plaid account_id -> Internal account UUID
//   - userAccountMap: Plaid account_id -> UserAccount UUID (stable reference)
func (s *transactionSyncService) buildAccountMaps(ctx context.Context, linkedItemID string, ownerUserID string) (map[string]string, map[string]string, error) {
	// Load ALL accounts for this linked item, regardless of status (Issue #1569 Task 1b).
	// GetByLinkedItemID (active-only) would drop a 'removed'/'expired'/'replaced' account
	// from this map, and every transaction Plaid delivers for it would then miss accountMap
	// and be Warn'd-and-skipped forever by processAddedTransactions — the item never stops
	// syncing it (CountActiveByLinkedItemID stays nonzero while siblings remain active), so
	// this was a permanent, silent transaction loss for any account the user disconnects.
	// These are real transactions — the money moved — so they are stored against the
	// account like any other; retained-account visibility in the unified feed is handled by
	// retainedAccountVisibilityLeg (transaction_repository.go), not by excluding rows here.
	// Do NOT switch this to GetByLinkedItemID: its remaining wipe-and-reinsert callers are the
	// holdings and liability syncs, whose ReplaceForAccount semantics must stay active-only
	// (see the doc comment on AccountRepository.GetByLinkedItemID). Investment-transaction
	// sync used to be grouped with them but was fixed in Issue #1569 round 11 task 1 — it now
	// reads GetByLinkedItemIDIncludingRetained too, for the identical reason this method does.
	// Balance-snapshot sync (createBalanceSnapshots below) was never a GetByLinkedItemID
	// caller either; it reads GetByLinkedItemIDIncludingRetained for its own, unrelated
	// reason (its account-restore pass needs to see removed rows) and filters to the active
	// subset itself before writing any snapshot.
	accounts, err := s.accountRepo.GetByLinkedItemIDIncludingRetained(ctx, linkedItemID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load accounts for linked item %s: %w", linkedItemID, err)
	}

	// Build maps: Plaid account_id -> Internal IDs
	accountMap := make(map[string]string, len(accounts))
	userAccountMap := make(map[string]string, len(accounts))

	for _, account := range accounts {
		// #1615 / #1537 (F14): mirrors the investment side's guard (investment_sync_service.go
		// ~:282-298, ~:523). GetByLinkedItemIDIncludingRetained is keyed on linked_item_id only —
		// it carries no user_id predicate — so ownerUserID (the linked item's owner) and each
		// account's UserID come from two sources nothing forces to agree. If they ever diverged,
		// this is the only place both values are in scope to catch it before a transaction gets
		// written against someone else's account.
		if account.UserID != ownerUserID {
			s.logger.Error("skipping an account that does not belong to the linked item's owner",
				"linked_item_id", linkedItemID,
				"account_id", account.ID,
				"account_user_id", account.UserID,
				"linked_item_user_id", ownerUserID,
			)
			continue
		}
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

	// Issue #1213: Pre-assign UUIDs before rule application so that the IDs used
	// to key BatchRuleApplication records match the ids returned by CreateBatch's
	// RETURNING clause.  CreateBatch preserves pre-assigned IDs via its
	// `if tx.ID == ""` guard, so this is safe.
	for i := range transactions {
		if transactions[i].ID == "" {
			transactions[i].ID = uuid.New().String()
		}
	}

	// Issue #1163: Batch-upsert merchants and stamp merchant_id (one DB round-trip, best-effort).
	//
	// ⚠ MUST run BEFORE rule application (#1625 Task 11). These rows come straight from the Plaid
	// mapper with no merchant_id, so a rule matching on effective_merchant could resolve nothing
	// while the same rule previewed correctly against stored transactions — preview and sync
	// disagreeing, which is the divergence this ticket already fixed once. Nothing in the merchant
	// upsert depends on rule output: it reads MerchantName and writes MerchantID, neither of which
	// rules touch.
	merchantIDs := s.upsertMerchants(ctx, transactions)
	if merchantIDs != nil {
		for i := range transactions {
			if transactions[i].MerchantName != nil {
				if id, ok := merchantIDs[*transactions[i].MerchantName]; ok {
					transactions[i].MerchantID = &id
				}
			}
		}
	}

	// Issue #346: Apply categorization rules to transactions before storing.
	// Rules are applied in-place.  Side-effects (analytics/match-count) are
	// deferred to EmitBatchSideEffects after CreateBatch so they are gated on
	// genuine inserts only (Issue #1213).
	var ruleApplications []BatchRuleApplication
	if s.ruleApplicationService != nil {
		apps, err := s.ruleApplicationService.ApplyRulesToBatch(ctx, transactions, linkedItem.UserID)
		if err != nil {
			// Log but don't fail - rule application is non-critical
			s.logger.Warn("failed to apply categorization rules", "error", err)
		} else {
			ruleApplications = apps
		}
	}

	// Store in database (ON CONFLICT DO UPDATE preserves user enrichments on conflict).
	insertedIDs, err := s.transactionRepo.CreateBatch(ctx, transactions)
	if err != nil {
		return fmt.Errorf("failed to create batch of %d transactions: %w", len(transactions), err)
	}

	s.logger.Info("stored added transactions", "stored", len(transactions), "skipped", len(added)-len(transactions))

	// Issue #1213: Emit analytics/match-count side-effects only for genuinely inserted rows.
	if s.ruleApplicationService != nil && len(ruleApplications) > 0 {
		s.ruleApplicationService.EmitBatchSideEffects(ctx, linkedItem.UserID, ruleApplications, insertedIDs)
	}

	// Issue #36: Monitor for transfer suggestion matches (non-blocking).
	// All persisted transactions are passed (inserted + conflict-updated), because the
	// monitor has intentional post-relink matching semantics via stable UserAccountID.
	if s.transactionMonitorService != nil {
		txnPtrs := make([]*domain.Transaction, len(transactions))
		for i := range transactions {
			txnPtrs[i] = &transactions[i]
		}
		if err := s.transactionMonitorService.MonitorForMatches(ctx, linkedItem.UserID, txnPtrs); err != nil {
			// Log but don't fail - transaction monitoring is non-critical
			s.logger.Warn("failed to monitor for transfer suggestion matches", "error", err)
		}
	}

	// Issue #1586: the stranded-annotation mirror. TombstoneByExternalID moves a settling pending
	// row's annotations onto its posted twin, but only a twin that is ALREADY stored — which is
	// guaranteed within one sync by the added/modified/removed ordering and guaranteed by nothing
	// across syncs. When Plaid delivers the removal in sync N and the posted twin in sync N+1, the
	// user's category, notes, tags and review state stay on a row that renders nowhere.
	//
	// So the twin, on arrival, looks BACK for an already-tombstoned pending row it points at. Only
	// rows carrying Plaid's own pending_transaction_id are candidates; the pairing is never a
	// heuristic on amount/date/merchant.
	//
	// ⚠ IT RUNS LAST, AFTER THE TWO SIDE-EFFECT BLOCKS ABOVE, AND THE ORDER IS DELIBERATE. Its
	// failure now returns (see the function's own note), and the rule-application side effects fire
	// only for GENUINELY INSERTED ids — on the re-delivered retry the rows already exist, so
	// insertedIDs is empty and those analytics would be lost for good if the return jumped over
	// them. Nothing here depends on the repair having happened.
	//
	// ⚠ IT DEPENDS ON CreateBatch HAVING WRITTEN THE PERSISTED ids BACK INTO `transactions` (#1586
	// gap G7). The slice is passed by reference precisely so that happens: every id here was minted
	// by this function, and a re-delivered row keeps the id it was first stored with, so before the
	// write-back the retry that this repair's fail-and-hold-the-cursor design exists to enable was
	// the one case guaranteed to repair nothing.
	if err := s.pullForwardStrandedSettlements(ctx, linkedItem.UserID, transactions); err != nil {
		return err
	}

	return nil
}

// processModifiedTransactions updates existing transactions in the database.
// CRITICAL: Preserves user_notes field from existing transaction.
//
// Process:
//  1. Fetch the existing transaction by external_transaction_id (BEFORE mapping — see UNMAPPABLE
//     ACCOUNTS below; the stored row is a source of account identity, so the map must not get to
//     veto an edit to a row we already hold)
//  2. Map the Plaid payload to domain.Transaction
//  3. Preserve user_notes from the existing transaction
//  4. Call Update with the new data + preserved user_notes + existing ID
//
// Error handling: a lookup failure, an Update failure and a missing-row upsert failure all return,
// so the cursor stays where the run started. Exactly two paths log and continue, both narrated
// below (UNMAPPABLE ACCOUNTS), and neither can lose an edit to a transaction we already store.
//
// UNMAPPABLE ACCOUNTS. mapPlaidTransaction fails for two unrelated reasons, and until #1569 round 1
// both were flattened into one `Warn; skippedCount++; continue` that returned nil — structurally
// the same defect as residual (3), just reached one branch earlier:
//
//   - the Plaid account_id is not in accountMap. If the transaction IS stored, its own row already
//     records account_id / linked_item_id / user_account_id, so the payload is remapped with
//     MapPlaidTransactionBase and that identity re-attached; the edit lands. Only when the row is
//     ALSO absent is there nothing to insert under, and that case is narrowed the same way task 1b
//     narrowed processAddedTransactions': buildAccountMaps loads every status
//     (GetByLinkedItemIDIncludingRetained), so it needs Plaid to name a plaid_account_id with no
//     accounts row AT ALL for this linked_item_id.
//   - MapPlaidTransactionBase itself fails — a malformed payload, e.g. an unparseable date. Skipped
//     and logged on purpose: re-delivery cannot fix a payload, so returning an error would wedge
//     this item's sync forever, and the failure direction is RETAINING the stored row unchanged.
//
// MISSING ROWS — residual (3) of the #1570 STORAGE-BEFORE-CURSOR CONTRACT. A `modified` entry whose
// row GetByExternalID cannot find used to be Warn'd and DROPPED, and the cursor then advanced past
// it, so the edit was lost permanently: Plaid does not re-deliver a modification the stored cursor
// has moved past.
//
// It is now UPSERTED through the same CreateBatch the `added` path uses. The justification is that
// a missing row is not evidence the transaction does not belong to the user — it is evidence an
// EARLIER sync failed to store it. Before #1570 moved the cursor write after storage, a partial
// page failure advanced the cursor over rows that were never written; every one of those rows can
// still arrive here as a `modified`. Dropping the edit leaves the user permanently missing a
// transaction the bank has told us about twice.
//
// It cannot resurrect a tombstone, and that was the specific risk worth checking rather than
// assuming. GetByExternalID now filters `deleted_at IS NULL`, so a TOMBSTONED row also lands in
// this branch. CreateBatch's ON CONFLICT arbiter is
// (source, external_transaction_id_blind_idx, user_id) — the tombstoned row itself — and its SET
// list names neither deleted_at, deleted_reason nor deleted_by, so the conflict path refreshes the
// provider-owned fields and leaves the tombstone exactly as it was. No new row is inserted either,
// because the unique index the arbiter uses does not exclude tombstoned rows. Verified against a
// real database by TestTombstone_UpsertDoesNotResurrect_Integration
// (repository/transaction_tombstone_integration_test.go).
//
// CreateBatch also preserves user_notes, category_override_kind, tags and tags_source on conflict,
// which is the same "sync overwrites provider fields, never user work" contract the Update branch
// below implements by hand.
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
	upsertedCount := 0
	recoveredCount := 0

	// #1586: rows this pass stored that carry Plaid's pending_transaction_id, collected for the
	// stranded-settlement pull-forward after the loop.
	var settledCandidates []domain.Transaction

	// Issue #1163: Batch-upsert merchants for modified transactions (best-effort)
	// Build domain txns temporarily just to use shared upsertMerchants helper
	modifiedDomainTxns := make([]domain.Transaction, len(modified))
	for i, plaidTxn := range modified {
		if mn := plaidTxn.MerchantName.Get(); mn != nil {
			modifiedDomainTxns[i].MerchantName = mn
		}
	}
	merchantIDs := s.upsertMerchants(ctx, modifiedDomainTxns)

	for _, plaidTxn := range modified {
		// LOOK UP FIRST, MAP SECOND. The order is load-bearing (#1569 round 1). Mapping first meant
		// an unmappable Plaid account_id discarded the modification before anything even checked
		// whether we already HELD that transaction — the same "Warn, skip, return nil, cursor
		// advances" shape as residual (3), applied to a row that exists. The stored row is itself a
		// perfectly good source of account identity, so it is consulted before the map can veto.
		existingTxn, lookupErr := s.transactionRepo.GetByExternalID(ctx, domain.SourcePlaid, plaidTxn.TransactionId, linkedItem.UserID)
		if lookupErr != nil && !errors.Is(lookupErr, repository.ErrTransactionNotFound) {
			// A real lookup failure is NOT the missing-row case and must not be absorbed: the
			// cursor would advance past a modification nobody applied.
			return fmt.Errorf("failed to look up modified transaction %s: %w", plaidTxn.TransactionId, lookupErr)
		}

		newTxn, err := s.mapPlaidTransaction(ctx, plaidTxn, linkedItem.ID, linkedItem.UserID, accountMap, userAccountMap)
		if err != nil {
			if existingTxn == nil {
				// Nothing stored AND nothing mappable: there is no account_id to insert under and
				// no row to update. This is the ONLY remaining log-and-continue path in this
				// function, and it is narrowed exactly as processAddedTransactions' equivalent is
				// by task 1b's GetByLinkedItemIDIncludingRetained — it needs Plaid to name a
				// plaid_account_id with no accounts row at all for this linked_item_id.
				s.logger.Warn("skipping modified transaction: not stored and not mappable",
					"transaction_id", plaidTxn.TransactionId, "error", err)
				skippedCount++
				continue
			}

			// We hold the row. Rebuild from the provider payload alone and re-attach the identity
			// the stored row already proves, rather than losing the edit.
			base, baseErr := MapPlaidTransactionBase(plaidTxn)
			if baseErr != nil {
				// The payload itself is malformed (e.g. an unparseable date). Re-delivery cannot
				// fix that, so returning an error here would wedge this item's sync permanently.
				// Logged and skipped deliberately, and it fails toward RETAINING the stored row
				// unchanged rather than destroying anything.
				s.logger.Warn("skipping modified transaction: provider payload cannot be mapped",
					"transaction_id", plaidTxn.TransactionId, "error", baseErr)
				skippedCount++
				continue
			}
			externalID := plaidTxn.GetTransactionId()
			base.UserID = linkedItem.UserID
			base.ExternalTransactionID = &externalID
			base.AccountID = existingTxn.AccountID
			base.LinkedItemID = existingTxn.LinkedItemID
			base.UserAccountID = existingTxn.UserAccountID
			newTxn = base
			recoveredCount++
		}

		if existingTxn == nil {
			// Residual (3): no live row to update. Upsert instead of dropping the edit — see the
			// MISSING ROWS note on this function for why, and for why this cannot resurrect a
			// tombstone.
			if newTxn.ID == "" {
				newTxn.ID = uuid.New().String()
			}
			if newTxn.MerchantName != nil && merchantIDs != nil {
				if id, ok := merchantIDs[*newTxn.MerchantName]; ok {
					newTxn.MerchantID = &id
				}
			}
			// #1586 (gap G7): the batch is a NAMED SLICE and the row is read back out of it. It
			// used to be an inline `[]domain.Transaction{*newTxn}` — a COPY — and CreateBatch
			// writes the PERSISTED id into the element it was handed. On a conflict (this branch's
			// most likely outcome: no LIVE row, but a tombstoned one the ON CONFLICT arbiter still
			// sees) that persisted id differs from the UUID minted six lines above, and writing it
			// into a discarded copy left settledCandidates carrying an id that names no row. The
			// repair below then matched nothing and reported success.
			batch := []domain.Transaction{*newTxn}
			if _, upsertErr := s.transactionRepo.CreateBatch(ctx, batch); upsertErr != nil {
				return fmt.Errorf("failed to upsert modified transaction %s with no stored row: %w",
					plaidTxn.TransactionId, upsertErr)
			}
			*newTxn = batch[0]
			s.logger.Info("upserted modified transaction that had no stored row",
				"transaction_id", plaidTxn.TransactionId)
			upsertedCount++
			settledCandidates = append(settledCandidates, *newTxn)
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

		settledCandidates = append(settledCandidates, *newTxn)
		successCount++
	}

	// Issue #1586: a `modified` delivery is the OTHER way a stored row can learn a pointer. Both
	// write paths derive pending_transaction_id_blind_idx, and Update writes it as
	// COALESCE(new, stored), so a pointer arriving on a modification is kept. Covering only the
	// added path would leave that route stranded for exactly the same reason the ticket describes.
	if err := s.pullForwardStrandedSettlements(ctx, linkedItem.UserID, settledCandidates); err != nil {
		return err
	}

	s.logger.Info("updated modified transactions",
		"updated", successCount, "upserted", upsertedCount,
		"identity_recovered_from_stored_row", recoveredCount, "skipped", skippedCount)
	return nil
}

// pullForwardStrandedSettlements hands the repository the ids of the rows just stored that carry
// Plaid's pending_transaction_id, so it can pull forward any annotations stranded on an
// already-tombstoned pending twin (#1586).
//
// ⚠ ITS FAILURE RETURNS, AND THAT IS THE FIX FOR A COMMENT THAT WAS FALSE. This used to log and
// swallow, justified by the claim that a failed repair "will be retried on next sync". It could
// not be: both callers are inside the storage steps that run BEFORE #1570's single cursor advance,
// so a swallowed failure was immediately followed by telling Plaid we had consumed the page — and
// /transactions/sync does not re-deliver a transaction the stored cursor is past. The posted twin
// stored fine, the repair did not, and the user's category, notes, tags and review state stayed on
// the tombstoned pending row FOREVER. The retry the comment promised had no way to happen.
//
// Returning makes the promise true: the sync fails, the cursor stays where the run started, Plaid
// re-delivers the same page, and the repair (which is idempotent) runs again. It is also the
// contract the writes either side of this already have — a CreateBatch or Update failure returns
// from these same two functions, and a failed repair is the same class of transient database error.
// "Idempotent" is not an assumption here: TestLateSettlement_IsIdempotent_Integration runs the
// repair twice against real Postgres and pins that the second run is a no-op.
//
// ⚠ THE COST IS REAL. A repair that fails PERMANENTLY now stalls this item's ingestion rather than
// quietly losing one user's annotations. That is the right direction to fail in — a stalled cursor
// surfaces as a sync failure with metrics and a dead-letter job, where the swallow was invisible to
// everyone including the user whose data it stranded — but it is a trade, not a free win. The
// escape hatch if this ever fires on a non-transient fault is a durable repair job; River exists,
// but this service holds no client for it, so that is a wiring change and not a one-line one.
//
// Rows with no pointer are filtered out here rather than in SQL: the overwhelming majority of a
// sync batch is not a settlement, and there is no reason to send their ids to the database.
func (s *transactionSyncService) pullForwardStrandedSettlements(
	ctx context.Context,
	userID string,
	stored []domain.Transaction,
) error {
	ids := make([]string, 0, len(stored))
	for i := range stored {
		if stored[i].PendingTransactionID == nil || *stored[i].PendingTransactionID == "" {
			continue
		}
		if stored[i].ID == "" {
			continue
		}
		ids = append(ids, stored[i].ID)
	}
	if len(ids) == 0 {
		return nil
	}

	moved, err := s.transactionRepo.PullForwardSettledPendingAnnotations(ctx, userID, ids)
	if err != nil {
		s.logger.Error("failed to pull forward stranded settlement annotations",
			"user_id", userID, "candidates", len(ids), "error", err)
		return fmt.Errorf("failed to pull forward stranded settlement annotations for %d candidate(s): %w",
			len(ids), err)
	}
	if moved > 0 {
		s.logger.Info("pulled forward annotations stranded by a cross-sync settlement",
			"user_id", userID, "candidates", len(ids), "moved", moved)
	}
	return nil
}

// detectAndUpsertProposedContributions creates proposed contribution rows for every
// posted transfer-in detected on the linked account for all active contributions-basis
// goals owned by userID. Called BEFORE RecomputeUser so that the ledger is up-to-date
// before progress is computed. Non-fatal: individual upsert failures are logged and skipped.
// Implements D6 source-b (Issue #1300 M18 / RG1).
func (s *transactionSyncService) detectAndUpsertProposedContributions(ctx context.Context, userID string) error {
	goals, err := s.savingsGoalReader.GetActiveByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("detectAndUpsertProposedContributions: get active goals: %w", err)
	}

	// Only contributions-basis goals need inflow detection.
	const lookbackDays = 37 // same window as transfer correlation
	start := time.Now().AddDate(0, 0, -lookbackDays)
	end := time.Now()

	for _, goal := range goals {
		if goal.ProgressBasis != domain.ProgressBasisContributions {
			continue
		}
		if goal.AccountID == "" {
			continue
		}

		inflows, err := s.savingsInflowRepo.GetInflowTransactionsForAccount(ctx, userID, goal.AccountID, start, end)
		if err != nil {
			s.logger.Warn("detectAndUpsertProposedContributions: failed to get inflows (skipping goal)",
				"goal_id", goal.ID, "account_id", goal.AccountID, "error", err)
			continue
		}

		for _, inflow := range inflows {
			if err := s.savingsProposedUpserter.UpsertProposedFromTransaction(
				ctx, userID, goal.ID, inflow.ID, inflow.Magnitude,
			); err != nil {
				s.logger.Warn("detectAndUpsertProposedContributions: upsert proposed contribution failed (skipping txn)",
					"goal_id", goal.ID, "txn_id", inflow.ID, "magnitude", inflow.Magnitude, "error", err)
			}
		}
	}
	return nil
}

// processRemovedTransactions TOMBSTONES the transactions Plaid reports as removed. Issue #1569.
//
// ⚠ WHAT THIS ALSO DOES, since #1578 (task 4): IT MOVES THE USER'S WORK OFF A SETTLED PENDING.
// The paragraph that used to stand here described the defect as investigated and NOT implemented.
// It is implemented now, and the implementation lives one layer down --
// TransactionRepository.TombstoneByExternalID, whose doc comment carries the full design. This
// function is unchanged; the move is folded into the statement it already issues.
//
// THE DEFECT IT CLOSES, which was live and routine. When a pending transaction posts, Plaid removes
// the pending one and adds a NEW transaction with a different transaction_id. This function
// tombstones the pending row, and task 7 established that a settled pending must render NOWHERE
// (deleted_reason = provider_removed_pending; see isSettledPending in
// apps/web/src/features/transactions/types.ts, and
// TestTombstone_SettledPendingIsNeverSurfaced_Integration). So the user's category override, notes,
// tags and `reviewed` state were RETAINED on a row nothing will ever show them on, and the posted
// twin arrived blank. Every time anybody categorised a pending charge.
//
// ⚠ THE CENSUS PROPOSED FUZZY MATCHING ON (account_id, amount, date±window, merchant). DO NOT, and
// the implementation does not. Plaid supplies the link exactly: Transaction.PendingTransactionId
// (pinned SDK plaid-go v23.0.0, plaid/model_transaction.go:47) is set on the POSTED transaction and
// holds the transaction_id of the pending one it replaced. A heuristic would risk attaching one
// transaction's category to a different transaction, which is worse than the bug it fixes; the
// provider's own pointer cannot.
//
// WHERE THE PIECES ARE, so the next reader does not have to search:
//  1. migration 000168 adds transactions.pending_transaction_id_blind_idx (blind index only -- the
//     value is never read back, only matched, so no plaintext and no ciphertext is stored);
//  2. services/plaid_mapper.go MapPlaidTransactionBase populates
//     domain.Transaction.PendingTransactionID, and BOTH repository write paths derive the blind
//     index from it -- CreateBatch for `added`, Update for `modified`. Both accept it via COALESCE
//     so a row can LEARN a pointer that arrives later and a pointer-less re-delivery cannot erase
//     one; omitting the column from CreateBatch's ON CONFLICT SET clause, which an earlier draft of
//     this note recommended, would have made the first of those impossible;
//  3. the annotation move is a data-modifying CTE inside TombstoneByExternalID's single statement,
//     so it is atomic with the tombstoning and there is no window in which a crash could leave the
//     annotations on both rows, i.e. double-categorised. (This line used to add "with no
//     transaction to introduce". T4 wrote that; T6 falsified it five commits later by routing the
//     statement through runCapturingStatement, which opens one -- see
//     repository/transaction_annotation_capture.go:~249.);
//  4. the refusal to overwrite the note asked for is implemented as two predicates: a MANUAL
//     category already on the twin wins over the pending row's, and tags move only onto a twin that
//     has none. A twin categorised by a RULE does NOT block the move -- rules run in place before
//     the twin is ever inserted, so a blank-row-only gate would have refused in the common case.
//
// ORDERING IS ALREADY IN THIS FUNCTION'S FAVOUR and does not need a second hook for the common
// case: processTransactions aggregates EVERY page into result.Added/Modified/Removed before storing
// anything, then runs step 6/7 (added, modified) before step 8 (this function), and only advances
// the cursor after all of it. So for any removal+addition Plaid delivers together -- which is how a
// settlement is delivered -- the posted twin is already in the database when this runs.
//
// RESIDUAL, unchanged and still open: a removal delivered in one sync whose posted twin arrives in a
// LATER one leaves the annotations on the tombstone. Closing that needs the mirror hook in the
// storage path (on inserting a posted transaction with a pending_transaction_id, look for an
// already-tombstoned pending row and pull its annotations forward). Whether that case can occur is
// NOT established here; it is a question about Plaid's delivery semantics, not about this code.
//
// It used to hard-delete them. That was the last unrecoverable destroyer of user-authored work in
// this codebase: a `removed` entry arrives with no user action behind it, and the DELETE took the
// user's category overrides, tags, notes and `reviewed` state with it, plus the
// rule_execution_history rows behind migration 000020's CASCADE. The row is now retained and
// marked, and the reason (settled pending vs. posted reversal) is decided in SQL from the row's own
// `pending` column — see TransactionRepository.TombstoneByExternalID.
//
// ERROR HANDLING, and this is residual (4) of the #1570 STORAGE-BEFORE-CURSOR CONTRACT:
//
//   - a nil transaction_id is skipped and counted (nothing identifies a row to act on);
//   - ErrTransactionNotFound is the IDEMPOTENT case — the row was never stored, or a re-delivery
//     already tombstoned it — and is skipped and counted;
//   - ANY OTHER error aborts the whole function. It used to be Warn'd and swallowed, with the
//     function still returning nil, which let the cursor advance past a removal that had not been
//     applied. Plaid never re-delivers a removal the stored cursor has moved past, so that row
//     stayed visible forever with no trace of the discrepancy. Failing here leaves the cursor
//     where the run started and the whole page is re-delivered, which CreateBatch/Update/
//     TombstoneByExternalID all absorb idempotently.
func (s *transactionSyncService) processRemovedTransactions(
	ctx context.Context,
	removed []plaid.RemovedTransaction,
	linkedItem *domain.LinkedItem,
) error {
	// Optimization: Skip processing if no transactions
	if len(removed) == 0 {
		return nil
	}

	tombstonedCount := 0
	skippedCount := 0
	reversalCount := 0

	for _, removedTxn := range removed {
		// Skip transactions with nil ID
		if removedTxn.TransactionId == nil {
			s.logger.Warn("removed transaction has nil transaction_id, skipping")
			skippedCount++
			continue
		}

		// Issue #1300 G2 (Codex C4): clean up reconciler-owned contribution rows for this
		// transaction, so a live contributions-basis goal stops counting money the bank withdrew.
		//
		// ⚠ The reason this comment used to give — "BEFORE hard-deleting the transaction so
		// ON DELETE SET NULL doesn't orphan managed rows" — went stale five commits after it was
		// written: T7 turned the removal below into a TOMBSTONE (TombstoneByExternalID), so nothing
		// is hard-deleted here and fk_contributions_transaction's SET NULL never fires. The call
		// still earns its place for a different reason: it also dedups against the posted twin,
		// whose own contribution the reconciler derives separately.
		//
		// The cleaner is scoped to LIVE goals (#1569 C1); a deleted goal's ledger is retained.
		if s.savingsContribCleaner != nil {
			// Look up internal UUID (encrypted external ID needs repo lookup)
			if internalTxn, lookupErr := s.transactionRepo.GetByExternalID(ctx, domain.SourcePlaid, *removedTxn.TransactionId, linkedItem.UserID); lookupErr == nil {
				if cleanErr := s.savingsContribCleaner.DeleteManagedForTransaction(ctx, internalTxn.ID); cleanErr != nil {
					s.logger.Warn("failed to delete managed contributions before transaction removal (non-fatal)",
						"transaction_id", *removedTxn.TransactionId, "error", cleanErr)
				}
			}
			// If GetByExternalID fails (already deleted or not found), skip — nothing to clean.
		}

		reason, err := s.transactionRepo.TombstoneByExternalID(
			ctx, domain.SourcePlaid, *removedTxn.TransactionId, linkedItem.UserID, domain.TxnDeletedByPlaidSync)
		switch {
		case errors.Is(err, repository.ErrTransactionNotFound):
			// Idempotent: never stored, or a re-delivery of a removal already applied.
			s.logger.Debug("removed transaction has no live row to tombstone (idempotent)",
				"transaction_id", *removedTxn.TransactionId)
			skippedCount++
			continue
		case err != nil:
			// Residual (4): a real database failure must NOT be absorbed. Returning here holds
			// the cursor at the run's starting position so Plaid re-delivers this removal.
			return fmt.Errorf("failed to tombstone removed transaction %s: %w", *removedTxn.TransactionId, err)
		}

		tombstonedCount++
		if reason == domain.TxnDeletedProviderRemovedPosted {
			reversalCount++
		}
	}

	s.logger.Info("tombstoned removed transactions",
		"tombstoned", tombstonedCount,
		"posted_reversals", reversalCount,
		"skipped", skippedCount)
	return nil
}

// createBalanceSnapshots fetches current account balances from Plaid and stores snapshots.
// Issue #353: Balance Tracking Dashboard - Capture balance snapshots during webhook processing
// Issue #364: Migrated to timestamp-based storage for high-frequency syncs
//
// Called after transaction sync completes to capture the current state of all account balances.
// Uses a 5-minute deduplication window to REDUCE excessive snapshots. Not prevent: the
// GetLatestByAccountID check and the Create are separate statements, and webhook processing takes no
// linked-item lock (the sync worker does, FOR UPDATE), so two concurrent paths can both see no recent
// snapshot and both insert. It bounds SEQUENTIAL duplication only, while allowing multiple
// snapshots per day for high-frequency syncs. (This used to end "— see #1570.", mid-sentence and
// pointing at the cursor issue, which never covered concurrent snapshot inserts. #1569 closed
// #1570, so the dangling pointer is removed rather than left to send the next reader somewhere
// irrelevant; the concurrency gap itself is unchanged and still untracked.)
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
		// Issue #1569 Task 5: this early return is also the outermost of the three guards against
		// the zero-account sweep — nothing downstream, reconciliation included, runs on a response
		// that named no accounts. (The other two: PlaidAccountRosterFrom marks such a roster
		// incomplete, and reconcileRemovedAccounts refuses an incomplete roster.)
		s.logger.Info("no accounts returned from Plaid, skipping balance snapshots and account reconciliation", "item_id", linkedItem.ID)
		return nil
	}

	// Step 2: Get this item's accounts. INCLUDING RETAINED (Issue #1569 Task 5, fix round 2): the
	// restore pass below has to see 'removed' rows, since those are the only candidates for coming
	// back. The active subset is split out immediately and is what the snapshot loop and the
	// removal pass use, so neither's behaviour changes — in particular, a removed account must
	// still accrue NO balance snapshots, which is what keeps a user-disconnected account from
	// quietly resuming data collection because the bank still reports it.
	allAccounts, err := s.accountRepo.GetByLinkedItemIDIncludingRetained(ctx, linkedItem.ID)
	if err != nil {
		return fmt.Errorf("failed to get internal accounts: %w", err)
	}
	internalAccounts := activeAccounts(allAccounts)

	// `err` is nil here (the fetch above returned early otherwise), which is exactly what the
	// roster's second argument asserts.
	roster := PlaidAccountRosterFrom(plaidAccounts, nil)

	// Step 2b (Issue #1569 Task 5): reverse-direction reconciliation. Everything else in this
	// function walks PLAID's accounts and looks up internal ones, so an account the bank closed —
	// which Plaid simply stops returning — is never visited and keeps a frozen balance in CURRENT
	// net worth forever. Walk the other way and soft-remove what this response proves is gone.
	// Non-fatal: snapshots must still be written either way.
	s.reconcileRemovedAccounts(ctx, linkedItem, roster, internalAccounts)

	// Step 2c (fix round 2): the inverse. An account we marked removed because Plaid stopped
	// returning it, which Plaid is now returning again, was not closed after all — and nothing
	// else in this codebase ever writes an account back to 'active'. Runs on `allAccounts`
	// (the retained rows are the whole point) and NEVER touches a user-disconnected account; see
	// restoreProviderRemovedAccounts. Ordering is deliberate: removal first, so an account absent
	// from this response cannot be removed and restored in the same pass.
	restoredCount := s.restoreProviderRemovedAccounts(ctx, linkedItem, roster, allAccounts)

	// Step 2d (Issue #1569 Task 10.3, item A): the restore above WROTE to the database, so
	// `internalAccounts` — filtered out of a read taken before that write — no longer says which
	// accounts are active. Building the snapshot map below from that stale slice skipped a
	// same-pass restore entirely: the account Plaid dropped and returned in ONE response came back
	// to 'active' but got no balance snapshot, so its net-worth contribution stayed frozen at the
	// pre-removal figure until some later snapshot event happened to fire, and balance history had
	// a hole at exactly the moment the account came back.
	//
	// Re-read rather than reorder. The removal-before-restore ordering above is deliberate (see
	// Step 2c) and must survive, and re-deriving the restore predicate here would duplicate its
	// ownership and cause checks — the database is the only authority on what the restore actually
	// changed. Guarded on restoredCount, so the overwhelmingly common path costs no extra query; a
	// failed re-read is non-fatal and simply leaves the pre-restore view in place, which is the
	// behaviour this step replaces.
	if restoredCount > 0 {
		refreshed, refreshErr := s.accountRepo.GetByLinkedItemIDIncludingRetained(ctx, linkedItem.ID)
		if refreshErr != nil {
			s.logger.Warn("failed to re-read accounts after a restore; the restored account's balance snapshot waits for the next sync",
				"item_id", linkedItem.ID, "error", refreshErr)
		} else {
			internalAccounts = activeAccounts(refreshed)
		}
	}

	// Step 3: Create balance snapshots for each account.
	//
	// #1626 Phase 3 Task G: the per-account loop that used to live here is now writeBalanceCohort
	// (balance_capture_service.go), shared with the periodic gap-filler. This call site keeps the
	// roster read and the two reconciliation passes above; the gap-filler calls the same writer with
	// no reconciliation at all.
	//
	// The dedup window, the NULL-balance rule, the unmatched-account rule and the removed-account
	// rule are all unchanged — they moved, they were not rewritten. What DID change is where the
	// error policy lives, and that is the point of the extraction: the writer now RETURNS its
	// per-account failures instead of swallowing them, and this caller applies the sync's policy
	// explicitly (below) while the capture path applies its own.
	now := time.Now().UTC()
	result, writeErr := writeBalanceCohort(ctx, balanceCohortInput{
		logger:         s.logger,
		linkedItem:     linkedItem,
		plaidAccounts:  plaidAccounts,
		activeAccounts: internalAccounts,
		source:         domain.SnapshotSourcePlaidWebhook,
		capturedAt:     now,
		dedupWindow:    balanceSnapshotDeduplicationWindow,
		latest:         s.balanceSnapshotRepo.GetLatestByAccountID,
		// Create, not CreateDailyCapture: webhook history deliberately permits several rows per
		// account per day (migration 000017 dropped that uniqueness on purpose), so there is no
		// arbiter to conflict against and none is wanted. The bool is always true on success.
		insert: func(ctx context.Context, snapshot *domain.BalanceSnapshot) (bool, error) {
			if err := s.balanceSnapshotRepo.Create(ctx, snapshot); err != nil {
				return false, err
			}
			return true, nil
		},
	})

	// ⚠ THE SYNC'S POLICY, STATED RATHER THAN INHERITED: a balance-snapshot failure is NON-FATAL
	// here, and that is unchanged behaviour, not a regression introduced by the extraction. This
	// function's own doc comment says why — balance snapshots are non-critical to a transaction
	// sync, and failing the sync would discard the transaction work that already committed while
	// the next webhook would retry the snapshot anyway.
	//
	// The periodic capture path makes the OPPOSITE choice on the same writer, because a job whose
	// entire remit is "every active account has a row for today" must not report success having
	// written some of them. Before the extraction there was one baked-in policy — swallow — and the
	// capture path would have inherited exactly the wrong one.
	if writeErr != nil {
		s.logger.Warn("some balance snapshots failed during sync; the sync itself is unaffected and "+
			"the next webhook will retry them",
			"item_id", linkedItem.ID, "error", writeErr)
	}

	s.logger.Info("created balance snapshots",
		"item_id", linkedItem.ID,
		"created", result.Written,
		"skipped", result.Unmatched+result.NoBalance,
		"deduped", result.Deduped,
	)
	return nil
}

// activeAccounts returns the active subset of a retained-inclusive account read.
//
// Split out because createBalanceSnapshots derives it twice: once before the reconciliation passes
// and, when a restore changed something, once from the re-read afterwards. Keeping one definition
// means the two views cannot drift on what "active" means — in particular, a removed account must
// accrue NO balance snapshots, which is what stops a user-disconnected account quietly resuming
// data collection because the bank still reports it.
func activeAccounts(accounts []domain.Account) []domain.Account {
	active := make([]domain.Account, 0, len(accounts))
	for _, account := range accounts {
		if account.Status == domain.AccountStatusActive {
			active = append(active, account)
		}
	}
	return active
}

// isRedundantSnapshot reports whether `next` says nothing `latest` does not already say.
//
// #1569 task 20 (S8). Both conditions must hold: the existing snapshot is INSIDE the window AND it
// carries the same figures. The value test is what stops the window discarding history — a real
// balance change arriving inside five minutes is new information, and the only place it is ever
// recorded is this table.
//
// Both balances are compared, and each is compared including its NULL-ness: Plaid reports
// available_balance for some account types and not others, and a transition between "known" and
// "unknown" is itself a change worth a row. Comparing only current_balance would silently drop it.
//
// No epsilon. These are *float64 carrying a provider-supplied decimal that this code neither
// computes with nor rounds — it is copied from the response into the row — so two syncs reporting
// the same figure produce bit-identical values, and an epsilon would only widen the set of real
// changes that get dropped.
//
// ⚠ THREE CONDITIONS, NOT TWO, AND THE THIRD CLOSED A SILENT DATA LOSS (#1626 gap G7). The window
// is five minutes of ELAPSED TIME and says nothing about the UTC DAY — but the daily gap-filler's
// entire unit of work IS the UTC day: balanceCaptureService.CaptureItemBalances asks
// AccountsMissingDailySnapshot whether TODAY's cohort is complete, and migration 000194's partial
// unique index is keyed on the UTC date. So a capture landing in the 00:00–00:05 UTC window — a
// RunOnStart deploy just after midnight, or River retrying a 23:5x attempt that failed — saw the
// account as missing today's row, deduped against yesterday's 23:58 IDENTICAL row, wrote nothing,
// and returned SUCCESS. The day it was asked to fill stayed empty permanently: there is no backfill
// on this path, and nothing in the outcome counters distinguished it from a routine skip.
//
// Gating on the UTC calendar day of latestSnapshot.SnapshotAt versus `now` makes "says nothing new"
// mean what the writers around it already mean by it. It costs at most ONE extra row per account per
// day, in the specific case where the previous row is both identical and on the previous day — which
// is the row the day-keyed index exists to hold.
//
// ⚠ IT APPLIES TO THE WEBHOOK PATH TOO, deliberately rather than incidentally: this function is
// shared by writeBalanceCohort's two provenances, and a webhook arriving at 00:02 with yesterday's
// figures is making the same statement about a new day that the gap-filler would. Suppressing it
// there would leave the same hole one path over.
//
// ⚠ UTC EXPLICITLY, on BOTH sides. Comparing in the process's local zone would put the day boundary
// somewhere neither AccountsMissingDailySnapshot nor migration 000194 puts it, which is the class of
// silently-ambiguous day boundary every other date decision on this path is spelled UTC to avoid.
func isRedundantSnapshot(latest, next *domain.BalanceSnapshot, now time.Time, window time.Duration) bool {
	if latest == nil || next == nil {
		return false
	}
	if now.Sub(latest.SnapshotAt) >= window {
		return false
	}
	if !sameUTCDay(latest.SnapshotAt, now) {
		return false
	}
	return sameBalance(latest.CurrentBalance, next.CurrentBalance) &&
		sameBalance(latest.AvailableBalance, next.AvailableBalance)
}

// sameUTCDay reports whether two instants fall on the same UTC calendar day.
//
// Both operands are converted with .UTC() rather than assumed to be in UTC already: every producer
// on this path passes a UTC instant today, but a caller passing a local-zone time would otherwise
// silently move the boundary, and this is the one comparison whose whole point is where the boundary
// is.
func sameUTCDay(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	return ay == by && am == bm && ad == bd
}

// sameBalance compares two optional balances, treating NULL as a distinct value rather than as a
// wildcard.
func sameBalance(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
