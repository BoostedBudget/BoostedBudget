package services

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// InvestmentAnalyticsService provides pre-computed single-round-trip analytics for
// the investment portfolio. Each method answers exactly one well-defined financial
// question and is designed to be called directly as an MCP tool operation.
//
// Realized gain/loss is NOT supported — Plaid does not provide per-lot cost basis
// and the 2-year transaction lookback truncates the buy history a FIFO replay would
// need. All unrealized methods carry RealizedGainLossSupported=false in the response.
//
// Issue #138: Investment Backend — analytics helpers (Code Mode).
type InvestmentAnalyticsService interface {
	// GetAllocationBreakdown returns portfolio value grouped per AllocationOptions.
	// Default (zero value): investment holdings only, grouped by security_type —
	// backward-compatible with #138 behavior. See AllocationOptions for extension docs.
	GetAllocationBreakdown(ctx context.Context, userID string, opts domain.AllocationOptions) (*domain.AllocationBreakdown, error)

	// GetTopMovers returns the top N gainers and losers by day_change.
	// Holdings with NULL institution_price or securities with NULL close_price are excluded.
	GetTopMovers(ctx context.Context, userID string, limit int) (*domain.TopMovers, error)

	// GetGainLossSummary returns unrealized gain/loss totals and a per-account breakdown.
	// RealizedGainLossSupported is always false — see type comment.
	GetGainLossSummary(ctx context.Context, userID string) (*domain.GainLossSummary, error)

	// GetIncomeSummary returns investment income (dividends, interest, reinvested
	// dividends/interest) within the given date range, grouped by normalized income type.
	// The income set is wider than the display "dividend" bucket — reinvested income is
	// economically income.
	GetIncomeSummary(ctx context.Context, userID string, start, end time.Time) (*domain.IncomeSummary, error)

	// GetPositionReconciliation is a DIAGNOSTIC (#1631 M8b): it compares each active holding's
	// quantity against the net share delta the trade ledger implies, and reports the mismatches.
	// It NEVER corrects a holding — Plaid's snapshot stays the position of record. See
	// domain.PositionReconciliationReport for the report shape and the 2-year-lookback
	// suppression rule.
	GetPositionReconciliation(ctx context.Context, userID string) (*domain.PositionReconciliationReport, error)
}

type investmentAnalyticsService struct {
	repo repository.InvestmentAnalyticsRepository
}

// NewInvestmentAnalyticsService creates a new InvestmentAnalyticsService.
func NewInvestmentAnalyticsService(repo repository.InvestmentAnalyticsRepository) InvestmentAnalyticsService {
	return &investmentAnalyticsService{repo: repo}
}

// GetAllocationBreakdown delegates to the repository aggregate query and returns the result.
func (s *investmentAnalyticsService) GetAllocationBreakdown(ctx context.Context, userID string, opts domain.AllocationOptions) (*domain.AllocationBreakdown, error) {
	result, err := s.repo.AllocationBreakdown(ctx, userID, opts)
	if err != nil {
		return nil, fmt.Errorf("GetAllocationBreakdown: %w", err)
	}
	return result, nil
}

// GetTopMovers delegates to the repository CTE query and returns the result.
func (s *investmentAnalyticsService) GetTopMovers(ctx context.Context, userID string, limit int) (*domain.TopMovers, error) {
	if limit <= 0 {
		limit = 5
	}
	result, err := s.repo.TopMovers(ctx, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("GetTopMovers: %w", err)
	}
	return result, nil
}

// GetGainLossSummary delegates to the repository aggregate query and returns the result.
// RealizedGainLossSupported is always false — see interface comment.
func (s *investmentAnalyticsService) GetGainLossSummary(ctx context.Context, userID string) (*domain.GainLossSummary, error) {
	result, err := s.repo.UnrealizedGainLoss(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("GetGainLossSummary: %w", err)
	}
	return result, nil
}

// GetIncomeSummary delegates to the repository subtype-grouped aggregate query.
func (s *investmentAnalyticsService) GetIncomeSummary(ctx context.Context, userID string, start, end time.Time) (*domain.IncomeSummary, error) {
	result, err := s.repo.IncomeSummary(ctx, userID, start, end)
	if err != nil {
		return nil, fmt.Errorf("GetIncomeSummary: %w", err)
	}
	return result, nil
}

// GetPositionReconciliation delegates to the repository's trade-vs-position diagnostic query.
// See domain.PositionReconciliationReport for what a result means and NewInvestmentAnalyticsService's
// interface comment for the "diagnostic, never a correction" guarantee.
func (s *investmentAnalyticsService) GetPositionReconciliation(ctx context.Context, userID string) (*domain.PositionReconciliationReport, error) {
	result, err := s.repo.PositionReconciliation(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("GetPositionReconciliation: %w", err)
	}
	return result, nil
}
