/**
 * MCP recipe templates tool.
 *
 * Recipes are user-scoped declarative specs stored in the Go backend. Every recipe capability is
 * a classified API operation, so there are no dedicated recipe MCP tools — they are reached
 * through the generic executors:
 *   - list / inspect: readExecute with listMCPRecipes / getMCPRecipe
 *   - create / update / delete: writeExecute with createMCPRecipe / updateMCPRecipe / deleteMCPRecipe
 *   - run: writeExecute with runMCPRecipe, then submit the returned calls to readExecute/writeExecute
 *
 * The only recipe tool registered here is `recipe.templates`, which returns non-runnable starter
 * templates for common financial workflows.
 */

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
 * {{MONTH_END}} with actual dates (e.g. 2025-01-01 / 2025-01-31), then instantiates
 * one with writeExecute({operationId:"createMCPRecipe"}).
 */
const RECIPE_RESOLUTION_GUIDANCE =
  'runMCPRecipe resolves the saved recipe to an ordered calls array; it does not execute those ' +
  'calls. Submit the returned calls to readExecute() or writeExecute().'

export const RECIPE_TEMPLATE_USAGE_GUIDANCE =
  'These are non-runnable templates. Replace {{MONTH_START}}/{{MONTH_END}} with actual dates ' +
  '(e.g. 2025-01-01 / 2025-01-31), then instantiate one as a saved recipe with ' +
  'writeExecute({operationId:"createMCPRecipe"}). List saved recipes with ' +
  'readExecute({operationId:"listMCPRecipes"}), inspect one with ' +
  'readExecute({operationId:"getMCPRecipe"}), and run one with ' +
  'writeExecute({operationId:"runMCPRecipe"}). ' +
  RECIPE_RESOLUTION_GUIDANCE

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

interface RecipeTools {
  templates(): McpToolResult
}

/**
 * Build the recipe tool handlers bound to a host-proxy.
 */
export function buildRecipeTools(_proxy: HostProxy): RecipeTools {
  return {
    templates(): McpToolResult {
      return {
        content: [{
          type: 'text',
          text: JSON.stringify({
            templates: STARTER_RECIPE_TEMPLATES,
            note: RECIPE_TEMPLATE_USAGE_GUIDANCE,
          }, null, 2),
        }],
        isError: false,
      }
    },
  }
}
