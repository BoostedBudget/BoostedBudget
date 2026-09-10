/**
 * BoostedBudget MCP Agent — Durable Object backed by the `agents` SDK.
 *
 * Handles Streamable HTTP transport for remote MCP connections.
 * Auth is validated by the Worker entry point (index.ts) before
 * requests reach this class — clerkUserId is passed via props.
 *
 * Tools registered here:
 *   search        — Discover API operations from the spec index (no auth needed)
 *   readExecute   — Run read-only API calls (issue #1610b split of the old `execute`)
 *   writeExecute  — Run any API calls, including confirmation-gated writes
 *   recipe.*      — Saved read-only workflow recipes (Phase 7)
 */

import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { McpAgent } from 'agents/mcp'
import { z } from 'zod'
import { handleSearch } from './tools/search'
import { buildReadExecuteHandler, buildWriteExecuteHandler } from './tools/execute'
import { buildRecipeTools } from './tools/recipes'
import { createHostProxy } from '../sandbox/host-proxy'
import { createFetchMutator } from '../client/fetch-mutator'
import { createRateLimiter } from '../limits/rate-limit'
import { createPosthogClient, MCP_EVENTS } from '../analytics/posthog-client'
import { createAuditClient } from '../audit/audit-client'
import { extractMcpHeaders, buildPropsFromHeaders } from './agent-auth'
import { toAuditEntry } from './audit-mapping'
import type { SpecIndex } from '../spec/search-runtime'
import type { Env } from '../env'

// The spec index is loaded once at module level (stateless Worker module)
// and shared across DO instances via closure.
let cachedSpecIndex: SpecIndex | null = null

async function getSpecIndex(): Promise<SpecIndex> {
  if (cachedSpecIndex) return cachedSpecIndex
  const raw = await import('../spec/generated/spec-index.json', {
    assert: { type: 'json' },
  })
  cachedSpecIndex = raw.default as SpecIndex
  return cachedSpecIndex
}

// Type alias (vs interface) to satisfy SDK's `Props extends Record<string, unknown>`
// generic constraint. Closed interfaces lack implicit index signatures (TS2344).
export type McpAgentProps = {
  /** Clerk user ID extracted from the validated OAuth token */
  clerkUserId: string
  /** Clerk grant ID from the OAuth token */
  grantId: string
  /**
   * Raw Clerk OAuth access token (actual JWT, not user ID).
   * Used as bearer token when calling the Go backend.
   * Injected by index.ts via X-MCP-Access-Token, available in init().
   */
  accessToken?: string
}

export class BoostedBudgetMcpAgent extends McpAgent<Env, Record<string, never>, McpAgentProps> {
  server = new McpServer({
    name: 'boostedbudget',
    version: '0.1.0',
  })

  /**
   * Override the DurableObject fetch() — the ONLY lifecycle hook that receives
   * the actual HTTP request with our trusted X-MCP-* headers.
   *
   * The agents SDK onStart(props) does NOT receive the request — it takes props.
   * We extract the Clerk OAuth access token here (before MCP protocol handling)
   * and store it via updateProps() so init() can use it as the bearer token.
   *
   * IMPORTANT: updateProps() is called on EVERY request (no guard on existing props).
   * This ensures a refreshed Clerk access token is picked up mid-session rather than
   * keeping the first token alive for the lifetime of the DO — which would cause
   * backend 401s after the initial token expires.
   *
   * Headers injected by index.ts after JWKS verification (delete-then-set,
   * injection-safe). The DO is not directly accessible; index.ts is the only entry.
   */
  override async fetch(request: Request): Promise<Response> {
    const headers = extractMcpHeaders(request)
    if (headers) {
      await this.updateProps(buildPropsFromHeaders(headers))
    }
    return super.fetch(request)
  }

