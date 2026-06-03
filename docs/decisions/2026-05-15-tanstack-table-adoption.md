# ADR: TanStack Table v8 Adoption

**Date:** 2026-05-15
**Status:** Accepted
**Issue:** #1127 (parent) / #1140 (Phase 0 setup)

---

## Context

BoostedBudget has 11 tables across the frontend:

- **6 tables sharing the shared `DataTable` facade:** TransactionTable, AccountTable, BudgetTable, RuleTable, AccountBreakdownTable, TopMerchantsTable
- **1 near-duplicate raw table:** AutoCategorizationRuleTable (413 LOC, hand-rolled thead/tbody)
- **3 admin raw tables:** UsersPage, WebhooksPage, DeadLetterPage (no shared component)
- **1 embedded raw table:** RuleExecutionHistory

The shared `DataTable` component (`apps/web/src/shared/components/DataTable/`) implements sorting, filtering (select + range), loading states, empty states, row actions, and PostHog PII masking (`data-ph-mask`). It currently owns all table primitives from scratch (no third-party table engine).

Upcoming requirements include column resizing, column reordering, column pinning, and virtual scrolling (for large transaction lists). Implementing these from scratch across 11 tables would require ~2,000+ LOC and carry high regression risk.

---

## Decision Table

| Decision | Choice | Rationale |
|---|---|---|
| Adopt TanStack? | Yes, headless `@tanstack/react-table` v8 | Future capability (resize/reorder/pin/virtualization) without writing table primitives from scratch. v8 is current stable (v8.21.3); v9 is alpha-only as of 2026-05-15. |
| Facade vs. expose? | Facade-over-TanStack inside `DataTable` | Rewriting all 6 `DataTable` consumers risks regressions; consumers keep the existing `BBColumnDef<T>` / `DataTableProps<T>` API surface. |
| Column type? | Narrow our own `BBColumnDef<T>`, translate internally | Intersecting `TanStackColumnDef` causes unusable TypeScript errors. We translate from `BBColumnDef` to `TanStackColumnDef` inside the facade. |
| Persistence? | `localStorage` first; server-side as follow-up | ~600 LOC of backend work without validated user demand. localStorage covers the immediate use-case. |
| Server pagination? | Optional opt-in prop; consumers own UI | Page-level paginator UX should not be coupled to the table primitive. |
| Bundle strategy? | New `tanstack` manualChunk; do NOT raise vendor budget | Per #1126 policy: every new dependency that is not a feature-level import must be budget-tracked independently. Isolates TanStack regressions from vendor. |
| PostHog masking? | `td` rendering stays in the `DataTable` facade, NOT delegated to `flexRender` | Highest GDPR risk if cell rendering is delegated; `data-ph-mask` placement must remain in our code. |

---

## Baseline Bundle Measurements

Captured at commit `cc830cec` (staging, post #1126 per-route bundle budgets).
Full numbers in `docs/decisions/bundle-baseline.json`.

| Chunk | Before (gz) | After Phase 0 (gz) | Delta |
|---|---|---|---|
| vendor | 760.72 KB | 760.72 KB | 0 KB |
| entry | 17.61 KB | 17.61 KB | 0 KB |
| transactions | 15.07 KB | 15.07 KB | 0 KB |
| accounts | 15.23 KB | 15.23 KB | 0 KB |
| budgets | 19.68 KB | 19.68 KB | 0 KB |
| rules | 14.20 KB | 14.20 KB | 0 KB |
| reports | 3.22 KB | 3.22 KB | 0 KB |
| net-worth | 4.82 KB | 4.82 KB | 0 KB |
| savings | 6.92 KB | 6.92 KB | 0 KB |
| dashboard | 18.00 KB | 18.00 KB | 0 KB |
| auto-categorization | 12.07 KB | 12.07 KB | 0 KB |
| admin | 18.17 KB | 18.17 KB | 0 KB |
| billing | 11.53 KB | 11.53 KB | 0 KB |
| legal | 14.87 KB | 14.87 KB | 0 KB |
| profile | 9.83 KB | 9.83 KB | 0 KB |
| css | 24.07 KB | 24.07 KB | 0 KB |
| **tanstack** | _(none)_ | **0.09 KB** | +0.09 KB |

The `tanstack` chunk (0.09 KB gz in Phase 0) is a Rollup edge artifact — TanStack library code is fully tree-shaken since no consuming code imports exported values yet. The chunk will grow to ~14–18 KB gz in Phase 1 when `useReactTable` is wired into DataTable against the 25 KB budget.

### react-window removal

`react-window` (216 KB on disk) had **zero import sites** in `apps/web/src/**` (confirmed via grep for `react-window`, `FixedSizeList`, `VariableSizeList`, `AutoSizer`). The library was already fully tree-shaken from the vendor bundle — removal produced **0 KB vendor delta**. The package and its `@types/react-window` dev dependency have been removed from `apps/web/package.json`.

---

## Consequences

### Positive
- Phase 1 can wire `DataTable` to TanStack headless APIs without touching any consumer.
- Column resizing, reordering, pinning, and virtualization become low-cost to add.
- The `tanstack` budget entry (25 KB gz) will catch any accidental bundle bloat from TanStack upgrades.
- react-window removal simplifies `package.json` (0 KB bundle impact since it was tree-shaken, but reduces attack surface).

### Negative / Risks
- **React 19 compatibility:** TanStack v8 is designed for React 16–19. The `flexRender` helper works with React 19's concurrent features. Monitor for issues during Phase 1.
- **`tanstack` chunk is 96 B in Phase 0:** The `DataTable` side-effect import (`import '@/shared/lib/tanstack'`) forces chunk creation. Phase 1 will replace the side-effect import with real usage, growing the chunk to ~14–18 KB.
- **Circular chunk warnings (pre-existing):** The build emits circular chunk warnings for `billing → admin → profile → billing` and others. These are pre-existing from before this ADR and do not affect correctness; tracked separately.

---

## Implementation Plan

| Phase | Issue | Work |
|---|---|---|
| Phase 0 (this PR) | #1140 | Install v8, manualChunk, size-limit entry, ADR, baseline measurement |
| Phase 1 | #1141 | Wire `DataTable` to `useReactTable`, maintain facade API |
| Phase 2 | #1142 | Migrate `AutoCategorizationRuleTable` and admin raw tables |
| Phase 3+ | #1143+ | Column persistence (localStorage), virtualization, server pagination opt-in |

---

## References

- `apps/web/src/shared/components/DataTable/DataTable.tsx` — facade component
- `apps/web/src/shared/components/DataTable/types.ts` — `BBColumnDef<T>` public API
- `apps/web/src/shared/lib/tanstack.ts` — single TanStack import/re-export point
- `.size-limit.json` — per-route bundle budgets
- `apps/web/vite.config.ts` — `manualChunks` configuration
- `docs/decisions/bundle-baseline.json` — raw measured numbers
- [TanStack Table v8 docs](https://tanstack.com/table/v8)
