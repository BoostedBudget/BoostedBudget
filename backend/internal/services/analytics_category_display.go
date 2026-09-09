package services

import (
	"sort"
	"strings"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// ─── Category display resolution (#1626) ─────────────────────────────────────────────────────────
//
// Analytics reads group by a synthetic KEY and separately select a raw DISPLAY column:
//
//	key      custom:<uuid>            (custom override)   |  FOOD_AND_DRINK / ..._FAST_FOOD (PFC)
//	display  the custom category name                     |  the RAW PFC enum
//
// The display column (transactions.effective_category_name) is deliberately NOT a display name for
// PFC rows — migration 000073 records that "display conversion is frontend responsibility". So the
// two kinds need opposite treatment, and getting that backwards is what produced #1626: the service
// pushed a `custom:<uuid>` token through PFCPrimaryDisplayName, which misses the registry map and
// title-cases whatever it is given, turning the UUID itself into the category's name.

// customCategoryTokenPrefix marks a grouping key that names a row in custom_categories rather than
// a Plaid PFC enum. It must match effectiveCategoryExpr's `'custom:' || ...` arm.
const customCategoryTokenPrefix = "custom:"

// unnamedCustomCategoryLabel is shown when a custom category's name has not reached
// effective_category_name yet. That column is nullable and is refreshed asynchronously after a
// rename (effective_category_refresh), so "no name yet" is a reachable state — and it is the exact
// state that used to render as a UUID.
const unnamedCustomCategoryLabel = "Custom category"

// resolveCategoryDisplay turns a grouping key plus the repository's raw display column into the
// label the API exposes.
//
// A custom token is NEVER resolved through the PFC registry: there is no PFC enum to find, and the
// registry's fallback would echo the token back as a name.
func resolveCategoryDisplay(categoryKey, storedDisplay string) string {
	if strings.HasPrefix(categoryKey, customCategoryTokenPrefix) {
		if name := strings.TrimSpace(storedDisplay); name != "" {
			return name
		}
		return unnamedCustomCategoryLabel
	}
	return pfcDisplayName(categoryKey)
}

// categoryDisplayRow is the minimal shape sortCategoryRows needs: a stable key, the RESOLVED
// display, and the amount. Callers project their own row types into it rather than this package
// growing a shared response type.
type categoryDisplayRow struct {
	Key     string
	Display string
	Total   float64
}

// sortCategoryRows applies the order the API promises, AFTER labels have been resolved.
//
// The SQL already orders by `total DESC, category_display ASC`, but `category_display` is the raw
// enum for PFC rows, so transport order and displayed order diverge the moment the service resolves
// a label. Re-sorting here keeps them in agreement. `total DESC` stays the primary leg — that is
// the existing contract — and the stable key is the final leg so rows tying on both total and label
// still have exactly one defined order.
func sortCategoryRows(rows []categoryDisplayRow) {
	sort.SliceStable(rows, func(i, j int) bool { return lessCategoryDisplay(rows[i], rows[j]) })
}

// lessCategoryDisplay is the single comparator every category-shaped response sorts through.
func lessCategoryDisplay(a, b categoryDisplayRow) bool {
	if a.Total != b.Total {
		return a.Total > b.Total
	}
	if a.Display != b.Display {
		return a.Display < b.Display
	}
	return a.Key < b.Key
}

// sortSpendingByCategory and sortIncomeSources apply sortCategoryRows' ordering to the two
// response shapes that carry a resolved label. They project into categoryDisplayRow rather than
// duplicating the comparator, so the two directions cannot drift apart.

func sortSpendingByCategory(rows []domain.SpendingByCategory) {
	sort.SliceStable(rows, func(i, j int) bool {
		return lessCategoryDisplay(
			categoryDisplayRow{Key: rows[i].Category, Display: rows[i].PFCPrimaryDisplay, Total: rows[i].Total},
			categoryDisplayRow{Key: rows[j].Category, Display: rows[j].PFCPrimaryDisplay, Total: rows[j].Total},
		)
	})
}

func sortIncomeSources(rows []domain.IncomeSourceItem) {
	sort.SliceStable(rows, func(i, j int) bool {
		return lessCategoryDisplay(
			categoryDisplayRow{Key: rows[i].Category, Display: rows[i].PFCPrimaryDisplay, Total: rows[i].Total},
			categoryDisplayRow{Key: rows[j].Category, Display: rows[j].PFCPrimaryDisplay, Total: rows[j].Total},
		)
	})
}