  async init(): Promise<void> {
    const specIndex = await getSpecIndex()
    const env = this.env as Env

    // Per-session rate limiter — bound to this DO instance lifetime, not the module global.
    // Keyed on props.clerkUserId so each user gets an independent bucket.
    const rateLimiter = createRateLimiter({ maxPerMinute: 60, maxConcurrent: 5 })

    const mutator = createFetchMutator({
      baseUrl: env.BOOSTEDBUDGET_API_URL,
      getCredentials: async () => {
        const props = this.props as McpAgentProps
        return {
          authorization: `Bearer ${props.accessToken}`,
          originSecret: env.BOOSTEDBUDGET_ORIGIN_SECRET,
          mcpWorkerSecret: env.MCP_WORKER_SECRET,
          grantId: props.grantId ?? 'unknown',
        }
      },
    })
    const proxy = createHostProxy({
      mutator,
      userId: (this.props as McpAgentProps).clerkUserId,
      // Pass grantId so confirmation tokens are bound to this OAuth grant.
      // Cross-grant replay (e.g. after re-authorizing) is rejected by host-proxy.
      grantId: (this.props as McpAgentProps).grantId ?? 'unknown',
      rateLimiter,
    })

    // PostHog analytics client (no-op if POSTHOG_API_KEY is unset)
    const posthog = createPosthogClient((env as unknown as Record<string, unknown>)['POSTHOG_API_KEY'] as string | undefined ?? '')

    // Audit client — posts read/protocol events to POST /api/v1/mcp/audit (fire-and-forget)
    const audit = createAuditClient(env)

    // ── search tool ──────────────────────────────────────────────────────────
    // @modelcontextprotocol/sdk types registerTool's inputSchema against Zod 3 (its AnySchema
    // is a Zod 3 ZodType, with _parse/_getType internals), while this Worker builds schemas with
    // Zod 4. Passing a Zod 4 schema therefore fails to typecheck (TS2322) even though it works at
    // runtime — the SDK declares `zod: ^3.25 || ^4.0` and resolves Zod 4 itself. Cast to any to
    // bridge the two type worlds; the explicit input types on the callbacks below are what
    // actually preserve type safety at the call boundary.
    // Was previously cast for a different reason (TS2589, deep instantiation under Zod 3.25);
    // verified on the Zod 4 bump that removing the casts still fails, now with TS2322.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ;(this.server as any).registerTool(
      'search',
      {
        description:
          'Search the BoostedBudget API spec to discover available operations. ' +
          'Returns matching operations with their operationId, method, path, and summary. ' +
          'Use this to find the right operation before calling readExecute() or writeExecute().',
        inputSchema: {
          query: z.string().optional().describe('Keyword search (e.g. "spending category", "budget create")'),
          // Issue #1610b: this enum previously omitted 'destructive', diverging from
          // search.ts's SearchInputSchema (which has accepted it since #1569 Task 16) — an
          // agent asking to filter FOR or AGAINST destructive operations got a schema mismatch
          // instead of the results it asked for. Reconciled here.
          class: z.enum(['read', 'write', 'destructive']).optional().describe('Filter by operation class'),
          tag: z.string().optional().describe('Filter by API tag (e.g. "Analytics", "Budgets", "Transactions")'),
          limit: z.number().int().min(1).max(50).optional().describe('Max results to return (default: 20)'),
        },
      },
      // Explicit type annotation: the `as any` cast above erases the inferred input type,
      // so this is what keeps the handler's argument checked.
      async (input: { query?: string; class?: 'read' | 'write' | 'destructive'; tag?: string; limit?: number }) => {
        const p = this.props as McpAgentProps
        posthog.capture(p.clerkUserId, MCP_EVENTS.TOOL_CALLED, { tool: 'search' })
        const result = handleSearch(input, specIndex)
        void audit.record({ actorUserId: p.clerkUserId, grantId: p.grantId ?? 'unknown',
          operationClass: 'read', operationId: 'search', result: 'success' })
        return result
      },
    )

