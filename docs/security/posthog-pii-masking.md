# PostHog PII Masking Policy

PostHog has **five independent leakage paths**. `data-ph-mask` covers the first; the others are
closed by client configuration, URL sanitization, and the `before_send` denylist:

| Path | Mechanism | How we protect it |
|---|---|---|
| Session replay — DOM text | `maskTextSelector: '*'` (default-deny) with `maskTextFn` un-masking `[data-ph-unmask]` | Wrap PII elements; DataTable `maskedColumnIds`; `data-ph-mask` markers are defence-in-depth under the global mask |
| Session replay — DOM attributes | `maskAllElementAttributes: true` masks recorded attribute values | Session-replay masking covers element attributes: `session_recording.maskAllElementAttributes: true` is forwarded to rrweb (probe-verified at the pinned SDK version); keep `aria-label`s de-PII'd regardless |
| Heatmap click data — URL keys | `$$heatmap` keys `$heatmap_data` by `location.href`, so a URL carrying PII leaks in the event key | `sanitizeValue` remaps each `$heatmap_data` key through `sanitizeAnalyticsUrl` and drops `isSensitiveAnalyticsPath` entries before capture |
| Autocapture event properties | `$el_text` captured on clicks/changes | `autocapture: false` in posthog config |
| `trackEvent` custom properties | Passed explicitly by caller | `before_send` denylist; pass IDs not names |

---

## How session-replay masking works

`posthog-js` is initialised with `maskTextSelector: '*'` (default-deny) and `maskAllInputs: true`, plus a `maskTextFn` that un-masks `[data-ph-unmask]`. Before any payload leaves the browser, PostHog replaces the **text content** of masked elements with `****`. The live DOM is unchanged — only the recorded stream is affected.

`data-ph-mask` is applied at the `<td>` level by `DataTable` (via `maskedColumnIds`), or inline on `<span>` / `<div>` wrappers for card/modal content. Element *attributes* are covered by `maskAllElementAttributes: true`; see the ARIA/attribute rule below for why structural labels remain required.

## How autocapture is handled

`autocapture` is **disabled** (`autocapture: false`). Autocapture records `$el_text`, CSS selectors, and attributes for every click/change/submit, independently of session replay. A click on `<button>Delete $1,234 transaction</button>` would leak the amount via `$el_text` even if the element is `data-ph-mask`'d in the replay. Disabling autocapture eliminates this path entirely.

A `before_send` hook additionally redacts known PII properties from any event before transmission, using the same matcher Sentry's `beforeSend`/`beforeBreadcrumb` share (`piiScrubbing.ts`'s `isPiiKey`, shared via `PII_PROPERTY_DENYLIST`). Matching is **token-based**, not exact-string: a key is split on camelCase boundaries and the full separator set `[_\-./\s:\\]` — not just `_`/`-` — so `currentBalance` → `['current', 'balance']`, `institutionName` → `['institution', 'name']`, and punctuation-delimited spellings like `merchant.name` or `merchant/name` also split into `['merchant', 'name']` rather than surviving as one un-split token. Tokens are lowercased and redacted if any contiguous window is in the denylist — so a camelCase *or* punctuation-joined property doesn't need to appear literally in the list to be caught; it's caught via its token(s).

The denylist itself (`PII_PROPERTY_DENYLIST` in `piiScrubbing.ts`) currently includes bare entries like `merchant`, `nickname`, `institution`, `balance`, `amount`, `email`, plus multi-word compounds like `account_name`, `institution_name`, `current_balance`, `target_amount`, `current_amount`, `total_saved`, `customer_email`, `query_string`, `cookies` — see that file for the authoritative, current list. Two words are **deliberately excluded** as bare entries to avoid over-scrubbing: `account` (would redact benign fields like `accountCount`) and `name` (would redact `typeName`/`fieldName`). `description` was also removed as a bare entry for the same reason (`descriptionLength` would over-match); this means a literal `description` field's *content* is no longer protected by this token match, and relies instead on `data-ph-mask` at the display layer. `accountName` is still caught, not via a bare `account` or `name` token, but via the two-token adjacent-window check matching the `account_name` compound entry.

