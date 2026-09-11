package services

import (
	"context"
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
	// GetIncomeSumForSources sums income from ALL configured pfc_primary/tag sources in ONE
	// query, so a transaction matching more than one source is counted once, not once per
	// source (Issue #1630 fixwave 4 — see CalculateBudgetIncome and
	// transactionRepository.GetIncomeSumForSources for the double-count this replaces).
	GetIncomeSumForSources(ctx context.Context, userID string, pfcPrimaries, tags []string, startDate, endDate time.Time) (float64, error)
	GetByUserIDWithFilters(ctx context.Context, userID string, filters domain.TransactionFilters, limit, offset int) ([]domain.Transaction, error)
}

// BudgetIncomeService defines the interface for calculating budget income from configured sources.
type BudgetIncomeService interface {
	// CalculateBudgetIncome calculates total income for a budget from all configured income sources.
	// Returns the sum of:
	// - pfc_primary sources: Sum of inflow (negative amount under PFCv2) transactions matching the category
	// - tag sources: Sum of inflow transactions with the matching tag
	// - manual sources: Parsed float value from IncomeSource.Value
	//
	// A transaction matching MORE THAN ONE configured pfc_primary/tag source contributes to the
	// total exactly ONCE (Issue #1630 fixwave 4 — an independent adversarial review found the
	// previous implementation summed each source's own query independently, so an overlapping
	// transaction was counted once per matching source; the prior version of this doc comment
	// even conceded the double-count as a known limitation instead of a defect). All non-manual
	// sources are now combined into a single query (GetIncomeSumForSources) whose predicates are
	// OR-ed together, so an overlapping row is still just one row in that query's result set.
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
//
// Issue #1630 fixwave 4: pfc_primary and tag sources are collected into dedup'd slices and
// resolved through ONE GetIncomeSumForSources call, instead of one query per source summed
// together. A caller can legally configure a pfc_primary source and a tag source that both match
// the same real-world deposit (e.g. pfc_primary=INCOME and tag="salary" on the same paycheck); the
// old per-source-sum approach counted that transaction once per matching source. Collecting into
// one query means an overlapping transaction is still just one row in that query's result set, so
// SUM(amount) counts it once no matter how many configured sources it satisfies.
func (s *budgetIncomeService) CalculateBudgetIncome(ctx context.Context, budget *domain.Budget, startDate, endDate time.Time) (float64, error) {
	var totalIncome float64

	pfcPrimarySeen := make(map[string]bool)
	var pfcPrimaries []string
	tagSeen := make(map[string]bool)
	var tags []string

	for _, source := range budget.IncomeSources {
		switch source.Type {
		case domain.IncomeSourceTypePFCPrimary:
			if !pfcPrimarySeen[source.Value] {
				pfcPrimarySeen[source.Value] = true
				pfcPrimaries = append(pfcPrimaries, source.Value)
			}

		case domain.IncomeSourceTypeTag:
			if !tagSeen[source.Value] {
				tagSeen[source.Value] = true
				tags = append(tags, source.Value)
			}

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

	if len(pfcPrimaries) > 0 || len(tags) > 0 {
		sum, err := s.transactionRepo.GetIncomeSumForSources(ctx, budget.UserID, pfcPrimaries, tags, startDate, endDate)
		if err != nil {
			s.logger.Error("failed to get income sum for sources",
				slog.Any("pfc_primaries", pfcPrimaries),
				slog.Any("tags", tags),
				slog.String("user_id", budget.UserID),
				slog.String("error", err.Error()),
			)
			return 0, fmt.Errorf("failed to calculate income for configured sources: %w", err)
		}
		totalIncome += sum
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
		// Only consider inflow transactions (negative amounts under PFCv2)
		if !tx.IsInflow() {
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

// transactionMatchesSource checks if a transaction matches an income source.
func (s *budgetIncomeService) transactionMatchesSource(tx domain.Transaction, source domain.IncomeSource) bool {
	switch source.Type {
	case domain.IncomeSourceTypePFCPrimary:
		effective := tx.GetEffectiveCategory()
		if effective == nil || effective.Kind != domain.EffectiveCategoryKindPFC || effective.PFCPrimary == nil {
			return false
		}
		return *effective.PFCPrimary == source.Value

	case domain.IncomeSourceTypeTag:
		for _, tag := range tx.Tags {
			if tag == source.Value {
				return true
			}
		}
		return false

	case domain.IncomeSourceTypeManual:
		return false

	default:
		return false
	}
}
