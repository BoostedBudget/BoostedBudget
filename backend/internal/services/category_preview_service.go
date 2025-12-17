package services

import (
	"context"
	"errors"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// =============================================================================
// Issue #497: Category Preview Service
// =============================================================================

// TransactionStatsCalculator is a minimal interface for calculating transaction stats.
// This follows the minimal dependency interface pattern used throughout the codebase.
type TransactionStatsCalculator interface {
	GetMatchingTransactionStats(ctx context.Context, userID string, categories [][]string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (count int, totalAmount float64, err error)
}

// CategoryPreviewService provides transaction preview functionality for budget category configuration.
// It shows how many transactions and what total amount would match a given category configuration.
type CategoryPreviewService interface {
	// PreviewCategoryMatches returns the count and total amount of transactions
	// that would match the given category configuration within the date range.
	PreviewCategoryMatches(ctx context.Context, userID string, req *domain.CategoryPreviewRequest) (*domain.CategoryPreviewResponse, error)
}

// categoryPreviewService implements CategoryPreviewService
type categoryPreviewService struct {
	statsCalculator TransactionStatsCalculator
}

// NewCategoryPreviewService creates a new CategoryPreviewService instance
func NewCategoryPreviewService(statsCalculator TransactionStatsCalculator) CategoryPreviewService {
	return &categoryPreviewService{statsCalculator: statsCalculator}
}

// PreviewCategoryMatches returns the count and total amount of transactions
// that would match the given category configuration within the date range.
func (s *categoryPreviewService) PreviewCategoryMatches(ctx context.Context, userID string, req *domain.CategoryPreviewRequest) (*domain.CategoryPreviewResponse, error) {
	if req == nil {
		return nil, errors.New("request is required")
	}

	// Validate match logic
	if !req.MatchLogic.IsValid() {
		return nil, errors.New("invalid match_logic")
	}

	// Parse dates
	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		return nil, errors.New("invalid start_date format, expected YYYY-MM-DD")
	}

	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		return nil, errors.New("invalid end_date format, expected YYYY-MM-DD")
	}

	// Validate date range
	if !endDate.After(startDate) && !endDate.Equal(startDate) {
		return nil, errors.New("end_date must be on or after start_date")
	}

	// Set end date to end of day for inclusive range
	endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	// Validate that we have categories or tags depending on match logic
	switch req.MatchLogic {
	case domain.MatchLogicCategoryOnly:
		if len(req.Categories) == 0 {
			return nil, errors.New("at least one category is required for category_only match logic")
		}
	case domain.MatchLogicTagOnly:
		if len(req.Tags) == 0 {
			return nil, errors.New("at least one tag is required for tag_only match logic")
		}
	case domain.MatchLogicAnd:
		if len(req.Categories) == 0 {
			return nil, errors.New("at least one category is required for and match logic")
		}
		if len(req.Tags) == 0 {
			return nil, errors.New("at least one tag is required for and match logic")
		}
	case domain.MatchLogicOr:
		if len(req.Categories) == 0 && len(req.Tags) == 0 {
			return nil, errors.New("at least one category or tag is required for or match logic")
		}
	}

	count, totalAmount, err := s.statsCalculator.GetMatchingTransactionStats(
		ctx,
		userID,
		req.Categories,
		req.Tags,
		req.MatchLogic,
		startDate,
		endDate,
	)
	if err != nil {
		return nil, err
	}

	return &domain.CategoryPreviewResponse{
		TransactionCount: count,
		TotalAmount:      totalAmount,
	}, nil
}
