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
import { createHostProxy } from '../sandbox/host-proxy'
import { createFetchMutator } from '../client/fetch-mutator'
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
}

export class BoostedBudgetMcpAgent extends McpAgent<Env, Record<string, never>, McpAgentProps> {
  server = new McpServer({
    name: 'boostedbudget',
    version: '0.1.0',
  })

  async init(): Promise<void> {
    const specIndex = await getSpecIndex()
    const env = this.env as Env

    // Build the host-proxy with credential injection from Worker secrets
    const mutator = createFetchMutator({
      baseUrl: env.BOOSTEDBUDGET_API_URL,
      getCredentials: async () => ({
        authorization: `Bearer ${(this.props as McpAgentProps).clerkUserId}`, // token injected via props
        originSecret: env.BOOSTEDBUDGET_ORIGIN_SECRET,
      }),
    })
    const proxy = createHostProxy({ mutator })

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
      async (input) => handleSearch(input, specIndex),
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
      async (input) => executeHandler(input),
    )
  }
}
