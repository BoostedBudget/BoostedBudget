# BoostedBudget MCP Server

Remote MCP (Model Context Protocol) server for BoostedBudget, hosted at `mcp.boostedbudget.com`.

Connect Claude Desktop, Cursor, or any MCP-compatible agent to your BoostedBudget account and query transactions, run analytics, manage budgets, and save repeatable workflow recipes — without leaving your agent.

---

## Setup

### Claude Desktop

1. Open `~/Library/Application Support/Claude/claude_desktop_config.json`
2. Add the BoostedBudget MCP server:

```json
{
  "mcpServers": {
    "boostedbudget": {
      "type": "remote",
      "url": "https://mcp.boostedbudget.com/mcp"
    }
  }
}
```

3. Restart Claude Desktop
4. An OAuth browser window will open — log in with your BoostedBudget account and grant access
5. You're connected. Try one of the example prompts below.

**Staging (for testing):** use `https://staging-mcp.boostedbudget.com/mcp` instead.

### Cursor / other MCP clients

Follow your client's instructions for adding a remote MCP server at `https://mcp.boostedbudget.com/mcp`.

---

## Available Tools

| Tool | Description |
|------|-------------|
| `search` | Discover available API operations by keyword, tag, or class |
| `readExecute` | Chain admitted non-destructive, caller-scoped API calls (no `confirmationToken` input) |
| `writeExecute` | Chain any API calls against your account, including confirmation-gated writes |
| `recipe.list` | List your saved workflow recipes |
| `recipe.save` | Save a named read-only workflow recipe for later reuse |
| `recipe.run` | Resolve a saved recipe's calls to pass to readExecute()/writeExecute() |

### Migrating from `execute`

The legacy `execute` tool was intentionally removed without an alias. If a client cached the old
tool list, reconnect or restart its MCP session so it re-enumerates the current tools. Use
`readExecute` for admitted non-destructive calls. Use `writeExecute` for writes and for ordered
read-to-write calls that must remain in one ordered batch.

`readExecute` refuses destructive, confirmation-gated, excluded, and ordinary mutating
operations. It is scoped to the authenticated caller, but it is not literally side-effect-free:
`getPrivacySettings` may lazily create that caller's default privacy settings on first access,
while `getUserProfile` attempts a best-effort update of the existing caller's last-login timestamp.
That update can fail without failing the read, and the returned `last_login_at` is the value fetched
before the update attempt. These are the two acknowledged caller-scoped side-effecting GETs.

---

## Example Prompts

### Response detail

Resource responses default to `compact`: useful financial and workflow fields stay,
while provider payloads, duplicate metadata, cosmetics, and bulky diagnostics are
available in `full`. Focused analytics, previews, counts, and acknowledgments already
contain useful summaries and are returned unchanged.

The response catalog explicitly classifies all 142 exposed operations: 71 have compact
projections and 71 intentionally pass through. Adding an exposed operation or changing
a projected response schema requires reviewing its catalog policy; artifact generation
checks coverage against the complete OpenAPI schemas.

Choose detail **on each call**, beside `operationId`, not in query `params` or `body`:

```json
{
  "calls": [
    { "operationId": "getTransactions", "params": { "limit": 10 } },
    { "operationId": "getAccounts", "detail": "full" }
  ]
}
```

`search({ query: "getTransactions", detail: "full" })` documents the full response;
the default documents the compact projection. Response descriptions distinguish
the underlying API contract from MCP's selected fields. Discovery omits `$schema`
tooling links from its field documentation; full execution preserves them.

Each actually reduced result includes a full-detail hint. After a completed write,
follow the suggested **read** operation to inspect current state; do not repeat the
write to obtain more detail. Execution-history reads in full return historical
definition snapshots, which can differ from a rule or boost's current definition.
Boost-scoped history recovery uses global history so it can find executions after
their parent is deleted. Follow the hint from offset zero and match the execution
IDs retained in the compact payload. If the status-filtered history ends before
all IDs are found, remove status and restart at offset zero. Stop once all IDs are
found or unfiltered history ends, and report any missing IDs; separate reads do
not provide a consistent snapshot. The MCP server does not fetch these pages
automatically.

