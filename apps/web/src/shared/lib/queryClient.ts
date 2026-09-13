import { MutationCache, QueryClient } from '@tanstack/react-query'

export function createAppQueryClient() {
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: 5 * 60 * 1000, retry: 1, refetchOnWindowFocus: false } },
    mutationCache: new MutationCache({
      onSuccess: () => {
        // Contributions and transaction edits can affect several budgets. Keep
        // these two derived query families fresh without delaying mutation UI.
        void client.invalidateQueries({
          predicate: ({ queryKey }) => typeof queryKey[0] === 'string' && /^\/api\/v1\/budgets\/[^/]+\/(?:plan-vs-actual|categories\/[^/]+\/transactions)$/.test(queryKey[0]),
          refetchType: 'active',
        }).catch(() => { /* A failed refresh remains a query error, not a failed mutation. */ })
      },
    }),
  })
  return client
}
