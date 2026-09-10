package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// TestResolveCategoryDisplay is the #1626 bug-A guard at the layer that actually decides the label.
//
// The reported symptom was a Sankey income node rendering a title-cased UUID. The mechanism: the
// grouping key for a custom-categorised row is the synthetic token `custom:<uuid>`, and the service
// handed that token to PFCPrimaryDisplayName, which misses the registry map and falls through to
// enumToTitleCase — so the UUID itself became the "display name".
//
// The rule this pins: a `custom:` token is NEVER resolved through the PFC registry. Its label is
// the stored custom-category name, or a neutral placeholder when that name is missing. The
// placeholder matters as much as the happy path — effective_category_name is nullable and is
// maintained asynchronously, so "no name yet" is a reachable state, and the old code turned exactly
// that state into a UUID.
func TestResolveCategoryDisplay(t *testing.T) {
	const customUUID = "custom:9f1c2b3a-0000-4000-8000-000000000001"

	cases := []struct {
		name          string
		categoryKey   string
		storedDisplay string
		want          string
		why           string
	}{
		{
			name:          "PFC primary resolves through the registry",
			categoryKey:   "FOOD_AND_DRINK",
			storedDisplay: "FOOD_AND_DRINK",
			want:          "Food And Drink",
			why:           "effective_category_name stores the RAW enum for PFC rows, so the stored value is not a display name and must not be used as one",
		},
		{
			name:          "PFC detailed resolves to its short name",
			categoryKey:   "FOOD_AND_DRINK_FAST_FOOD",
			storedDisplay: "FOOD_AND_DRINK",
			want:          "Fast Food",
			why:           "the stored display carries the PRIMARY's enum even for a detailed row, so the detailed label has to come from the key",
		},
		{
			name:          "custom category uses its own stored name",
			categoryKey:   customUUID,
			storedDisplay: "Side hustle",
			want:          "Side hustle",
		},
		{
			name:          "custom category with a missing name never leaks the token",
			categoryKey:   customUUID,
			storedDisplay: "",
			want:          unnamedCustomCategoryLabel,
			why:           "this is the exact #1626 regression: PFCPrimaryDisplayName would title-case the UUID and render it as the category's name",
		},
		{
			name:          "UNCATEGORIZED sentinel",
			categoryKey:   "UNCATEGORIZED",
			storedDisplay: "",
			want:          "Uncategorized",
		},
		{
			name:          "unknown PFC enum still title-cases",
			categoryKey:   "SOME_FUTURE_PRIMARY",
			storedDisplay: "",
			want:          "Some Future Primary",
			why:           "an enum the registry has not learned yet must degrade to something readable, not to empty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveCategoryDisplay(tc.categoryKey, tc.storedDisplay)
			assert.Equal(t, tc.want, got, tc.why)
			assert.NotContains(t, got, "custom:",
				"a resolved display must never contain the raw grouping token")
		})
	}
}

// TestResolveCategoryDisplay_NeverReturnsAUUIDForACustomToken is the property form of the bug.
// Enumerating cases proves the labels we thought of; this proves the one thing that must never
// happen regardless of the name's contents.
func TestResolveCategoryDisplay_NeverReturnsAUUIDForACustomToken(t *testing.T) {
	for _, stored := range []string{"", "   ", "Groceries", "9f1c2b3a"} {
		got := resolveCategoryDisplay("custom:9f1c2b3a-0000-4000-8000-000000000001", stored)
		require.NotContains(t, got, "9f1c2b3a-0000-4000-8000",
			"the UUID must never reach a user-visible label, whatever the stored name is (stored=%q)", stored)
	}
}