    // ── readExecute / writeExecute tools (issue #1610b) ───────────────────────
    // Hard cut from the old single `execute` tool — no alias kept. Production has zero users,
    // and MCP clients re-enumerate tools on connect, so a retained alias would leave the agent
    // three overlapping choices instead of two clear ones.
    //
    // readExecute admits a call only when all three of class === 'read', a conventionally
    // non-mutating HTTP method, and confirmationRequired === false hold — see the SECURITY POINT
    // in tools/execute.ts for why `class` alone is not a safe gate (excluded() hard-codes
    // class:'read' onto a genuinely state-changing GET; runMCPRecipe is honestly class:'read'
    // and confirmationRequired:false yet its method is POST). It therefore has NO
    // confirmationToken input — under that gate, one is unreachable by construction.
    //
    // NOT "read-only by construction": that claim was corrected in tools/execute.ts and survived
    // here until #1609 Phase 4. Passing the gate proves an admitted call is non-destructive, not
    // confirmation-gated, and scoped to the authenticated user — it does NOT prove the call has
    // no server-side side effect. `getPrivacySettings` and `getUserProfile` are admitted GETs
    // that lazily create the caller's OWN default row on first access. See execute.ts's module
    // doc for the full statement of the real guarantee.
    const readExecuteHandler = buildReadExecuteHandler(proxy, specIndex.entries)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- see search tool comment above
    ;(this.server as any).registerTool(
      'readExecute',
      {
        description:
          'Execute one or more READ-ONLY BoostedBudget API operations. ' +
          'Use search() first to find the right operationId, then call readExecute() ' +
          'with a list of operation calls to chain. Only operations that are non-destructive ' +
          'and scoped to your own data are admitted here — a couple of them create your own ' +
          'default settings row on first read. Use writeExecute() for anything else.',
        inputSchema: {
          calls: z.array(
            z.object({
              operationId: z.string().describe('The operationId from search() results'),
              params: z.record(z.string(), z.unknown()).optional().describe('Query parameters'),
              pathParams: z.record(z.string(), z.string()).optional().describe('Path parameters (e.g. { id: "abc" })'),
            }),
          ).max(25, 'Maximum 25 calls per readExecute() invocation').describe('Ordered list of read-only API calls to execute (max 25)'),
        },
      },
      async (input: {
        calls: Array<{ operationId: string; params?: Record<string, unknown>; pathParams?: Record<string, string> }>
      }) => {
        const p = this.props as McpAgentProps
        posthog.capture(p.clerkUserId, MCP_EVENTS.TOOL_CALLED, { tool: 'readExecute' })
        const { toolResult, auditEvents } = await readExecuteHandler(input)
        const callCount = input.calls?.length ?? 0
        posthog.capture(p.clerkUserId, MCP_EVENTS.CODE_EXECUTED, { callCount })
        const actor = { clerkUserId: p.clerkUserId, grantId: p.grantId ?? 'unknown' }
        for (const event of auditEvents) {
          void audit.record(toAuditEntry(event, actor))
        }
        return toolResult
      },
    )

    // writeExecute accepts every class and keeps confirmationToken. Reads stay legal inside a
    // write batch — a read-then-write chain must keep working; the property that matters is
    // that a client can bind readExecute ALONE.
    const writeExecuteHandler = buildWriteExecuteHandler(proxy, specIndex.entries)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- see search tool comment above
    ;(this.server as any).registerTool(
      'writeExecute',
      {
        description:
          'Execute one or more BoostedBudget API operations, including writes. ' +
          'Use search() first to find the right operationId, then call writeExecute() ' +
          'with a list of operation calls to chain. ' +
          'Write operations return a confirmation challenge on the first call and execute ' +
          'NOTHING; one token covers every write in the batch. Re-invoke with the IDENTICAL ' +
          'calls array plus the provided confirmationToken to complete them. The token is bound ' +
          'to the exact batch, so changing any field means you must request a fresh token. ' +
          'Prefer readExecute() for read-only calls.',
        inputSchema: {
          calls: z.array(
            z.object({
              operationId: z.string().describe('The operationId from search() results'),
              params: z.record(z.string(), z.unknown()).optional().describe('Query parameters'),
              body: z.record(z.string(), z.unknown()).optional().describe('Request body for write operations'),
              pathParams: z.record(z.string(), z.string()).optional().describe('Path parameters (e.g. { id: "abc" })'),
            }),
          ).max(25, 'Maximum 25 calls per writeExecute() invocation').describe('Ordered list of API calls to execute (max 25)'),
          confirmationToken: z.string().optional().describe('Confirmation token for write operations'),
        },
      },
      async (input: {
        calls: Array<{ operationId: string; params?: Record<string, unknown>; body?: Record<string, unknown>; pathParams?: Record<string, string> }>
        confirmationToken?: string
      }) => {
        const p = this.props as McpAgentProps
        posthog.capture(p.clerkUserId, MCP_EVENTS.TOOL_CALLED, { tool: 'writeExecute' })
        // ExecuteResult includes per-call audit events with the correct operationClass/result.
        // Previously this hardcoded operationClass:'read'/'execute' for all calls — fixed here.
        const { toolResult, auditEvents } = await writeExecuteHandler(input)
        const callCount = input.calls?.length ?? 0
        posthog.capture(p.clerkUserId, MCP_EVENTS.CODE_EXECUTED, { callCount })
        // Record one audit entry per executed call with the real class and result.
        const actor = { clerkUserId: p.clerkUserId, grantId: p.grantId ?? 'unknown' }
        for (const event of auditEvents) {
          void audit.record(toAuditEntry(event, actor))
        }
        return toolResult
      },
    )

