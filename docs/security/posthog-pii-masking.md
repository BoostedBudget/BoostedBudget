# PostHog PII Masking Policy

PostHog has **three independent leakage paths**. `data-ph-mask` only covers the first:

| Path | Mechanism | How we protect it |
|---|---|---|
| Session replay — DOM text | `maskTextSelector: '[data-ph-mask]'` | Wrap PII elements; DataTable `maskedColumnIds` |
| Session replay — DOM attributes | Attributes recorded verbatim | Never put PII in `aria-label`, `title`, `placeholder`, `alt` |
| Autocapture event properties | `$el_text` captured on clicks/changes | `autocapture: false` in posthog config |
| `trackEvent` custom properties | Passed explicitly by caller | `before_send` denylist; pass IDs not names |

---

## How session-replay masking works

`posthog-js` is initialised with `maskTextSelector: '[data-ph-mask]'` and `maskAllInputs: true`. Before any payload leaves the browser, PostHog replaces the **text content** of matching elements with `****`. The live DOM is unchanged — only the recorded stream is affected.

`data-ph-mask` is applied at the `<td>` level by `DataTable` (via `maskedColumnIds`), or inline on `<span>` / `<div>` wrappers for card/modal content. It does **not** mask element *attributes* — see the ARIA/attribute rule below.

## How autocapture is handled

`autocapture` is **disabled** (`autocapture: false`). Autocapture records `$el_text`, CSS selectors, and attributes for every click/change/submit, independently of session replay. A click on `<button>Delete $1,234 transaction</button>` would leak the amount via `$el_text` even if the element is `data-ph-mask`'d in the replay. Disabling autocapture eliminates this path entirely.

A `before_send` hook additionally strips known PII property names from any event before transmission:

```
merchant, merchant_name, nickname, institution, institution_name,
description, account_name, balance, amount, current_balance,
target_amount, current_amount, total_saved
```

`trackEvent` callers must pass IDs (UUIDs) as properties, never human-readable names.

---

## PII field classification

### MUST mask

- Currency amounts and account balances
- Percent-of-net-worth when the absolute total is also visible
- Account-number masks (`••••1234`, `****1234`, `xxxx1234`)
- Email addresses

### SHOULD mask

- Merchant names and transaction descriptions
- Account nicknames and official account names
- Institution names
- User-authored names: budget names, rule names, savings-goal names, category names (custom only — not Plaid system categories)
- User-authored transaction tags
- Custom rule descriptions

### MAY leave unmasked

- Aggregate counts (e.g. "3 accounts")
- Dates without accompanying amounts
- System-provided Plaid category labels (e.g. "Food and Drink")
- Currency codes (USD, EUR) without amounts
- Percentage change alone (e.g. "+2.3%")
- Status badges, generic UI text

---

## How to mask

### Inline content (cards, modals, spans)

```tsx
<span data-ph-mask>{account.nickname}</span>
```

Keep wraps tight — wrap the PII span, not the card root, to preserve replay usefulness.

### DataTable columns

Pass column ids to `maskedColumnIds`:

```tsx
<DataTable maskedColumnIds={['amount', 'merchant']} ... />
```

`DataTable` applies `data-ph-mask` at the `<td>` level. **Never** apply masking via `flexRender` cell renderers — this is an established invariant (`shared/components/DataTable/README.md`).

### Portals (filter dropdowns, dialogs, toasts)

Radix `ColumnFilterDropdown` renders option content in a portal — it does **not** inherit a parent `data-ph-mask`. Wrap PII in the option label:

```tsx
label: <span data-ph-mask>{account.nickname}</span>
```

Sonner toasts mount outside the React tree. Wrap PII inside the toast content itself:

```tsx
toast('Budget alert', {
  description: <span data-ph-mask>${amount}</span>,
});
```

Radix Dialog content renders in a portal but typically inherits `data-ph-mask` from a wrapper inside the dialog body — verify with `document.body` scan.

---

## What NOT to do

- **ARIA labels with PII**: `aria-label="Balance: $1,234"` is recorded verbatim in session replays. Use structural labels: `aria-label="Account balance"`.
- **`title` / `placeholder` / `alt` with PII**: Same reason. Use generic labels.
- **URL search params with PII**: PostHog records the full URL. Don't push merchant names, account nicknames, or amounts into URL state.
- **`trackEvent` with human-readable PII**: `trackEvent('drilldown', { merchant: merchantName })` leaks the name. Pass `{ merchant_id: uuid }` instead.
- **Wrapping card roots**: Wrapping `<div data-ph-mask>` around an entire card masks CTAs and sort buttons in the replay, reducing its debugging value.

---

## Known limitations

- **ECharts SVG tooltips** (`NetWorthHistoryChart`, `BalanceHistoryChart`, `CashFlowChartWidget`): ECharts renders tooltips as DOM nodes inside the SVG tree. The `<EChartsChart>` wrapper already applies `data-ph-mask` to its container div, which masks all descendant tooltip content from PostHog session replay. If you add a new chart that renders financial amounts in tooltips, verify the chart's ancestor carries `data-ph-mask`.
- **`merchant_id` URL param** (`SpendingReportsPage`): The backend `/spending/by-merchant` endpoint accepts `search` but not `merchant_id`, so the merchant name is currently in the URL. Follow-up: backend API change to accept `merchant_id`.
- **Shadow DOM**: `TreeWalker` does not cross Shadow DOM boundaries. The codebase uses light DOM only (Radix portals render to `document.body`). If Shadow DOM is ever adopted, this policy must be revisited.
- **PostHog server-side**: `before_send` is client-side only. Consider a PostHog Cloud transformation pipeline as a defence-in-depth backstop.

---

## How to verify

```bash
# Automated regression gate (10 page-level tests)
cd apps/web && bun run test src/test/pii-coverage.test.tsx -v

# Full frontend suite
cd apps/web && bun run test

# Manual: PostHog session replay
# 1. Deploy branch to staging
# 2. Log in and traverse each feature page (open filter dropdowns, trigger toasts)
# 3. Open PostHog > Session Recordings for the resulting session
# 4. Confirm no currency / balance / account-mask / merchant / nickname / institution
#    / budget name / rule name / tag appears unmasked in the recording
# 5. Open PostHog > Events tab — confirm no $el_text or trackEvent property leaks PII
# 6. In the replay's DOM tree view — confirm no aria-label/title/placeholder/alt leaks
```

---

## Adding a new PII field

1. Classify it (MUST / SHOULD / MAY) using the table above.
2. Choose the mechanism: inline `data-ph-mask`, `maskedColumnIds`, portal label wrap, or `before_send` denylist.
3. Add a sentinel to the relevant page test in `src/test/pii-coverage.test.tsx`.
4. Update this doc's classification table.
5. If passing as `trackEvent` property, confirm the property name is in the `before_send` denylist.