**`*_id` / `*Id` keys are covered by an explicit named allowlist, not a blanket carve-out — and the exact token list used for the comparison matters.** `isPiiKey` (`piiScrubbing.ts`) holds an `ID_ALLOWLIST` containing exactly two entries, matched on the key's *tokenized* form so both spellings hit one entry: `merchant_id` (covers `merchant_id` and `merchantId`) and `institution_id` (covers `institution_id` and `institutionId`). The allowlist comparison deliberately runs against the key's **unfiltered** token join — before digit tokens are dropped — so that a digit-infix variant like `merchant_1_id` (tokens `['merchant', '1', 'id']`, unfiltered join `merchant_1_id`) does not collapse into the exact string `merchant_id` and slip through. Only a key whose *unfiltered* tokens join to exactly `merchant_id` or `institution_id` skips the bare-token window scan; every other key — including any other `*_id`/`*Id` field, present or future, and any digit-infix variant such as `merchant_1_id`, `merchant_99_id`, `institution_1_id`, or `merchant_id2` — goes through the ordinary scan and is redacted if it contains a denylisted token. This is why `{ merchant_id: uuid }` (the sanctioned `trackEvent` pattern below) survives even though `merchant` alone is a bare denylist entry, while `emailId`, `balance_id`, and `nickname_id` are all still redacted (each contains a denylisted token — `email`, `balance`, `nickname` — and none is in `ID_ALLOWLIST`). `account_id` is also still redacted, but via a different path: it is itself a full `PII_PROPERTY_DENYLIST` entry, so the bare-key check on line 1 of `isPiiKey` catches it before the allowlist is even consulted. The digit filter still runs, but only afterward and only for the *window scan*'s token list, not the allowlist comparison — that's a second, narrower purpose: dropping the `1` from `account1Name` lets the remaining `account`/`name` tokens sit adjacent and form the `account_name` window, so `account1Name` still gets redacted. Reusing that same digit-filtered list for the allowlist comparison — as a future "simplification" that looks equivalent — would silently reopen the `merchant_1_id`-style hole; the two token lists exist for different reasons and must stay separate. This is the **third** distinct version of this carve-out: v1 exempted *any* key whose last token was `id`, which let `email_id`/`nickname_id`/`balance_id` through — a real privacy regression covered by `piiScrubbing.test.ts`'s "defect 2" test block; v2 named the two `ID_ALLOWLIST` entries but compared them against digit-filtered tokens, which let `merchant_1_id`-style digit-infix variants through; v3 (current) compares against unfiltered tokens and closes both holes. Add a new key to `ID_ALLOWLIST` only if it is itself an opaque identifier with no PII in its name — never just because it ends in `id`. Because `tokenize` now splits on the full punctuation set above, the allowlist comparison also exempts punctuation spellings of the same two identifiers — `merchant.id`, `merchant/id`, `institution.id`, `institution/id` — since they tokenize to the identical `['merchant', 'id']` / `['institution', 'id']` pair as `merchant_id`/`merchantId`. This is deliberate, not a gap: the allowlist's rationale is about the value an opaque id identifies, not which separator joins the two words, so a dotted or slashed spelling of the same identifier carries the same non-PII payload.

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
- **Rule dry-run results** — `RulePreviewPanel` (#1625) renders RAW transactions: the merchant,
  the description, the amount and the current category of every row a candidate rule would match.
  It is as sensitive as the transactions table itself, and more concentrated — the rows shown are
  precisely the ones the user is about to act on. Masked per-cell via `data-ph-mask` on every
  `<td>`, never on the table, so the column headers and the fixed field/operator vocabulary stay
  legible in replay while the contents do not. TWO paths render user content and both are
  masked: the match rows, and the VALIDATION ERROR line — the mapper returns the domain error
  verbatim and several of those quote the user's own value back (a malformed date is echoed;
  an unknown-field message names what was sent), so the error is not a fixed string.
  Coverage is uneven by design and worth knowing: the match rows are enforced twice (a local
  assertion in `RulePreviewPanel.test.tsx` and a sentinel sweep in `pii-coverage.test.tsx`),
  the error line only by the local test, because the central sweep drives a SUCCESSFUL
  response. Codex review found the error path unmasked after this entry already claimed
  coverage.
  ⚠ Its `RULE_PREVIEWED` / `RULE_SAVED` analytics carry counts, booleans and the field/operator
  vocabulary ONLY — never a condition value, which is user-typed financial detail. None of those
  property names is an `isPiiKey` match (verified, not assumed), so scrubbing is not what keeps
  them safe; the choice of what to send is.
