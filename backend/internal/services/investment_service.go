package services

import (
	"context"
	"fmt"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	phclient "github.com/maxnussbaum/BoostedBudget/internal/integration/posthog"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// InvestmentService provides read operations for the investment portfolio stack.
// All methods are scoped to the authenticated user and respect chain-expansion
// (user_account_id) for holdings and investment transactions.
//
// Issue #138: Investment Backend — portfolio_snapshots + API endpoints.
type InvestmentService interface {
	// ListHoldings returns all holdings for the user, optionally filtered to a
	// single account (chain-expanded via user_account_id). Each HoldingView includes
	// joined security metadata and pre-computed gain/loss and day_change values.
	ListHoldings(ctx context.Context, userID string, accountID *string) ([]*domain.HoldingView, error)

	// GetPortfolioSummary returns the live portfolio summary computed from current holdings.
	// day_change is the difference between today's total value and the most-recent-prior
	// portfolio snapshot; null when no prior snapshot exists.
	GetPortfolioSummary(ctx context.Context, userID string) (*domain.PortfolioSummary, error)

	// GetPortfolioHistory returns portfolio value history between start and end (inclusive),
	// ordered by date ascending. Each point is one portfolio_snapshots row.
	GetPortfolioHistory(ctx context.Context, userID string, start, end time.Time) ([]domain.PortfolioHistoryPoint, error)

	// ListInvestmentTransactions retrieves investment transactions using keyset pagination.
	// Returns the transactions and an encoded next-cursor string (nil = end of list).
	ListInvestmentTransactions(
		ctx context.Context,
		userID string,
		filters domain.InvestmentTransactionFilters,
		cursor *domain.InvestmentTransactionCursor,
		limit int,
	) ([]domain.InvestmentTransaction, *string, error)

	// SetAnalytics sets the PostHog analytics client (setter injection).
	SetAnalytics(a phclient.Analytics)
}

type investmentService struct {
	holdingRepo   repository.HoldingRepository
	snapshotRepo  repository.PortfolioSnapshotRepository
	invTxnRepo    repository.InvestmentTransactionRepository
	analytics     phclient.Analytics
}

// NewInvestmentService creates a new InvestmentService.
func NewInvestmentService(
	holdingRepo repository.HoldingRepository,
	snapshotRepo repository.PortfolioSnapshotRepository,
	invTxnRepo repository.InvestmentTransactionRepository,
) InvestmentService {
	return &investmentService{
		holdingRepo:  holdingRepo,
		snapshotRepo: snapshotRepo,
		invTxnRepo:   invTxnRepo,
	}
}

// SetAnalytics sets the PostHog analytics client.
func (s *investmentService) SetAnalytics(a phclient.Analytics) {
	s.analytics = a
}

// ListHoldings returns all holdings for the user with security metadata and
// pre-computed gain/loss and day_change values. Optionally filters to a single
// account (chain-expanded).
func (s *investmentService) ListHoldings(ctx context.Context, userID string, accountID *string) ([]*domain.HoldingView, error) {
	holdings, err := s.holdingRepo.GetByUserIDWithSecurities(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}

	views := make([]*domain.HoldingView, len(holdings))
	for i := range holdings {
		views[i] = domain.NewHoldingView(&holdings[i])
	}
	return views, nil
}

// GetPortfolioSummary returns the live portfolio summary computed from current holdings.
// TotalValue, TotalCostBasis, TotalGainLoss, and TotalGainLossPct are computed in Go
// from the current holdings rows (live, no lag). DayChange compares against the most
// recent prior portfolio snapshot (strict < today). DayChange is null when no prior
// snapshot exists (first use, or never synced before today).
func (s *investmentService) GetPortfolioSummary(ctx context.Context, userID string) (*domain.PortfolioSummary, error) {
	holdings, err := s.holdingRepo.GetByUserIDWithSecurities(ctx, userID, nil)
	if err != nil {
		return nil, fmt.Errorf("GetPortfolioSummary: holdings: %w", err)
	}

	var totalValue, totalCostBasis float64
	for i := range holdings {
		h := &holdings[i]
		if h.InstitutionValue != nil {
			totalValue += *h.InstitutionValue
		}
		if h.CostBasis != nil {
			totalCostBasis += *h.CostBasis
		}
	}
	totalGainLoss := totalValue - totalCostBasis
	var totalGainLossPct *float64
	if totalCostBasis > 0 {
		v := totalGainLoss / totalCostBasis * 100
		totalGainLossPct = &v
	}

	// Day-change: compare against yesterday's (most-recent-prior) snapshot.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	prior, err := s.snapshotRepo.GetLatestBefore(ctx, userID, today)
	if err != nil {
		return nil, fmt.Errorf("GetPortfolioSummary: prior snapshot: %w", err)
	}

	var dayChange, dayChangePct *float64
	if prior != nil {
		dc := totalValue - prior.TotalValue
		dayChange = &dc
		if prior.TotalValue != 0 {
			pct := dc / prior.TotalValue * 100
			dayChangePct = &pct
		}
	}

	return &domain.PortfolioSummary{
		TotalValue:       totalValue,
		TotalCostBasis:   totalCostBasis,
		TotalGainLoss:    totalGainLoss,
		TotalGainLossPct: totalGainLossPct,
		DayChange:        dayChange,
		DayChangePct:     dayChangePct,
		HoldingsCount:    len(holdings),
	}, nil
}

// GetPortfolioHistory returns portfolio value snapshots between start and end (inclusive).
// Each PortfolioHistoryPoint has the snapshot_date and total_value from portfolio_snapshots.
func (s *investmentService) GetPortfolioHistory(ctx context.Context, userID string, start, end time.Time) ([]domain.PortfolioHistoryPoint, error) {
	snaps, err := s.snapshotRepo.GetByUserDateRange(ctx, userID, start, end)
	if err != nil {
		return nil, fmt.Errorf("GetPortfolioHistory: %w", err)
	}

	pts := make([]domain.PortfolioHistoryPoint, len(snaps))
	for i, s := range snaps {
		pts[i] = domain.PortfolioHistoryPoint{
			Date:       s.SnapshotDate,
			TotalValue: s.TotalValue,
		}
	}
	return pts, nil
}

// ListInvestmentTransactions retrieves investment transactions using keyset pagination.
// Returns the slice of transactions and an encoded next-cursor string (nil = last page).
func (s *investmentService) ListInvestmentTransactions(
	ctx context.Context,
	userID string,
	filters domain.InvestmentTransactionFilters,
	cursor *domain.InvestmentTransactionCursor,
	limit int,
) ([]domain.InvestmentTransaction, *string, error) {
	txns, nextCursor, err := s.invTxnRepo.GetByUserIDKeyset(ctx, userID, filters, cursor, limit)
	if err != nil {
		return nil, nil, err
	}

	if nextCursor == nil {
		return txns, nil, nil
	}

	encoded, err := domain.EncodeInvestmentTransactionCursor(*nextCursor)
	if err != nil {
		return nil, nil, err
	}
	return txns, &encoded, nil
}