// TestSortCategoryRows pins the order the API promises AFTER the service has resolved labels.
//
// The SQL orders by `category_display`, which for a PFC row is the RAW enum — so once the service
// converts FOOD_AND_DRINK into "Food and Drink", transport order and display order disagree. The
// comparator therefore re-sorts on the RESOLVED label, keeping total DESC as the primary leg (that
// is the existing contract) and the stable key as the final tiebreaker so equal totals with equal
// labels still have one defined order.
func TestSortCategoryRows(t *testing.T) {
	rows := []categoryDisplayRow{
		{Key: "TRANSPORTATION", Display: "Transportation", Total: 100},
		{Key: "custom:b", Display: "Aardvark supplies", Total: 100},
		{Key: "custom:a", Display: "Aardvark supplies", Total: 100},
		{Key: "FOOD_AND_DRINK", Display: "Food And Drink", Total: 500},
	}

	sortCategoryRows(rows)

	assert.Equal(t, []string{"FOOD_AND_DRINK", "custom:a", "custom:b", "TRANSPORTATION"},
		[]string{rows[0].Key, rows[1].Key, rows[2].Key, rows[3].Key},
		"expected total DESC, then resolved display ASC, then stable key ASC — "+
			"the two 'Aardvark supplies' rows tie on both total and label, so only the key separates them")
}

// ── Service-level contract (#1626) ───────────────────────────────────────────────────────────────
//
// The helper tests above pin resolveCategoryDisplay in isolation. These pin what the two service
// methods actually emit for PRODUCTION-shaped repository output — which is the shape no existing
// unit test simulated, and therefore the shape the bug lived in.
//
// The distinction matters: every pre-existing fixture left the display column EMPTY, so the old
// code's `if displayName == "" { displayName = pfcDisplayName(...) }` fallback ran and produced the
// right answer. In production that column is never empty for a PFC row — it holds the RAW enum —
// so the fallback never ran and the raw enum was returned as the "display name". A fixture that
// omits the column cannot observe that, which is why the whole suite stayed green through the bug.

func TestAnalyticsService_ResolvesDisplayForProductionShapedRows(t *testing.T) {
	ctx := context.Background()
	const customKey = "custom:9f1c2b3a-0000-4000-8000-000000000001"

	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}

	t.Run("expense", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getSpendingByCategoryFn: func(context.Context, string, domain.SpendingFilters) ([]domain.SpendingByCategory, error) {
				return []domain.SpendingByCategory{
					// Production shape: the repo returns effective_category_name, which is the RAW
					// enum for a PFC row and the category's own name for a custom row.
					{Category: "FOOD_AND_DRINK", PFCPrimaryDisplay: "FOOD_AND_DRINK", Total: 300, TransactionCount: 20},
					{Category: customKey, PFCPrimaryDisplay: "Coffee runs", Total: 200, TransactionCount: 9},
					{Category: "custom:9f1c2b3a-0000-4000-8000-000000000002", PFCPrimaryDisplay: "", Total: 100, TransactionCount: 2},
				}, nil
			},
		}

		result, err := NewAnalyticsService(mockRepo).GetSpendingByCategory(ctx, "user-123", filters)
		require.NoError(t, err)
		require.Len(t, result.Categories, 3)

		assert.Equal(t, "Food And Drink", result.Categories[0].PFCPrimaryDisplay,
			"a PFC row must be resolved through the registry, not echoed back as its raw enum")
		assert.Equal(t, "Coffee runs", result.Categories[1].PFCPrimaryDisplay)
		assert.Equal(t, unnamedCustomCategoryLabel, result.Categories[2].PFCPrimaryDisplay,
			"a custom row whose name has not reached effective_category_name yet must not render as a UUID")

		for _, c := range result.Categories {
			assert.NotContains(t, c.PFCPrimaryDisplay, "custom:")
		}
	})

	t.Run("income", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getIncomeSourceBreakdownFn: func(context.Context, string, domain.SpendingFilters) ([]domain.IncomeSourceRow, error) {
				return []domain.IncomeSourceRow{
					{Category: "INCOME", CategoryDisplay: "INCOME", Total: 5000, Count: 3},
					{Category: customKey, CategoryDisplay: "Side hustle", Total: 750, Count: 4},
					{Category: "custom:9f1c2b3a-0000-4000-8000-000000000002", CategoryDisplay: "", Total: 250, Count: 1},
				}, nil
			},
		}

		result, err := NewAnalyticsService(mockRepo).GetIncomeSourceBreakdown(ctx, "user-123", filters)
		require.NoError(t, err)
		require.Len(t, result.Sources, 3)

		assert.Equal(t, "Income", result.Sources[0].PFCPrimaryDisplay)
		assert.Equal(t, "Side hustle", result.Sources[1].PFCPrimaryDisplay,
			"THE reported #1626 symptom: this used to render as a title-cased UUID")
		assert.Equal(t, unnamedCustomCategoryLabel, result.Sources[2].PFCPrimaryDisplay)

		for _, src := range result.Sources {
			assert.NotContains(t, src.PFCPrimaryDisplay, "custom:")
			assert.NotContains(t, src.PFCPrimaryDisplay, "9f1c2b3a-0000-4000-8000")
		}
	})
}

