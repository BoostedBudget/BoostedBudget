import { describe, expect, it, mock } from 'bun:test'
import { buildRecipeTools, STARTER_RECIPE_TEMPLATES } from './recipes'
import { getCapability } from '../../manifest/capability-manifest'

function makeProxy() {
  return { invoke: mock(() => Promise.resolve([])) }
}

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
    expect(parsed.note).toContain('createMCPRecipe')
    expect(parsed.note).toContain('runMCPRecipe')
    expect(parsed.note).toContain('readExecute()')
    expect(parsed.note).toContain('writeExecute()')
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

  it('each starter template call conforms to the recipe call shape at runtime', () => {
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

  it('each template call conforms to the recipe call shape', () => {
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
