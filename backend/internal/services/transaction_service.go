package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

var (
	// ErrInvalidLimit is returned when the limit parameter is invalid
	ErrInvalidLimit = errors.New("limit must be between 1 and 100")
	// ErrInvalidOffset is returned when the offset parameter is negative
	ErrInvalidOffset = errors.New("offset must be non-negative")
	// ErrInvalidDateRange is returned when start date is after end date
	ErrInvalidDateRange = errors.New("start date must be before or equal to end date")
	// ErrInvalidAmountRange is returned when min_amount is greater than max_amount
	// Issue #529: Amount range filter validation
	ErrInvalidAmountRange = errors.New("min_amount must be less than or equal to max_amount")
	// ErrCursorSortMismatch is returned when a cursor's sort key/dir does not match the current filters.
	ErrCursorSortMismatch = errors.New("cursor sort key/direction does not match filters")
)

const (
	// DefaultLimit is the default number of transactions to return
	DefaultLimit = 50
	// MaxLimit is the maximum number of transactions allowed per request
	MaxLimit = 100
)

// TransactionService handles business logic for transaction operations
type TransactionService interface {
	// ListTransactions retrieves transactions for a user with optional filters and pagination
	// Returns paginated results with metadata (total_count, has_more)
	// Validates input parameters
	ListTransactions(
		ctx context.Context,
		userID string,
		filters domain.TransactionFilters,
		limit int,
		offset int,
	) (*TransactionListResponse, error)

	// ListTransactionsByCursor retrieves transactions using keyset (cursor) pagination.
	// cursor=nil returns the first page. Returns NextCursor=nil when there are no more pages.
	// Validates that the cursor's sort key/direction matches filters.SortBy/SortDir.
	ListTransactionsByCursor(
		ctx context.Context,
		userID string,
		filters domain.TransactionFilters,
		cursor *domain.TransactionCursor,
		limit int,
	) (*TransactionListResponse, error)

	// GetTransactionByID retrieves a transaction by its ID
	// Returns error if transaction doesn't exist or user doesn't own it
	GetTransactionByID(ctx context.Context, userID, transactionID string) (*domain.Transaction, error)

	// UpdateTransactionTags updates the tags on a transaction
	// Returns the updated transaction
	UpdateTransactionTags(ctx context.Context, userID, transactionID string, tags []string) (*domain.Transaction, error)

	// UpdateTransactionCategory updates the category override on a transaction using the PFC v2 discriminated union.
	// overrideKind must be "pfc", "custom", or "clear".
	UpdateTransactionCategory(ctx context.Context, userID, transactionID string, overrideKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string) (*domain.Transaction, error)

	// GetDistinctPlaidCategories returns raw `personal_finance_category->>'primary'` enums
	// observed in user transactions (e.g., "FOOD_AND_DRINK"). NOT a legacy Category[0] source.
	// DEPRECATED: Consumers should prefer pfc.AllPrimaries() for the curated list.
	// This method is kept for backward compatibility until confirmed no external consumers remain.
	GetDistinctPlaidCategories(ctx context.Context, userID string) ([]string, error)

	// ListByBudgetCategory returns transactions matching a budget category with PFC display enrichment.
	ListByBudgetCategory(ctx context.Context, userID string, budgetCategory *domain.BudgetCategory, startDate, endDate time.Time, limit, offset int) ([]domain.Transaction, int, float64, error)

	// SetReviewed marks the given transactions reviewed/unreviewed for the user
	// (Issue #1203). Returns the number of transactions updated.
	SetReviewed(ctx context.Context, userID string, transactionIDs []string, reviewed bool) (int64, error)

	// BulkUpdateTransactionCategory recategorizes multiple transactions for a user (Issue #1210).
	// Best-effort: unowned/unknown IDs are silently ignored.
	// Returns the number of rows actually updated.
	BulkUpdateTransactionCategory(ctx context.Context, userID string, transactionIDs []string, kind string, pfcPrimary, pfcDetailed, customCategoryID *string) (int64, error)

	// GetReviewSummary returns pre-computed review-state counts and top unreviewed categories (Issue #1210).
	// Dates in filters are optional — omitting them returns all-time data (transfers included).
	GetReviewSummary(ctx context.Context, userID string, filters domain.ReviewSummaryFilters) (*domain.ReviewSummaryResponse, error)

	// SetAnalytics sets the PostHog analytics client for event capture (Issue #1210).
	// Nil-safe: calling Capture on nil analytics is a no-op.
	SetAnalytics(analytics phclient.Analytics)

	// SetAccountRepository wires in the account repository so the service can populate
	// ActivityItem.Account for investment rows in the unified feed (#1329).
	// Nil-safe: if not called, enrichment is silently skipped.
	SetAccountRepository(accountRepo repository.AccountRepository)
}