Transaction dates, signs, currency when supplied, pending/reversal/review flags,
effective categories, and investment fields survive compaction. Missing currency
never implies USD. The unified feed's `source` distinguishes banking from investment;
`data_source` identifies the provider. Bare transaction `source` identifies the
provider instead. Embedded account identity is included only when already supplied
by the API; the MCP server does not issue extra account lookups.

Full detail preserves the API's decoded response shape, subject to the existing
per-result and batch output limits. Unknown response fields or incompatible shapes
cause a complete-result fallback with a warning, rather than silently losing data.
Compaction reduces model-facing output, not backend queries or API-to-worker traffic.

The deterministic synthetic [size regression suite](src/responses/size-regression.test.ts)
measures 51,470 full versus 27,567 compact rendered characters (46.4% less), including
headers and full-detail hints. It covers transaction pages, accounts, definitions,
history, budgets, goals, notifications, a focused summary, and a production-config
recipe fixture. These are character measurements, not a guaranteed token reduction
for every live response. A small response can grow because its safe full-detail hint
costs more than the fields removed.

Saved recipe calls retain their detail settings; existing recipes default to compact.
`recipe.run` resolves complete call objects without executing them. Execute the returned
calls in batches of at most 25. A resolution exceeding 100,000 rendered characters
returns an explicit error with no executable calls, rather than a truncated call.

Deploy the recipe-detail-compatible backend before this worker. Rolling back the
worker is independent; keep a backend that preserves recipe detail when decoding and
reserializing JSONB specs, since an older backend can otherwise discard saved settings.

### 1. "What's my dining spend this month?"

The agent uses `search` to find the right analytics operation, then `readExecute` to fetch the data:

```
You: What's my dining spend this month?

Agent calls:
  search({ query: "spending category", tag: "Analytics" })
  → finds getCategoryTotalForPeriod

  readExecute({
    calls: [{
      operationId: "getCategoryTotalForPeriod",
      params: {
        category: "FOOD_AND_DRINK",
        start_date: "2026-05-01",
        end_date: "2026-05-31"
      }
    }]
  })
  → { total: 312.47, transaction_count: 18, category: "FOOD_AND_DRINK" }

Agent responds:
  You've spent $312.47 on Food & Drink this month across 18 transactions.
```

---

### 2. "Review my unreviewed transactions — recategorize wrong ones, approve the rest"

The review workflow has two phases:
1. **Triage phase** — check the inbox, recategorize any misclassified transactions (`bulkUpdateTransactionCategory`)
2. **Approve phase** — mark the batch as reviewed (`bulkSetTransactionReviewed`)

> **Important:** each call's result is truncated at ~100,000 characters, and self-describes when it is. Always use a small `limit` (≤ 10) when fetching transactions and page through with `next_cursor` if `unreviewed_count` is larger. `getTransactionReviewSummary` gives you the total before you start paging.

