import { describe, expect, it, mock } from 'bun:test'
import { buildRecipeTools, STARTER_RECIPE_TEMPLATES } from './recipes'
import { HostProxyError } from '../../sandbox/host-proxy'
import { getCapability } from '../../manifest/capability-manifest'

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

  it('returns confirmation challenge for write without token (R8-2: auditResult is confirmation_required)', async () => {
    const proxy = {
      invoke: mock(() =>
        Promise.reject(new HostProxyError('confirmation_required', 'Confirm', 'tok-123')),
      ),
    }
    const tools = buildRecipeTools(proxy)
    const { toolResult, auditResult } = await tools.save({ name: 'Recipe', calls: [{ operationId: 'getTransactions' }] })
    expect(toolResult.content[0]?.text).toContain('tok-123')
    // Key assertion: challenge path must NOT be labelled 'success' in the audit
    expect(auditResult).toBe('confirmation_required')
  })

  it('R8-2: returns success auditResult when confirmed write completes', async () => {
    const proxy = {
      invoke: mock(() => Promise.resolve({ id: 'r1', name: 'Recipe' })),
    }
    const tools = buildRecipeTools(proxy)
    const { toolResult, auditResult } = await tools.save({
      name: 'Recipe',
      calls: [{ operationId: 'getTransactions' }],
      confirmationToken: 'tok-confirm',
    })
    expect(toolResult.content[0]?.text).toContain('saved successfully')
    expect(auditResult).toBe('success')
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

describe('recipe.templates', () => {
  it('returns a non-empty list of starter templates', () => {
    const proxy = makeProxy()
    const tools = buildRecipeTools(proxy)
    const result = tools.templates()

    expect(result.isError).toBe(false)
    expect(result.content[0]?.type).toBe('text')
    const parsed = JSON.parse(result.content[0]?.text ?? '{}')
    expect(Array.isArray(parsed.templates)).toBe(true)
    expect(parsed.templates.length).toBeGreaterThan(0)
    expect(parsed.note).toContain('recipe.save()')
  })

  it('every operationId in every template exists in capability-manifest as read', () => {
    for (const template of STARTER_RECIPE_TEMPLATES) {
      for (const call of template.calls) {
        const entry = getCapability(call.operationId)
        expect(entry).toBeDefined()
        expect(entry?.class).toBe('read')
        expect(entry?.excluded).toBe(false)
      }
    }
  })

  it('each template call conforms to the SaveRecipeInput.calls shape', () => {
    for (const template of STARTER_RECIPE_TEMPLATES) {
      expect(typeof template.name).toBe('string')
      expect(template.name.length).toBeGreaterThan(0)
      expect(Array.isArray(template.calls)).toBe(true)
      for (const call of template.calls) {
        // operationId must be a non-empty string
        expect(typeof call.operationId).toBe('string')
        expect(call.operationId.length).toBeGreaterThan(0)
        // params, body, pathParams are optional but must be objects if present
        if (call.params !== undefined) {
          expect(typeof call.params).toBe('object')
        }
        if (call.body !== undefined) {
          expect(typeof call.body).toBe('object')
        }
        if (call.pathParams !== undefined) {
          expect(typeof call.pathParams).toBe('object')
        }
      }
    }
  })
})
