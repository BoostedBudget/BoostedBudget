package services

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

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

// validateCategoryToken rejects a `custom:` grouping key whose suffix is not a UUID (#1626 Phase 2,
// review finding X2).
//
// WHY IT IS A GATE AND NOT A NORMALISATION. GetCategoryTotalForPeriod resolves a custom category's
// name through `cc.id = $5::uuid`, so the suffix is CAST by Postgres rather than compared as text —
// and an unparseable one fails the whole statement, which MapError classifies as a 500. Before that
// scalar subquery landed the token was only ever a text comparison and a malformed one matched no
// transactions, so this closes a regression rather than a pre-existing hole.
//
// ⚠ It says nothing about whether the category EXISTS, and must not: a valid UUID naming a
// tombstoned or absent custom category is a legitimate request that answers with a zero total and
// the "Custom category" fallback label. Only the SHAPE is checked, and only for the custom prefix —
// a PFC enum is not a UUID and is left alone.
//
// ⚠ uuid.Parse, not a regexp: it is the same parser the ::uuid cast will apply, so the gate cannot
// admit a spelling Postgres would then reject.
func validateCategoryToken(categoryKey string) error {
	if !strings.HasPrefix(categoryKey, customCategoryTokenPrefix) {
		return nil
	}
	suffix := strings.TrimPrefix(categoryKey, customCategoryTokenPrefix)
	if _, err := uuid.Parse(suffix); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidCategoryToken, suffix)
	}
	return nil
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

func sortCategoryBreakdownItems(rows []domain.CategoryBreakdownItem) {
	sort.SliceStable(rows, func(i, j int) bool {
		return lessCategoryDisplay(
			categoryDisplayRow{Key: rows[i].Category, Display: rows[i].PFCPrimaryDisplay, Total: rows[i].Total},
			categoryDisplayRow{Key: rows[j].Category, Display: rows[j].PFCPrimaryDisplay, Total: rows[j].Total},
		)
	})
}

// mergeCategoryRows folds rows sharing a stable KEY into one, preserving first-seen order.
//
// WHY EVERY CATEGORY-SHAPED READ NEEDS THIS. The SQL groups by key AND display so a custom
// category can be named rather than showing a UUID, but `effective_category_name` is denormalised
// per transaction and refreshed ASYNCHRONOUSLY after a rename. Mid-refresh, one category yields
// TWO rows — one per name — so a read that emits them verbatim shows the category twice with its
// spend split and both percentages halved. Fixing a cosmetic bug that way creates a correctness one.
//
// ⚠ Merging is only two thirds of the fix. The caller must ALSO compute percentages from the
// merged total and RE-SORT afterwards: two merged 60s outrank an unmerged 100, and the rows arrive
// in the SQL's pre-merge order. A fixture with only one key cannot catch either omission.
//
// addTo accumulates row i's numbers into the survivor and is where the display tie-break lives, so
// each response shape keeps its own field names without duplicating the merge.
func mergeCategoryRows(n int, keyAt func(int) string, addTo func(dst, src int)) []int {
	survivor := make(map[string]int, n)
	order := make([]int, 0, n)
	for i := 0; i < n; i++ {
		k := keyAt(i)
		if j, ok := survivor[k]; ok {
			addTo(j, i)
			continue
		}
		survivor[k] = i
		order = append(order, i)
	}
	return order
}
