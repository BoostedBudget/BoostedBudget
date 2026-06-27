# Cash Flow Sankey Diagram — Design Doc

**Issue**: #958  
**PR**: #1346 (`feature/958-cash-flow-sankey`)  
**Gap remediation**: covered by PR #1346 (second commit; see `/requirements-gap-analysis` run)

---

## Why

The Spending Reports page showed category-level totals but no picture of where money flows from (income sources) to (expenses). Issue #958 requested a Monarch Money-style Sankey diagram. The primary value is cognitive: users immediately see which income streams fund which spending buckets, and whether they end the period in surplus or deficit.

---

## Hub model (key design decision)

BoostedBudget's analytics endpoints return **grouped totals only** — there is no transaction-level attribution of which income source funded which expense category. A naïve all-to-all Sankey (N income nodes × M expense nodes with proportional links) would invent causality that doesn't exist in the data.

**Chosen model: central hub with two-level fan.**
```
[income source A] ──┐
[income source B] ──┼──→ [Total Income hub] ──→ [expense cat 1]
[income source C] ──┘                       ──→ [expense cat 2]
                                            ──→ [Savings]         (surplus)
[Drawn from Savings/Credit] ────────────────┘                    (deficit)
```

Hub invariant: `hub inflow == hub outflow == max(incomeSum, expenseSum)`. This is computed from **filtered array sums** (not from the API's `total_income`/`total_spent` fields) so that zero/negative rows that are filtered client-side don't imbalance the diagram.

**Deficit path**: when `expenseSum > incomeSum`, a synthetic "Drawn from Savings / Credit" node flows into the hub to make inflow == outflow. Conversely when `incomeSum > expenseSum`, a "Savings" node captures the surplus outflow.

---

## ECharts contract

- **Registration**: `SankeyChart` registered in `shared/charts/echarts-setup.ts` (same pattern as all other chart types).
- **Node IDs**: `in:<category>`, `out:<category>`, `hub`, `savings`, `deficit`, `other`. The `in:`/`out:` prefixes prevent id collisions between income and expense nodes sharing the same category key (e.g. `TRANSFER_IN` / `TRANSFER_OUT`).
- **Label formatter**: always renders `params.data.name` (display name), never the internal id.
- **Link style**: `lineStyle: { color: 'gradient', opacity: 0.5 }` — theme-independent constant; node colors follow `CASH_FLOW_COLORS` / `CATEGORY_COLORS`.
- **Top-10 cap**: expenses sorted descending by total; top 10 shown individually; the rest collapsed into a non-navigable "Other Expenses (N categories)" node.
- **`total <= 0` filter**: income and expense items with zero or negative totals are excluded before building the graph.

---

## Click navigation

| Node kind | URL params |
|-----------|-----------|
| expense (PFC) | `pfc_primaries=<category>&amount_type=expense` |
| expense (custom) | `custom_category_ids=<uuid>&amount_type=expense` |
| income | `pfc_primaries=<category>&amount_type=income` |
| hub / savings / deficit / other | non-navigable (no click) |

All navigable links also append `start_date` and `end_date` from the Sankey's date range.

**Edge-click guard**: ECharts fires the same `onEvents.click` handler for node clicks AND link/edge clicks. The guard checks `p.dataType === 'node'` for the real-ECharts path; the test-mock path uses `!p.dataType && typeof p.dataIndex === 'number'` to index `sankeyData.nodes[dataIndex]`.

---

## Drilldown immunity

The Spending Reports page uses two independent `useSpendingByCategory` fetches:
1. **`sankeyTopLevelData`** — no `category` filter; feeds the Sankey.
2. **`categoryData`** — carries `drilldown?.category`; feeds the category breakdown chart.

Drilling into a PFC category changes `categoryData` without affecting `sankeyTopLevelData`, so the Sankey always displays the full expense universe (Codex fix #4).

---

## Percentage base (single source of truth)

The tooltip and sr-only table express each link as a percentage: `(value / hubTotal) * 100`, passed to `formatPercent()` which expects percentage **points** (0–100), not a ratio. `hubTotal` is returned by `buildSankeyData` directly (`max(filteredIncomeSum, filteredExpenseSum)`) rather than recomputed in the component from the API's `total_income`/`total_spent` fields. This ensures percentages and link widths always use the same denominator even when filtered rows cause the API total to diverge from the visible sum (Codex fix #5 + G10 + G12 fixes).

**Privacy mode and the sr-only table**: privacy mode hides dollar amounts in the visual ECharts tooltip (shows `label % only`). The sr-only accessibility table **always shows amounts**, even in privacy mode. Rationale: privacy mode is scoped to *screenshot sharing* (sr-only content never appears in a screenshot); a screen-reader user viewing their own finances should retain full dollar figures. This is a deliberate product decision confirmed in Round-2 gap review.

---

## Accessibility

- **sr-only table**: all `<tr>` entries mirror the Sankey links with source → target labels, dollar amounts, and percentages. The `<table>` has `aria-label="Cash flow data table"`.
- **Keyboard buttons**: navigable rows render `<button>` elements with `focus-visible:not-sr-only` classes so keyboard focus escapes the sr-only context and shows a visible ring (Codex fix #3).
- **PII masking**: the `<table>` carries `data-ph-mask` so PostHog session replay never captures currency amounts in the accessible table (Codex fix #2). The `[data-testid="echarts-chart"]` ECharts wrapper also carries `data-ph-mask` (via `MockEChartsChart` in tests / `EChartsChart.tsx` in production).

---

## Privacy toggle

Privacy mode (Eye / EyeOff button in the Cash Flow section header) switches the ECharts tooltip from `amount + %` to `% only`. The sr-only table always shows amounts (for screen readers). The toggle fires `ANALYTICS_EVENTS.REPORT_SANKEY_PRIVACY_TOGGLED` with `{ enabled: bool }`.

---

## Codex reviews (summary)

**Design review (pre-impl):** redesigned to hub model; explicit `in:`/`out:` id prefixes; `total<=0` filter; non-clickable "Other Expenses (N)"; explicit nav-param contract; explicit `lineStyle.color: 'gradient'`; WCAG sr-only table. All incorporated.

**Implementation review (post-impl, 5 MAJOR — all fixed in PR #1346 initial commit):** edge-click guard; `data-ph-mask` on sr-only table; `focus-visible:not-sr-only` on sr-only buttons; separate top-level Sankey fetch; hub invariant from array sums.

**Gap analysis Round 1 (`/requirements-gap-analysis`):** one functional gap (G10 — percentage denominator used API totals instead of filtered array sums, fixed in PR #1346 second commit); test-coverage gaps G1–G6/G11 addressed; G7/G9 downgraded (transitively covered). Secondary review: `codex:codex-rescue` stalled (600s watchdog) → fresh-context `feature-dev:code-reviewer` fallback per CLAUDE.md; it caught G10 that the primary pass missed.

**Gap analysis Round 2 (`/requirements-gap-analysis`, post-remediation):** Codex (`codex:codex-rescue`) completed and caught G12 — `formatPercent` expects percentage points (0–100) but the Sankey passed a raw ratio (0–1), rendering every percentage 100× too small (e.g. "0.75%" instead of "75.00%"). Fixed: both `pct` sites in `CashFlowSankeyChart.tsx` now compute `(value / hubTotal) * 100`. The round-1 G10 regression test was corrected to assert `'75.00%'`. G13 (residual `Math.max` on API aggregates in label copy) cleaned up. G14 date-range nav tests added. Privacy/sr-only table behavior confirmed deliberate (see §Percentage base). Second independent pass was load-bearing both rounds.

**Gap analysis Round 3 (`/requirements-gap-analysis`, convergence check):** `codex:codex-rescue` exited after `git status` without emitting a structured verdict (no usable deliverable). Per CLAUDE.md secondary-review fallback, a fresh-context `feature-dev:code-reviewer` was dispatched with an adversarial brief. It ran 8 refutation attempts — searched for a missed `formatPercent` caller, a ratio-bug re-locked in tests, percentages outside [0,100], G11 label-id leak, G4 edge-click nav, G6 PII mask — all held up or were confirmed non-defects by design. **Verdict: CONVERGED — all 15 requirements MET, no functional gaps.** The only candidate (AC4 page-level date-state test) was rated LOW / not worth adding: the meaningful date-branching logic is proven by component-level G14 tests, and a page test would only assert React prop-passing. Feature is complete.
