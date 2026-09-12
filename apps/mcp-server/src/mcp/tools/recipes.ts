/**
 * MCP recipe tools — save, list, and run named workflow compositions.
 *
 * Recipes are user-scoped declarative specs stored in the Go backend.
 * v1: read-only compositions only (validated at the Go service layer).
 *
 * Tools registered:
 *   recipe.list  — list saved recipes
 *   recipe.save  — create/update a recipe (write, confirmation-gated)
 *   recipe.run   — resolve a recipe's calls for execution via readExecute()/writeExecute()
 *
 * ── recipe.run and the readExecute/writeExecute split (issue #1610b) ────────────────────────
 * `recipe.run` is NOT subject to the read/write boundary, and this is deliberate rather than an
 * oversight: it only RESOLVES a saved recipe into its ordered list of API calls (one proxy call
 * to `POST /api/v1/mcp/recipes/{id}/run`, see `run()` below) — it never executes any of those
 * calls itself. The model receives the call list as plain data and must hand it to readExecute()
 * or writeExecute() itself, where the read/write boundary applies normally to each individual
 * call. So there is no hole here: the boundary is enforced at the point calls are actually
 * invoked, not at the point they are merely listed. Writing this down because "a tool named
 * `run` that isn't subject to the read/write split" is exactly the kind of thing an auditor
 * would otherwise flag as a gap.
 *
 * (Out of scope, note only: `recipe.run` is itself a POST audited as `operationClass: 'read'`
 * in agent.ts — a pre-existing mislabel unrelated to the above.)
 */

import { HostProxyError } from '../../sandbox/host-proxy'
import { z } from 'zod'
import { executeCallSchema } from './call-schema'
import { MAX_RESULT_CHARS } from './execute'
import type { McpToolResult } from './search'
import type { HostProxy } from './execute'

export interface RecipeCall {
  operationId: string
  params?: Record<string, unknown>
  body?: Record<string, unknown>
  pathParams?: Record<string, string>
  detail?: 'compact' | 'full'
}

export interface RecipeTemplate {
  name: string
  description: string
  calls: RecipeCall[]
}

/**
 * Starter recipe templates for common financial workflows.
 * These are non-runnable templates — the agent fills in {{MONTH_START}} /
 * {{MONTH_END}} with actual dates (e.g. 2025-01-01 / 2025-01-31) before
 * passing to recipe.save(), then recipe.run() to execute.
 */
export const STARTER_RECIPE_TEMPLATES: RecipeTemplate[] = [
  {
    name: 'Monthly Financial Review',
    description: 'A complete monthly financial health check: overall summary, largest expenses, and unusual spending.',
    calls: [
      { operationId: 'getFinancialSummary', params: { start_date: '{{MONTH_START}}', end_date: '{{MONTH_END}}' } },
      { operationId: 'getLargestTransactionsForPeriod', params: { start_date: '{{MONTH_START}}', end_date: '{{MONTH_END}}', limit: 10 } },
      { operationId: 'getUnusualSpend', params: { start_date: '{{MONTH_START}}', end_date: '{{MONTH_END}}' } },
    ],
  },
  {
    name: 'Spending Deep-Dive',
    description: 'Detailed spending analysis: category breakdown, month-over-month trends, and top merchant categories.',
    calls: [
      { operationId: 'getSpendingByCategory', params: { start_date: '{{MONTH_START}}', end_date: '{{MONTH_END}}' } },
      { operationId: 'getMonthOverMonthSpendingChange', params: { months: 6 } },
      { operationId: 'getLargestTransactionsForPeriod', params: { start_date: '{{MONTH_START}}', end_date: '{{MONTH_END}}', limit: 5 } },
    ],
  },
  {
    name: 'Subscription Audit',
    description: 'Review all detected recurring subscriptions and your largest recent charges.',
    calls: [
      { operationId: 'getRecurringSubscriptions' },
      { operationId: 'getLargestTransactionsForPeriod', params: { start_date: '{{MONTH_START}}', end_date: '{{MONTH_END}}', limit: 10 } },
    ],
  },
  {
    name: 'Boosts Review',
    description: 'Review savings automation: pending recommendations, transfers awaiting a match, and overall savings performance.',
    calls: [
      { operationId: 'getTransferSuggestions', params: { status: 'pending' } },
      { operationId: 'getTransferSuggestions', params: { status: 'accepted' } },
      { operationId: 'getSavingsImpactMe' },
    ],
  },
  // R8-2d: Additional Boosts recipes (#1267)
  {
    name: 'Awaiting Matches',
    description: 'Show accepted transfer suggestions waiting for a matching transaction to be confirmed.',
    calls: [
      { operationId: 'getTransferSuggestions', params: { status: 'accepted' } },
    ],
  },
  {
    name: 'Monthly Savings Performance',
    description: 'Assess personal savings impact and compare to platform-wide aggregate for the current month.',
    calls: [
      { operationId: 'getSavingsImpactMe' },
      { operationId: 'getSavingsImpactAggregate' },
    ],
  },
  {
    name: 'Transfer Boosts Audit',
    description: 'List transfer-generating boosts (transfer_suggestion and savings_goal types) and recent execution history for a specific boost.',
    calls: [
      { operationId: 'getBoosts', params: { boost_type: ['transfer_suggestion', 'savings_goal'] } },
      { operationId: 'getBoostExecutions', pathParams: { id: '{{BOOST_ID}}' } },
    ],
  },
  // Issue #138: Investment portfolio recipe
  {
    name: 'Portfolio Review',
    description: 'Investment portfolio health check: live summary (total value, day_change, unrealized gain/loss), allocation breakdown by security type, and top movers.',
    calls: [
      { operationId: 'getPortfolioSummary' },
      { operationId: 'getPortfolioAllocationBreakdown' },
      { operationId: 'getTopMovers', params: { limit: 5 } },
    ],
  },
]

