# `useFilterState` — Design Spec

**Issue**: #1129 | **Date**: 2026-05-17

## Problem

Five per-feature filter hooks (`useTransactionFilters`, `useAccountFilters`, `useRulesFilters`, `useBudgetFilters`, and a nascent `useGoalsFilters`) had grown to ~960 LOC of near-duplicate logic: URL param serialization, active-filter counting, clear-all behavior, and codec wiring — all re-implemented for each feature. Any cross-cutting change (e.g., switching to `{ replace: true }` writes) required touching every hook individually.

## Solution

A single generic hook at `apps/web/src/shared/hooks/useFilterState.ts`. Consumers declare a typed spec literal describing their filter fields; the hook owns all URL state mechanics and returns a stable, richly-typed interface.

## API

```ts
// Field descriptors
type FilterFieldSpec =
  | { type: 'scalar'; defaultValue?: string }
  | { type: 'multi-select' }
  | { type: 'range'; valueType?: 'number' | 'integer' | 'date' };

// A map of field key → descriptor
type FilterSpec = Record<string, FilterFieldSpec>;

interface UseFilterStateOptions {
  /** URL param keys to set to '' whenever any filter write occurs */
  clearKeysOnWrite?: string[];
}

type InferFilters<S extends FilterSpec> = {
  [K in keyof S]: S[K] extends { type: 'multi-select' }
    ? string[]
    : S[K] extends { type: 'range' }
      ? { min?: number | string; max?: number | string }
      : string | undefined;
};

interface UseFilterStateReturn<S extends FilterSpec> {
  filters: InferFilters<S>;
  setFilter: <K extends keyof S>(key: K, value: InferFilters<S>[K]) => void;
  clearAllFilters: () => void;
  activeFilterCount: number;
}

function useFilterState<S extends FilterSpec>(
  spec: S,
  options?: UseFilterStateOptions,
): UseFilterStateReturn<S>;
```

> **Important**: declare spec literals with `as const satisfies FilterSpec` — not `: FilterSpec` — so TypeScript preserves the literal `type` discriminants and `InferFilters<S>` resolves correctly.

## Consumer Pattern

All migrated hooks follow the same four-line shape:

```ts
const SPEC = { search: { type: 'scalar' }, status: { type: 'multi-select' } } as const satisfies FilterSpec;

export function useMyFilters(options?: UseFilterStateOptions) {
  return useFilterState(SPEC, options);
}
```

Any feature-specific projections (e.g., derived `hasActiveFilters`, formatted labels) live in the thin wrapper, not in `useFilterState`.

## Key Design Decisions

**No sort or derive in FilterSpec.** Sort state is managed externally via `useSortUrlState`; projections stay in consumer hooks. Keeping `useFilterState` focused prevents it from becoming another monolith.

**`clearKeysOnWrite` as declarative escape hatch.** Rather than exposing an `onParamsChange` callback, consumers declare which extra URL keys (e.g., `offset` for pagination) should be blanked on every filter write. This keeps the hook stateless and composable.

**`activeFilterCount` semantics.** Multi-select fields count when `length > 0`. Range fields count when `min` or `max` is defined. Scalar fields count when the value is present and differs from `defaultValue` (or an explicit `activeWhen` predicate overrides the comparison).

**`clearAllFilters()` scope.** Only removes keys declared in the spec — sort state is untouched. This matches user expectation: clearing filters should not reset the current sort order.

**All writes use `{ replace: true }`.** Filter changes do not push new history entries, avoiding a back-button trap where each keystroke is a navigation step.

## Hooks Migrated

| Hook | Approx. LOC before | LOC after | Notes |
|---|---|---|---|
| `useTransactionFilters` | ~320 | ~40 | `clearKeysOnWrite: ['offset']`; projection helpers retained in wrapper |
| `useAccountFilters` | ~180 | ~25 | Straightforward scalar + multi-select fields |
| `useRulesFilters` | ~240 | ~35 | Integer range codec (`valueType: 'integer'`) for amount bounds |
| `useBudgetFilters` | ~220 | ~30 | Date range fields; period projection stays in wrapper |

Total reduction: ~960 → ~130 LOC across consumer hooks, plus ~110 LOC in the shared implementation.

## Not Migrated

`useNetWorthBreakdownFilters` — the Net Worth Breakdown feature is not yet present on this branch. It will adopt the same pattern when that feature lands.
