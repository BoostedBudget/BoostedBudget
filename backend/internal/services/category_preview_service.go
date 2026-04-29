package services

import (
	"context"
	"errors"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/dateutil"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// =============================================================================
// Issue #497: Category Preview Service
// =============================================================================

// TransactionStatsCalculator is a minimal interface for calculating transaction stats.
// This follows the minimal dependency interface pattern used throughout the codebase.
type TransactionStatsCalculator interface {
	GetMatchingTransactionStats(ctx context.Context, userID string, categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (count int, totalAmount float64, err error)
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
	startDate, endDate, err := dateutil.ParseDateRange(req.StartDate, req.EndDate)
	if err != nil {
		return nil, errors.New(err.Error())
	}

	// Set end date to end of day for inclusive range
	endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	// Validate that we have a category or tags depending on match logic
	hasCategoryKind := req.CategoryKind != ""
	switch req.MatchLogic {
	case domain.MatchLogicCategoryOnly:
		if !hasCategoryKind {
			return nil, errors.New("category_kind is required for category_only match logic")
		}
	case domain.MatchLogicTagOnly:
		if len(req.Tags) == 0 {
			return nil, errors.New("at least one tag is required for tag_only match logic")
		}
	case domain.MatchLogicAnd:
		if !hasCategoryKind {
			return nil, errors.New("category_kind is required for and match logic")
		}
		if len(req.Tags) == 0 {
			return nil, errors.New("at least one tag is required for and match logic")
		}
	case domain.MatchLogicOr:
		if !hasCategoryKind && len(req.Tags) == 0 {
			return nil, errors.New("at least one category or tag is required for or match logic")
		}
	}

	count, totalAmount, err := s.statsCalculator.GetMatchingTransactionStats(
		ctx,
		userID,
		req.CategoryKind,
		req.PFCPrimary,
		req.PFCDetailed,
		req.CustomCategoryID,
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
