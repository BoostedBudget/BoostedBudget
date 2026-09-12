import { describe, expect, it, mock } from 'bun:test'
import { buildRecipeTools, type SaveRecipeInput } from './recipes'
import { MAX_RESULT_CHARS } from './execute'

describe('recipe detail contract', () => {
  it('saves detail presence and all arguments without limiting saved calls to 25', async () => {
    const calls = Array.from({ length: 26 }, (_, i) => ({
      operationId: 'getAccounts', params: { limit: i }, body: { filter: ['a'] }, pathParams: { id: 'a' },
      ...(i % 3 === 0 ? {} : { detail: i % 3 === 1 ? 'compact' as const : 'full' as const }),
    }))
    const proxy = { invoke: mock(async () => ({ id: 'r1' })) }
    const result = await buildRecipeTools(proxy).save({ name: 'Review', calls })
    expect(proxy.invoke).toHaveBeenCalledWith('createMCPRecipe', expect.objectContaining({
      data: { name: 'Review', description: undefined, spec: calls.map(({ operationId, pathParams, ...rest }) => ({ operation_id: operationId, path_params: pathParams, ...rest })) },
    }), expect.any(Object))
    expect(result.toolResult.content[0]!.text).toContain('getMCPRecipe')
    expect(result.toolResult.content[0]!.text).toContain('detail: "full"')
    expect(result.toolResult.content[0]!.text).toContain('r1')
  })

  for (const detail of [null, '', 'verbose']) {
    it(`rejects invalid save detail ${JSON.stringify(detail)} before proxy`, async () => {
      const proxy = { invoke: mock(async () => ({})) }
      const result = await buildRecipeTools(proxy).save({ name: 'Review', calls: [{ operationId: 'getAccounts', detail }] } as unknown as SaveRecipeInput)
      expect(result.auditResult).toBe('error')
      expect(result.toolResult.isError).toBe(true)
      expect(proxy.invoke).not.toHaveBeenCalled()
    })
  }

  it('resolves complete executable calls including legacy omission and null, even above 25', async () => {
    const calls = Array.from({ length: 27 }, (_, i) => ({
      operation_id: 'getAccounts', params: { limit: i }, body: { value: true }, path_params: { id: 'a' },
      ...(i % 4 === 0 ? {} : { detail: [null, 'compact', 'full'][i % 4 - 1] }),
    }))
    const proxy = { invoke: mock(async () => ({ name: 'Review', calls })) }
    const result = await buildRecipeTools(proxy).run({ id: 'r1' })
    const text = result.content[0]!.text!
    const resolved = JSON.parse(text.slice(text.indexOf('{"calls":')))
    expect(resolved.calls).toEqual(calls.map(({ operation_id, path_params, detail, ...rest }) => ({
      operationId: operation_id, pathParams: path_params, ...rest, ...(detail == null ? {} : { detail }),
    })))
    expect(text).toContain('at most 25')
    expect(proxy.invoke).toHaveBeenCalledTimes(1)
  })

  it('rejects an invalid resolution entirely', async () => {
    for (const call of [{ operation_id: 'getAccounts', future_control: true }, { operation_id: 'getAccounts', detail: '' }, { operation_id: 1 }, { operation_id: 'getAccounts', params: [] }]) {
      const result = await buildRecipeTools({ invoke: mock(async () => ({ name: 'Review', calls: [{ operation_id: 'getAccounts' }, call] })) }).run({ id: 'r1' })
      expect(result.isError).toBe(true)
      expect(result.content[0]!.text).not.toContain('"operationId"')
    }
  })

  it('guides nested read-class POST resolutions through the appropriate execute tool', async () => {
    const result = await buildRecipeTools({ invoke: mock(async () => ({ name: 'Nested', calls: [{ operation_id: 'runMCPRecipe', path_params: { id: 'nested' } }] })) }).run({ id: 'r1' })
    expect(result.content[0]!.text).toContain('writeExecute()')
    expect(result.content[0]!.text).toContain('"operationId":"runMCPRecipe"')
  })

  it('budgets the entire rendered resolution including the name', async () => {
    const result = await buildRecipeTools({ invoke: mock(async () => ({ name: 'x'.repeat(MAX_RESULT_CHARS), calls: [{ operation_id: 'getAccounts' }] })) }).run({ id: 'r1' })
    expect(result.isError).toBe(true)
    expect(result.content[0]!.text).toContain('too large')
    expect(result.content[0]!.text).not.toContain('"operationId"')
    expect(result.content[0]!.text!.length).toBeLessThanOrEqual(MAX_RESULT_CHARS)
  })

  it('lists a safe full read hint', async () => {
    const result = await buildRecipeTools({ invoke: mock(async () => [{ id: 'r1', name: 'Review', enabled: true }]) }).list({})
    expect(result.content[0]!.text).toContain('getMCPRecipe')
    expect(result.content[0]!.text).toContain('detail: "full"')
  })
})
