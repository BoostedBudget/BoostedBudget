/**
 * BoostedBudget MCP Agent — Durable Object backed by the `agents` SDK.
 *
 * Handles Streamable HTTP transport for remote MCP connections.
 * Auth is validated by the Worker entry point (index.ts) before
 * requests reach this class — clerkUserId is passed via props.
 *
 * Tools registered here:
 *   search   — Discover API operations from the spec index (no auth needed)
 *   execute  — Run typed API calls against the BoostedBudget backend (Phase 5)
 *   recipe.* — Saved read-only workflow recipes (Phase 7)
 */

import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { McpAgent } from 'agents/mcp'
import { z } from 'zod'
import { handleSearch } from './tools/search'
import { buildExecuteHandler } from './tools/execute'
import { buildRecipeTools } from './tools/recipes'
import { createHostProxy } from '../sandbox/host-proxy'
import { createFetchMutator } from '../client/fetch-mutator'
import { createPosthogClient, MCP_EVENTS } from '../analytics/posthog-client'
import { extractMcpHeaders, buildPropsFromHeaders } from './agent-auth'
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

export interface McpAgentProps {
  /** Clerk user ID extracted from the validated OAuth token */
  clerkUserId: string
  /** Clerk grant ID from the OAuth token */
  grantId: string
  /**
   * The raw Clerk OAuth access token — used as the bearer token when
   * calling the Go backend. Injected by index.ts via X-MCP-Access-Token header
   * and stored here so it is available in init().
   *
   * IMPORTANT: This is the ACTUAL Clerk JWT (not the user ID).
   * The Go backend's Clerk middleware validates this token.
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
   * Headers injected by index.ts after JWKS verification (delete-then-set,
   * injection-safe). The DO is not directly accessible; index.ts is the only entry.
   */
  override async fetch(request: Request): Promise<Response> {
    if (!this.props?.accessToken) {
      const headers = extractMcpHeaders(request)
      if (headers) {
        await this.updateProps(buildPropsFromHeaders(headers))
      }
    }
    return super.fetch(request)
  }

  async init(): Promise<void> {
    const specIndex = await getSpecIndex()
    const env = this.env as Env

    const props = this.props as McpAgentProps

    // Inject access token into credentials; fall back to sentinel string to fail closed
    const accessToken = props.accessToken ?? 'MISSING_ACCESS_TOKEN'
    const mutator = createFetchMutator({
      baseUrl: env.BOOSTEDBUDGET_API_URL,
      getCredentials: async () => ({
        authorization: `Bearer ${accessToken}`,
        originSecret: env.BOOSTEDBUDGET_ORIGIN_SECRET,
      }),
    })
    const proxy = createHostProxy({ mutator })

    // PostHog analytics client (no-op if POSTHOG_API_KEY is unset)
    const posthog = createPosthogClient((env as Record<string, unknown>)['POSTHOG_API_KEY'] as string ?? '')

    // ── search tool ──────────────────────────────────────────────────────────
    this.server.registerTool(
      'search',
      {
        description:
          'Search the BoostedBudget API spec to discover available operations. ' +
          'Returns matching operations with their operationId, method, path, and summary. ' +
          'Use this to find the right operation before calling execute().',
        inputSchema: {
          query: z.string().optional().describe('Keyword search (e.g. "spending category", "budget create")'),
          class: z.enum(['read', 'write']).optional().describe('Filter by operation class'),
          tag: z.string().optional().describe('Filter by API tag (e.g. "Analytics", "Budgets", "Transactions")'),
          limit: z.number().int().min(1).max(50).optional().describe('Max results to return (default: 20)'),
        },
      },
      async (input) => {
        posthog.capture(props.clerkUserId, MCP_EVENTS.TOOL_CALLED, { tool: 'search' })
        return handleSearch(input, specIndex)
      },
    )

    // ── execute tool (reads + confirmation-gated writes) ─────────────────────
    const executeHandler = buildExecuteHandler(proxy, specIndex.entries)
    this.server.registerTool(
      'execute',
      {
        description:
          'Execute one or more BoostedBudget API operations. ' +
          'Use search() first to find the right operationId, then call execute() ' +
          'with a list of operation calls to chain. ' +
          'Write operations will return a confirmation challenge on first call — ' +
          're-invoke with the provided confirmationToken to complete the write.',
        inputSchema: {
          calls: z.array(
            z.object({
              operationId: z.string().describe('The operationId from search() results'),
              params: z.record(z.unknown()).optional().describe('Query parameters'),
              body: z.record(z.unknown()).optional().describe('Request body for write operations'),
              pathParams: z.record(z.string()).optional().describe('Path parameters (e.g. { id: "abc" })'),
            }),
          ).describe('Ordered list of API calls to execute'),
          confirmationToken: z.string().optional().describe('Confirmation token for write operations'),
        },
      },
      async (input) => {
        posthog.capture(props.clerkUserId, MCP_EVENTS.TOOL_CALLED, { tool: 'execute' })
        const result = await executeHandler(input)
        posthog.capture(props.clerkUserId, MCP_EVENTS.CODE_EXECUTED, {
          callCount: input.calls?.length ?? 0,
        })
        return result
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
      async () => recipeTools.list({}),
    )

    this.server.registerTool(
      'recipe.save',
      {
        description:
          'Save a named workflow recipe (read-only compositions only in v1). ' +
          'Recipes can be scheduled or run on-demand via recipe.run().',
        inputSchema: {
          name: z.string().describe('Recipe name'),
          description: z.string().optional().describe('Optional description'),
          calls: z.array(z.object({
            operationId: z.string(),
            params: z.record(z.unknown()).optional(),
            body: z.record(z.unknown()).optional(),
            pathParams: z.record(z.string()).optional(),
          })).describe('Ordered read-only API calls'),
          schedule: z.string().optional().describe('Cron schedule (e.g. "0 9 * * 1" for weekly)'),
          confirmationToken: z.string().optional(),
        },
      },
      async (input) => recipeTools.save(input),
    )

    this.server.registerTool(
      'recipe.run',
      {
        description: 'Resolve a saved recipe to its ordered list of API calls, then pass them to execute().',
        inputSchema: {
          id: z.string().describe('Recipe ID (from recipe.list())'),
        },
      },
      async (input) => recipeTools.run(input),
    )
  }
}