```
You: Review my unreviewed transactions.

Agent calls:
  search({ query: "review summary inbox" })
  → finds getTransactionReviewSummary, getTransactions, bulkUpdateTransactionCategory,
           bulkSetTransactionReviewed

  readExecute({
    calls: [
      {
        operationId: "getTransactionReviewSummary"
      },
      {
        operationId: "getTransactions",
        params: { reviewed: false, limit: 10 }
      }
    ]
  })
  → getTransactionReviewSummary: { unreviewed_count: 23, reviewed_by_rule_count: 5,
      top_unreviewed_categories: [{ category_key: "FOOD_AND_DRINK", display_name: "Food & Drink", count: 9 }, ...] }
  → getTransactions: { transactions: [{id: "txn-1", name: "Starbucks", ...}, ...], next_cursor: "abc123" }

Agent: I see 23 unreviewed transactions. Showing the first 10. "Starbucks" is tagged
       as FOOD_AND_DRINK — looks right. "Netflix" is tagged as ENTERTAINMENT but you
       want it under your custom "Subscriptions" category. Should I recategorize Netflix
       and then approve the batch?
You: Yes, recategorize Netflix to Subscriptions and approve all 10.

  writeExecute({
    calls: [
      {
        operationId: "bulkUpdateTransactionCategory",    // recategorize first
        body: {
          transaction_ids: ["txn-netflix"],
          kind: "custom",
          custom_category_id: "cat-subscriptions-uuid"
        }
      },
      {
        operationId: "bulkSetTransactionReviewed",       // then approve the batch
        body: { transaction_ids: ["txn-1", "txn-2", ..., "txn-10"], reviewed: true }
      }
    ]
  })
  → bulkUpdateTransactionCategory requires confirmation.
    "Re-invoke with confirmationToken: '3f7a9c...'"

Agent: Ready to recategorize Netflix and mark 10 transactions as reviewed. Confirm?
You: Yes.

Agent re-invokes with confirmationToken → Netflix recategorized, all 10 marked reviewed.

Agent: Done. 13 unreviewed transactions remain (next_cursor: "abc123" to continue).
       Run again to review the next page.
```

**Approval-only shortcut** (when no recategorization is needed):

```
  writeExecute({
    calls: [{
      operationId: "bulkSetTransactionReviewed",
      body: { transaction_ids: ["txn-1", ..., "txn-10"], reviewed: true }
    }]
  })
```

---

### 3. "Save a reusable cash flow summary recipe"

The agent saves a named read-only recipe you can resolve on demand, then submit to an executor:

```
You: Create a recipe called "Weekly Cash Flow" that summarizes my cash flow
     for the last 7 days. I want to resolve and execute its calls whenever I need it.

Agent calls:
  recipe.save({
    name: "Weekly Cash Flow",
    description: "Cash flow summary for the past 7 days",
    calls: [
      {
        operationId: "getCashFlowSummary",
        params: {
          start_date: "{{date_minus_7}}",
          end_date: "{{date_today}}"
        }
      },
      {
        operationId: "getSpendingByCategory",
        params: {
          start_date: "{{date_minus_7}}",
          end_date: "{{date_today}}"
        }
      }
    ]
  })

  → confirmation prompt (save is a write)
  → confirmed → recipe saved with id: "rec_abc123"

Agent responds:
  Saved "Weekly Cash Flow" recipe (id: rec_abc123).
  Resolve it anytime with: recipe.run({ id: "rec_abc123" })
  This returns, but does not execute, the API calls. Submit them to readExecute() where admitted,
  or writeExecute() otherwise.
```

---

### 4. "Show my pending savings transfer recommendations" (Boosts)

The Boosts feature surfaces automated transfer suggestions. Use the `status` parameter to view different stages:

```
You: Show me my pending savings recommendations.

Agent calls:
  search({ query: "transfer suggestions status" })
  → finds getTransferSuggestions
  → description: "Use the `status` query parameter... Valid values: pending, accepted, completed, rejected, expired."

  readExecute({
    calls: [{
      operationId: "getTransferSuggestions",
      params: { status: "pending" }
    }]
  })
  → { items: [...], total: 3, ... }

You: Which ones are waiting for a matching transaction?

  readExecute({
    calls: [{
      operationId: "getTransferSuggestions",
      params: { status: "accepted" }
    }]
  })
  → { items: [{id: "ts-1", to_account_name: "High-Yield Savings", amount: 500, ...}], total: 1 }

Agent: You have 1 accepted suggestion awaiting a match: $500 to High-Yield Savings.
       It'll auto-complete when a matching transfer is detected.
```

**Valid `status` values:** `pending` · `accepted` · `completed` · `rejected` · `expired`

---

