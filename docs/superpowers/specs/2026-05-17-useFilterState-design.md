# `useFilterState` — Design Spec

**Issue**: #1129 | **Date**: 2026-05-17

## Problem

Five per-feature filter hooks (`useTransactionFilters`, `useAccountFilters`, `useRuleFilters`, `useBudgetFilters`, `useNetWorthBreakdownFilters`) had grown to ~960 LOC of near-duplicate logic: URL param serialization, active-filter counting, clear-all behavior, and codec wiring — all re-implemented for each feature. Any cross-cutting change required touching every hook individually.

## Solution

A single generic hook at `apps/web/src/shared/hooks/useFilterState.ts`. Consumers declare a typed spec literal describing their filter fields; the hook owns all URL state mechanics and returns a stable, richly-typed interface.

## API

```ts
// Codecs for scalar fields
type ScalarCodec =
  | 'string' | 'number' | 'integer' | 'boolean'
  | { parse: (raw: string) => unknown; serialize: (v: unknown) => string | null }

// Field descriptors — use `kind` (not `type`)
type FilterFieldSpec =
  | {
      kind: 'scalar'
      urlKey?: string           // defaults to the spec object key
      codec: ScalarCodec
      defaultValue?: unknown    // field is "inactive" when value === defaultValue
      activeWhen?: (v: unknown) => boolean  // override active-count predicate
    }
  | {
      kind: 'multi-select'
      urlKey?: string
      serialize?: 'csv' | 'repeat'   // default 'csv'
    }
  | {
      kind: 'range'
      urlKeys?: { min: string; max: string }  // defaults: '<key>_min' / '<key>_max'
      codec?: 'float' | 'integer'             // default 'float'
    }

type FilterSpec = Record<string, FilterFieldSpec>

interface UseFilterStateOptions {
  /** URL keys to delete on every filter mutation (e.g., ['offset'] for pagination). */
  clearKeysOnWrite?: string[]
}

// InferFilters<S> maps spec keys to their runtime types.
// Custom codecs preserve the parse return type (e.g. parse: () => Foo → filters.x: Foo | undefined).
type InferFilters<S extends FilterSpec> = { ... }

interface UseFilterStateReturn<S extends FilterSpec> {
  filters: InferFilters<S>
  setFilter:  <K extends keyof S & string>(key: K, value: InferFilters<S>[K]) => void
  setFilters: (updates: Partial<InferFilters<S>>) => void
  clearFilter: (key: keyof S & string) => void
  clearAllFilters: () => void   // only removes spec-declared keys; sort is untouched
  activeFilterCount: number
  hasActiveFilters: boolean
}

function useFilterState<S extends FilterSpec>(
  spec: S,
  options?: UseFilterStateOptions,
): UseFilterStateReturn<S>
```

> **Important**: declare spec literals with `as const satisfies FilterSpec` — not `: FilterSpec` — so TypeScript preserves the literal `kind` discriminants and `InferFilters<S>` resolves correctly.

## Consumer Pattern

All migrated hooks follow this shape:

```ts
const SPEC = {
  institution: { kind: 'multi-select' },
  balance:     { kind: 'range' },
  search:      { kind: 'scalar', codec: 'string' as const },
  amount_type: { kind: 'scalar', codec: 'string' as const, defaultValue: 'all',
                 activeWhen: (v) => v !== undefined && v !== 'all' },
} as const satisfies FilterSpec

export function useMyFilters() {
  const state = useFilterState(SPEC)
  const { sort, setSort, resetSort, toggleSortColumn } = useSortUrlState({ ... })

  const clearAllFilters = useCallback(
    (opts?: { includeSort?: boolean }) => {
      state.clearAllFilters()
      if (opts?.includeSort) resetSort()
    },
    [state.clearAllFilters, resetSort],
  )

  return { ...state.filters, sort, clearAllFilters, ... }
}
```

Feature-specific projections (e.g. `datePreset`, `statusFilter` in transactions) live in the thin wrapper, not in `useFilterState`.

## Key Design Decisions

**No sort or derive in FilterSpec.** Sort state is managed externally via `useSortUrlState`; projections stay in consumer hooks. Keeping `useFilterState` focused prevents it from becoming another monolith.

**`clearKeysOnWrite` as declarative escape hatch.** Rather than exposing an `onParamsChange` callback, consumers declare which extra URL keys (e.g., `offset` for pagination) should be deleted on every filter mutation. This keeps the hook stateless and composable.

**`activeFilterCount` semantics per `kind`.** Multi-select: active when `length > 0`. Range: active when `min` or `max` is defined. Scalar: active when present and not equal to `defaultValue` (or an explicit `activeWhen` predicate overrides).

**Custom codecs preserve parse return type.** `InferFilters<S>` uses a conditional type to infer `T` from the codec's `parse` return — `{ parse: () => Foo }` produces `filters.x: Foo | undefined`. Consumers using the string-literal codecs (`'string'`, `'number'`, etc.) get exact types with no casting.

**Atomic `clearAllFilters` when composing with sort.** In React Router v6, two sequential `setSearchParams` functional calls both read the pre-update URL — the second overwrites the first. Consumers that need atomic filter+sort clearing (e.g., `useNetWorthBreakdownFilters`) implement a single `setSearchParams` call rather than chaining `genericClearAll()` + `resetSort()`.

**`clearAllFilters()` scope.** Only removes keys declared in the spec — sort state is untouched. This matches user expectation: clearing filters should not reset the current sort order.

**All writes use `{ replace: true }`.** Filter changes do not push new history entries, avoiding a back-button trap where each keystroke is a navigation step.

## Hooks Migrated

All 5 hooks now consume `useFilterState`. Final shipped LOC:

| Hook | Before | After | Notes |
|---|---|---|---|
| `useTransactionFilters` | ~308 | 166 | `clearKeysOnWrite: ['offset']`; date/amount projections retained; custom `setSort` clears offset |
| `useAccountFilters` | ~171 | 95 | |
| `useBudgetFilters` | ~143 | 87 | |
| `useRuleFilters` | ~171 | 95 | `priority` uses `codec: 'integer'` |
| `useNetWorthBreakdownFilters` | ~167 | 133 | Exposes legacy `sortColumn`/`sortDirection`/`setSortColumn` surface for `AccountBreakdownTable`; atomic `clearAllFilters` |

Total consumer reduction: ~960 → ~576 LOC. Shared implementation: ~400 LOC (with full codec support, type inference, and test suite).
