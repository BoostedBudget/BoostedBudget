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
| `execute` | Chain API calls against your account (reads + confirmation-gated writes) |
| `recipe.list` | List your saved workflow recipes |
| `recipe.save` | Save a named read-only workflow recipe for later reuse |
| `recipe.run` | Resolve a saved recipe's calls to execute |

---

## Example Prompts

### 1. "What's my dining spend this month?"

The agent uses `search` to find the right analytics operation, then `execute` to fetch the data:

```
You: What's my dining spend this month?

Agent calls:
  search({ query: "spending category", tag: "Analytics" })
  → finds getCategoryTotalForPeriod

  execute({
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

### 2. "Categorize all my unreviewed transactions and show me my top expense this month"

The agent chains three calls: list uncategorized transactions, bulk-mark them as reviewed, then find the top expense. Writes require confirmation.

```
You: Review all my uncategorized transactions and show me my biggest expense this month.

Agent calls:
  execute({
    calls: [
      {
        operationId: "getTransactions",
        params: { reviewed: false, limit: 50 }
      },
      {
        operationId: "bulkSetTransactionReviewed",
        body: { transaction_ids: ["txn-1", "txn-2", ...], reviewed: true }
      },
      {
        operationId: "getTopExpense",
        params: { start_date: "2026-05-01", end_date: "2026-05-31" }
      }
    ]
  })

  → First call: list 12 unreviewed transactions
  → Second call: returns confirmation challenge (write operation)
    "bulkSetTransactionReviewed requires confirmation. Re-invoke with
     confirmationToken: '3f7a9c...' "

Agent: I found 12 unreviewed transactions. Should I mark them all as reviewed?
You: Yes, go ahead.

Agent re-invokes with confirmationToken → all 12 marked reviewed
  → Third call: { merchant: "AMZN", amount: 187.43, date: "2026-05-14" }

Agent responds:
  Marked 12 transactions as reviewed. Your biggest expense this month was
  $187.43 at Amazon on May 14th.
```

---

### 3. "Save a reusable cash flow summary recipe"

The agent saves a named read-only recipe you can run on-demand anytime:

```
You: Create a recipe called "Weekly Cash Flow" that summarizes my cash flow
     for the last 7 days. I want to run it whenever I need it.

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
  Run it anytime with: recipe.run({ id: "rec_abc123" })
  This will return the API calls to pass to execute().
```

> **Note:** Scheduled recipe execution (running recipes automatically on a cron) is planned
> for a future release — see [follow-up #1220](https://github.com/BoostedBudget/BoostedBudget/issues/1220).
> In v1, recipes are saved and run on-demand.

---

## Write Safety

All write operations (categorize transactions, create budgets, etc.) require **explicit confirmation** before executing:

1. Agent calls `execute` with a write operation
2. Server returns a `confirmationToken` and describes what will happen
3. Agent presents the confirmation to you
4. You say yes → agent re-invokes with the `confirmationToken`
5. Write executes and is recorded in the audit log

Tokens are **one-time use** and expire in 5 minutes.

**Permanently excluded from MCP** (cannot be enabled): Plaid account linking/unlinking, account deletion, admin operations, money movement.

---

## Architecture

```
Claude Desktop  ←→  mcp.boostedbudget.com/mcp  (Cloudflare Worker)
                          │  Clerk OAuth 2.1 (DCR + PKCE)
                          │  McpAgent (Durable Object)
                          │  search / execute / recipe.*
                          ▼
                    api.boostedbudget.com  (Go backend, unchanged)
                          │
                          ▼
                       PostgreSQL
```

The Worker **never** touches the database directly. All data goes through the Go backend, which enforces user ownership scoping, subscription tier limits, and validation. Your credentials are injected server-side and never visible in tool inputs or outputs.

---

## Known Limitations

- **Revocation**: Revoking the OAuth grant in the **Clerk dashboard** (or via a BoostedBudget account settings page once that UI is built) takes effect within the token TTL (~5 minutes) rather than instantly. This is a known limitation of stateless JWTs.
- **Sandbox**: `execute()` calls a typed API client (not agent-authored TypeScript in a V8 isolate). This is a simplified Code Mode implementation.
- **Rate limits**: 60 requests/minute, 5 concurrent per session.
- **Unattended writes**: Automation rules and scheduled write recipes are not yet supported (planned for a future release).

---

## Deployment

See [DEPLOYMENT.md](./DEPLOYMENT.md) for infrastructure setup instructions (Cloudflare Workers, Clerk OAuth app, secrets, DNS).