    // ── recipe tools ──────────────────────────────────────────────────────────
    const recipeTools = buildRecipeTools(proxy)

    this.server.registerTool(
      'recipe.list',
      {
        description: 'List your saved MCP workflow recipes.',
        inputSchema: {},
      },
      async () => {
        const p = this.props as McpAgentProps
        const result = await recipeTools.list({})
        void audit.record({ actorUserId: p.clerkUserId, grantId: p.grantId ?? 'unknown',
          operationClass: 'read', operationId: 'recipe.list', result: 'success' })
        return result
      },
    )

    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- see search tool comment above
    ;(this.server as any).registerTool(
      'recipe.save',
      {
        description:
          'Save a named workflow recipe (read-only compositions only in v1). ' +
          'Recipes can be run on-demand via recipe.run().',
        // NOTE: the `schedule` cron field is intentionally absent from the v1 schema.
        // The backend scheduler is not yet built; it will be added in a follow-up issue.
        // The `schedule` column is preserved in the DB for future use.
        inputSchema: {
          name: z.string().describe('Recipe name'),
          description: z.string().optional().describe('Optional description'),
          calls: z.array(z.object({
            operationId: z.string(),
            params: z.record(z.string(), z.unknown()).optional(),
            body: z.record(z.string(), z.unknown()).optional(),
            pathParams: z.record(z.string(), z.string()).optional(),
          })).describe('Ordered read-only API calls'),
          confirmationToken: z.string().optional(),
        },
      },
      async (input: {
        name: string
        description?: string
        calls: Array<{ operationId: string; params?: Record<string, unknown>; body?: Record<string, unknown>; pathParams?: Record<string, string> }>
        confirmationToken?: string
      }) => {
        const p = this.props as McpAgentProps
        // save() returns {toolResult, auditResult} so we can emit the correct audit label.
        // auditResult='confirmation_required' on the first (challenge) call; 'success' on confirm.
        const { toolResult, auditResult } = await recipeTools.save(input)
        void audit.record({
          actorUserId: p.clerkUserId,
          grantId: p.grantId ?? 'unknown',
          operationClass: 'write',
          operationId: 'recipe.save',
          result: auditResult,
          confirmationPresented: auditResult === 'confirmation_required',
          confirmationConfirmed: auditResult === 'success',
        })
        return toolResult
      },
    )

    // eslint-disable-next-line @typescript-eslint/no-explicit-any -- see search tool comment above
    ;(this.server as any).registerTool(
      'recipe.run',
      {
        description: 'Resolve a saved recipe to its ordered list of API calls, then pass them to readExecute() or writeExecute().',
        inputSchema: {
          id: z.string().describe('Recipe ID (from recipe.list())'),
        },
      },
      async (input: { id: string }) => {
        const p = this.props as McpAgentProps
        const result = await recipeTools.run(input)
        void audit.record({ actorUserId: p.clerkUserId, grantId: p.grantId ?? 'unknown',
          operationClass: 'read', operationId: 'recipe.run', result: 'success' })
        return result
      },
    )

    this.server.registerTool(
      'recipe.templates',
      {
        description: 'List available starter recipe templates. These are non-runnable; replace {{MONTH_START}}/{{MONTH_END}} with actual dates (e.g. 2025-01-01 / 2025-01-31), then use recipe.save() to instantiate one as a saved recipe. Use recipe.run() to execute it.',
        inputSchema: {},
      },
      async () => {
        const p = this.props as McpAgentProps
        const result = recipeTools.templates()
        void audit.record({ actorUserId: p.clerkUserId, grantId: p.grantId ?? 'unknown',
          operationClass: 'read', operationId: 'recipe.templates', result: 'success' })
        return result
      },
    )
  }
}
