import { describe, it, expect } from 'bun:test'
import { QueryClient, QueryObserver } from '@tanstack/react-query'
import { mergeTransactionEdit, invalidateTransactionEdit } from './transactionEditCache'
import type { Transaction } from '../types'

const updated = { id: 'txn-1', source: 'plaid', tags: ['Saved'] } as Transaction

describe('transaction edit cache', () => {
  it('restarts a read begun before the edit so its stale response cannot win', async () => {
    const client = new QueryClient()
    const key = ['transactions', 'infinite']
    client.setQueryData(key, { transactions: [updated] })
    let finishOldRead!: (value: { transactions: Transaction[] }) => void
    let calls = 0
    const observer = new QueryObserver(client, { queryKey: key, queryFn: () => {
      calls++
      return calls === 1 ? new Promise<{ transactions: Transaction[] }>(resolve => { finishOldRead = resolve }) : Promise.resolve({ transactions: [] })
    } })
    const unsubscribe = observer.subscribe(() => {})
    mergeTransactionEdit(client, updated)
    const invalidated = invalidateTransactionEdit(client, updated.id)
    finishOldRead({ transactions: [updated] })
    await invalidated
    expect(calls).toBe(2)
    expect(client.getQueryData<object>(key)).toEqual({ transactions: [] })
    unsubscribe()
    client.clear()
  })
  it('merges existing list/detail/budget rows, preserves banking source and never inserts absent rows', async () => {
    const client = new QueryClient()
    const list = ['/api/v1/transactions']
    const detail = ['/api/v1/transactions/txn-1']
    const budget = ['/api/v1/budgets/b1/categories/c1/transactions']
    const empty = ['transactions', 'infinite', { search: 'other' }]
    client.setQueryData(list, { transactions: [{ id: 'txn-1', source: 'banking', name: 'Keep' }] })
    client.setQueryData(detail, { id: 'txn-1', source: 'plaid' })
    client.setQueryData(budget, { transactions: [{ id: 'txn-1', source: 'plaid' }] })
    client.setQueryData(empty, { pages: [{ transactions: [] }], pageParams: [null] })
    mergeTransactionEdit(client, updated)
    expect(client.getQueryData<object>(list)).toEqual({ transactions: [{ ...updated, source: 'banking', name: 'Keep' }] })
    expect(client.getQueryData<object>(detail)).toEqual(updated)
    expect(client.getQueryData<object>(budget)).toEqual({ transactions: [updated] })
    expect(client.getQueryData<object>(empty)).toEqual({ pages: [{ transactions: [] }], pageParams: [null] })
    await invalidateTransactionEdit(client, updated.id)
    for (const key of [list, detail, budget, empty]) expect(client.getQueryState(key)?.isInvalidated).toBe(true)
  })

  it('lets an authoritative filtered refetch remove the edited transaction', async () => {
    const client = new QueryClient()
    const key = ['transactions', 'infinite', { reviewed: false }]
    client.setQueryData(key, { pages: [{ transactions: [{ ...updated, source: 'banking' }] }] })
    const observer = new QueryObserver(client, { queryKey: key, staleTime: Infinity, queryFn: async () => ({ pages: [{ transactions: [] }] }) })
    const unsubscribe = observer.subscribe(() => {})
    mergeTransactionEdit(client, updated)
    await invalidateTransactionEdit(client, updated.id)
    expect(client.getQueryData<object>(key)).toEqual({ pages: [{ transactions: [] }] })
    unsubscribe()
    client.clear()
  })
})
