import type { QueryClient, QueryKey } from '@tanstack/react-query'
import type { Transaction } from '../types'

function isTransactionQuery(key: QueryKey, id: string): boolean {
  const path = key[0]
  return path === 'transactions' || path === '/api/v1/transactions' ||
    path === `/api/v1/transactions/${id}` ||
    (typeof path === 'string' && /^\/api\/v1\/budgets\/[^/]+\/categories\/[^/]+\/transactions$/.test(path))
}

// Only visit the known detail/list/infinite envelopes. Never insert a row into a
// filtered result, and retain the feed discriminator (distinct from provider source).
function mergeExisting(data: unknown, updated: Transaction): unknown {
  if (!data || typeof data !== 'object') return data
  const record = data as Record<string, unknown>
  if (record.id === updated.id) {
    return { ...record, ...updated, source: record.source === 'banking' ? 'banking' : updated.source }
  }
  if (Array.isArray(record.pages)) {
    return { ...record, pages: record.pages.map(page => mergeExisting(page, updated)) }
  }
  if (Array.isArray(record.transactions)) {
    return { ...record, transactions: record.transactions.map(row => mergeExisting(row, updated)) }
  }
  return data
}

export function mergeTransactionEdit(client: QueryClient, updated: Transaction): void {
  client.setQueriesData({ predicate: query => isTransactionQuery(query.queryKey, updated.id) },
    data => mergeExisting(data, updated))
}

export function invalidateTransactionEdit(
  client: QueryClient,
  id: string,
  { reviewHookInvalidated = false } = {},
): Promise<void> {
  return client.invalidateQueries({
    predicate: query => {
      const path = query.queryKey[0]
      // Review already started fresh reads for these keys. Other edit paths must
      // restart any read that predates the write; its response could be stale.
      if (reviewHookInvalidated && (path === 'transactions' || path === 'review-summary')) return false
      return isTransactionQuery(query.queryKey, id) || path === 'review-summary' ||
        path === '/api/v1/transactions/review-summary'
    },
  })
}
