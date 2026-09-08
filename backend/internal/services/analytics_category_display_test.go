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