// TestIncomeSourcesMergeByKeyNotDisplay guards the asynchronous-rename window.
//
// The SQL groups income by key AND category_display, and effective_category_name is refreshed by a
// queued job after a rename. Mid-window, one custom category therefore arrives as TWO rows — an
// old-name one and a new-name one. Emitting both would split the user's income source in half and
// halve each percentage, with nothing anywhere reporting an error.
func TestIncomeSourcesMergeByKeyNotDisplay(t *testing.T) {
	const key = "custom:9f1c2b3a-0000-4000-8000-000000000001"
	mockRepo := &mockAnalyticsRepository{
		getIncomeSourceBreakdownFn: func(context.Context, string, domain.SpendingFilters) ([]domain.IncomeSourceRow, error) {
			return []domain.IncomeSourceRow{
				{Category: key, CategoryDisplay: "Old Name", Total: 600, Count: 3},
				{Category: key, CategoryDisplay: "New Name", Total: 400, Count: 2},
			}, nil
		},
	}

	result, err := NewAnalyticsService(mockRepo).GetIncomeSourceBreakdown(
		context.Background(), "user-123",
		domain.SpendingFilters{
			StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			EndDate:   time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
		})
	require.NoError(t, err)

	require.Len(t, result.Sources, 1, "one key is one income source, whatever the display says")
	assert.InDelta(t, 1000.0, result.Sources[0].Total, 0.001)
	assert.Equal(t, 5, result.Sources[0].TransactionCount)
	assert.InDelta(t, 100.0, result.Sources[0].Percentage, 0.001,
		"a split source would show two halves, each with half the percentage")
}

// TestSpendingTrendsResolvesDisplayLikeEveryOtherRead closes the last leak of the #1626 mechanism.
//
// GetSpendingTrends used `stored display, else pfcDisplayName`, which is wrong in BOTH directions:
// the stored display holds the RAW enum for a PFC row (so it echoed FOOD_AND_DRINK), and an empty
// one for a custom row fell through to the registry, which title-cases the `custom:<uuid>` token
// into a UUID — the original reported symptom, in a different chart.
func TestSpendingTrendsResolvesDisplayLikeEveryOtherRead(t *testing.T) {
	const customKey = "custom:9f1c2b3a-0000-4000-8000-000000000001"
	mockRepo := &mockAnalyticsRepository{
		getSpendingTrendsFn: func(context.Context, string, domain.SpendingFilters) ([]domain.SpendingTrendRow, error) {
			return []domain.SpendingTrendRow{
				// Production shape: category_display carries the raw enum for PFC rows.
				{PeriodStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Category: "FOOD_AND_DRINK", CategoryDisplay: "FOOD_AND_DRINK", Total: 300},
				{PeriodStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Category: customKey, CategoryDisplay: "Therapy", Total: 200},
				// A custom row whose name has not reached the denormalized column yet.
				{PeriodStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Category: "custom:9f1c2b3a-0000-4000-8000-000000000002", CategoryDisplay: "", Total: 100},
			}, nil
		},
	}

	result, err := NewAnalyticsService(mockRepo).GetSpendingTrends(context.Background(), "user-123",
		domain.SpendingFilters{
			StartDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			EndDate:   time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
		})
	require.NoError(t, err)
	require.NotEmpty(t, result.Periods)

	byKey := map[string]string{}
	for _, c := range result.Periods[0].Categories {
		byKey[c.Category] = c.PFCPrimaryDisplay
	}

	assert.Equal(t, "Food And Drink", byKey["FOOD_AND_DRINK"],
		"a PFC row must resolve through the registry, not echo its raw enum")
	assert.Equal(t, "Therapy", byKey[customKey])
	assert.Equal(t, unnamedCustomCategoryLabel, byKey["custom:9f1c2b3a-0000-4000-8000-000000000002"],
		"a custom row with no stored name must never render as a title-cased UUID")

	for _, display := range byKey {
		assert.NotContains(t, display, "custom:")
		assert.NotContains(t, display, "9f1c2b3a-0000-4000-8000")
	}
}

