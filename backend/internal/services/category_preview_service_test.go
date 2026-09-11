package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Issue #497: Category Preview Service Tests
// =============================================================================

// MockTransactionStatsCalculator is a mock implementation of TransactionStatsCalculator
type MockTransactionStatsCalculator struct {
	mock.Mock
}

func (m *MockTransactionStatsCalculator) GetMatchingTransactionStats(ctx context.Context, userID string, categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) (int, float64, error) {
	args := m.Called(ctx, userID, categoryKind, pfcPrimary, pfcDetailed, customCategoryID, tags, matchLogic, startDate, endDate)
	return args.Int(0), args.Get(1).(float64), args.Error(2)
}

func (m *MockTransactionStatsCalculator) GetCategoryMatchBreakdown(ctx context.Context, userID string, categoryKind string, pfcPrimary *string, pfcDetailed *string, customCategoryID *string, tags []string, matchLogic domain.MatchLogic, startDate, endDate time.Time) ([]domain.CategoryPreviewBreakdownItem, int, float64, int, float64, error) {
	args := m.Called(ctx, userID, categoryKind, pfcPrimary, pfcDetailed, customCategoryID, tags, matchLogic, startDate, endDate)
	var children []domain.CategoryPreviewBreakdownItem
	if v := args.Get(0); v != nil {
		children = v.([]domain.CategoryPreviewBreakdownItem)
	}
	return children, args.Int(1), args.Get(2).(float64), args.Int(3), args.Get(4).(float64), args.Error(5)
}

