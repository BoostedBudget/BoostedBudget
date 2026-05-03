package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
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
	// Validates input parameters and sanitizes search queries
	ListTransactions(
		ctx context.Context,
		userID string,
		filters domain.TransactionFilters,
		limit int,
		offset int,
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
}

// TransactionListResponse contains paginated transaction results with metadata
type TransactionListResponse struct {
	Transactions []domain.Transaction `json:"transactions"`
	TotalCount   int                  `json:"total_count"`
	Limit        int                  `json:"limit"`
	Offset       int                  `json:"offset"`
	HasMore      bool                 `json:"has_more"`
}

// transactionService implements TransactionService
type transactionService struct {
	repo repository.TransactionRepository
}

// NewTransactionService creates a new TransactionService instance
func NewTransactionService(repo repository.TransactionRepository) TransactionService {
	return &transactionService{
		repo: repo,
	}
}

// ListTransactions retrieves transactions for a user with filters and pagination
//
// Validation:
// - Enforces max limit of 100 transactions per request
// - Enforces non-negative offset
// - Validates date range (start before or equal to end)
// - Sanitizes search input to prevent SQL injection
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

	// Sanitize search input to prevent SQL injection
	if filters.Search != nil {
		sanitized := sanitizeSearchInput(*filters.Search)
		filters.Search = &sanitized
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

	// Enrich each transaction with PFC display names.
	for i := range transactions {
		transactions[i].PopulatePFCDisplay()
	}

	// Calculate has_more flag for pagination
	// hasMore is true if there are more results beyond current page
	hasMore := (offset + len(transactions)) < totalCount

	return &TransactionListResponse{
		Transactions: transactions,
		TotalCount:   totalCount,
		Limit:        limit,
		Offset:       offset,
		HasMore:      hasMore,
	}, nil
}

// sanitizeSearchInput removes or escapes potentially dangerous characters from search input
// This prevents SQL injection and other malicious input
//
// Strategy:
// - Remove SQL comment markers (-- and /* */)
// - Remove semicolons (statement terminators)
// - Remove dangerous SQL keywords (DROP, DELETE, INSERT, UPDATE, UNION, etc.)
// - Trim whitespace
// - PostgreSQL parameterized queries already handle single quotes safely,
//   but we still sanitize for defense in depth
func sanitizeSearchInput(input string) string {
	// Remove SQL comment markers
	sanitized := strings.ReplaceAll(input, "--", "")
	sanitized = strings.ReplaceAll(sanitized, "/*", "")
	sanitized = strings.ReplaceAll(sanitized, "*/", "")

	// Remove semicolons (SQL statement terminators)
	sanitized = strings.ReplaceAll(sanitized, ";", "")

	// Remove dangerous SQL keywords (case-insensitive)
	dangerousKeywords := []string{
		"DROP TABLE", "DROP ", "DELETE FROM", "DELETE ",
		"INSERT INTO", "INSERT ", "UPDATE ", "UNION SELECT",
		"UNION ", "OR 1=1", "OR 1 =", "AND 1=1",
	}

	upper := strings.ToUpper(sanitized)
	for _, keyword := range dangerousKeywords {
		if strings.Contains(upper, keyword) {
			// Remove the keyword (case-insensitive)
			upperKeyword := strings.ToUpper(keyword)
			for {
				idx := strings.Index(upper, upperKeyword)
				if idx == -1 {
					break
				}
				// Remove the keyword from both strings
				sanitized = sanitized[:idx] + sanitized[idx+len(keyword):]
				upper = upper[:idx] + upper[idx+len(keyword):]
			}
		}
	}

	// Trim whitespace
	sanitized = strings.TrimSpace(sanitized)

	return sanitized
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
	return tx, nil
}

// UpdateTransactionTags updates the tags on a transaction
func (s *transactionService) UpdateTransactionTags(ctx context.Context, userID, transactionID string, tags []string) (*domain.Transaction, error) {
	// Get existing transaction to verify ownership
	tx, err := s.GetTransactionByID(ctx, userID, transactionID)
	if err != nil {
		return nil, err
	}

	// Build tags source - all manual for now
	tagsSource := make([]domain.TagSource, len(tags))
	for i, tag := range tags {
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

	// Map kind to nullable for clear semantics
	var kindPtr *string
	if overrideKind != "clear" {
		kindPtr = &overrideKind
	}

	if err := s.repo.UpdateCategory(ctx, transactionID, userID, kindPtr, pfcPrimary, pfcDetailed, customCategoryID, domain.CategorySourceManual); err != nil {
		return nil, fmt.Errorf("failed to update transaction category: %w", err)
	}

	return s.GetTransactionByID(ctx, userID, transactionID)
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

// ListByBudgetCategory returns transactions for a budget category, enriched with PFC display fields.
func (s *transactionService) ListByBudgetCategory(ctx context.Context, userID string, budgetCategory *domain.BudgetCategory, startDate, endDate time.Time, limit, offset int) ([]domain.Transaction, int, float64, error) {
	transactions, totalCount, totalAmount, err := s.repo.GetTransactionsForBudgetCategory(ctx, userID, budgetCategory, startDate, endDate, limit, offset)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("list transactions for budget category: %w", err)
	}
	for i := range transactions {
		transactions[i].PopulatePFCDisplay()
	}
	return transactions, totalCount, totalAmount, nil
}
