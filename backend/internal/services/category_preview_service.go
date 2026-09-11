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

	// GetCategoryMatchBreakdown discloses a real partition of the rolled-up total —
	// category-direct + nested-descendant + tag-only-outside-category (Issue #1630 Task
	// B5). See repository.TransactionRepository's doc comment for the bucket definitions
	// and the leaf/unsupported no-op case.
	GetCategoryMatchBreakdown(ctx context.Context, userID string, categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (children []domain.CategoryPreviewBreakdownItem, directCount int, directAmount float64, tagOnlyCount int, tagOnlyAmount float64, err error)
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

	// Issue #1630 Task B5 (gap #21): StartDate/EndDate are domain.FlexibleDate — the JSON
	// unmarshal boundary already rejected anything but YYYY-MM-DD/RFC3339, so only ordering
	// remains to check here.
	startDate := req.StartDate.Time()
	endDate := req.EndDate.Time()
	if err := dateutil.ValidateDateRange(startDate, endDate); err != nil {
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

	resp := &domain.CategoryPreviewResponse{
		TransactionCount: count,
		TotalAmount:      totalAmount,
	}

	// Issue #1630 Task B5: disclose a real partition of the total whenever there is
	// anything to disclose — a category-shaped request (which may roll up nested
	// children) OR a tag-involving match_logic (or/tag_only/and) that can pull in
	// transactions the category clause never matched. The repository itself is a no-op
	// for category_only against a non-rollup-eligible target (nothing to disclose there).
	if hasCategoryKind || len(req.Tags) > 0 {
		children, directCount, directAmount, tagOnlyCount, tagOnlyAmount, err := s.statsCalculator.GetCategoryMatchBreakdown(
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
		if len(children) > 0 || directCount > 0 || tagOnlyCount > 0 {
			resp.Children = children
			resp.DirectTransactionCount = directCount
			resp.DirectAmount = directAmount
			resp.TagOnlyTransactionCount = tagOnlyCount
			resp.TagOnlyAmount = tagOnlyAmount
		}
	}

	return resp, nil
}
