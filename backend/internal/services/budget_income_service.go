package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// =============================================================================
// Issue #492: Budget Income Service for Envelope Budgeting
// =============================================================================

// IncomeTransactionGetter is a minimal interface for retrieving income transaction data.
// This allows BudgetIncomeService to depend only on what it needs from TransactionRepository.
type IncomeTransactionGetter interface {
	GetIncomeSumByCategory(ctx context.Context, userID string, category string, startDate, endDate time.Time) (float64, error)
	GetIncomeSumByTag(ctx context.Context, userID string, tag string, startDate, endDate time.Time) (float64, error)
	GetByUserIDWithFilters(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) ([]domain.Transaction, error)
}

// BudgetIncomeService defines the interface for calculating budget income from configured sources.
type BudgetIncomeService interface {
	// CalculateBudgetIncome calculates total income for a budget from all configured income sources.
	// Returns the sum of:
	// - plaid_category sources: Sum of positive transactions matching the category
	// - tag sources: Sum of positive transactions with the matching tag
	// - manual sources: Parsed float value from IncomeSource.Value
	//
	// Note: Overlapping sources (same transaction matching multiple sources) may result in double-counting.
	// Users should configure non-overlapping income sources for accurate totals.
	CalculateBudgetIncome(ctx context.Context, budget *domain.Budget, startDate, endDate time.Time) (float64, error)

	// GetMatchingIncomeTransactions returns all transactions matching any of the income sources.
	// Transactions are deduplicated by ID (a transaction matching multiple sources appears once).
	// Manual income sources are ignored (they don't correspond to actual transactions).
	GetMatchingIncomeTransactions(ctx context.Context, userID string, incomeSources []domain.IncomeSource, startDate, endDate time.Time) ([]domain.Transaction, error)
}

// budgetIncomeService implements BudgetIncomeService
type budgetIncomeService struct {
	transactionRepo IncomeTransactionGetter
	logger          *slog.Logger
}

// NewBudgetIncomeService creates a new BudgetIncomeService instance.
func NewBudgetIncomeService(transactionRepo IncomeTransactionGetter, logger *slog.Logger) BudgetIncomeService {
	return &budgetIncomeService{
		transactionRepo: transactionRepo,
		logger:          logger,
	}
}

// CalculateBudgetIncome calculates total income for a budget from all configured income sources.
func (s *budgetIncomeService) CalculateBudgetIncome(ctx context.Context, budget *domain.Budget, startDate, endDate time.Time) (float64, error) {
	var totalIncome float64

	for _, source := range budget.IncomeSources {
		switch source.Type {
		case domain.IncomeSourceTypePlaidCategory:
			sum, err := s.transactionRepo.GetIncomeSumByCategory(ctx, budget.UserID, source.Value, startDate, endDate)
			if err != nil {
				s.logger.Error("failed to get income by category",
					slog.String("category", source.Value),
					slog.String("user_id", budget.UserID),
					slog.String("error", err.Error()),
				)
				return 0, fmt.Errorf("failed to calculate income for category %s: %w", source.Value, err)
			}
			totalIncome += sum

		case domain.IncomeSourceTypeTag:
			sum, err := s.transactionRepo.GetIncomeSumByTag(ctx, budget.UserID, source.Value, startDate, endDate)
			if err != nil {
				s.logger.Error("failed to get income by tag",
					slog.String("tag", source.Value),
					slog.String("user_id", budget.UserID),
					slog.String("error", err.Error()),
				)
				return 0, fmt.Errorf("failed to calculate income for tag %s: %w", source.Value, err)
			}
			totalIncome += sum

		case domain.IncomeSourceTypeManual:
			amount, err := strconv.ParseFloat(source.Value, 64)
			if err != nil {
				s.logger.Warn("failed to parse manual income value",
					slog.String("value", source.Value),
					slog.String("user_id", budget.UserID),
					slog.String("error", err.Error()),
				)
				// Continue without adding - invalid manual values are skipped
				continue
			}
			if amount < 0 {
				s.logger.Warn("ignoring negative manual income value",
					slog.Float64("amount", amount),
					slog.String("user_id", budget.UserID),
				)
				continue
			}
			totalIncome += amount

		default:
			s.logger.Warn("unknown income source type",
				slog.String("type", string(source.Type)),
				slog.String("user_id", budget.UserID),
			)
		}
	}

	return totalIncome, nil
}

// GetMatchingIncomeTransactions returns all transactions matching any of the income sources.
func (s *budgetIncomeService) GetMatchingIncomeTransactions(ctx context.Context, userID string, incomeSources []domain.IncomeSource, startDate, endDate time.Time) ([]domain.Transaction, error) {
	// Set up filters for date range
	filters := domain.TransactionFilters{
		StartDate: &startDate,
		EndDate:   &endDate,
	}

	// Get all transactions in date range
	transactions, err := s.transactionRepo.GetByUserIDWithFilters(ctx, userID, filters, 10000, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get transactions: %w", err)
	}

	// Collect all matching transactions, deduplicated by ID
	transactionMap := make(map[string]domain.Transaction)

	for _, tx := range transactions {
		// Only consider income transactions (positive amounts)
		if tx.Amount <= 0 {
			continue
		}

		// Check if transaction matches any income source
		for _, source := range incomeSources {
			if s.transactionMatchesSource(tx, source) {
				transactionMap[tx.ID] = tx
				break // Already matched, no need to check other sources
			}
		}
	}

	// Convert map to slice
	result := make([]domain.Transaction, 0, len(transactionMap))
	for _, tx := range transactionMap {
		result = append(result, tx)
	}

	return result, nil
}

// transactionMatchesSource checks if a transaction matches an income source
func (s *budgetIncomeService) transactionMatchesSource(tx domain.Transaction, source domain.IncomeSource) bool {
	switch source.Type {
	case domain.IncomeSourceTypePlaidCategory:
		if s.categoryContains(tx.Category, source.Value) {
			return true
		}
		if s.categoryContains(tx.CategoryOverride, source.Value) {
			return true
		}
		// Fall back to PFC primary enum for transactions with display names in legacy category field
		if pfc := ParsePersonalFinanceCategoryJSON(tx.PersonalFinanceCategory); pfc != nil {
			return pfc.Primary == source.Value
		}
		return false

	case domain.IncomeSourceTypeTag:
		// Check if transaction has the tag
		for _, tag := range tx.Tags {
			if tag == source.Value {
				return true
			}
		}
		return false

	case domain.IncomeSourceTypeManual:
		// Manual sources don't match transactions
		return false

	default:
		return false
	}
}

// categoryContains checks if a JSON category array contains the given value
func (s *budgetIncomeService) categoryContains(categoryJSON *string, value string) bool {
	if categoryJSON == nil {
		return false
	}
	var categories []string
	if err := json.Unmarshal([]byte(*categoryJSON), &categories); err != nil {
		return false
	}
	for _, cat := range categories {
		if cat == value {
			return true
		}
	}
	return false
}