// TestCategoryTotalForPeriod_ResolvesDisplayForProductionShapedRows closes one of the five #1626
// Phase 2 gap-item-1 reads: GetCategoryTotalForPeriod returns exactly ONE row (a single-category
// aggregate over a date range), so unlike GetSpendingByCategory/GetIncomeSourceBreakdown above it
// cannot be exercised by a single 3-row fixture — three SEPARATE calls are needed, one per shape.
func TestCategoryTotalForPeriod_ResolvesDisplayForProductionShapedRows(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC)

	t.Run("PFC token resolves through the registry, not the stored raw enum", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getCategoryTotalForPeriodFn: func(context.Context, string, string, time.Time, time.Time) (*domain.CategoryTotalRow, error) {
				// Production shape: CategoryDisplay carries the RAW enum for a PFC row.
				return &domain.CategoryTotalRow{Total: 450.0, TransactionCount: 18, CategoryDisplay: "FOOD_AND_DRINK"}, nil
			},
		}
		result, err := NewAnalyticsService(mockRepo).GetCategoryTotalForPeriod(ctx, "user-1", "FOOD_AND_DRINK", start, end)
		require.NoError(t, err)
		assert.Equal(t, "Food And Drink", result.PFCPrimaryDisplay)
	})

	t.Run("named custom token resolves to its stored name", func(t *testing.T) {
		const customKey = "custom:9f1c2b3a-0000-4000-8000-000000000001"
		mockRepo := &mockAnalyticsRepository{
			getCategoryTotalForPeriodFn: func(context.Context, string, string, time.Time, time.Time) (*domain.CategoryTotalRow, error) {
				return &domain.CategoryTotalRow{Total: 750.0, TransactionCount: 4, CategoryDisplay: "Side hustle"}, nil
			},
		}
		result, err := NewAnalyticsService(mockRepo).GetCategoryTotalForPeriod(ctx, "user-1", customKey, start, end)
		require.NoError(t, err)
		assert.Equal(t, "Side hustle", result.PFCPrimaryDisplay)
	})

	t.Run("unnamed custom token never leaks the UUID", func(t *testing.T) {
		const customKey = "custom:9f1c2b3a-0000-4000-8000-000000000002"
		mockRepo := &mockAnalyticsRepository{
			getCategoryTotalForPeriodFn: func(context.Context, string, string, time.Time, time.Time) (*domain.CategoryTotalRow, error) {
				// CategoryDisplay is NULL in production until effective_category_refresh runs; the
				// repository normalises that to "" (analytics_repository.go GetCategoryTotalForPeriod).
				return &domain.CategoryTotalRow{Total: 250.0, TransactionCount: 1, CategoryDisplay: ""}, nil
			},
		}
		result, err := NewAnalyticsService(mockRepo).GetCategoryTotalForPeriod(ctx, "user-1", customKey, start, end)
		require.NoError(t, err)
		assert.Equal(t, unnamedCustomCategoryLabel, result.PFCPrimaryDisplay)
		assert.NotContains(t, result.PFCPrimaryDisplay, "9f1c2b3a-0000-4000-8000")
	})
}

