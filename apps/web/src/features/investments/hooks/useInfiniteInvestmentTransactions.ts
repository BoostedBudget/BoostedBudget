/**
 * useInfiniteInvestmentTransactions Hook — cursor-based infinite scroll for investment transactions.
 * Issue #1296: Portfolio tab — investment transactions table.
 *
 * The generated useGetInvestmentTransactions is a single-page useQuery and cannot accumulate
 * pages for infinite scroll. This hook wraps the raw getInvestmentTransactions fetcher with
 * useInfiniteQuery, mirroring the pattern in useInfiniteTransactions.ts.
 */

import { useMemo } from 'react'
import { useInfiniteQuery } from '@tanstack/react-query'
import { getInvestmentTransactions } from '@boostedbudget/api-client/endpoints/investments/investments'
import type { GetInvestmentTransactionsOutputBody } from '@boostedbudget/api-client/models/getInvestmentTransactionsOutputBody'
import type { InvestmentTransaction } from '@boostedbudget/api-client/models/investmentTransaction'

export interface UseInfiniteInvestmentTransactionsParams {
  account_id?: string
  security_id?: string
  activity_types?: string
  start_date?: string
  end_date?: string
  limit?: number
}

export type { InvestmentTransaction }

export function useInfiniteInvestmentTransactions(
  params: UseInfiniteInvestmentTransactionsParams,
) {
  const limit = params.limit ?? 50

  const queryKey = [
    'investment-transactions',
    'infinite',
    params.account_id,
    params.security_id,
    params.activity_types,
    params.start_date,
    params.end_date,
    limit,
  ] as const

  const query = useInfiniteQuery({
    queryKey,
    queryFn: ({ pageParam, signal }) =>
      getInvestmentTransactions(
        {
          cursor: pageParam,
          limit,
          ...(params.account_id && { account_id: params.account_id }),
          ...(params.security_id && { security_id: params.security_id }),
          ...(params.activity_types && { activity_types: params.activity_types }),
          ...(params.start_date && { start_date: params.start_date }),
          ...(params.end_date && { end_date: params.end_date }),
        },
        undefined,
        signal,
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage: GetInvestmentTransactionsOutputBody) =>
      lastPage.next_cursor ?? undefined,
    staleTime: 5 * 60_000,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })

  const transactions = useMemo(
    () => query.data?.pages.flatMap((p) => p.transactions ?? []) ?? [],
    [query.data],
  )

  return { ...query, transactions }
}