- **Postgres error context in the DLQ** — `JobErrorList` (#1631) renders what a `*pgconn.PgError`
  actually carried, and Postgres's `DETAIL` embeds the OFFENDING ROW VALUES. For the motivating
  holdings-sync 23505 those are UUIDs, but the identical mechanism on a unique-email constraint
  prints `Key (email)=(someone@example.com) already exists.` The same is true of `HINT`, of the
  Postgres `message` (a 22P02 quotes the value it could not parse), of every layer of the wrapped
  error chain, and of the flattened raw message that was always there. All of those are masked at
  the element, individually — never at the block, and never via `maskedColumnIds`.
  ⚠ The SQLSTATE, constraint name, table, schema, column, severity and routine are schema
  identifiers rather than row data and are deliberately left LEGIBLE; `DeadLetterPage`'s
  `last_error` column shows only those two of them (code + constraint) and is deliberately absent
  from that table's `maskedColumnIds`, because masking it would delete the only signal it carries.
  Round 22 in `pii-coverage.test.tsx` enforces both directions.
  ⚠ Nothing here may reach an `aria-label`/`title`/`alt`/`placeholder`. The scanner checks
  attributes irrespective of masking precisely so this cannot regress quietly.
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
- **The attribute rule, in full**: any value interpolated into `aria-label`, `title`, `alt`, or `placeholder` is recorded by session replay unless masked by `maskAllElementAttributes: true` — `data-ph-mask` itself only strips *text content*, never attributes, and attribute masking is defence-in-depth, not a substitute for a structural label. The durable fix is to make the attribute value itself free of PII — a structural/generic label, not a masked one. Two worked examples in this tree now share both the same `aria-labelledby` → masked `sr-only` span *mechanism* **and** the same opaque-id source:
  - `AccountCard.tsx` (`aria-labelledby`, not `aria-label`): the link points at a same-page `<span id={useId()} data-ph-mask>` whose *text* carries the account type and mask digits, and that text is masked normally. The `id` attribute and the `aria-labelledby` reference to it are both an opaque `useId()` value, never source-derived — this branch (commit `48d69253`) had introduced `account.id` into both of those attributes, and that has been corrected here in favour of `useId()`. The link's `href` (`to={`/accounts/${account.id}`}`) is the **one** remaining place `account.id` appears in attribute text, and it predates this branch: routing depends on the id being in the URL, and reworking account URLs to avoid that is an out-of-scope routing change, not a TODO to pick up here. There is a real tension worth naming there: `account_id` is itself a `PII_PROPERTY_DENYLIST` entry in `piiScrubbing.ts` (redacted from analytics event properties), yet it appears in this one plain attribute (`href`). That is accepted for `href` specifically, not for the `id`/`aria-labelledby` pair, which no longer needs the trade-off at all.
  - `IncomeSourceItem.tsx`'s `getRemoveLabelText` (`aria-labelledby`, not `aria-label`): the value (e.g. a tag name) is interpolated into a label string, but that string is never placed in an attribute — it's rendered as the *text* of a same-page `<span id={useId()} className="sr-only" data-ph-mask>`, referenced by the remove button's `aria-labelledby`. The `id` attribute itself is always an opaque `useId()` value, never source-derived, so the button's own attributes never carry the interpolated value; the value is free to appear in the *computed accessible name* because that name is resolved from masked text, not read from an attribute.
- **URL search params with PII**: PostHog records the full URL. Don't push merchant names, account nicknames, or amounts into URL state.
- **`trackEvent` with human-readable PII**: `trackEvent('drilldown', { merchant: merchantName })` leaks the name. Pass `{ merchant_id: uuid }` instead.
- **Wrapping card roots**: Wrapping `<div data-ph-mask>` around an entire card masks CTAs and sort buttons in the replay, reducing its debugging value.
- **Secrets/capability tokens are not covered by this policy, and session replay does not care about the distinction**: everything above is about *identifying* a user. A bearer token, magic link, or other capability credential rendered in the DOM (e.g. `DeleteAccountSection.tsx`'s old account-deletion cancellation link, embedded in both `href` and link text) is a different risk — anyone who obtains it can *act*, not just learn who someone is — but session replay captures it exactly as readily, and an `href`/`src` attribute is exactly as unmaskable as any other attribute. There is no masking fix for a secret: the only correct answer is to never render it (e.g. call the API directly from a button handler, keeping the token in memory only). If you find one of these, treat it as a security bug, not a masking gap — `data-ph-mask` cannot and should not be reached for.

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