// TestGetTopExpense_ResolvesDisplayForProductionShapedRows closes another of the five: GetTopExpense
// returns a single item too, so it gets the same three-separate-cases treatment as
// GetCategoryTotalForPeriod above rather than one shared fixture.
func TestGetTopExpense_ResolvesDisplayForProductionShapedRows(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC)
	txDate := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)

	t.Run("PFC token resolves through the registry, not the stored raw enum", func(t *testing.T) {
		mockRepo := &mockAnalyticsRepository{
			getTopExpenseFn: func(context.Context, string, time.Time, time.Time) (*domain.TopExpenseRow, error) {
				return &domain.TopExpenseRow{
					TransactionID: "txn-abc", MerchantName: "Amazon", Amount: 299.99,
					Category: "GENERAL_MERCHANDISE", CategoryDisplay: "GENERAL_MERCHANDISE", Date: txDate,
				}, nil
			},
		}
		result, err := NewAnalyticsService(mockRepo).GetTopExpense(ctx, "user-1", start, end)
		require.NoError(t, err)
		assert.Equal(t, "General Merchandise", result.PFCPrimaryDisplay)
	})

	t.Run("named custom token resolves to its stored name", func(t *testing.T) {
		const customKey = "custom:9f1c2b3a-0000-4000-8000-000000000001"
		mockRepo := &mockAnalyticsRepository{
			getTopExpenseFn: func(context.Context, string, time.Time, time.Time) (*domain.TopExpenseRow, error) {
				return &domain.TopExpenseRow{
					TransactionID: "txn-def", MerchantName: "Blue Bottle", Amount: 42.50,
					Category: customKey, CategoryDisplay: "Coffee runs", Date: txDate,
				}, nil
			},
		}
		result, err := NewAnalyticsService(mockRepo).GetTopExpense(ctx, "user-1", start, end)
		require.NoError(t, err)
		assert.Equal(t, "Coffee runs", result.PFCPrimaryDisplay)
	})

	t.Run("unnamed custom token never leaks the UUID", func(t *testing.T) {
		const customKey = "custom:9f1c2b3a-0000-4000-8000-000000000002"
		mockRepo := &mockAnalyticsRepository{
			getTopExpenseFn: func(context.Context, string, time.Time, time.Time) (*domain.TopExpenseRow, error) {
				return &domain.TopExpenseRow{
					TransactionID: "txn-ghi", MerchantName: "Unknown", Amount: 10.0,
					Category: customKey, CategoryDisplay: "", Date: txDate,
				}, nil
			},
		}
		result, err := NewAnalyticsService(mockRepo).GetTopExpense(ctx, "user-1", start, end)
		require.NoError(t, err)
		assert.Equal(t, unnamedCustomCategoryLabel, result.PFCPrimaryDisplay)
		assert.NotContains(t, result.PFCPrimaryDisplay, "9f1c2b3a-0000-4000-8000")
	})
}

