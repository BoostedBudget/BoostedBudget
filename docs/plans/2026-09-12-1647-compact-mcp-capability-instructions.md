# Compact MCP Capability Instructions Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add compact, protocol-level capability guidance to the BoostedBudget MCP initialize response for GitHub issue #1647 while preserving the 2024-11-05 handshake shape.

**Architecture:** Pass the approved guidance through the MCP SDK's dedicated `McpServer` options argument so it appears as top-level initialize `instructions`. Keep `serverInfo.description` unset and verify both the exact copy and its byte budget through the authenticated transport integration test.

**Tech Stack:** TypeScript, Bun, Vitest, Model Context Protocol SDK, Cloudflare Workers Agents SDK

---

## Approved copy

```text
Analyze accounts, transactions, spending, net worth, and investments; manage envelope budgets, savings goals, categories, and tags. Boost definitions are read-only here; review their history, budget alerts, and transfer, rollover, or savings-goal suggestions. MCP writes require confirmation; BoostedBudget never moves money. Use search first, then readExecute or writeExecute.
```

### Task 1: Pin the initialize-response contract (RED)

**Files:**
- Modify: `apps/mcp-server/integration/mcp/transport.integration.test.ts`

1. Extend `initialize()` to return top-level `instructions` and `serverInfo.description` as `serverDescription`, in addition to its existing fields.
2. In Scenario 1, destructure both fields and assert that `instructions` exactly equals an independently duplicated literal of the approved copy.
3. Assert `new TextEncoder().encode(instructions).byteLength <= 400` and `serverDescription` is `undefined`.
4. Run `cd apps/mcp-server && bun run test:integration:transport`.
5. Confirm the suite fails because the initialize response is missing the required instructions.

### Task 2: Supply protocol-level instructions (GREEN)

**Files:**
- Modify: `apps/mcp-server/src/mcp/agent.ts`

1. Change only `BoostedBudgetMcpAgent.server` by passing this second `McpServer` argument:

```ts
{
  instructions:
    'Analyze accounts, transactions, spending, net worth, and investments; manage envelope budgets, savings goals, categories, and tags. Boost definitions are read-only here; review their history, budget alerts, and transfer, rollover, or savings-goal suggestions. MCP writes require confirmation; BoostedBudget never moves money. Use search first, then readExecute or writeExecute.',
}
```

2. Do not add `serverInfo.description`, constants, helpers, dependencies, generated artifacts, or tool/manifest/schema/config changes.
3. Run `cd apps/mcp-server && bun run test:integration:transport`; expect all eight tests to pass.
4. Run `cd apps/mcp-server && bun run typecheck`; expect exit code 0.

### Task 3: Refactor, verify, review, and hand off

**Files:**
- Review only: `docs/plans/2026-09-12-1647-compact-mcp-capability-instructions.md`
- Review only: `apps/mcp-server/integration/mcp/transport.integration.test.ts`
- Review only: `apps/mcp-server/src/mcp/agent.ts`

1. Apply the `code-simplifier` review to only the touched code; retain the independently duplicated test literal.
2. Re-run `cd apps/mcp-server && bun run test:integration:transport` and `cd apps/mcp-server && bun run typecheck` after any refactor.
3. Run `git diff HEAD --check` and `git status --short`, then inspect `git diff HEAD -- docs/plans/2026-09-12-1647-compact-mcp-capability-instructions.md apps/mcp-server/integration/mcp/transport.integration.test.ts apps/mcp-server/src/mcp/agent.ts` for scope and exact copy.
4. Obtain the required adversarial code review before delivery.
5. Hand off the changed files plus RED/GREEN evidence and concerns. Do not commit, push, or create a PR in this implementation subtask; the parent agent owns those delivery actions.