export interface SaveRecipeInput {
  name: string
  description?: string
  calls: RecipeCall[]
  // schedule is intentionally absent in v1: scheduler not built yet (follow-up issue).
  confirmationToken?: string
}

export interface RunRecipeInput {
  id: string
}

/**
 * SaveRecipeResult carries the tool result plus the real audit outcome so that
 * agent.ts can emit the correct audit event (success vs confirmation_required vs error).
 * This matches the ExecuteResult pattern used by the execute tool.
 */
export interface SaveRecipeResult {
  toolResult: McpToolResult
  /** The actual outcome to record in the audit log — NOT always 'success'. */
  auditResult: 'success' | 'confirmation_required' | 'error'
}

interface RecipeTools {
  list(input: Record<string, never>): Promise<McpToolResult>
  save(input: SaveRecipeInput): Promise<SaveRecipeResult>
  run(input: RunRecipeInput): Promise<McpToolResult>
  templates(): McpToolResult
}

/**
 * Build the recipe tool handlers bound to a host-proxy.
 */
export function buildRecipeTools(proxy: HostProxy): RecipeTools {
  return {
    async list(_input): Promise<McpToolResult> {
      try {
        const result = await proxy.invoke('listMCPRecipes', {
          url: '/api/v1/mcp/recipes',
          method: 'GET',
        })
        const recipes = Array.isArray(result) ? result : []
        if (recipes.length === 0) {
          return { content: [{ type: 'text', text: 'No recipes saved yet. Use recipe.save() to create one.' }] }
        }
        const lines = recipes.map((r: Record<string, unknown>) =>
          `• ${r['name']} (id: ${r['id']}, enabled: ${r['enabled']})`,
        )
        return { content: [{ type: 'text', text: `Saved recipes:\n${lines.join('\n')}\nFull recipe: readExecute({ calls: [{ operationId: "getMCPRecipe", pathParams: { id: "<recipe id>" }, detail: "full" }] }).` }] }
      } catch (err) {
        const msg = err instanceof HostProxyError ? err.message : 'Failed to list recipes'
        return { content: [{ type: 'text', text: `Error: ${msg}` }] }
      }
    },

    async save(input: SaveRecipeInput): Promise<SaveRecipeResult> {
      try {
        const parsed = z.array(executeCallSchema).safeParse(input.calls)
        if (!parsed.success) {
          return { toolResult: { content: [{ type: 'text', text: 'Error: Invalid recipe calls; detail must be compact or full when provided.' }], isError: true }, auditResult: 'error' }
        }
        const spec = parsed.data.map((c) => ({
          operation_id: c.operationId,
          ...(c.params && { params: c.params }),
          ...(c.body && { body: c.body }),
          ...(c.pathParams && { path_params: c.pathParams }),
          ...(c.detail !== undefined && { detail: c.detail }),
        }))

        const saved = await proxy.invoke(
          'createMCPRecipe',
          { url: '/api/v1/mcp/recipes', method: 'POST', data: { name: input.name, description: input.description, spec } },
          { confirmationToken: input.confirmationToken },
        )
        const id = typeof saved === 'object' && saved !== null && 'id' in saved && typeof saved.id === 'string' ? saved.id : '<recipe id>'
        return {
          toolResult: { content: [{ type: 'text', text: `Recipe "${input.name}" saved successfully.\nFull recipe: readExecute({ calls: [{ operationId: "getMCPRecipe", pathParams: { id: ${JSON.stringify(id)} }, detail: "full" }] }).` }] },
          auditResult: 'success',
        }
      } catch (err) {
        if (err instanceof HostProxyError && err.code === 'confirmation_required') {
          return {
            toolResult: {
              content: [{
                type: 'text',
                text: `Recipe creation requires confirmation. Re-invoke recipe.save() with confirmationToken: "${err.confirmationToken}"`,
              }],
            },
            auditResult: 'confirmation_required',
          }
        }
        const msg = err instanceof HostProxyError ? err.message : 'Failed to save recipe'
        return {
          toolResult: { content: [{ type: 'text', text: `Error: ${msg}` }] },
          auditResult: 'error',
        }
      }
    },

    async run(input: RunRecipeInput): Promise<McpToolResult> {
      try {
        // `as Record<string, unknown>` is erased at runtime, so it asserts nothing about what
        // actually arrives. A body-less response (the 204 -> undefined case that produced cluster
        // B) then threw a TypeError on property access and surfaced as the generic "Failed to run
        // recipe" -- an error that describes the wrong thing and hides the real one. Narrow it.
        const raw = await proxy.invoke('runMCPRecipe', {
          url: `/api/v1/mcp/recipes/${encodeURIComponent(input.id)}/run`,
          method: 'POST',
        })
        const result: Record<string, unknown> =
          typeof raw === 'object' && raw !== null ? (raw as Record<string, unknown>) : {}

        const rawCalls = result['calls']
        if (!Array.isArray(rawCalls) || rawCalls.length === 0) {
          return { content: [{ type: 'text', text: 'Error: Recipe resolution has no calls or an invalid calls array.' }], isError: true }
        }
        const mapped = rawCalls.map((call: unknown) => {
          if (typeof call !== 'object' || call === null || Array.isArray(call)) return null
          const c = call as Record<string, unknown>
          if (Object.keys(c).some(key => !['operation_id', 'params', 'body', 'path_params', 'detail'].includes(key))) return null
          return {
            operationId: c['operation_id'],
            ...(c['params'] !== undefined && { params: c['params'] }),
            ...(c['body'] !== undefined && { body: c['body'] }),
            ...(c['path_params'] !== undefined && { pathParams: c['path_params'] }),
            ...(c['detail'] != null && { detail: c['detail'] }),
          }
        })
        const parsed = z.array(executeCallSchema.extend({ operationId: z.string().min(1) })).safeParse(mapped)
        if (!parsed.success || typeof result['name'] !== 'string') {
          return { content: [{ type: 'text', text: 'Error: Invalid recipe resolution; no executable calls returned.' }], isError: true }
        }
        const calls = parsed.data
        const text =
          `Recipe "${result['name']}" resolved to ${calls.length} call(s):\n` +
          'Use readExecute() where admitted, or writeExecute() otherwise, preserving order in batches of at most 25. No calls have been executed.\n' +
          JSON.stringify({ calls })
        if (text.length > MAX_RESULT_CHARS) {
          return { content: [{ type: 'text', text: 'Error: Recipe resolution is too large; no executable calls returned. Save smaller recipes and resolve them separately.' }], isError: true }
        }

        return { content: [{ type: 'text', text }] }
      } catch (err) {
        const msg = err instanceof HostProxyError ? err.message : 'Failed to run recipe'
        return { content: [{ type: 'text', text: `Error: ${msg}` }] }
      }
    },

    templates(): McpToolResult {
      return {
        content: [{
          type: 'text',
          text: JSON.stringify({
            templates: STARTER_RECIPE_TEMPLATES,
            note: 'These are non-runnable templates. Replace {{MONTH_START}}/{{MONTH_END}} with actual dates (e.g. 2025-01-01 / 2025-01-31), then use recipe.save() to instantiate as a saved recipe. Use recipe.run() to execute it.',
          }, null, 2),
        }],
        isError: false,
      }
    },
  }
}
