/**
 * MCP recipe tools — save, list, and run named workflow compositions.
 *
 * Recipes are user-scoped declarative specs stored in the Go backend.
 * v1: read-only compositions only (validated at the Go service layer).
 *
 * Tools registered:
 *   recipe.list  — list saved recipes
 *   recipe.save  — create/update a recipe (write, confirmation-gated)
 *   recipe.run   — resolve a recipe's calls for execution via execute()
 */

import { HostProxyError } from '../../sandbox/host-proxy'
import type { McpToolResult } from './search'
import type { HostProxy } from './execute'

export interface RecipeCall {
  operationId: string
  params?: Record<string, unknown>
  body?: Record<string, unknown>
  pathParams?: Record<string, string>
}

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
        return { content: [{ type: 'text', text: `Saved recipes:\n${lines.join('\n')}` }] }
      } catch (err) {
        const msg = err instanceof HostProxyError ? err.message : 'Failed to list recipes'
        return { content: [{ type: 'text', text: `Error: ${msg}` }] }
      }
    },

    async save(input: SaveRecipeInput): Promise<SaveRecipeResult> {
      try {
        const spec = input.calls.map((c) => ({
          operation_id: c.operationId,
          ...(c.params && { params: c.params }),
          ...(c.body && { body: c.body }),
          ...(c.pathParams && { path_params: c.pathParams }),
        }))

        await proxy.invoke(
          'createMCPRecipe',
          { url: '/api/v1/mcp/recipes', method: 'POST', data: { name: input.name, description: input.description, spec } },
          { confirmationToken: input.confirmationToken },
        )
        return {
          toolResult: { content: [{ type: 'text', text: `Recipe "${input.name}" saved successfully.` }] },
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
        const result = await proxy.invoke('runMCPRecipe', {
          url: `/api/v1/mcp/recipes/${encodeURIComponent(input.id)}/run`,
          method: 'POST',
        }) as Record<string, unknown>

        const calls = result['calls'] as { operation_id: string }[] ?? []
        if (calls.length === 0) {
          return { content: [{ type: 'text', text: 'Recipe has no calls.' }] }
        }

        const text =
          `Recipe "${result['name']}" resolved to ${calls.length} call(s):\n` +
          calls.map((c, i) => `  ${i + 1}. ${c.operation_id}`).join('\n') +
          '\n\nPass these to execute() to run the recipe.'

        return { content: [{ type: 'text', text }] }
      } catch (err) {
        const msg = err instanceof HostProxyError ? err.message : 'Failed to run recipe'
        return { content: [{ type: 'text', text: `Error: ${msg}` }] }
      }
    },
  }
}
