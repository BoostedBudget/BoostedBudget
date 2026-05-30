import { describe, expect, it, mock } from 'bun:test'
import { buildRecipeTools } from './recipes'
import { HostProxyError } from '../../sandbox/host-proxy'

function makeProxy(getResult: unknown = [], runResult: unknown = { calls: [] }) {
  return {
    invoke: mock((opId: string) => {
      if (opId === 'listMCPRecipes') return Promise.resolve(getResult)
      if (opId === 'runMCPRecipe') return Promise.resolve(runResult)
      return Promise.resolve({})
    }),
  }
}

describe('recipe.list', () => {
  it('returns list of recipes as text', async () => {
    const proxy = makeProxy([
      { id: 'r1', name: 'Weekly Summary', enabled: true },
      { id: 'r2', name: 'Monthly Budget', enabled: false },
    ])
    const tools = buildRecipeTools(proxy)
    const result = await tools.list({})

    expect(result.content[0]?.type).toBe('text')
    expect(result.content[0]?.text).toContain('Weekly Summary')
    expect(result.content[0]?.text).toContain('Monthly Budget')
  })

  it('returns empty message when no recipes', async () => {
    const proxy = makeProxy([])
    const tools = buildRecipeTools(proxy)
    const result = await tools.list({})
    expect(result.content[0]?.text).toContain('No recipes')
  })
})

describe('recipe.save', () => {
  it('calls createMCPRecipe through proxy', async () => {
    const proxy = makeProxy()
    const tools = buildRecipeTools(proxy)
    await tools.save({ name: 'My Recipe', calls: [{ operationId: 'getTransactions' }] })
    expect(proxy.invoke).toHaveBeenCalledWith(
      'createMCPRecipe',
      expect.objectContaining({ data: expect.objectContaining({ name: 'My Recipe' }) }),
      expect.any(Object),
    )
  })

  it('returns confirmation challenge for write without token', async () => {
    const proxy = {
      invoke: mock(() =>
        Promise.reject(new HostProxyError('confirmation_required', 'Confirm', 'tok-123')),
      ),
    }
    const tools = buildRecipeTools(proxy)
    const result = await tools.save({ name: 'Recipe', calls: [{ operationId: 'getTransactions' }] })
    expect(result.content[0]?.text).toContain('tok-123')
  })
})

describe('recipe.run', () => {
  it('returns calls to execute from a saved recipe', async () => {
    const proxy = {
      invoke: mock(() =>
        Promise.resolve({
          recipe_id: 'r1',
          name: 'Weekly Summary',
          calls: [{ operation_id: 'getCashFlowSummary', params: {} }],
        }),
      ),
    }
    const tools = buildRecipeTools(proxy)
    const result = await tools.run({ id: 'r1' })

    expect(result.content[0]?.type).toBe('text')
    expect(result.content[0]?.text).toContain('getCashFlowSummary')
  })
})