// TestCategoryPreviewService_PreviewCategoryMatches_Success tests successful preview
func TestCategoryPreviewService_PreviewCategoryMatches_Success(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    domain.FlexibleDate(time.Now()),
		EndDate:      domain.FlexibleDate(time.Now()),
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil), []string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Return(5, 250.50, nil)
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil),
		[]string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Return(nil, 0, 0.0, 0, 0.0, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 5, resp.TransactionCount)
	assert.Equal(t, 250.50, resp.TotalAmount)
	assert.Empty(t, resp.Children)

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_WithChildBreakdown tests that a
// pfc_primary rollup discloses its nested custom-category children (Issue #1630 Task 5):
// a "2 txns, $615.28" total that includes a $423.20 "HOA" custom category should say so.
func TestCategoryPreviewService_PreviewCategoryMatches_WithChildBreakdown(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("RENT_AND_UTILITIES"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    domain.FlexibleDate(time.Now()),
		EndDate:      domain.FlexibleDate(time.Now()),
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("RENT_AND_UTILITIES"), (*string)(nil), (*string)(nil), []string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Return(2, 615.28, nil)
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "pfc", ptrStr("RENT_AND_UTILITIES"), (*string)(nil), (*string)(nil),
		[]string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Return([]domain.CategoryPreviewBreakdownItem{
		{CategoryID: "cc-hoa", CategoryName: "HOA", TransactionCount: 1, TotalAmount: 423.20},
	}, 1, 192.08, 0, 0.0, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.TransactionCount)
	assert.Equal(t, 615.28, resp.TotalAmount)
	assert.Equal(t, 1, resp.DirectTransactionCount)
	assert.Equal(t, 192.08, resp.DirectAmount)
	require.Len(t, resp.Children, 1)
	assert.Equal(t, "HOA", resp.Children[0].CategoryName)
	assert.Equal(t, 423.20, resp.Children[0].TotalAmount)
	assert.Equal(t, 1, resp.Children[0].TransactionCount)

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_TagOnly tests tag_only match logic
func TestCategoryPreviewService_PreviewCategoryMatches_TagOnly(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "",
		Tags:         []string{"Essential", "groceries"},
		MatchLogic:   domain.MatchLogicTagOnly,
		StartDate:    domain.FlexibleDate(time.Now()),
		EndDate:      domain.FlexibleDate(time.Now()),
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "", (*string)(nil), (*string)(nil), (*string)(nil), []string{"Essential", "groceries"}, domain.MatchLogicTagOnly,
		mock.Anything, mock.Anything,
	).Return(8, 400.00, nil)
	// No category_kind, so the tag clause is the only reason anything matched — the whole
	// total lands in the tag-only-outside-category bucket.
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "", (*string)(nil), (*string)(nil), (*string)(nil),
		[]string{"Essential", "groceries"}, domain.MatchLogicTagOnly,
		mock.Anything, mock.Anything,
	).Return(nil, 0, 0.0, 8, 400.00, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 8, resp.TransactionCount)
	assert.Equal(t, 400.00, resp.TotalAmount)
	assert.Equal(t, 8, resp.TagOnlyTransactionCount)
	assert.Equal(t, 400.00, resp.TagOnlyAmount)
	assert.Equal(t, resp.DirectAmount+sumChildren(resp.Children)+resp.TagOnlyAmount, resp.TotalAmount,
		"the three buckets must be a real partition of total_amount")

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_AndLogic tests AND match logic
func TestCategoryPreviewService_PreviewCategoryMatches_AndLogic(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{"Essential"},
		MatchLogic:   domain.MatchLogicAnd,
		StartDate:    domain.FlexibleDate(time.Now()),
		EndDate:      domain.FlexibleDate(time.Now()),
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil), []string{"Essential"}, domain.MatchLogicAnd,
		mock.Anything, mock.Anything,
	).Return(3, 150.00, nil)
	// Under "and", every included row is required to satisfy BOTH the category clause and
	// the tag clause — so category-direct + nested-descendant must equal the total exactly,
	// and the tag-only-outside bucket must be zero (an and-only breakdown can never exceed
	// its own total the way the pre-B5 tag-blind query could).
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil),
		[]string{"Essential"}, domain.MatchLogicAnd,
		mock.Anything, mock.Anything,
	).Return([]domain.CategoryPreviewBreakdownItem{
		{CategoryID: "cc-snacks", CategoryName: "Snacks", TransactionCount: 1, TotalAmount: 50.00},
	}, 2, 100.00, 0, 0.0, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 3, resp.TransactionCount)
	assert.Equal(t, 150.00, resp.TotalAmount)
	assert.Equal(t, 0, resp.TagOnlyTransactionCount, "and logic can never pull in a tag-only-outside-category row")
	assert.Equal(t, 0.0, resp.TagOnlyAmount)
	assert.Equal(t, resp.DirectAmount+sumChildren(resp.Children)+resp.TagOnlyAmount, resp.TotalAmount,
		"the three buckets must be a real partition of total_amount, not a value that can exceed it")

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_OrLogic tests OR match logic
func TestCategoryPreviewService_PreviewCategoryMatches_OrLogic(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{"Essential"},
		MatchLogic:   domain.MatchLogicOr,
		StartDate:    domain.FlexibleDate(time.Now()),
		EndDate:      domain.FlexibleDate(time.Now()),
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil), []string{"Essential"}, domain.MatchLogicOr,
		mock.Anything, mock.Anything,
	).Return(10, 500.00, nil)
	// Under "or", a transaction tagged "Essential" but filed under a totally different
	// category is still part of the $500.00 total — the pre-B5 breakdown had no bucket for
	// it at all (an unrelated tag match was mislabelled or silently dropped). It must show
	// up here as tag-only-outside-category, and direct + children + tag-only must equal
	// the total exactly.
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil),
		[]string{"Essential"}, domain.MatchLogicOr,
		mock.Anything, mock.Anything,
	).Return([]domain.CategoryPreviewBreakdownItem{
		{CategoryID: "cc-snacks", CategoryName: "Snacks", TransactionCount: 1, TotalAmount: 50.00},
	}, 6, 300.00, 3, 150.00, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 10, resp.TransactionCount)
	assert.Equal(t, 500.00, resp.TotalAmount)
	assert.Equal(t, 3, resp.TagOnlyTransactionCount)
	assert.Equal(t, 150.00, resp.TagOnlyAmount)
	assert.Equal(t, resp.DirectAmount+sumChildren(resp.Children)+resp.TagOnlyAmount, resp.TotalAmount,
		"the three buckets must be a real partition of total_amount: a tag-only match must be disclosed, not silently missing")

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_NilRequest tests nil request handling
func TestCategoryPreviewService_PreviewCategoryMatches_NilRequest(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"

	resp, err := service.PreviewCategoryMatches(ctx, userID, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request is required")
}

// TestCategoryPreviewService_PreviewCategoryMatches_InvalidMatchLogic tests invalid match logic
func TestCategoryPreviewService_PreviewCategoryMatches_InvalidMatchLogic(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   "invalid_logic",
		StartDate:    domain.FlexibleDate(time.Now()),
		EndDate:      domain.FlexibleDate(time.Now()),
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid match_logic")
}

// TestCategoryPreviewService_PreviewCategoryMatches_EndBeforeStart tests date ordering.
// Issue #1630 Task B5 (gap #21): StartDate/EndDate are now domain.FlexibleDate, so an
// unrecognized format is rejected at the JSON unmarshal boundary (see
// TestPreviewCategoryMatches_StartDate_RejectsUnrecognizedFormat in the huma/operations
// package) rather than here — this service-level test now covers what remains for the
// service to validate: ordering.
func TestCategoryPreviewService_PreviewCategoryMatches_EndBeforeStart(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    domain.FlexibleDate(time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)),
		EndDate:      domain.FlexibleDate(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "end_date")
}