// TransactionListResponse contains paginated transaction results with metadata.
// The offset-based path populates TotalCount, Offset, and HasMore.
// The cursor-based path populates NextCursor and leaves the offset fields zero/false.
//
// Transactions is []*domain.ActivityItem (superset of Transaction) so the unified
// investment feed and the banking-only path share one response type. Banking rows
// have Source="banking"; investment rows have Source="investment". Clients that
// previously consumed []*domain.Transaction see all the same fields — ActivityItem
// is a strict superset.
type TransactionListResponse struct {
	Transactions []*domain.ActivityItem `json:"transactions"`
	// TotalCount and TotalAmount describe the WHOLE filtered set, not this page.
	//
	// ⚠ POINTERS, and that is the whole point (#1626 Phase 2). nil means "not computed on this
	// page"; a non-nil zero means "computed, and it is zero". Non-pointer scalars with `omitempty`
	// cannot express that at all — the zero value IS the omitted value, so a first page whose
	// filter matched nothing would be byte-identical to a cursored page that computed no aggregate,
	// and the DEFAULT mixed income/expense feed (which nets to exactly zero under PFCv2 signs)
	// would report a count beside an absent amount. Pinned by
	// TestApplyFeedTotals_DistinguishesAbsenceFromZero and, at the wire, by
	// TestGetTransactions_WholeSetTotals_OmittedNeverZeroed.
	//
	// The offset path always computes TotalCount and therefore always sets it.
	TotalCount  *int     `json:"total_count,omitempty"`
	TotalAmount *float64 `json:"total_amount,omitempty"`
	Limit       int      `json:"limit"`
	Offset      int      `json:"offset,omitempty"`
	HasMore     bool     `json:"has_more,omitempty"`
	NextCursor  *string  `json:"next_cursor,omitempty"`
}

// transactionService implements TransactionService
type transactionService struct {
	repo               repository.TransactionRepository
	customCategoryRepo repository.CustomCategoryRepository
	analytics          phclient.Analytics
	accountRepo        repository.AccountRepository // optional; wired via SetAccountRepository (#1329)
}

// NewTransactionService creates a new TransactionService instance
func NewTransactionService(repo repository.TransactionRepository, customCategoryRepo repository.CustomCategoryRepository) TransactionService {
	return &transactionService{
		repo:               repo,
		customCategoryRepo: customCategoryRepo,
	}
}

