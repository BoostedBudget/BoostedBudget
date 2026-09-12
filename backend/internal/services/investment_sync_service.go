package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/cache"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
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
	// TransactionSyncService's convention).
	//
	// ⚠ #1537 CORRECTION: the original rationale here — "GetNetWorthTrend is a cached read that folds
	// in holdings/investment-transaction data" — is FALSE, and the M7 note on bumpCacheForUser below
	// already contradicted it. GetNetWorthTrend reads balance_snapshots ONLY
	// (analytics_service.go GetNetWorthTrend -> balanceSnapshotRepo.GetAggregatedHistory[ForHousehold]),
	// and this service writes holdings / investment_transactions / portfolio_snapshots — none of which
	// any cache.GetOrLoad-backed read observes. The four disposition entries that claimed
	// ExplicitBump coverage from this bump are therefore now dispositionDerivedNoCacheImpact.
	//
	// The bump is KEPT deliberately: over-invalidation costs a cache miss and nothing else, and it is
	// cheap insurance if a future cached read ever touches holdings. It is defence, not coverage — do
	// not cite it as the reason a write is invalidated. Issue #1562 tracks the history.
	//
	// A Plaid HOLDINGS or INVESTMENTS_TRANSACTIONS webhook — routed through ProcessWebhook, which
	// calls SyncHoldings/SyncInvestmentTransactions below — still bumps the synced user once those
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
	userAccountRepo       repository.UserAccountRepository
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
//
// Issue #1531 M7 — deliberately user-scope ONLY, no household bump: unlike
// transaction_sync_service.go's bumpCacheForSync and effective_category_refresh.go's
// bumpCacheForUser (both of which also bump cache.ScopeHousehold), this method does not, because
// this service never writes balance_snapshots — the table household-scoped GetNetWorthTrend reads
// via BalanceSnapshotRepository.GetAggregatedHistoryForHousehold. SyncHoldings/
// SyncInvestmentTransactions write holdings/investment_transactions only; nothing they write is
// visible to any household-scoped cached query today. If a future change makes this service (or
// a repository method it calls) write balance_snapshots, this reasoning no longer holds and a
// household bump must be added here too — see liability_sync_service.go's bumpCacheForUser for
// the identical decision and reasoning on the liability side.
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
	// userAccountRepo carries the investment backfill obligation
	// (user_accounts.investment_backfill_requested_at, #1631 M4). A POSITIONAL dependency, not a
	// SetX setter like analytics and cache invalidation above: those are optional and nil-safe
	// because losing them costs a metric or a cache miss, whereas a nil here would silently
	// abandon every backfill a user asked for. The compiler is the right place to catch a missed
	// wiring for an obligation.
	userAccountRepo repository.UserAccountRepository,
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
		userAccountRepo:       userAccountRepo,
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
		if err == nil && holdingResult != nil {
			switch {
			case len(holdingResult.Errors) > 0:
				// Partial failure: some accounts skipped due to unmapped securities.
				// Existing holdings are preserved but not updated — trigger retry.
				err = fmt.Errorf("holdings sync partial failure: %v", holdingResult.Errors)
			case len(holdingResult.SkippedIncomplete) > 0:
				// Issue #1569 Task 4: some accounts were absent from Plaid's response
				// (partial/incomplete), so their clear was skipped to preserve existing
				// holdings. Retry so a transient partial response gets a chance to complete —
				// this does NOT suppress the portfolio snapshot (see the snapshot gate in
				// SyncHoldings, which checks Errors only).
				err = fmt.Errorf("holdings sync incomplete: %v", holdingResult.SkippedIncomplete)
			}
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

// describeHoldingDuplicateDifferences lists, field by field, how a DISCARDED duplicate holding
// disagreed with the row kept for its (account_id, security_id) pair. An empty result means the
// two rows were identical, which is the likely real case — a provider repeating a row — and makes
// discarding one a lossless no-op that is still visible in the logs.
//
// Why this function exists at all, instead of arithmetic (#1639, correcting #1631's first attempt):
// a merge that summed the two rows produced a number no reader could trace back to anything Plaid
// sent, and it silently doubled the position. Naming the contradiction leaves it a contradiction.
//
// A nil is ABSENT, not zero, and absent-vs-known is reported as a difference rather than treated as
// a match: "Plaid reported no cost basis here and $600 there" is exactly the disagreement that must
// not disappear. Values are compared by what they point AT, never by pointer identity.
func describeHoldingDuplicateDifferences(kept, discarded domain.Holding) []string {
	diffs := make([]string, 0, 5)
	if kept.Quantity != discarded.Quantity {
		diffs = append(diffs, "quantity: kept "+formatFloatForLog(kept.Quantity)+
			", discarded "+formatFloatForLog(discarded.Quantity))
	}
	for _, f := range []struct {
		name            string
		kept, discarded *float64
	}{
		{"institution_value", kept.InstitutionValue, discarded.InstitutionValue},
		{"institution_price", kept.InstitutionPrice, discarded.InstitutionPrice},
		{"cost_basis", kept.CostBasis, discarded.CostBasis},
	} {
		if !optionalFloatsEqual(f.kept, f.discarded) {
			diffs = append(diffs, f.name+": kept "+formatOptionalFloatForLog(f.kept)+
				", discarded "+formatOptionalFloatForLog(f.discarded))
		}
	}
	if !optionalStringsEqual(kept.CurrencyCode, discarded.CurrencyCode) {
		diffs = append(diffs, "currency_code: kept "+formatOptionalStringForLog(kept.CurrencyCode)+
			", discarded "+formatOptionalStringForLog(discarded.CurrencyCode))
	}
	return diffs
}

func optionalFloatsEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func optionalStringsEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// formatFloatForLog renders an amount the way a human reads it back out of a log line: 1650, not
// 1.65e+03 and not 1650.000000.
func formatFloatForLog(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func formatOptionalFloatForLog(v *float64) string {
	if v == nil {
		return "absent"
	}
	return formatFloatForLog(*v)
}

func formatOptionalStringForLog(v *string) string {
	if v == nil {
		return "absent"
	}
	return *v
}

// SyncHoldings fetches the complete holdings snapshot from Plaid and atomically replaces holdings
// for accounts classified as investments. Since Plaid returns a full snapshot (no delta), eligible
// accounts delete existing holdings and insert fresh ones inside a single DB transaction.
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
	domainSecurities := mapPlaidSecuritiesToDomain(resp.Securities, s.logger)
	securityMap, err := s.securityRepo.UpsertBatch(ctx, domainSecurities)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert securities: %w", err)
	}

	// Build plaid_account_id → internal account ID and user_account_id maps from our DB.
	// userAccountMap is used to set user_account_id on each holding at insert time
	// so the stable chain-expansion key is populated (Issue #1109).
	//
	// #1569 round 12 task 5: a round-11 commit message (4589ac99) called this
	// GetByLinkedItemID call "the tenancy guard" — that framing is false. This query filters
	// only linked_item_id and status='active'; it carries no user_id predicate at all. The
	// real cross-user protection is the explicit `if a.UserID != linkedItem.UserID` check a
	// few lines below (~:282-290), with a twin in SyncInvestmentTransactions (~:517-525).
	// Harmless in effect either way — the real guard runs regardless of which fetch produced
	// the row — but the description was wrong.
	accounts, err := s.accountRepo.GetByLinkedItemID(ctx, linkedItem.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts for linked item: %w", err)
	}
	accountMap := make(map[string]string, len(accounts))
	holdingUserAccountMap := make(map[string]*string, len(accounts))
	nonInvestmentAccountIDs := make(map[string]struct{}, len(accounts))
	accountsSkippedNonInvestment := 0
	for _, a := range accounts {
		// #1537 (F14): the (actor, account) pair is assembled from two sources that nothing forces to
		// agree — UserID comes from linkedItem.UserID below, while these accounts come from
		// GetByLinkedItemID, which is keyed on linked_item_id and is NOT user-scoped. If
		// accounts.user_id ever diverged from linked_items.user_id, every holding would carry a
		// self-consistent-looking pair: assertActorOwnsRows would see the right actor on every row
		// and assertRowsTargetAccount would see the right account on every row, so BOTH repository
		// assertions pass while the write lands on someone else's account. Rejecting it here, where
		// the two values first meet, is the only place either fact is available.
		if a.UserID != linkedItem.UserID {
			s.logger.Error("skipping an account that does not belong to the linked item's owner",
				"linked_item_id", linkedItem.ID,
				"account_id", a.ID,
				"account_user_id", a.UserID,
				"linked_item_user_id", linkedItem.UserID,
			)
			continue
		}
		// Issues #1640 and #1616: account type—not exclude_investment_sync—defines holdings
		// eligibility. Skipping here preserves legacy rows and keeps inapplicable accounts out of
		// completeness checks. The retention ADR records why the transaction preference cannot apply.
		if a.Type != "investment" {
			nonInvestmentAccountIDs[a.PlaidAccountID] = struct{}{}
			accountsSkippedNonInvestment++
			continue
		}
		accountMap[a.PlaidAccountID] = a.ID
		holdingUserAccountMap[a.PlaidAccountID] = a.UserAccountID
	}

	// Group Plaid holdings by plaid_account_id for efficient lookup
	holdingsByAccount := make(map[string][]plaid.Holding, len(accounts))
	holdingsSkippedNonInvestment := 0
	for _, h := range resp.Holdings {
		if _, skipped := nonInvestmentAccountIDs[h.AccountId]; skipped {
			holdingsSkippedNonInvestment++
			continue
		}
		holdingsByAccount[h.AccountId] = append(holdingsByAccount[h.AccountId], h)
	}

	result := &domain.HoldingSyncResult{
		SecuritiesUpserted:           len(domainSecurities),
		AccountsSkippedNonInvestment: accountsSkippedNonInvestment,
		HoldingsSkippedNonInvestment: holdingsSkippedNonInvestment,
	}

	// Issue #1569 Task 4: the only per-account completeness signal Plaid's
	// /investments/holdings/get response exposes is presence in `accounts` (see
	// plaid_completeness.go — the same reasoning as the liability sync applies here: no
	// account-level error/status field, and item.error is documented as populated only by
	// /item/get). An account absent from this set was not returned by Plaid on this call at all;
	// zero holdings for it below is then a partial/degraded response, not a confirmed liquidation.
	// (Task 5 reshaped the return value into PlaidAccountRoster — see plaid_completeness.go. This
	// gate is unchanged: Present is the same plain membership test the bare set gave.)
	roster := PlaidAccountRosterFrom(resp.Accounts, nil)

	// The UTC day the row-level captures filed under, read from the SERVER by
	// ReplaceForAccount. Zero until the first account is replaced — see the fallback below.
	var latestCapturedDay time.Time

	// For each internal account, replace holdings atomically
	for plaidAccountID, internalAccountID := range accountMap {
		plaidHoldings := holdingsByAccount[plaidAccountID] // empty if liquidated OR response is partial for this account

		if len(plaidHoldings) == 0 {
			if !roster.Present(plaidAccountID) {
				observability.PlaidWipeSkippedIncompleteResponse.WithLabelValues("holdings").Inc()
				s.logger.Warn("skipping holdings clear: account absent from Plaid's holdings response, preserving existing rows",
					"account_id", internalAccountID,
					"item_id", linkedItem.ID,
				)
				// Issue #1569 Task 4 fix round 1: this MUST be its own field, not result.Errors.
				// Errors gates the portfolio snapshot upsert below — an account Plaid has
				// permanently stopped returning (closed accounts are never re-reported, per
				// AccountBase.AccountId's doc comment) would otherwise suppress this user's
				// snapshot forever, even though the preserved holdings are exactly what the
				// snapshot should be summing. SkippedIncomplete still drives the ProcessWebhook
				// retry decision below without touching the snapshot gate.
				result.SkippedIncomplete = append(result.SkippedIncomplete, fmt.Sprintf("account %s: skipped holdings clear, account absent from Plaid's response (partial/incomplete)", plaidAccountID))
				continue
			}
		}

		domainHoldings := make([]domain.Holding, 0, len(plaidHoldings))
		// Production DLQ 2026-09-11 (dead_letter_jobs.id 9 = river_job.id 350,
		// webhook_processing): holdings is
		// UNIQUE (account_id, security_id), so a payload carrying one security twice for one
		// account cannot be written AT ALL — every attempt fails identically until the job dead-
		// letters, and the account's holdings stop syncing entirely. Two payloads produce that:
		// Plaid listing the security twice for the account, and two distinct plaid_security_ids
		// resolving to ONE securities row (the forked-identity problem #1631's consolidation
		// columns exist for). Both arrive here as two elements with the same internal SecurityID,
		// so the FIRST is kept and the rest are discarded on that key — and the log names every
		// plaid_security_id involved, so the two remain distinguishable after the fact instead of
		// being flattened into "some duplicate".
		//
		// ⚠ #1639 corrects this: the duplicates used to be SUMMED, on the theory that two rows were
		// two lots of one position. Plaid's own SDK says they are not (plaid/model_holding.go):
		// Quantity is "the total quantity of the asset held" and CostBasis "the total cost basis of
		// the holding (e.g., the total amount spent to acquire all assets currently in the
		// holding)". One Holding row IS the complete current position for the pair — which is why
		// the table is unique on it. Two rows are therefore the same position stated twice, and
		// summing them doubled the user's holdings, every portfolio total over them and every
		// allocation percentage derived from them, silently.
		//
		// So: pick one row, whole, and do NO arithmetic across duplicates. First-wins rather than
		// refusing the account outright because refusing returns us to the dead-lettered job this
		// fix exists to end, and because picking is bounded-wrong (the stored figure is one of the
		// two the provider actually sent) where summing is unbounded-wrong (the stored figure is
		// one the provider never sent, and grows with every repetition). It is a deliberate trade,
		// not laziness: the row we did not keep is named in full in the log below.
		//
		// Two consequences fall out of taking one row WHOLE, rather than being handled separately:
		// a figure and the currency_code it is denominated in always travel together, so no sum of
		// dollars and euros can be labelled "USD" (which is what defeated is_mixed_currency
		// detection — it inspects persisted holding currency codes only); and an ABSENT cost basis
		// stays absent rather than adopting the duplicate's figure as a complete total and
		// fabricating an unrealized gain from it.
		//
		// None of this is the fix for a concurrent sync racing this one — that one is fixed where
		// it lives, by the per-account advisory lock in holdingRepository.ReplaceForAccount. This
		// only makes a self-inconsistent PAYLOAD writable, and says so out loud when it does.
		indexBySecurity := make(map[string]int, len(plaidHoldings))
		plaidIDsBySecurity := make(map[string][]string, len(plaidHoldings))
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
			// #1631 M8a: the currency of the three figures just assigned. Read from the HOLDING,
			// not from the security: Plaid prices a foreign-currency security in USD while the
			// institution reports the position in its own currency, so these are two different
			// facts and conflating them would hide the exact mismatch this records.
			h.CurrencyCode = plaidCurrencyCode(ph.IsoCurrencyCode.Get(), ph.UnofficialCurrencyCode.Get(),
				s.logger, "holding of security "+ph.SecurityId+" in account "+plaidAccountID)

			if cb := ph.CostBasis.Get(); cb != nil {
				val := *cb
				h.CostBasis = &val
			}

			plaidIDsBySecurity[internalSecurityID] = append(plaidIDsBySecurity[internalSecurityID], ph.SecurityId)

			if existing, seen := indexBySecurity[internalSecurityID]; seen {
				rowsSeen := len(plaidIDsBySecurity[internalSecurityID])
				s.logger.Warn("discarded a duplicate holding: each Plaid holding row is the COMPLETE "+
					"position for its (account, security), so the first is kept whole and the rest are "+
					"dropped without arithmetic",
					"account_id", internalAccountID,
					"plaid_account_id", plaidAccountID,
					"security_id", internalSecurityID,
					"plaid_security_id", ph.SecurityId,
					"plaid_security_ids_for_this_security", plaidIDsBySecurity[internalSecurityID],
					"rows_seen_for_this_security", rowsSeen,
					"rows_discarded_for_this_security", rowsSeen-1,
					"plaid_holdings_in_payload", len(plaidHoldings),
					// Empty when the discarded row was identical to the kept one — the likely real
					// case, and then this whole line records a lossless no-op. Non-empty means the
					// provider contradicted itself, and names both values so it reads as the
					// contradiction it is instead of disappearing into an untraceable number.
					"differences", describeHoldingDuplicateDifferences(domainHoldings[existing], h),
					"item_id", linkedItem.ID,
				)
				continue
			}

			indexBySecurity[internalSecurityID] = len(domainHoldings)
			domainHoldings = append(domainHoldings, h)
		}

		if skippedAny {
			result.Errors = append(result.Errors, fmt.Sprintf("account %s: skipped replace due to unmapped security", plaidAccountID))
			continue
		}

		capturedDay, err := s.holdingRepo.ReplaceForAccount(ctx, internalAccountID, linkedItem.UserID, domainHoldings)
		if err != nil {
			// Production DLQ 2026-09-11: five identical attempts dead-lettered carrying only the
			// constraint name, with nothing in the logs about the payload behind them. The two
			// counts are what make a repeat diagnosable without a production query — a payload
			// count above the row count means duplicates were merged, and equal counts mean the
			// collision came from rows already in the table.
			s.logger.Error("holdings replace failed",
				"item_id", linkedItem.ID,
				"account_id", internalAccountID,
				"plaid_account_id", plaidAccountID,
				"plaid_holdings_in_payload", len(plaidHoldings),
				"holdings_rows_written", len(domainHoldings),
				"distinct_securities", len(indexBySecurity),
				"error", err,
			)
			return result, fmt.Errorf("failed to replace holdings for account %s: %w", internalAccountID, err)
		}
		// #1569 T9.1 item C: the aggregate below must be filed under the day the ROWS were filed
		// under, and that day comes from Postgres. Keeping the LATEST is the choice for an item
		// whose accounts straddle UTC midnight: both days then have partial coverage — which the
		// repair reports honestly rather than silently rebuilding — and the aggregate names the
		// more recent state, which is the one the next sync and the daily whole-user capture will
		// complete.
		if capturedDay.After(latestCapturedDay) {
			latestCapturedDay = capturedDay
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
		"accounts_skipped_non_investment", result.AccountsSkippedNonInvestment,
		"holdings_skipped_non_investment", result.HoldingsSkippedNonInvestment,
	)

	if s.analytics != nil {
		s.analytics.Capture(linkedItem.UserID, phclient.EventInvestmentHoldingsSynced, map[string]interface{}{
			"securities_upserted":             result.SecuritiesUpserted,
			"holdings_replaced":               result.HoldingsReplaced,
			"accounts_processed":              result.AccountsProcessed,
			"accounts_skipped_non_investment": result.AccountsSkippedNonInvestment,
			"holdings_skipped_non_investment": result.HoldingsSkippedNonInvestment,
		})
	}

	// Issue #138: Upsert portfolio snapshot from committed holdings state.
	// Skip when accounts were skipped due to a real correctness problem — result.Errors, i.e.
	// unmapped securities — a partial sync in that sense must not become a day-change baseline.
	// Each per-item webhook recomputes the full-user aggregate via aggregate-at-write SQL, so
	// concurrent multi-institution syncs converge to the correct total.
	//
	// Deliberately NOT gated on result.SkippedIncomplete (Issue #1569 Task 4 fix round 1): an
	// account Plaid has permanently stopped returning is a stable state, not a repeated failure,
	// and its holdings were correctly PRESERVED, not lost — they are exactly the rows this
	// snapshot should be summing. Gating on SkippedIncomplete would suppress the snapshot forever
	// for that account instead of just for one retriable sync.
	if len(result.Errors) == 0 && s.portfolioSnapshotRepo != nil {
		// #1569 T9.1 item C: take the day from the capture, never from a second clock.
		//
		// The Go process and Postgres keep independent clocks, so a run that crosses UTC midnight
		// while the other side has not files the holding_snapshots rows under day D and this
		// aggregate under D+1 — an aggregate with nothing behind it, and a day of rows no
		// aggregate names. portfolio_snapshot_repair.go excludes the current server day, so that
		// orphan survives the next repair pass and is later refused for lack of coverage.
		// portfolio_snapshot_worker.go already takes its day from CaptureForUser's return for
		// exactly this reason; this path was the outlier.
		//
		// ⚠ The Go clock survives only as the no-capture fallback, and that case is not the bug:
		// nothing was written this run, so there is no row-level day to agree with. Every path
		// that DID write rows uses theirs. Do not "fix" the fallback by comparing the two clocks —
		// the recorded skew between this host and the container is about a millisecond, so a
		// comparison would be measuring noise.
		snapshotDate := latestCapturedDay
		if snapshotDate.IsZero() {
			snapshotDate = time.Now().UTC().Truncate(24 * time.Hour)
		}
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
//
// Start date, in precedence order:
//  1. the FULL lookback, if any chain on this item carries an outstanding backfill obligation
//     (user_accounts.investment_backfill_requested_at — the owner re-included a previously
//     excluded chain and we owe it history we never stored);
//  2. investments_last_synced_date - 1 day (safety overlap), on an incremental sync;
//  3. the full lookback, on a first sync.
//
// investments_last_synced_date advances ONLY when the run stored every row Plaid delivered — see
// domain.InvestmentTransactionSyncResult.AnyTransactionDropped.
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

	// Build account maps for transaction → account ID and user_account_id resolution.
	// userAccountMap carries the stable user_account_id (survives Plaid re-links)
	// so investment transactions written here have the chain-expansion key set on
	// insert (mirrors the banking UpsertBatch approach — Issue #1109).
	//
	// ⚠ This block runs BEFORE the start date is chosen (#1631 M4), which is a change from the
	// original ordering: the backfill obligation is a property of the ACCOUNTS on this item, so the
	// window cannot be decided until they are known.
	//
	// Issue #1569 round 11 task 1: this MUST be GetByLinkedItemIDIncludingRetained, not
	// GetByLinkedItemID. Unlike SyncHoldings above (which wipes-and-reinserts via
	// ReplaceForAccount and must stay active-only), investment transactions are written by
	// investmentTransactionRepository.UpsertBatch — a pure INSERT ... ON CONFLICT DO UPDATE,
	// identical in shape to the banking path fixed by task 1b
	// (transaction_sync_service.go:915). An active-only map here means a retained
	// (removed/expired/replaced) brokerage account inside a still-live linked item drops out
	// of accountMap, and mapPlaidInvestmentTransactions Warn+skips every transaction Plaid
	// delivers for it, forever. See TestInvestmentSyncService_SyncInvestmentTransactions_RetainedAccount_StillStored.
	accounts, err := s.accountRepo.GetByLinkedItemIDIncludingRetained(ctx, linkedItem.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accounts for linked item: %w", err)
	}
	accountMap := make(map[string]string, len(accounts))
	userAccountMap := make(map[string]*string, len(accounts))
	// excludedPlaidAccountIDs is a SEPARATE set rather than an absence from accountMap, because
	// absence already means something else and something worse. See mapPlaidInvestmentTransactions.
	excludedPlaidAccountIDs := make(map[string]struct{})
	// includedChainIDs is ordered and de-duplicated: it becomes a query parameter, and a stable
	// order keeps the SQL, the logs and the tests deterministic when an item carries several
	// accounts on one chain (which a re-link chain routinely does).
	var includedChainIDs []string
	seenChain := make(map[string]struct{})
	for _, a := range accounts {
		// #1537 (F14): same guard as SyncHoldings above — see that comment for why the repository
		// assertions cannot catch this. These rows go to UpsertBatch carrying linkedItem.UserID.
		if a.UserID != linkedItem.UserID {
			s.logger.Error("skipping an account that does not belong to the linked item's owner",
				"linked_item_id", linkedItem.ID,
				"account_id", a.ID,
				"account_user_id", a.UserID,
				"linked_item_user_id", linkedItem.UserID,
			)
			continue
		}
		if a.ExcludeInvestmentSync {
			// #1631 M4: the owner asked us not to ingest this chain's investment activity, because
			// Plaid reports the same brokerage cash movement through /transactions/sync as well and
			// storing both double-counts it. Logged at INFO with an explicit "deliberately
			// excluded" phrasing: the Warn in mapPlaidInvestmentTransactions means "we lost an
			// account we should have had", and the two must not be confusable at 3am.
			s.logger.Info("deliberately excluded account from investment transaction ingestion "+
				"(user set exclude_investment_sync on its chain); this is not a lost account",
				"linked_item_id", linkedItem.ID,
				"account_id", a.ID,
				"user_account_id", a.UserAccountID,
			)
			excludedPlaidAccountIDs[a.PlaidAccountID] = struct{}{}
			continue
		}
		accountMap[a.PlaidAccountID] = a.ID
		userAccountMap[a.PlaidAccountID] = a.UserAccountID
		if a.UserAccountID != nil {
			if _, seen := seenChain[*a.UserAccountID]; !seen {
				seenChain[*a.UserAccountID] = struct{}{}
				includedChainIDs = append(includedChainIDs, *a.UserAccountID)
			}
		}
	}

	result := &domain.InvestmentTransactionSyncResult{}

	// Consume any outstanding backfill obligation. Asking only about the INCLUDED chains is the
	// point: a chain that is still excluded owes nothing, and honouring a marker on it would
	// re-fetch history we are about to refuse to store anyway.
	//
	// ⚠ This also resolves, without having to choose, which linked item a backfill belongs to. A
	// chain spans re-links, so user_account → linked_item is not one-to-one; because the obligation
	// lives on the chain row, whichever live item currently carries that account discharges it.
	//
	// ⚠ linkedItem.UserID is passed so the SQL, not this loop, enforces the tenant boundary. The
	// account-owner guard above checks each ACCOUNT's user_id and says nothing about the owner of
	// the CHAIN it points at, so one mis-assigned accounts.user_account_id would otherwise let this
	// sync read — and, worse, discharge below — another user's obligation. A discharged obligation
	// is invisible to every read, so it is the one failure here nothing downstream would reveal.
	pendingBackfills, err := s.userAccountRepo.PendingInvestmentBackfills(ctx, includedChainIDs, linkedItem.UserID)
	if err != nil {
		// Non-fatal, and deliberately so: an unreadable marker must not stop the ordinary
		// incremental sync for every OTHER account on this item. The obligation is durable on the
		// row, so the next run retries it — that is the whole reason it is not a queued job.
		s.logger.Warn("failed to read investment backfill obligations; continuing with the "+
			"ordinary window (the obligation stays on the row and will be retried)",
			"linked_item_id", linkedItem.ID,
			"error", err,
		)
		pendingBackfills = nil
	}
	var backfillChainIDs []string
	var newestBackfillMarker time.Time
	for _, id := range includedChainIDs {
		requestedAt, owed := pendingBackfills[id]
		if !owed {
			continue
		}
		backfillChainIDs = append(backfillChainIDs, id)
		if requestedAt.After(newestBackfillMarker) {
			newestBackfillMarker = requestedAt
		}
	}
	result.BackfilledChains = len(backfillChainIDs)

	var startDate string
	switch {
	case len(backfillChainIDs) > 0:
		// The obligation outranks the watermark: the excluded period is precisely the period the
		// incremental window does NOT cover.
		startDate = now.AddDate(-investmentTxnLookbackYears, 0, 0).Format("2006-01-02")
		s.logger.Info("investment transaction backfill: widening the window to the full lookback "+
			"because a chain was re-included after being excluded",
			"item_id", linkedItem.ID,
			"start_date", startDate,
			"lookback_years", investmentTxnLookbackYears,
			"chains", len(backfillChainIDs),
		)
	case linkedItem.InvestmentsLastSyncedDate != nil:
		// Incremental sync: overlap by 1 day so we don't miss transactions posted
		// after the previous sync's cutoff (unique constraint ensures idempotency).
		startDate = linkedItem.InvestmentsLastSyncedDate.AddDate(0, 0, -investmentTxnOverlapDays).Format("2006-01-02")
		s.logger.Info("incremental investment transaction sync",
			"item_id", linkedItem.ID,
			"start_date", startDate,
		)
	default:
		// First sync: 2-year lookback for full history
		startDate = now.AddDate(-investmentTxnLookbackYears, 0, 0).Format("2006-01-02")
		s.logger.Info("first investment transaction sync, using lookback",
			"item_id", linkedItem.ID,
			"start_date", startDate,
			"lookback_years", investmentTxnLookbackYears,
		)
	}

	var offset int32
	const pageSize int32 = investmentTxnPageSize

	for {
		resp, err := s.plaidService.InvestmentsTransactionsGet(ctx, accessToken, startDate, endDate, offset, pageSize)
		if err != nil {
			// Return partial results — caller can retry; date NOT updated so next retry covers same range
			return result, fmt.Errorf("failed to fetch investment transactions at offset %d: %w", offset, err)
		}

		result.PagesProcessed++

		// Upsert securities for this page, building plaid_security_id → UUID map.
		//
		// ⚠ Securities are upserted for EXCLUDED accounts too, and that is deliberate (#1631 M4):
		// securities are shared reference data keyed by plaid_security_id, not per-account history,
		// the write is cheap and idempotent, and a security a sibling account also holds must not
		// go missing because some other account on the item is excluded.
		securityMap := make(map[string]string)
		if len(resp.Securities) > 0 {
			domainSecurities := mapPlaidSecuritiesToDomain(resp.Securities, s.logger)
			securityMap, err = s.securityRepo.UpsertBatch(ctx, domainSecurities)
			if err != nil {
				return result, fmt.Errorf("failed to upsert securities on page %d: %w", result.PagesProcessed, err)
			}
			result.SecuritiesUpserted += len(securityMap)
		}

		// Map and upsert investment transactions for this page
		domainTxns, drops := mapPlaidInvestmentTransactions(
			resp.InvestmentTransactions, linkedItem.UserID,
			accountMap, userAccountMap, excludedPlaidAccountIDs, securityMap, s.logger,
		)
		result.TransactionsExcluded += drops.excluded
		result.TransactionsDroppedUnmappedAccount += drops.unmappedAccount
		result.TransactionsDroppedUnparsableDate += drops.unparsableDate
		result.TransactionsDroppedUnresolvedSecurity += drops.unresolvedSecurity
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

	// #1631 (M7, merged into M4): the watermark is a promise that everything before it is STORED.
	// It used to advance on "no page returned an HTTP error", which let a dropped row become
	// permanent — the next run starts at lastSynced - 1 day and never asks for that date again.
	// A drop is now a reason to leave the cursor where it is, so the very next run re-fetches the
	// same window and gets another chance once the missing account or security shows up.
	//
	// ⚠ The cost, stated honestly: a PERMANENTLY undroppable row (a date Plaid will always send
	// malformed) pins the cursor indefinitely, and every run then re-fetches the whole window. That
	// is a repeated cost, not a loss — the upserts are idempotent — and the Dropped* counters in
	// the completion log below name the offending reason so it can be fixed rather than tolerated.
	if result.AnyTransactionDropped() {
		s.logger.Warn("NOT advancing investments_last_synced_date: this run failed to store rows "+
			"Plaid delivered, and advancing the cursor past them would make the loss permanent",
			"item_id", linkedItem.ID,
			"dropped_unmapped_account", result.TransactionsDroppedUnmappedAccount,
			"dropped_unparsable_date", result.TransactionsDroppedUnparsableDate,
			"dropped_unresolved_security", result.TransactionsDroppedUnresolvedSecurity,
		)
	} else {
		// Update investments_last_synced_date on a run that stored everything it was given.
		// Truncate now to midnight UTC (endDate already in this format).
		//
		// ⚠ An EXCLUDED row does not block this. The window was fully processed; the owner asked us
		// not to store part of it. See AnyTransactionDropped's doc comment.
		today := now.Truncate(24 * time.Hour)
		if err := s.linkedItemRepo.UpdateInvestmentsLastSyncedDate(ctx, linkedItem.ID, today); err != nil {
			// Non-fatal: data is stored; log warning and continue
			s.logger.Warn("failed to update investments_last_synced_date",
				"item_id", linkedItem.ID,
				"error", err,
			)
		}

		// Discharge the backfill obligation, and ONLY here — inside the same "stored everything"
		// branch as the watermark. A lossy run did not finish the backfill, so the marker must
		// survive for the next run. That is also what makes the obligation recoverable across a
		// crash: nothing but a clean run clears it.
		if len(backfillChainIDs) > 0 {
			if err := s.userAccountRepo.ClearInvestmentBackfillRequests(ctx, backfillChainIDs, linkedItem.UserID, newestBackfillMarker); err != nil {
				// Non-fatal: the backfill itself succeeded and its rows are committed. The
				// surviving marker costs one more full-lookback re-fetch, whose upserts are
				// idempotent — strictly better than the alternative failure mode of clearing an
				// obligation that was never met.
				s.logger.Warn("investment backfill completed but its obligation could not be "+
					"cleared; the next sync will redo it (idempotently)",
					"item_id", linkedItem.ID,
					"chains", len(backfillChainIDs),
					"error", err,
				)
			}
		}
	}

	s.logger.Info("investment transactions sync complete",
		"item_id", linkedItem.ID,
		"pages", result.PagesProcessed,
		"inserted", result.TransactionsInserted,
		"skipped", result.TransactionsSkipped,
		"excluded", result.TransactionsExcluded,
		"backfilled_chains", result.BackfilledChains,
		"dropped_unmapped_account", result.TransactionsDroppedUnmappedAccount,
		"dropped_unparsable_date", result.TransactionsDroppedUnparsableDate,
		"dropped_unresolved_security", result.TransactionsDroppedUnresolvedSecurity,
		"securities_upserted", result.SecuritiesUpserted,
	)

	if s.analytics != nil {
		s.analytics.Capture(linkedItem.UserID, phclient.EventInvestmentTransactionsSynced, map[string]interface{}{
			"pages_processed":                          result.PagesProcessed,
			"transactions_inserted":                    result.TransactionsInserted,
			"transactions_skipped":                     result.TransactionsSkipped,
			"transactions_excluded":                    result.TransactionsExcluded,
			"backfilled_chains":                        result.BackfilledChains,
			"transactions_dropped_unmapped_account":    result.TransactionsDroppedUnmappedAccount,
			"transactions_dropped_unparsable_date":     result.TransactionsDroppedUnparsableDate,
			"transactions_dropped_unresolved_security": result.TransactionsDroppedUnresolvedSecurity,
		})
	}

	return result, nil
}

// ============================================================================
// Plaid → Domain Mapping Helpers
// ============================================================================

// mapPlaidSecuritiesToDomain converts Plaid SDK security objects to domain.Security.
func mapPlaidSecuritiesToDomain(plaidSecs []plaid.Security, logger *slog.Logger) []domain.Security {
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
		sec.InstitutionSecurityID = ps.InstitutionSecurityId.Get()
		sec.ProxySecurityID = ps.ProxySecurityId.Get()
		sec.ClosePrice = ps.ClosePrice.Get()
		sec.ClosePriceAsOf = parsePlaidDate(ps.ClosePriceAsOf.Get())
		sec.CurrencyCode = plaidCurrencyCode(ps.IsoCurrencyCode.Get(), ps.UnofficialCurrencyCode.Get(),
			logger, "security "+ps.SecurityId)
		sec.Name = synthesizeSecurityName(sec.Name, sec.TickerSymbol, ps.SecurityId)
		result = append(result, sec)
	}
	return result
}

// synthesizeSecurityName fills in a security's Name when Plaid sends a null or blank one (#1631).
//
// Root cause: Security.Validate()'s first rule is "name is required", but Validate() is never
// called on this sync path (securityRepository.UpsertBatch does not validate either), and the
// `name VARCHAR(500) NOT NULL` column constraint is satisfied by an empty string — so nothing
// caught it, and production has securities with name = "".
//
// Synthesising is deliberate, not "call Validate() and skip": refusing the row would drop a
// security that an investment transaction already references, and UpsertBatch's ON CONFLICT never
// repairs `security_id` on a later sync (the same trap noted on parsePlaidDate above). A named
// fallback keeps the row resolvable while still being honest that this is a synthesised label,
// not provider data.
func synthesizeSecurityName(name string, tickerSymbol *string, plaidSecurityID string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	if tickerSymbol != nil {
		if trimmed := strings.TrimSpace(*tickerSymbol); trimmed != "" {
			return trimmed
		}
	}
	return "Security " + plaidSecurityID
}

// plaidCurrencyCode collapses Plaid's two currency fields into the one column this schema keeps
// (#1631 M8a).
//
// Plaid guarantees exactly one of them is non-null: iso_currency_code carries an ISO-4217 code,
// and unofficial_currency_code carries a code for a currency that HAS no ISO code —
// cryptocurrencies and the currencies of certain countries. Both answer the same question ("what
// unit is this figure in"), and an aggregate only needs the currency IDENTITY to compare, so
// storing them in two columns would double the threading for no reader.
//
// ⚠ The unofficial code is taken rather than dropped, which is where this departs from
// accounts.currency_code (account_service.go reads iso_currency_code only). Dropping it would
// record a crypto position as having NO currency, and NULL is defined to mean "unknown, does not
// conflict with anything" — so a Bitcoin holding would have silently joined a USD portfolio's sum
// as though it were dollars. That is the precise failure this whole task exists to stop.
//
// Returns nil when Plaid sends neither, or sends a blank one: absent is recorded as absent.
//
// #1631 I5: currency_code is VARCHAR(16) (migration 000202) and Postgres HARD-ERRORS (22001) on a
// value that overflows it rather than truncating. Before this, a Plaid unofficial_currency_code
// longer than 16 bytes turned into "failed to upsert securities on page N", aborting the whole
// UpsertBatch page — the watermark never advanced, and that item's investment sync stayed broken
// on every subsequent run until code changed. logger and label are for that one Warn only; label
// identifies which row hit the limit (a Plaid security/account/transaction id) for triage.
//
// It drops only the FIELD, never the record — the same principle parsePlaidDate states two
// functions down for its own optional field.
const maxCurrencyCodeLen = 16

func plaidCurrencyCode(iso, unofficial *string, logger *slog.Logger, label string) *string {
	for _, candidate := range []*string{iso, unofficial} {
		if candidate == nil {
			continue
		}
		trimmed := strings.TrimSpace(*candidate)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > maxCurrencyCodeLen {
			if logger != nil {
				logger.Warn("dropping currency_code: exceeds the column's 16-byte limit",
					"value", trimmed, "length", len(trimmed), "for", label)
			}
			continue
		}
		return &trimmed
	}
	return nil
}

// parsePlaidDate converts a Plaid date-only string ("YYYY-MM-DD") into a *time.Time, returning nil
// for an absent or unparsable value.
//
// ⚠ It drops only the FIELD, never the record — deliberately unlike the transaction-date handling in
// mapPlaidInvestmentTransactions, which skips the whole row. That asymmetry is the point (#1631):
// close_price_as_of is optional pricing metadata, whereas skipping the record here would write a
// security with no identity, and UpsertBatch's ON CONFLICT never repairs a row afterwards. A
// malformed optional date must not be able to cost us the security.
func parsePlaidDate(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", *s)
	if err != nil {
		return nil
	}
	return &t
}

// investmentTxnDrops counts, BY REASON, the Plaid investment transactions one mapping pass did not
// turn into a domain row.
//
// Separate fields rather than one total (#1631): `excluded` is the owner's instruction being
// carried out and leaves the sync free to advance its watermark, while the other three are losses
// that must pin it. Collapsing them was how a securityMap miss came to be indistinguishable from a
// cash-only transaction for two releases.
type investmentTxnDrops struct {
	excluded           int
	unmappedAccount    int
	unparsableDate     int
	unresolvedSecurity int
}

// mapPlaidInvestmentTransactions converts Plaid investment transactions to domain types,
// resolving account IDs, user_account_ids, and security IDs using the provided maps.
// user_account_id is the stable chain-expansion key (Issue #1109): setting it here on
// insert ensures pre-relink rows remain visible after a Plaid re-link.
//
// It returns the rows to persist AND a per-reason account of what it refused, because every reason
// it refuses for is invisible to the caller otherwise — which is exactly the defect #1631 fixes.
//
// ⚠ excludedPlaidAccountIDs is checked BEFORE accountMap and is not the same thing as being absent
// from it. An excluded account is one the owner told us to ignore (INFO, expected, harmless); an
// account absent from accountMap is one we should have had and do not (WARN, a real fault, and the
// reason the caller then refuses to advance the cursor). Merging them would make a routine
// preference look like a data-integrity incident in production logs, and vice versa.
func mapPlaidInvestmentTransactions(
	plaidTxns []plaid.InvestmentTransaction,
	userID string,
	accountMap map[string]string,
	userAccountMap map[string]*string,
	excludedPlaidAccountIDs map[string]struct{},
	securityMap map[string]string,
	logger *slog.Logger,
) ([]domain.InvestmentTransaction, investmentTxnDrops) {
	result := make([]domain.InvestmentTransaction, 0, len(plaidTxns))
	var drops investmentTxnDrops

	for _, pt := range plaidTxns {
		if _, excluded := excludedPlaidAccountIDs[pt.AccountId]; excluded {
			// No log line per transaction: the account-level INFO in SyncInvestmentTransactions
			// already said this once, and a brokerage can deliver thousands of rows per page.
			drops.excluded++
			continue
		}

		internalAccountID, ok := accountMap[pt.AccountId]
		if !ok {
			drops.unmappedAccount++
			if logger != nil {
				logger.Warn("no internal account for investment transaction, skipping",
					"plaid_account_id", pt.AccountId,
				)
			}
			continue
		}

		date, err := time.Parse("2006-01-02", pt.Date)
		if err != nil {
			drops.unparsableDate++
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

		// Resolve security ID. A genuinely absent security_id means a CASH transaction and is the
		// normal case for a dividend or a fee.
		//
		// ⚠ A security_id Plaid SENT but we could not resolve is a different thing entirely, and
		// until #1631 it fell through this block silently: SecurityID stayed nil, and per the
		// schema comment on investment_transactions.security_id a NULL there MEANS "cash-only".
		// The row was therefore stored as a lie, indistinguishable from a real cash movement, with
		// no warning, no counter and no metric. SyncHoldings refuses to write an account's holdings
		// at all for this same class of failure (an unmapped security) rather than write a wrong
		// row; the ledger path now matches it — the row is refused, counted, and the caller declines
		// to advance the cursor so the next run can retry once the security resolves.
		if plaidSecID := pt.SecurityId.Get(); plaidSecID != nil && *plaidSecID != "" {
			internalSecID, resolved := securityMap[*plaidSecID]
			if !resolved {
				drops.unresolvedSecurity++
				if logger != nil {
					logger.Warn("investment transaction references a security this page did not "+
						"resolve, refusing to store it as cash-only",
						"investment_transaction_id", pt.InvestmentTransactionId,
						"plaid_security_id", *plaidSecID,
					)
				}
				continue
			}
			txn.SecurityID = &internalSecID
			txn.PlaidSecurityID = *plaidSecID
		}

		// Optional fields
		if subtype := string(pt.Subtype); subtype != "" {
			txn.Subtype = &subtype
		}
		txn.Quantity = &pt.Quantity
		txn.Price = &pt.Price
		txn.Fees = pt.Fees.Get()
		txn.CurrencyCode = plaidCurrencyCode(pt.IsoCurrencyCode.Get(), pt.UnofficialCurrencyCode.Get(),
			logger, "investment transaction "+pt.InvestmentTransactionId)

		result = append(result, txn)
	}

	return result, drops
}