// TestCategoryPreviewService_PreviewCategoryMatches_NoCategoryKindForCategoryOnly tests validation
func TestCategoryPreviewService_PreviewCategoryMatches_NoCategoryKindForCategoryOnly(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "",
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    domain.FlexibleDate(time.Now()),
		EndDate:      domain.FlexibleDate(time.Now()),
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "category_kind is required")
}

// TestCategoryPreviewService_PreviewCategoryMatches_NoTagsForTagOnly tests validation
func TestCategoryPreviewService_PreviewCategoryMatches_NoTagsForTagOnly(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "",
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicTagOnly,
		StartDate:    domain.FlexibleDate(time.Now()),
		EndDate:      domain.FlexibleDate(time.Now()),
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "at least one tag is required")
}

// TestCategoryPreviewService_PreviewCategoryMatches_AndLogicValidation tests AND logic validation
func TestCategoryPreviewService_PreviewCategoryMatches_AndLogicValidation(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"

	// Test missing category_kind
	req1 := &domain.CategoryPreviewRequest{
		CategoryKind: "",
		Tags:         []string{"Essential"},
		MatchLogic:   domain.MatchLogicAnd,
		StartDate:    domain.FlexibleDate(time.Now()),
		EndDate:      domain.FlexibleDate(time.Now()),
	}

	resp, err := service.PreviewCategoryMatches(ctx, userID, req1)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "category_kind is required")

	// Test missing tags
	req2 := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicAnd,
		StartDate:    domain.FlexibleDate(time.Now()),
		EndDate:      domain.FlexibleDate(time.Now()),
	}

	resp, err = service.PreviewCategoryMatches(ctx, userID, req2)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "at least one tag is required")
}

// TestCategoryPreviewService_PreviewCategoryMatches_RepositoryError tests repository error handling
func TestCategoryPreviewService_PreviewCategoryMatches_RepositoryError(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    domain.FlexibleDate(time.Now()),
		EndDate:      domain.FlexibleDate(time.Now()),
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil), []string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Return(0, 0.0, errors.New("database error"))

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "database error")

	mockCalc.AssertExpectations(t)
}

// TestCategoryPreviewService_PreviewCategoryMatches_ZeroResults tests zero results case
func TestCategoryPreviewService_PreviewCategoryMatches_ZeroResults(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("TRANSFER_IN"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    domain.FlexibleDate(time.Now()),
		EndDate:      domain.FlexibleDate(time.Now()),
	}

	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("TRANSFER_IN"), (*string)(nil), (*string)(nil), []string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Return(0, 0.0, nil)
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "pfc", ptrStr("TRANSFER_IN"), (*string)(nil), (*string)(nil),
		[]string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Return(nil, 0, 0.0, 0, 0.0, nil)

	resp, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.TransactionCount)
	assert.Equal(t, 0.0, resp.TotalAmount)

	mockCalc.AssertExpectations(t)
}