// ListTransactions retrieves transactions for a user with filters and pagination
//
// Validation:
// - Enforces max limit of 100 transactions per request
// - Enforces non-negative offset
// - Validates date range (start before or equal to end)
//
// Pagination:
// - Default limit: 50
// - Returns total count for pagination metadata
// - Calculates has_more flag for infinite scroll UIs
func (s *transactionService) ListTransactions(
	ctx context.Context,
	userID string,
	filters domain.TransactionFilters,
	limit int,
	offset int,
) (*TransactionListResponse, error) {
	// Validate and apply default limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		return nil, ErrInvalidLimit
	}

	// Validate offset
	if offset < 0 {
		return nil, ErrInvalidOffset
	}

	// Validate date range
	if filters.StartDate != nil && filters.EndDate != nil {
		if filters.StartDate.After(*filters.EndDate) {
			return nil, ErrInvalidDateRange
		}
	}

	// Validate amount range (Issue #529)
	if filters.MinAmount != nil && filters.MaxAmount != nil {
		if *filters.MinAmount > *filters.MaxAmount {
			return nil, ErrInvalidAmountRange
		}
	}

	// Get transactions from repository
	transactions, err := s.repo.GetByUserIDWithFilters(ctx, userID, filters, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get transactions: %w", err)
	}

	// Get total count for pagination metadata
	totalCount, err := s.repo.CountByUserID(ctx, userID, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to count transactions: %w", err)
	}

	// Enrich each transaction with PFC display names and custom-category display names,
	// then wrap in ActivityItem (source="banking") for the unified response shape.
	ptrs := make([]*domain.Transaction, len(transactions))
	for i := range transactions {
		transactions[i].PopulatePFCDisplay()
		ptrs[i] = &transactions[i]
	}
	if err := s.enrichCustomCategoryDisplay(ctx, userID, ptrs); err != nil {
		return nil, err
	}
	items := make([]*domain.ActivityItem, len(ptrs))
	for i, p := range ptrs {
		items[i] = domain.NewBankingActivityItem(p)
	}

	// Calculate has_more flag for pagination
	// hasMore is true if there are more results beyond current page
	hasMore := (offset + len(transactions)) < totalCount

	return &TransactionListResponse{
		Transactions: items,
		TotalCount:   &totalCount,
		Limit:        limit,
		Offset:       offset,
		HasMore:      hasMore,
	}, nil
}

// ListTransactionsByCursor retrieves transactions using keyset (cursor) pagination.
// Validates cursor sort key/dir matches filters and encodes next cursor for pagination.
func (s *transactionService) ListTransactionsByCursor(
	ctx context.Context,
	userID string,
	filters domain.TransactionFilters,
	cursor *domain.TransactionCursor,
	limit int,
) (*TransactionListResponse, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		return nil, ErrInvalidLimit
	}

	if filters.StartDate != nil && filters.EndDate != nil {
		if filters.StartDate.After(*filters.EndDate) {
			return nil, ErrInvalidDateRange
		}
	}
	if filters.MinAmount != nil && filters.MaxAmount != nil {
		if *filters.MinAmount > *filters.MaxAmount {
			return nil, ErrInvalidAmountRange
		}
	}

	sortBy := filters.SortBy
	if sortBy == "" {
		sortBy = "date"
	}
	sortDir := filters.SortDir
	if sortDir == "" {
		sortDir = "desc"
	}
	if cursor != nil && (cursor.K != sortBy || cursor.D != sortDir) {
		return nil, ErrCursorSortMismatch
	}

	// Unified investment feed path: merge banking + investment_transactions via UNION ALL.
	if filters.IncludeInvestmentActivity {
		if sortBy != "date" {
			return nil, domain.ErrInvestmentMergeRequiresDateSort
		}
		// Reject cursors minted under a different IncludeInvestmentActivity flag.
		if cursor != nil {
			if cursor.V == 2 && !cursor.Inc {
				return nil, domain.ErrCursorFilterMismatch
			}
			if cursor.V == 1 {
				// V:1 cursor carries no Inc flag — treat as mismatch when investment merge is requested.
				return nil, domain.ErrCursorFilterMismatch
			}
		}

		items, nextRepoCursor, totals, err := s.repo.GetByUserIDKeysetUnified(ctx, userID, filters, cursor, limit)
		if err != nil {
			return nil, fmt.Errorf("failed to get unified feed by cursor: %w", err)
		}

		// Populate PFC display names and custom-category display names for banking items.
		// Investment items have no PFC fields so the call is a no-op for them.
		for _, item := range items {
			if item.Source == domain.FeedSourceBanking {
				item.PopulatePFCDisplay()
			}
		}
		if err := s.enrichCustomCategoryDisplayForItems(ctx, userID, items); err != nil {
			return nil, err
		}

		// Resolve effective merchant display names for banking rows (G6 fix, #957).
		// Uses the same SQL as the banking-only keyset path so overrides/merges apply consistently.
		if err := s.enrichEffectiveMerchantNamesForItems(ctx, userID, items); err != nil {
			return nil, err
		}

		// Populate ActivityItem.Account for investment rows so the drawer can resolve
		// the account nickname immediately on cold load, before accountsMap resolves (#1329).
		if err := s.enrichAccountForInvestmentItems(ctx, userID, items); err != nil {
			return nil, err
		}

		encodedCursor, err := encodeNextCursor(nextRepoCursor)
		if err != nil {
			return nil, err
		}
		resp := &TransactionListResponse{
			Transactions: items,
			Limit:        limit,
			NextCursor:   encodedCursor,
		}
		applyFeedTotals(resp, totals)
		return resp, nil
	}

	// Banking-only path: V:2 cursor with Inc=true means it was minted under the investment-feed
	// flag; reject so the client resets to page 1 with the new flag.
	if cursor != nil && cursor.V == 2 && cursor.Inc {
		return nil, domain.ErrCursorFilterMismatch
	}

	transactions, nextRepoCursor, totals, err := s.repo.GetByUserIDKeyset(ctx, userID, filters, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get transactions by cursor: %w", err)
	}

	ptrs := make([]*domain.Transaction, len(transactions))
	for i := range transactions {
		transactions[i].PopulatePFCDisplay()
		ptrs[i] = &transactions[i]
	}
	if err := s.enrichCustomCategoryDisplay(ctx, userID, ptrs); err != nil {
		return nil, err
	}
	bankingItems := make([]*domain.ActivityItem, len(ptrs))
	for i, p := range ptrs {
		bankingItems[i] = domain.NewBankingActivityItem(p)
	}

	encodedCursor, err := encodeNextCursor(nextRepoCursor)
	if err != nil {
		return nil, err
	}
	resp := &TransactionListResponse{
		Transactions: bankingItems,
		Limit:        limit,
		NextCursor:   encodedCursor,
	}
	applyFeedTotals(resp, totals)
	return resp, nil
}