### 5. "List my transfer automation boosts" (Boosts)

Use the `boost_type` parameter to scope the boosts list to transfer-generating types:

```
You: What automation boosts do I have set up for savings transfers?

Agent calls:
  search({ query: "automation boosts filter type" })
  → finds getBoosts
  → description: "Use the repeatable `boost_type` query parameter... Transfer-generating types are transfer_suggestion and savings_goal."

  readExecute({
    calls: [{
      operationId: "getBoosts",
      params: { boost_type: ["transfer_suggestion", "savings_goal"] }
    }]
  })
  → [{ name: "Monthly Savings", boost_type: "savings_goal", enabled: true, ... }]

Agent: You have 1 transfer boost: "Monthly Savings" (savings_goal type, enabled).
```

**Transfer-generating `boost_type` values:** `transfer_suggestion` · `savings_goal`
**All types:** `transfer_suggestion` · `savings_goal` · `budget_alert` · `category_rollover`

> **Note:** Scheduled recipe execution (running recipes automatically on a cron) is planned
> for a future release — see [follow-up #1220](https://github.com/BoostedBudget/BoostedBudget/issues/1220).
> In v1, recipes are saved and resolved on demand; submit the returned calls to
> `readExecute` where admitted or `writeExecute` otherwise.

---

## Write Safety

All confirmation-gated write operations (categorize transactions, create budgets, etc.) require
**explicit confirmation** before executing. `writeExecute` may also contain reads, including an
ordered read-to-write batch. When confirmation is required, the token covers the exact complete
`calls` array; the challenge round executes none of its calls.

1. Agent calls `writeExecute` with a write operation
2. Server returns a `confirmationToken` and describes what will happen; nothing executes yet
3. Agent presents the confirmation to you
4. You say yes → agent re-invokes with the identical `calls` array and `confirmationToken`
5. Write executes and is recorded in the audit log

Tokens are **one-time use** and expire in 5 minutes.

**Permanently excluded from MCP** (cannot be enabled): Plaid account linking/unlinking, account deletion, admin operations, money movement.

---

## Architecture

```
Claude Desktop  ←→  mcp.boostedbudget.com/mcp  (Cloudflare Worker)
                          │  Clerk OAuth 2.1 (DCR + PKCE)
                          │  McpAgent (Durable Object)
                          │  search / readExecute / writeExecute / recipe.*
                          ▼
                    api.boostedbudget.com  (Go backend, unchanged)
                          │
                          ▼
                       PostgreSQL
```

The Worker **never** touches the database directly. All data goes through the Go backend, which enforces user ownership scoping, subscription tier limits, and validation. Your credentials are injected server-side and never visible in tool inputs or outputs.

---

## Known Limitations

- **Revocation**: Revoking the OAuth grant in the **Clerk dashboard** (or via a BoostedBudget account settings page once that UI is built) is detected within approximately **30–60 seconds** via a layer-2 Clerk token introspection check on every `/mcp` and `/sse` request (cached per isolate for 30 s). This is a best-effort SLA — not a hard security boundary. Caveats: (a) the check fails open if Clerk's API is unreachable (Clerk outages do not block MCP); (b) the per-isolate cache means a revoked grant may remain valid for up to 30 s per Worker isolate; (c) long-lived `/sse` streams are checked at connection time, not per message — they may survive until the next reconnect after revocation. The underlying Clerk OAuth access token TTL is ~1 day.
- **Sandbox**: `readExecute()`/`writeExecute()` call a typed API client (not agent-authored TypeScript in a V8 isolate). This is a simplified Code Mode implementation.
- **Rate limits**: 60 requests/minute, 5 concurrent per session.
- **Unattended writes**: Automation rules and scheduled write recipes are not yet supported (planned for a future release).

---

## Deployment

See [DEPLOYMENT.md](./DEPLOYMENT.md) for infrastructure setup instructions (Cloudflare Workers, Clerk OAuth app, secrets, DNS).
