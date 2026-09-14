import { describe, expect, it, mock } from 'bun:test'
import {
  classifyExecuteFamily,
  durationBucket,
  instrumentTool,
  type MCPToolTelemetry,
} from './tool-telemetry'
import type { SpecEntry } from '../spec/search-runtime'

function entry(operationId: string, tags: string[]): SpecEntry {
  return {
    operationId,
    tags,
    method: 'GET',
    path: `/api/${operationId}`,
    summary: operationId,
    manifestClass: 'read',
    excluded: false,
  }
}

describe('durationBucket', () => {
  it.each([
    [0, 'under_250ms'],
    [249.999, 'under_250ms'],
    [250, '250ms_to_1s'],
    [999.999, '250ms_to_1s'],
    [1000, '1s_to_5s'],
    [4999.999, '1s_to_5s'],
    [5000, 'over_5s'],
  ])('maps %dms to %s', (milliseconds, expected) => {
    expect(durationBucket(milliseconds)).toBe(expected)
  })
})

describe('classifyExecuteFamily', () => {
  const entries = [
    entry('getAccounts', ['Accounts']),
    entry('getTransactions', ['Transactions']),
    entry('getCashFlow', ['Analytics']),
    entry('getProfile', ['Profile']),
  ]

  it('uses the one selected manifest family for a same-family batch', () => {
    expect(classifyExecuteFamily([{ operationId: 'getAccounts' }], entries)).toBe('accounts')
  })

  it.each([
    ['User Accounts', 'accounts'],
    ['Household', 'accounts'],
    ['Dashboard', 'analytics'],
    ['Investments', 'analytics'],
    ['Budget', 'budgets'],
    ['Categories', 'transactions'],
    ['Auto-Categorization', 'transactions'],
    ['Merchants', 'transactions'],
    ['Tags', 'transactions'],
    ['Contributions', 'savings'],
    ['SavingsImpact', 'savings'],
    ['TransferSuggestions', 'savings'],
    ['MCP Recipes', 'workflows'],
  ])('maps the manifest tag %s into the bounded %s family', (tag, family) => {
    const operationId = `get${tag.replace(/[^A-Za-z]/g, '')}`
    expect(classifyExecuteFamily([{ operationId }], [entry(operationId, [tag])])).toBe(family)
  })

  it('classifies batches spanning bounded families as cross_domain', () => {
    expect(classifyExecuteFamily([
      { operationId: 'getAccounts' },
      { operationId: 'getTransactions' },
    ], entries)).toBe('cross_domain')
  })

  it('maps unrecognized or absent operations to other without forwarding an operation ID', () => {
    expect(classifyExecuteFamily([{ operationId: 'getProfile' }], entries)).toBe('other')
    expect(classifyExecuteFamily([{ operationId: 'user-supplied-secret' }], entries)).toBe('other')
    expect(classifyExecuteFamily([], entries)).toBe('other')
  })
})