// applyFeedTotals copies whole-set totals onto the response when the repository computed them.
//
// It is deliberately a no-op for nil rather than writing zeroes: nil means "not computed on this
// page". Both keyset paths route through here so neither can grow a divergent convention for
// absence (#1626 Phase 2).
//
// ⚠ It assigns POINTERS, so a computed zero survives as a present zero. Assigning the scalars
// instead would collapse "computed (0, 0)" onto "not computed" and silently undo the nil-vs-zero
// discipline the repository and this function are both built around.
func applyFeedTotals(resp *TransactionListResponse, totals *domain.FeedTotals) {
	if totals == nil {
		return
	}
	count, amount := totals.Count, totals.Amount
	resp.TotalCount = &count
	resp.TotalAmount = &amount
}

// encodeNextCursor encodes a cursor for pagination. Returns nil if cursor is nil.
func encodeNextCursor(cursor *domain.TransactionCursor) (*string, error) {
	if cursor == nil {
		return nil, nil
	}
	encoded, err := domain.EncodeTransactionCursor(*cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to encode next cursor: %w", err)
	}
	return &encoded, nil
}

// GetTransactionByID retrieves a transaction by its ID
func (s *transactionService) GetTransactionByID(ctx context.Context, userID, transactionID string) (*domain.Transaction, error) {
	tx, err := s.repo.GetByID(ctx, transactionID)
	if err != nil {
		if err == repository.ErrTransactionNotFound {
			return nil, repository.ErrTransactionNotFound
		}
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	// Verify ownership
	if tx.UserID != userID {
		return nil, repository.ErrTransactionNotFound
	}

	tx.PopulatePFCDisplay()
	if err := s.enrichCustomCategoryDisplay(ctx, userID, []*domain.Transaction{tx}); err != nil {
		return nil, err
	}
	if err := s.enrichEffectiveMerchantNamesForTransactions(ctx, userID, []*domain.Transaction{tx}); err != nil {
		return nil, err
	}
	return tx, nil
}

// UpdateTransactionTags updates the tags on a transaction
func (s *transactionService) UpdateTransactionTags(ctx context.Context, userID, transactionID string, tags []string) (*domain.Transaction, error) {
	// Get existing transaction to verify ownership
	tx, err := s.GetTransactionByID(ctx, userID, transactionID)
	if err != nil {
		return nil, err
	}

	// Issue #1569 Task 20 (S11). This used to read "Build tags source - all manual for now" and
	// stamp Source: "manual" on every entry, which relabelled every rule-applied tag the moment the
	// user touched the list for any reason — including adding an unrelated one.
	//
	// tags_source exists to say which tags the USER chose and which a RULE chose, and to name the
	// rule. Losing that is not cosmetic: ruleApplicationService.applyTagsToTransaction's entire
	// merge is "manual tags are always preserved, rule tags are re-derived", so a relabel converts
	// rule output into user intent that no later rule run may touch. And it is not recomputable —
	// once the row says "manual", nothing records that a rule ever put the tag there.
	//
	// ⚠ THE AMBIGUITY THE FIX CANNOT REMOVE, stated rather than papered over. This endpoint takes a
	// whole tag LIST, not a delta, so "the user kept the rule's tag" and "the user typed the same
	// word" arrive as the identical request. The rule below preserves the recorded source for a tag
	// that already had one, and marks only genuinely NEW tags manual. That is the direction that
	// loses no information: a wrongly-kept "rule" is correctable by removing the tag, a wrongly
	// written "manual" is correctable by nothing.
	//
	// Removing a tag DOES drop its entry — the result is keyed on the incoming list, not merged
	// into the old slice — so tags_source cannot accumulate entries for tags that no longer exist.
	priorSource := make(map[string]domain.TagSource, len(tx.TagsSource))
	for _, ts := range tx.TagsSource {
		priorSource[ts.Tag] = ts
	}
	tagsSource := make([]domain.TagSource, len(tags))
	for i, tag := range tags {
		if prior, ok := priorSource[tag]; ok {
			tagsSource[i] = prior
			continue
		}
		tagsSource[i] = domain.TagSource{
			Tag:    tag,
			Source: "manual",
		}
	}

	// Update tags in database
	if err := s.repo.UpdateTags(ctx, transactionID, userID, tags, tagsSource); err != nil {
		return nil, fmt.Errorf("failed to update transaction tags: %w", err)
	}

	// Update the transaction object
	tx.Tags = tags
	tx.TagsSource = tagsSource

	tx.PopulatePFCDisplay()
	return tx, nil
}

// UpdateTransactionCategory updates the category override on a transaction using the PFC v2 discriminated union.
func (s *transactionService) UpdateTransactionCategory(ctx context.Context, userID, transactionID string, overrideKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string) (*domain.Transaction, error) {
	// Verify ownership before update
	if _, err := s.GetTransactionByID(ctx, userID, transactionID); err != nil {
		return nil, err
	}

	// Validate custom-category ownership so a transaction can't reference another user's
	// category — a cross-user reference would block that user's deletion via the
	// custom_category_override_id ON DELETE RESTRICT foreign key (Issue #1203).
	if overrideKind == "custom" && customCategoryID != nil {
		cat, err := s.customCategoryRepo.GetByID(ctx, *customCategoryID)
		if err != nil {
			return nil, err
		}
		if cat.UserID != userID {
			return nil, domain.ErrCategoryNotFound
		}
	}

	// Map kind to nullable for clear semantics
	var kindPtr *string
	if overrideKind != "clear" {
		kindPtr = &overrideKind
	}

	if err := s.repo.UpdateCategory(ctx, transactionID, userID, kindPtr, pfcPrimary, pfcDetailed, customCategoryID, domain.CategorySourceManual); err != nil {
		return nil, fmt.Errorf("failed to update transaction category: %w", err)
	}

	if s.analytics != nil {
		s.analytics.Capture(userID, phclient.EventTransactionRecategorized, map[string]interface{}{
			"transaction_id":  transactionID,
			"kind":            overrideKind,
			"category_source": string(domain.CategorySourceManual),
		})
	}

	return s.GetTransactionByID(ctx, userID, transactionID)
}

// SetReviewed marks the given transactions reviewed/unreviewed for the user (Issue #1203).
func (s *transactionService) SetReviewed(ctx context.Context, userID string, transactionIDs []string, reviewed bool) (int64, error) {
	if len(transactionIDs) == 0 {
		return 0, nil
	}
	n, err := s.repo.SetReviewed(ctx, userID, transactionIDs, reviewed)
	if err != nil {
		return 0, err
	}
	if s.analytics != nil && n > 0 {
		s.analytics.Capture(userID, phclient.EventTransactionReviewStateChanged, map[string]interface{}{
			"reviewed": reviewed,
			"count":    n,
		})
	}
	return n, nil
}

// BulkUpdateTransactionCategory recategorizes many transactions at once (Issue #1210).
// Best-effort: unowned/unknown IDs are silently ignored; returns rows affected.
// Validates custom-category ownership once (single query) to prevent cross-user references.
func (s *transactionService) BulkUpdateTransactionCategory(ctx context.Context, userID string, transactionIDs []string, kind string, pfcPrimary, pfcDetailed, customCategoryID *string) (int64, error) {
	if len(transactionIDs) == 0 {
		return 0, nil
	}

	// Validate custom-category ownership once (single query)
	if kind == "custom" && customCategoryID != nil {
		cat, err := s.customCategoryRepo.GetByID(ctx, *customCategoryID)
		if err != nil {
			return 0, err
		}
		if cat.UserID != userID {
			return 0, domain.ErrCategoryNotFound
		}
	}

	// Map clear → nil kind for repo layer
	var kindPtr *string
	if kind != "clear" {
		kindPtr = &kind
	}

	n, err := s.repo.BulkUpdateCategory(ctx, userID, transactionIDs, kindPtr, pfcPrimary, pfcDetailed, customCategoryID, domain.CategorySourceManual)
	if err != nil {
		return 0, err
	}
	if s.analytics != nil && n > 0 {
		s.analytics.Capture(userID, phclient.EventTransactionRecategorized, map[string]interface{}{
			"kind":            kind,
			"category_source": string(domain.CategorySourceManual),
			"count":           n,
		})
	}
	return n, nil
}

// enrichCustomCategoryDisplayForItems fetches custom-category names in one batched query and
// sets CustomCategoryOverrideDisplay on banking ActivityItems that have a custom override.
// Investment rows never have a CustomCategoryOverrideID, so they are skipped automatically.
// No-ops when no item has a custom override ID.
func (s *transactionService) enrichCustomCategoryDisplayForItems(ctx context.Context, userID string, items []*domain.ActivityItem) error {
	ids := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.CustomCategoryOverrideID != nil {
			id := *item.CustomCategoryOverrideID
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	names, err := s.customCategoryRepo.GetNamesByIDs(ctx, userID, ids)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.CustomCategoryOverrideID != nil {
			if name, ok := names[*item.CustomCategoryOverrideID]; ok {
				item.CustomCategoryOverrideDisplay = &name
			}
		}
	}
	return nil
}

// enrichCustomCategoryDisplay fetches custom-category names in one batched query and sets
// CustomCategoryOverrideDisplay on each transaction. Transactions without a custom override
// have their display field set to nil. No-ops when no transaction has a custom override ID.
func (s *transactionService) enrichCustomCategoryDisplay(ctx context.Context, userID string, txs []*domain.Transaction) error {
	ids := make([]string, 0, len(txs))
	seen := make(map[string]struct{}, len(txs))
	for _, tx := range txs {
		if tx.CustomCategoryOverrideID != nil {
			id := *tx.CustomCategoryOverrideID
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	names, err := s.customCategoryRepo.GetNamesByIDs(ctx, userID, ids)
	if err != nil {
		return err
	}
	for _, tx := range txs {
		if tx.CustomCategoryOverrideID != nil {
			if name, ok := names[*tx.CustomCategoryOverrideID]; ok {
				tx.CustomCategoryOverrideDisplay = &name
			}
		}
	}
	return nil
}

// GetDistinctPlaidCategories returns distinct Plaid categories from user's transactions
func (s *transactionService) GetDistinctPlaidCategories(ctx context.Context, userID string) ([]string, error) {
	categories, err := s.repo.GetDistinctPlaidCategories(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get distinct plaid categories: %w", err)
	}

	// Return empty slice instead of nil for consistent JSON serialization
	if categories == nil {
		return []string{}, nil
	}

	return categories, nil
}

// SetAnalytics sets the PostHog analytics client for event capture (Issue #1210).
// Nil-safe: if analytics is nil, all Capture calls are skipped.
func (s *transactionService) SetAnalytics(analytics phclient.Analytics) {
	s.analytics = analytics
}

// SetAccountRepository wires in the account repository used to populate
// ActivityItem.Account for investment rows in the unified feed (#1329).
// Nil-safe: if not called, enrichment is silently skipped.
func (s *transactionService) SetAccountRepository(accountRepo repository.AccountRepository) {
	s.accountRepo = accountRepo
}

// enrichAccountForInvestmentItems populates ActivityItem.Account for every investment
// row whose account resolves to an active account.
//
// Resolution order (mirrors the frontend's accountsMap → embedded account priority):
//  1. Direct match: byID[item.AccountID] — the common case.
//  2. Chain match:  byUserAccountID[*item.UserAccountID] — for rows whose account_id
//     points to a replaced account after a Plaid re-link; the active chain account
//     shares the same user_account_id.
//
// Accounts are fetched via GetAccountsWithInstitution which returns active, fully-
// decrypted accounts — no inline crypto needed here (#1329, Codex finding #1).
// Banking rows are intentionally left untouched (out of scope).
func (s *transactionService) enrichAccountForInvestmentItems(ctx context.Context, userID string, items []*domain.ActivityItem) error {
	if s.accountRepo == nil {
		return nil
	}

	// Early exit: skip the DB fetch when there are no investment rows.
	hasInvestment := false
	for _, item := range items {
		if item.Source == domain.FeedSourceInvestment {
			hasInvestment = true
			break
		}
	}
	if !hasInvestment {
		return nil
	}

	// Use the filtered variant so nickname and customization fields are included.
	// Default opts: active-only, hidden excluded — matches what the frontend accountsMap shows.
	accts, err := s.accountRepo.GetAccountsWithInstitutionFiltered(ctx, userID, repository.AccountFilterOptions{})
	if err != nil {
		return fmt.Errorf("enrichAccountForInvestmentItems: %w", err)
	}

	// Build lookup maps: by direct account ID and by stable user_account_id.
	byID := make(map[string]*domain.Account, len(accts))
	byUserAccountID := make(map[string]*domain.Account)
	for i := range accts {
		a := &accts[i]
		byID[a.ID] = a
		if a.UserAccountID != nil {
			byUserAccountID[*a.UserAccountID] = a
		}
	}

	for _, item := range items {
		if item.Source != domain.FeedSourceInvestment {
			continue
		}

		var resolved *domain.Account
		if a, ok := byID[item.AccountID]; ok {
			resolved = a
		} else if item.UserAccountID != nil {
			if a, ok := byUserAccountID[*item.UserAccountID]; ok {
				resolved = a
			}
		}

		if resolved != nil {
			// Copy to avoid aliasing — all items must point to independent Account values.
			copy := *resolved
			item.Account = &copy
		}
	}

	return nil
}

// enrichEffectiveMerchantNamesForItems resolves per-user merchant display names for
// ActivityItem slices (unified feed path) in one extra DB round-trip.
// Only banking rows with a non-nil MerchantID are enriched; investment rows are skipped.
// This mirrors enrichEffectiveMerchantNames (Transaction slices) and shares the same SQL via
// repo.ResolveEffectiveMerchantNames so the two paths never diverge (G6 fix, #957).
func (s *transactionService) enrichEffectiveMerchantNamesForItems(ctx context.Context, userID string, items []*domain.ActivityItem) error {
	merchantIDSet := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.Source == domain.FeedSourceBanking && item.MerchantID != nil && *item.MerchantID != "" {
			merchantIDSet[*item.MerchantID] = struct{}{}
		}
	}
	if len(merchantIDSet) == 0 {
		return nil
	}

	merchantIDs := make([]string, 0, len(merchantIDSet))
	for id := range merchantIDSet {
		merchantIDs = append(merchantIDs, id)
	}

	nameByID, err := s.repo.ResolveEffectiveMerchantNames(ctx, userID, merchantIDs)
	if err != nil {
		return fmt.Errorf("enrichEffectiveMerchantNamesForItems: %w", err)
	}

	for _, item := range items {
		if item.Source == domain.FeedSourceBanking && item.MerchantID != nil {
			if name, ok := nameByID[*item.MerchantID]; ok {
				nameCopy := name
				item.EffectiveMerchantName = &nameCopy
			}
		}
	}
	return nil
}

// enrichEffectiveMerchantNamesForTransactions resolves per-user merchant display names for
// Transaction slices (offset/budget-category paths) that are enriched at the service layer.
// Transactions without a MerchantID are skipped. Shares the same SQL via
// repo.ResolveEffectiveMerchantNames so all paths stay in sync (G10 fix, #957).
func (s *transactionService) enrichEffectiveMerchantNamesForTransactions(ctx context.Context, userID string, txns []*domain.Transaction) error {
	merchantIDSet := make(map[string]struct{}, len(txns))
	for _, tx := range txns {
		if tx.MerchantID != nil && *tx.MerchantID != "" {
			merchantIDSet[*tx.MerchantID] = struct{}{}
		}
	}
	if len(merchantIDSet) == 0 {
		return nil
	}

	merchantIDs := make([]string, 0, len(merchantIDSet))
	for id := range merchantIDSet {
		merchantIDs = append(merchantIDs, id)
	}

	nameByID, err := s.repo.ResolveEffectiveMerchantNames(ctx, userID, merchantIDs)
	if err != nil {
		return fmt.Errorf("enrichEffectiveMerchantNamesForTransactions: %w", err)
	}

	for _, tx := range txns {
		if tx.MerchantID != nil {
			if name, ok := nameByID[*tx.MerchantID]; ok {
				nameCopy := name
				tx.EffectiveMerchantName = &nameCopy
			}
		}
	}
	return nil
}

// GetReviewSummary returns pre-computed review-state counts and top unreviewed categories (Issue #1210).
// Delegates directly to the repository's single-query CTE implementation.
func (s *transactionService) GetReviewSummary(ctx context.Context, userID string, filters domain.ReviewSummaryFilters) (*domain.ReviewSummaryResponse, error) {
	summary, err := s.repo.GetReviewSummary(ctx, userID, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get review summary: %w", err)
	}
	return summary, nil
}

// ListByBudgetCategory returns transactions for a budget category, enriched with PFC display fields.
func (s *transactionService) ListByBudgetCategory(ctx context.Context, userID string, budgetCategory *domain.BudgetCategory, startDate, endDate time.Time, limit, offset int) ([]domain.Transaction, int, float64, error) {
	transactions, totalCount, totalAmount, err := s.repo.GetTransactionsForBudgetCategory(ctx, userID, budgetCategory, startDate, endDate, limit, offset)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("list transactions for budget category: %w", err)
	}
	ptrs := make([]*domain.Transaction, len(transactions))
	for i := range transactions {
		transactions[i].PopulatePFCDisplay()
		ptrs[i] = &transactions[i]
	}
	if err := s.enrichCustomCategoryDisplay(ctx, userID, ptrs); err != nil {
		return nil, 0, 0, err
	}
	if err := s.enrichEffectiveMerchantNamesForTransactions(ctx, userID, ptrs); err != nil {
		return nil, 0, 0, err
	}
	return transactions, totalCount, totalAmount, nil
}
