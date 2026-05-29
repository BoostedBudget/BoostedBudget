# Design: #1204 — Transactions Column Spacing, Sub-category Filter, 3-state Sort

**Date**: 2026-05-28  
**Issue**: [#1204](https://github.com/BoostedBudget/BoostedBudget/issues/1204)  
**PR**: [#1205](https://github.com/BoostedBudget/BoostedBudget/pull/1205)  
**Status**: Implemented

## Problem

Three UX problems on the Transactions page:
1. Horizontal scrolling — total default column widths (~1230px) overflow 1280px viewports
2. Sub-category column had no filter UI despite the backend and URL state both supporting `pfc_detaileds`
3. The 3-state sort cycle (asc → desc → clear) was broken: five table wrappers converted the `null` clear signal into a direction toggle or silently dropped it; unsorted sortable columns showed no affordance

## Design Decisions

### Column Spacing

**Approach**: Reduce `defaultSize` values in `TransactionTable.tsx` — merchant 320→240, account/category 200→160, sub_category 180→140. Total: ~950px.

**Why px reduction over percentage widths**: All 11 tables in the app use fixed-px sizing with TanStack Table's `columnResizeMode: 'onChange'`. Switching to percentage widths would require reworking the entire resize mechanism. The px reduction is surgical and consistent with existing patterns.

**Sizing reset**: Bumped `SIZING_VERSION` constant (`useTablePreferences.ts`) from `v2-fixed-layout` → `v3-compact`. This global constant resets ALL tables' saved column widths on next load (order and visibility preserved). Acceptable — widths on other tables were also adjusted proportionally as part of this PR.

### Sub-category Filter

**Approach**: Add a `ColumnFilterDropdown` (multi-select, searchable) to the sub_category column def, backed by a new `getPfcDetailedFilterOptions()` helper that flattens `PFC_HIERARCHY` into a `{ value, label }[]`.

**State sharing**: The sub_category column filter shares `selectedDetaileds`/`onDetailedsChange` props with the existing `HierarchicalCategoryTreeSelect` on the Category column. Both write to the same `pfc_detaileds` URL state. This means:
- Selecting sub-cats in the category tree reflects in the sub-category column filter (and vice versa)
- The Category column header shows "active" (blue) when sub-category column filter is applied — intentional and informative, not a bug

**Alternative not taken**: A fully isolated `pfc_detaileds` state for the sub_category column separate from the category tree. Rejected because it would create two separate URL params for the same backend filter and confuse the active-filter count.

### Sort Prop API Unification

**Root cause**: Five table wrappers (`TransactionTable`, `BudgetTable`, `AccountTable`, `AutoCategorizationRuleTable`, `RuleTable`) each had a `handleSortChange` adapter. When `DataTable` emitted `null` (3rd click = clear), the adapter either converted it to a direction toggle or silently dropped it.

**Fix**: Remove all `handleSortChange` adapters. Unify prop API across all tables to `sort?: SortState` + `onSortChange?: (sort: SortState) => void`, matching `DataTable`'s native contract. Pass `onSortChange` (or `onSortChange!`) directly to `DataTable.onSortChange`.

**TransactionTable URL snap-back**: `useSortUrlState` always returns `DEFAULT_SORT` (Date desc) when the URL `sort` param is absent. So `setSort(null)` (3rd click) deletes the param → sort snaps back to Date desc. This is **correct behavior** for an API-driven table that always requires a sort order. "Clear" = "reset to default".

**Sort affordance**: Added `ChevronsUpDown` (Lucide) to `TableHeaderCell` — faint at rest, brightens on hover — so users see which columns are sortable before clicking. Benefits all 11 tabular UIs via the shared component.

## Files Changed

- `apps/web/src/features/transactions/components/TransactionTable.tsx` — column widths, filter, sort refactor
- `apps/web/src/features/transactions/TransactionsPage.tsx` — sort wiring
- `apps/web/src/features/budgets/components/BudgetTable.tsx` + `BudgetsPage.tsx`
- `apps/web/src/features/accounts/components/AccountTable.tsx` + `AccountsPage.tsx`
- `apps/web/src/features/rules/components/RuleTable.tsx` + `RulesPage.tsx`
- `apps/web/src/features/auto-categorization/components/AutoCategorizationRuleTable.tsx` + `AutoCategorizationPage.tsx`
- `apps/web/src/shared/categories/pfcOptions.ts` — `getPfcDetailedFilterOptions()`
- `apps/web/src/shared/components/TableHeaderCell/TableHeaderCell.tsx` — `ChevronsUpDown`
- `apps/web/src/shared/components/DataTable/useTablePreferences.ts` — `SIZING_VERSION` bump