describe('instrumentTool', () => {
  it('schedules exactly one succeeded completion after the handler settles', async () => {
    const records: unknown[] = []
    const telemetry: MCPToolTelemetry = { record: mock(async value => { records.push(value) }) }
    const scheduled: Promise<void>[] = []
    const ticks = [100, 350]
    const wrapped = instrumentTool({
      surface: 'read_execute',
      family: () => 'transactions',
      telemetry,
      schedule: promise => scheduled.push(promise),
      now: () => ticks.shift()!,
    }, async () => 'business-result')

    await expect(wrapped({ calls: [] })).resolves.toBe('business-result')
    await Promise.all(scheduled)

    expect(records).toEqual([{
      tool_surface: 'read_execute',
      tool_family: 'transactions',
      outcome: 'succeeded',
      duration_bucket: '250ms_to_1s',
    }])
  })

  it('records one failed completion and rethrows the original error, including cancellation', async () => {
    const records: unknown[] = []
    const telemetry: MCPToolTelemetry = { record: async value => { records.push(value) } }
    const scheduled: Promise<void>[] = []
    const cancellation = new DOMException('raw cancellation detail', 'AbortError')
    const wrapped = instrumentTool({
      surface: 'write_execute',
      family: () => 'budgets',
      telemetry,
      schedule: promise => scheduled.push(promise),
      now: () => 5_000,
    }, async () => { throw cancellation })

    await expect(wrapped({ calls: [] })).rejects.toBe(cancellation)
    await Promise.all(scheduled)
    expect(records).toEqual([{
      tool_surface: 'write_execute',
      tool_family: 'budgets',
      outcome: 'failed',
      duration_bucket: 'under_250ms',
    }])
    expect(JSON.stringify(records)).not.toContain('raw cancellation detail')
  })

  it('classifies admission and validation tool error results as failed', async () => {
    const records: unknown[] = []
    const scheduled: Promise<void>[] = []
    const wrapped = instrumentTool({
      surface: 'read_execute',
      family: () => 'other',
      telemetry: { record: async value => { records.push(value) } },
      schedule: promise => scheduled.push(promise),
      now: () => 0,
    }, async () => ({ isError: true, content: [{ type: 'text', text: 'private validation detail' }] }))

    const result = await wrapped({ calls: [] })
    await Promise.all(scheduled)

    expect(result.isError).toBe(true)
    expect(records).toEqual([{
      tool_surface: 'read_execute',
      tool_family: 'other',
      outcome: 'failed',
      duration_bucket: 'under_250ms',
    }])
    expect(JSON.stringify(records)).not.toContain('private validation detail')
  })

  it('lets a handler report a failed outcome without changing its response shape', async () => {
    const records: unknown[] = []
    const scheduled: Promise<void>[] = []
    const wrapped = instrumentTool({
      surface: 'read_execute',
      family: () => 'transactions',
      telemetry: { record: async value => { records.push(value) } },
      schedule: promise => scheduled.push(promise),
      now: () => 0,
    }, async (_input, reportOutcome) => {
      reportOutcome('failed')
      return { content: [{ type: 'text', text: 'unchanged error response' }] }
    })

    const result = await wrapped({ calls: [] })
    await Promise.all(scheduled)

    expect(result).toEqual({ content: [{ type: 'text', text: 'unchanged error response' }] })
    expect(records).toEqual([expect.objectContaining({ outcome: 'failed' })])
  })

  it('does not let telemetry failure change or delay the business result', async () => {
    let rejectTelemetry!: (error: Error) => void
    const pendingTelemetry = new Promise<void>((_, reject) => { rejectTelemetry = reject })
    const scheduled: Promise<void>[] = []
    const wrapped = instrumentTool({
      surface: 'search',
      family: () => 'discovery',
      telemetry: { record: () => pendingTelemetry },
      schedule: promise => scheduled.push(promise),
      now: () => 0,
    }, async () => 'search-result')

    await expect(wrapped({ query: 'private search text' })).resolves.toBe('search-result')
    expect(scheduled).toHaveLength(1)
    rejectTelemetry(new Error('backend unavailable'))
    await expect(scheduled[0]).resolves.toBeUndefined()
  })

  it('preserves the business result when record or waitUntil throws synchronously', async () => {
    const recordThrows = instrumentTool({
      surface: 'search',
      family: () => 'discovery',
      telemetry: { record: () => { throw new Error('sync telemetry failure') } },
      schedule: () => {},
      now: () => 0,
    }, async () => 'record-safe')
    await expect(recordThrows({})).resolves.toBe('record-safe')

    const scheduleThrows = instrumentTool({
      surface: 'search',
      family: () => 'discovery',
      telemetry: { record: async () => {} },
      schedule: () => { throw new Error('waitUntil failure') },
      now: () => 0,
    }, async () => 'schedule-safe')
    await expect(scheduleThrows({})).resolves.toBe('schedule-safe')
  })

  it('preserves the business result and attempts bounded telemetry when clock or family classification fails', async () => {
    const records: unknown[] = []
    const scheduled: Promise<void>[] = []
    const wrapped = instrumentTool({
      surface: 'read_execute',
      family: () => { throw new Error('private operation detail') },
      telemetry: { record: async value => { records.push(value) } },
      schedule: promise => scheduled.push(promise),
      now: () => { throw new Error('clock unavailable') },
    }, async () => 'business-result')

    await expect(wrapped({ calls: [] })).resolves.toBe('business-result')
    await Promise.all(scheduled)
    expect(records).toEqual([{
      tool_surface: 'read_execute',
      tool_family: 'other',
      outcome: 'succeeded',
      duration_bucket: 'under_250ms',
    }])
  })
})
