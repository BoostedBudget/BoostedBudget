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

A `before_send` hook additionally redacts known PII properties from any event before transmission, using the same matcher Sentry's `beforeSend`/`beforeBreadcrumb` share (`piiScrubbing.ts`'s `isPiiKey`, shared via `PII_PROPERTY_DENYLIST`). Matching is **token-based**, not exact-string: a key is split on camelCase boundaries and `_`/`-` (e.g. `currentBalance` → `['current', 'balance']`, `institutionName` → `['institution', 'name']`), lowercased, and redacted if any contiguous token window is in the denylist — so a camelCase property doesn't need to appear literally in the list to be caught; it's caught via its token(s).

The denylist itself (`PII_PROPERTY_DENYLIST` in `piiScrubbing.ts`) currently includes bare entries like `merchant`, `nickname`, `institution`, `balance`, `amount`, `email`, plus multi-word compounds like `account_name`, `institution_name`, `current_balance`, `target_amount`, `current_amount`, `total_saved`, `customer_email`, `query_string`, `cookies` — see that file for the authoritative, current list. Two words are **deliberately excluded** as bare entries to avoid over-scrubbing: `account` (would redact benign fields like `accountCount`) and `name` (would redact `typeName`/`fieldName`). `description` was also removed as a bare entry for the same reason (`descriptionLength` would over-match); this means a literal `description` field's *content* is no longer protected by this token match, and relies instead on `data-ph-mask` at the display layer. `accountName` is still caught, not via a bare `account` or `name` token, but via the two-token adjacent-window check matching the `account_name` compound entry.

**`*_id` / `*Id` keys are allowlisted, not scanned like other compounds.** A key whose last token is `id` (e.g. `merchant_id`, `institutionId`) is redacted only if the *whole* key is itself an explicit `PII_PROPERTY_DENYLIST` entry — currently only `account_id` and `link_session_id`. This is why `{ merchant_id: uuid }` (the sanctioned `trackEvent` pattern below) survives even though `merchant` alone is a bare denylist entry: without this carve-out, the trailing `id` token is irrelevant to the bare-token window scan, and `merchant_id`/`institution_id` would be deleted anyway, defeating the exact pattern this doc recommends. The carve-out is narrow and has two named exceptions, not a blanket "IDs are safe" guarantee: `account_id` and `link_session_id` are deliberately still redacted because they're explicit denylist entries in their own right (an account or session identifier is itself considered sensitive here, unlike an opaque merchant/institution UUID). Any new `*_id` field that should be redacted must be added to the denylist as its own full compound entry — adding its non-id word as a new bare token will not catch it, since id-ending keys skip the bare-token scan entirely.

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
- **Staff-authored free text about a user** — e.g. the orphaned-Plaid-Item operator resolution note (`PlaidOrphanedItemsPage`). Nothing prevents an operator typing a name into it, and in that table the subject has already been erased. Masked on the note element, **not** via `maskedColumnIds`: masking the whole `<td>` would hide the attesting staff id and make the attestation anonymous, which is what the feature exists to prevent. Round 15 in `pii-coverage.test.tsx` enforces both directions.
- **Verbatim vendor payloads** — the Paddle webhook `payload` block (`WebhookDetailModal`) is shown precisely because nobody knows in advance what is in it; it routinely carries customer email and billed totals. Masked on the `<pre>`, matching `DeadLetterDetailModal`'s raw-args and error blocks.
- **Security / holding names** — ticker symbols (e.g. `AAPL`), full security names (e.g. "Apple Inc."), and fund names reveal portfolio composition. The hand-built holdings table in `PortfolioHoldingsWidget` masks these per-cell via `data-ph-mask`; the `PortfolioHoldingsWidget` PII regression (Round 14 in `pii-coverage.test.tsx`) enforces this with a named sentinel (`SENTINEL_SECURITY_NAME`).

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
- **The attribute rule, in full**: any value interpolated into `aria-label`, `title`, `alt`, or `placeholder` is recorded **verbatim** by session replay — `data-ph-mask` only strips *text content*, never attributes, and there is no attribute-masking mechanism to reach for instead. The only fix is to make the attribute value itself free of PII — a structural/generic label, not a masked one. Two worked examples in this tree:
  - `AccountCard.tsx` (`aria-labelledby`, not `aria-label`): the link points at a same-page `<span id=... data-ph-mask>` whose *text* carries the account details and is masked normally; the attribute itself only ever holds the element id, never the PII.
  - `IncomeSourceItem.tsx`'s `getRemoveAriaLabel` (commit `010c136e`): the label is built from a `switch` on `source.type` and never interpolates the source's amount, tag text, or category label — it's generic by construction, so there's nothing in the attribute to leak.
- **URL search params with PII**: PostHog records the full URL. Don't push merchant names, account nicknames, or amounts into URL state.
- **`trackEvent` with human-readable PII**: `trackEvent('drilldown', { merchant: merchantName })` leaks the name. Pass `{ merchant_id: uuid }` instead.
- **Wrapping card roots**: Wrapping `<div data-ph-mask>` around an entire card masks CTAs and sort buttons in the replay, reducing its debugging value.

---

## Known limitations

- **ECharts SVG tooltips** (`NetWorthHistoryChart`, `BalanceHistoryChart`, `CashFlowChartWidget`, `AllocationDonutChart`, `PortfolioValueChart`): ECharts renders tooltips as DOM nodes inside the SVG tree. The `<EChartsChart>` wrapper already applies `data-ph-mask` to its container div, which masks all descendant tooltip content from PostHog session replay. The three investment dashboard widgets (`PortfolioSummaryWidget`, `PortfolioHoldingsWidget`, `AssetAllocationWidget`) reuse `AllocationDonutChart` / `PortfolioSummaryCard` components that carry `data-ph-mask` on their containers; the masked-ancestor invariant is verified by the Round-14 PII regression tests in `pii-coverage.test.tsx`. If you add a new chart that renders financial amounts in tooltips, verify the chart's ancestor carries `data-ph-mask`.
- ~~**`merchant_id` URL param** (`SpendingReportsPage`): The backend `/spending/by-merchant` endpoint accepts `search` but not `merchant_id`, so the merchant name is currently in the URL.~~ **Resolved** by issues #1163/#1165: a `merchants` table (migration 000075) gives every merchant a stable UUID; `GET /api/v1/analytics/spending-by-merchant` now returns `merchant_id`; `SpendingReportsPage` navigates to `/transactions?merchant_id=<uuid>` (never the name string); `GET /api/v1/transactions` accepts `merchant_id` as a filter.
- **Shadow DOM**: `TreeWalker` does not cross Shadow DOM boundaries. The codebase uses light DOM only (Radix portals render to `document.body`). If Shadow DOM is ever adopted, this policy must be revisited.
- **PostHog server-side**: `before_send` is client-side only. Consider a PostHog Cloud transformation pipeline as a defence-in-depth backstop.

---

## How to verify

```bash
# Automated regression gate (Rounds 1–15; run the whole file, the round count moves)
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
5. If passing as a `trackEvent` property, confirm the property name is caught by `isPiiKey` (`piiScrubbing.ts`) — either as a bare `PII_PROPERTY_DENYLIST` entry or via its camelCase/`_`/`-` tokens, not by grepping for the literal string in the list. Run `isPiiKey('yourFieldName')` (or check it in the `pii-coverage.test.tsx` suite) rather than assuming; `account`, `name`, and `description` are deliberately *not* bare entries, so a field like `descriptionText` needs an explicit mask elsewhere, not this denylist.
