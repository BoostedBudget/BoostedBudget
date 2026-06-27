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

The tooltip and sr-only table express each link as `value / hubTotal`. `hubTotal` is now returned by `buildSankeyData` directly (computed from `max(incomeSum, expenseSum)` — the filtered array sums) rather than recomputed in the component from the API's `total_income`/`total_spent` fields. This ensures percentages and link widths always use the same denominator even when filtered rows cause the API total to diverge from the visible sum (Codex fix #5 + G10 gap fix).

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

**Gap analysis (`/requirements-gap-analysis`):** one functional gap found (percentage denominator inconsistency, G10 — fixed in PR #1346 second commit); test-coverage gaps G1/G2/G3/G4/G5/G6/G11 addressed; G7/G9 downgraded (transitively covered). Secondary review by `feature-dev:code-reviewer` (Codex fallback per CLAUDE.md) caught G10 and confirmed all other gaps.