// ===========================================================================
// Issue #1630 review round 2: an RFC3339 end_date must be respected exactly, not extended
// almost a full day forward the way a date-only end_date correctly is.
// ===========================================================================

// TestCategoryPreviewService_PreviewCategoryMatches_DateOnlyEndCoversWholeDay is the
// GREEN-already regression guard for the date-only half: "2026-09-30" must still cover
// ALL of Sep 30 (through 23:59:59.999999999), the pre-existing, correct behavior.
func TestCategoryPreviewService_PreviewCategoryMatches_DateOnlyEndCoversWholeDay(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    domain.FlexibleDate(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
		EndDate:      domain.FlexibleDate(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)),
	}

	var gotEnd time.Time
	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil), []string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Run(func(args mock.Arguments) {
		gotEnd = args.Get(9).(time.Time)
	}).Return(0, 0.0, nil)
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil),
		[]string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Return(nil, 0, 0.0, 0, 0.0, nil)

	_, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)

	want := time.Date(2026, 9, 30, 23, 59, 59, 999999999, time.UTC)
	assert.True(t, gotEnd.Equal(want),
		"a date-only end_date must cover the ENTIRE day it names: got %v, want %v", gotEnd, want)
	assert.Equal(t, 30, gotEnd.Day(), "must not have rolled over into October")
}

// TestCategoryPreviewService_PreviewCategoryMatches_RFC3339EndRespectedExactly is the
// RED/GREEN test for the review finding: a full RFC3339 end_date
// ("2026-09-30T15:00:00Z") must end the query EXACTLY there, not almost a full day later
// at "2026-10-01T14:59:59Z".
//
// RED (pre-fix): the old code unconditionally added 23h59m59s to EVERY end_date,
// including one that already carried a real, non-midnight time-of-day.
func TestCategoryPreviewService_PreviewCategoryMatches_RFC3339EndRespectedExactly(t *testing.T) {
	mockCalc := new(MockTransactionStatsCalculator)
	service := NewCategoryPreviewService(mockCalc)

	ctx := context.Background()
	userID := "user-123"
	req := &domain.CategoryPreviewRequest{
		CategoryKind: "pfc",
		PFCPrimary:   ptrStr("FOOD_AND_DRINK"),
		Tags:         []string{},
		MatchLogic:   domain.MatchLogicCategoryOnly,
		StartDate:    domain.FlexibleDate(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
		EndDate:      domain.FlexibleDate(time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)),
	}

	var gotEnd time.Time
	mockCalc.On("GetMatchingTransactionStats",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil), []string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Run(func(args mock.Arguments) {
		gotEnd = args.Get(9).(time.Time)
	}).Return(0, 0.0, nil)
	mockCalc.On("GetCategoryMatchBreakdown",
		ctx, userID, "pfc", ptrStr("FOOD_AND_DRINK"), (*string)(nil), (*string)(nil),
		[]string{}, domain.MatchLogicCategoryOnly,
		mock.Anything, mock.Anything,
	).Return(nil, 0, 0.0, 0, 0.0, nil)

	_, err := service.PreviewCategoryMatches(ctx, userID, req)
	require.NoError(t, err)

	want := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	assert.True(t, gotEnd.Equal(want),
		"a full RFC3339 end_date must be respected EXACTLY: got %v, want %v", gotEnd, want)
	assert.Equal(t, 30, gotEnd.Day(),
		"must not have rolled over into October — the pre-fix bug ended at 2026-10-01T14:59:59Z")
}

// sumChildren totals a CategoryPreviewResponse's nested-descendant bucket, for asserting
// the category-direct + nested-descendant + tag-only-outside-category partition invariant
// (Issue #1630 Task B5).
func sumChildren(children []domain.CategoryPreviewBreakdownItem) float64 {
	var sum float64
	for _, c := range children {
		sum += c.TotalAmount
	}
	return sum
}
