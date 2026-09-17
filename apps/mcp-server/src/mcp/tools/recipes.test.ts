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

  it('marks a backend failure as an MCP error result', async () => {
    const tools = buildRecipeTools({ invoke: mock(() => Promise.reject(new Error('backend unavailable'))) })

    const result = await tools.list({})

    expect(result.isError).toBe(true)
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

  // Cluster B's twin (#1625). execute.ts was fixed structurally for the 204 -> undefined case;
  // this path was not. `as Record<string, unknown>` is a compile-time assertion with no runtime
  // effect, so an undefined body reached `result['calls']`, threw a TypeError, and was caught by
  // the generic handler and reported as "Failed to run recipe" -- a misleading error for a
  // response that simply had no body.
  it('reports an empty response as such, not as a generic failure', async () => {
    for (const empty of [undefined, null]) {
      const proxy = { invoke: mock(() => Promise.resolve(empty)) }
      const tools = buildRecipeTools(proxy)
      const result = await tools.run({ id: 'r1' })

      const text = result.content[0]?.text ?? ''
      expect(text).not.toContain('Failed to run recipe')
      expect(text).toContain('no calls')
    }
  })

  it('does not crash when calls is present but not an array', async () => {
    const proxy = { invoke: mock(() => Promise.resolve({ name: 'x', calls: 'not-an-array' })) }
    const tools = buildRecipeTools(proxy)
    const result = await tools.run({ id: 'r1' })

    expect(result.content[0]?.text ?? '').not.toContain('Failed to run recipe')
  })

  it('marks a backend failure as an MCP error result', async () => {
    const tools = buildRecipeTools({ invoke: mock(() => Promise.reject(new Error('backend unavailable'))) })

    const result = await tools.run({ id: 'r1' })

    expect(result.isError).toBe(true)
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
    expect(parsed.note).toContain('recipe.run() resolves')
    expect(parsed.note).toContain('readExecute()')
    expect(parsed.note).toContain('writeExecute()')
    expect(parsed.note).not.toContain('recipe.run() to execute it')
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

  it('each starter template call conforms to SaveRecipeInput.calls shape at runtime', () => {
    for (const tmpl of STARTER_RECIPE_TEMPLATES) {
      expect(typeof tmpl.name).toBe('string')
      expect(tmpl.name.length).toBeGreaterThan(0)
      expect(Array.isArray(tmpl.calls)).toBe(true)
      expect(tmpl.calls.length).toBeGreaterThan(0)
      for (const call of tmpl.calls) {
        // Required field: operationId must be a non-empty string
        expect(typeof call.operationId).toBe('string')
        expect(call.operationId.length).toBeGreaterThan(0)
        // Optional fields must be plain objects when present (not arrays, not strings)
        if (call.params !== undefined) {
          expect(typeof call.params).toBe('object')
          expect(Array.isArray(call.params)).toBe(false)
        }
        if (call.body !== undefined) {
          expect(typeof call.body).toBe('object')
          expect(Array.isArray(call.body)).toBe(false)
        }
        if (call.pathParams !== undefined) {
          expect(typeof call.pathParams).toBe('object')
          expect(Array.isArray(call.pathParams)).toBe(false)
        }
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

  // R7-3: Boosts Review recipe template
  it('includes a "Boosts Review" template that references getTransferSuggestions and getSavingsImpactMe', () => {
    const boostsRecipe = STARTER_RECIPE_TEMPLATES.find((t) => t.name === 'Boosts Review')
    expect(boostsRecipe).toBeDefined()
    const opIds = boostsRecipe!.calls.map((c) => c.operationId)
    expect(opIds).toContain('getTransferSuggestions')
    expect(opIds).toContain('getSavingsImpactMe')
  })

  it('"Boosts Review" pending call has status param set to "pending"', () => {
    const boostsRecipe = STARTER_RECIPE_TEMPLATES.find((t) => t.name === 'Boosts Review')!
    const pendingCall = boostsRecipe.calls.find(
      (c) => c.operationId === 'getTransferSuggestions' && c.params?.['status'] === 'pending',
    )
    expect(pendingCall).toBeDefined()
  })

  it('"Boosts Review" accepted call has status param set to "accepted"', () => {
    const boostsRecipe = STARTER_RECIPE_TEMPLATES.find((t) => t.name === 'Boosts Review')!
    const acceptedCall = boostsRecipe.calls.find(
      (c) => c.operationId === 'getTransferSuggestions' && c.params?.['status'] === 'accepted',
    )
    expect(acceptedCall).toBeDefined()
  })

  // R8-2d: Additional Boosts recipes (#1267)
  it('includes an "Awaiting Matches" template with accepted transfer suggestions', () => {
    const recipe = STARTER_RECIPE_TEMPLATES.find((t) => t.name === 'Awaiting Matches')
    expect(recipe).toBeDefined()
    const opIds = recipe!.calls.map((c) => c.operationId)
    expect(opIds).toContain('getTransferSuggestions')
    const acceptedCall = recipe!.calls.find(
      (c) => c.operationId === 'getTransferSuggestions' && c.params?.['status'] === 'accepted',
    )
    expect(acceptedCall).toBeDefined()
  })

  it('includes a "Monthly Savings Performance" template with savings impact', () => {
    const recipe = STARTER_RECIPE_TEMPLATES.find((t) => t.name === 'Monthly Savings Performance')
    expect(recipe).toBeDefined()
    const opIds = recipe!.calls.map((c) => c.operationId)
    expect(opIds).toContain('getSavingsImpactMe')
    expect(opIds).toContain('getSavingsImpactAggregate')
  })

  it('includes a "Transfer Boosts Audit" template with getBoosts and getBoostExecutions', () => {
    // #1283: renamed from "Transfer Rules Audit" / getAutomationRules / getRuleExecutions
    // #1287: query param also renamed from rule_type to boost_type
    const recipe = STARTER_RECIPE_TEMPLATES.find((t) => t.name === 'Transfer Boosts Audit')
    expect(recipe).toBeDefined()
    const opIds = recipe!.calls.map((c) => c.operationId)
    expect(opIds).toContain('getBoosts')
    expect(opIds).toContain('getBoostExecutions')
    // Should filter to transfer-generating boost types
    const boostsCall = recipe!.calls.find((c) => c.operationId === 'getBoosts')
    expect(boostsCall?.params?.['boost_type']).toEqual(['transfer_suggestion', 'savings_goal'])
  })
})