// TestGetUnusualSpend_ResolvesDisplayForProductionShapedRows closes another of the five: unlike the
// two single-item reads above, GetUnusualSpend returns a slice, so one 3-row fixture (mirroring the
// expense/income pattern already in this file) covers all three shapes.
func TestGetUnusualSpend_ResolvesDisplayForProductionShapedRows(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	txDate := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	const namedCustomKey = "custom:9f1c2b3a-0000-4000-8000-000000000001"
	const unnamedCustomKey = "custom:9f1c2b3a-0000-4000-8000-000000000002"

	mockRepo := &mockAnalyticsRepository{
		getUnusualSpendFn: func(context.Context, string, time.Time, time.Time, time.Time, float64) ([]domain.UnusualSpendRow, error) {
			return []domain.UnusualSpendRow{
				// Production shape: CategoryDisplay carries the RAW enum for a PFC row.
				{TransactionID: "txn-1", MerchantName: "Whole Foods", Amount: 300, Category: "FOOD_AND_DRINK", CategoryDisplay: "FOOD_AND_DRINK", Date: txDate, BaselineAvg: 100, SigmaAbove: 4},
				{TransactionID: "txn-2", MerchantName: "Therapist", Amount: 200, Category: namedCustomKey, CategoryDisplay: "Therapy", Date: txDate, BaselineAvg: 50, SigmaAbove: 3},
				{TransactionID: "txn-3", MerchantName: "Unknown", Amount: 100, Category: unnamedCustomKey, CategoryDisplay: "", Date: txDate, BaselineAvg: 20, SigmaAbove: 2},
			}, nil
		},
	}

	result, err := NewAnalyticsService(mockRepo).GetUnusualSpend(ctx, "user-1", start, end)
	require.NoError(t, err)
	require.Len(t, result.Items, 3)

	assert.Equal(t, "Food And Drink", result.Items[0].PFCPrimaryDisplay,
		"a PFC row must be resolved through the registry, not echoed back as its raw enum")
	assert.Equal(t, "Therapy", result.Items[1].PFCPrimaryDisplay)
	assert.Equal(t, unnamedCustomCategoryLabel, result.Items[2].PFCPrimaryDisplay,
		"a custom row whose name has not reached effective_category_name yet must not render as a UUID")

	for _, item := range result.Items {
		assert.NotContains(t, item.PFCPrimaryDisplay, "custom:")
		assert.NotContains(t, item.PFCPrimaryDisplay, "9f1c2b3a-0000-4000-8000")
	}
}

// TestGetCategoryBreakdownForMerchant_ResolvesDisplayForProductionShapedRows closes the last of the
// five. TestAnalyticsService_GetCategoryBreakdownForMerchant (analytics_service_test.go) already
// covers the merge-by-key behaviour for a NAMED custom category; this covers the PFC and
// UNNAMED-custom shapes that test does not, using three distinct keys so nothing merges.
func TestGetCategoryBreakdownForMerchant_ResolvesDisplayForProductionShapedRows(t *testing.T) {
	ctx := context.Background()
	merchantID := "550e8400-e29b-41d4-a716-446655440000"
	filters := domain.SpendingFilters{
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
	}
	const namedCustomKey = "custom:9f1c2b3a-0000-4000-8000-000000000001"
	const unnamedCustomKey = "custom:9f1c2b3a-0000-4000-8000-000000000002"

	mockRepo := &mockAnalyticsRepository{
		getCategoryBreakdownForMerchantFn: func(context.Context, string, string, domain.SpendingFilters) ([]domain.CategoryBreakdownForMerchantRow, error) {
			return []domain.CategoryBreakdownForMerchantRow{
				// Production shape: CategoryDisplay carries the RAW enum for a PFC row.
				{Category: "GENERAL_MERCHANDISE", CategoryDisplay: "GENERAL_MERCHANDISE", Total: 400, Count: 5},
				{Category: namedCustomKey, CategoryDisplay: "Gifts", Total: 200, Count: 3},
				{Category: unnamedCustomKey, CategoryDisplay: "", Total: 100, Count: 2},
			}, nil
		},
	}

	result, err := NewAnalyticsService(mockRepo).GetCategoryBreakdownForMerchant(ctx, "user-123", merchantID, filters)
	require.NoError(t, err)
	require.Len(t, result.Categories, 3)

	byKey := map[string]string{}
	for _, c := range result.Categories {
		byKey[c.Category] = c.PFCPrimaryDisplay
	}
	assert.Equal(t, "General Merchandise", byKey["GENERAL_MERCHANDISE"],
		"a PFC row must be resolved through the registry, not echoed back as its raw enum")
	assert.Equal(t, "Gifts", byKey[namedCustomKey])
	assert.Equal(t, unnamedCustomCategoryLabel, byKey[unnamedCustomKey],
		"a custom row whose name has not reached effective_category_name yet must not render as a UUID")

	for _, display := range byKey {
		assert.NotContains(t, display, "custom:")
		assert.NotContains(t, display, "9f1c2b3a-0000-4000-8000")
	}
}
