import { describe, expect, it, mock } from 'bun:test'
import { readFileSync } from 'node:fs'
import { buildReadExecuteHandler } from '../mcp/tools/execute'
import { projectResponse } from './projection'
import { sizeFixtures, type SizeFixture } from './size-fixtures'
import { readSpecIndex } from '../../scripts/generate-mcp-capability-summary'

const recipe = JSON.parse(readFileSync(new URL('../../../../backend/internal/huma/operations/testdata/mcp_recipe_response.json', import.meta.url), 'utf8'))
const fixtures: SizeFixture[] = [...sizeFixtures, { operationId: 'getMCPRecipe', pathParams: { id: recipe.id }, payload: recipe, projected: true }]
const specEntries = readSpecIndex().entries

describe('rendered response size regression (synthetic data)', () => {
  it('projects the exact production Huma recipe wire fixture and preserves full including $schema', () => {
    expect(recipe.$schema).toBe('https://example.com/schemas/MCPRecipe.json')
    const compact = projectResponse('getMCPRecipe', recipe)
    expect(compact.warning).toBeUndefined()
    expect(compact.reduced).toBe(true)
    expect((compact.value as Record<string, unknown>).spec).toEqual(recipe.spec)
    expect(JSON.stringify(compact.value).length).toBeLessThan(JSON.stringify(recipe).length)
    expect(projectResponse('getMCPRecipe', recipe, 'full').value).toEqual(recipe)
  })

  it('reduces total rendered characters with every row and envelope preserved, counting headers and hints', async () => {
    let totalFull = 0
    let totalCompact = 0
    for (const { operationId, payload, collection, projected, pathParams } of fixtures) {
      const projection = projectResponse(operationId, payload)
      expect(projection.warning).toBeUndefined()
      expect(projection.reduced).toBe(projected)
      if (Array.isArray(payload)) {
        expect((projection.value as Record<string, unknown>[]).map(row => row.id)).toEqual(payload.map(row => row.id))
      } else if (collection) {
        const original = payload as Record<string, unknown>
        const compact = projection.value as Record<string, unknown>
        expect((compact[collection] as Record<string, unknown>[]).map(row => row.id)).toEqual((original[collection] as Record<string, unknown>[]).map(row => row.id))
        for (const [key, value] of Object.entries(original)) {
          if (key !== collection && key !== '$schema') expect(compact[key]).toEqual(value)
        }
      }
      const proxy = { invoke: mock(async () => payload) }
      const execute = buildReadExecuteHandler(proxy, specEntries)
      const full = (await execute({ calls: [{ operationId, pathParams, detail: 'full' }] })).toolResult.content[0]!.text!
      const compact = (await execute({ calls: [{ operationId, pathParams }] })).toolResult.content[0]!.text!
      expect(full).toBe(`✅ ${operationId}: ${JSON.stringify(payload)}`)
      expect(compact).toContain(JSON.stringify(projection.value))
      expect(compact).not.toContain('Compact response unavailable')
      expect(compact).not.toContain('too large')
      if (projected) expect(compact).toContain('detail: "full"')
      else expect(compact).toBe(full)
      totalFull += full.length
      totalCompact += compact.length
      console.log(`SIZE ${operationId}: full=${full.length} compact=${compact.length}`)
    }
    console.log(`SIZE TOTAL: full=${totalFull} compact=${totalCompact}`)
    expect(totalCompact).toBeLessThan(totalFull)
  })
})
