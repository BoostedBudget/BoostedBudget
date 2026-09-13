import { describe, expect, it } from 'bun:test'
import { createAppQueryClient } from './queryClient'
import { QueryObserver } from '@tanstack/react-query'

describe('budget accounting query invalidation', () => {
  it('refetches an active report without delaying a successful mutation', async () => {
    const client = createAppQueryClient()
    const key = ['/api/v1/budgets/one/plan-vs-actual']
    client.setQueryData(key, { value: 1 })
    let requested = false
    let resolve!: (value: { value: number }) => void
    const observer = new QueryObserver(client, { queryKey: key, staleTime: Infinity, queryFn: () => { requested = true; return new Promise<{ value: number }>((done) => { resolve = done }) } })
    const unsubscribe = observer.subscribe(() => {})
    const result = await client.getMutationCache().build(client, { mutationFn: async () => 'saved' }).execute(undefined)
    expect(result).toBe('saved')
    expect(requested).toBe(true)
    resolve({ value: 2 })
    await new Promise((done) => setTimeout(done, 0))
    expect(client.getQueryData(key)).toEqual({ value: 2 })
    unsubscribe(); client.clear()
  })
  it('invalidates report and drilldown pages across budgets after a successful mutation', async () => {
    const client = createAppQueryClient()
    const keys = [['/api/v1/budgets/one/plan-vs-actual'], ['/api/v1/budgets/two/categories/line/transactions', { offset: 20 }]]
    for (const key of keys) client.setQueryData(key, { value: 1 })
    client.setQueryData(['/api/v1/accounts'], [])
    let localCallback = false
    await client.getMutationCache().build(client, { mutationFn: async () => 'saved', onSuccess: () => { localCallback = true } }).execute(undefined)
    for (const key of keys) expect(client.getQueryState(key)?.isInvalidated).toBe(true)
    expect(client.getQueryState(['/api/v1/accounts'])?.isInvalidated).toBe(false)
    expect(localCallback).toBe(true)
    client.clear()
  })
  it('does not invalidate on a failed mutation', async () => {
    const client = createAppQueryClient()
    const key = ['/api/v1/budgets/one/plan-vs-actual']
    client.setQueryData(key, { value: 1 })
    await expect(client.getMutationCache().build(client, { mutationFn: async () => { throw new Error('Rejected') } }).execute(undefined)).rejects.toThrow('Rejected')
    expect(client.getQueryState(key)?.isInvalidated).toBe(false)
    client.clear()
  })
})
