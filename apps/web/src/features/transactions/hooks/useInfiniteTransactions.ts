/**
 * useInfiniteTransactions Hook — cursor-based infinite scroll for transactions.
 * Issue #1128: Migrated from offset pagination to cursor pagination.
 */

import { useMemo, useCallback } from 'react';
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';
import type { InfiniteData } from '@tanstack/react-query';
import { getTransactions } from '@boostedbudget/api-client/endpoints/transactions/transactions';
import type { GetTransactionsOutputBody } from '@boostedbudget/api-client/models/getTransactionsOutputBody';
import type { GetTransactionsSortBy } from '@boostedbudget/api-client/models/getTransactionsSortBy';
import type { GetTransactionsSortDir } from '@boostedbudget/api-client/models/getTransactionsSortDir';
import type { Transaction as ApiTransaction } from '@boostedbudget/api-client/models/transaction';
import type { TransactionFilters } from '../types';

export type TransactionFiltersState = TransactionFilters;
export type SortBy = GetTransactionsSortBy;
export type SortDir = GetTransactionsSortDir;

export interface UseInfiniteTransactionsOptions {
  filters: TransactionFiltersState;
  sortBy: SortBy;
  sortDir: SortDir;
  limit?: number;
}

function flattenFilters(filters: TransactionFiltersState): Record<string, string | undefined> {
  return {
    ...(filters.account_id && { account_id: filters.account_id }),
    ...(filters.start_date && { start_date: filters.start_date }),
    ...(filters.end_date && { end_date: filters.end_date }),
    ...(filters.search && { search: filters.search }),
    ...(filters.pending !== undefined && { pending: String(filters.pending) }),
    ...(filters.account_ids?.length && { account_ids: filters.account_ids.join(',') }),
    ...(filters.pfc_primaries?.length && { pfc_primaries: filters.pfc_primaries.join(',') }),
    ...(filters.min_amount !== undefined && { min_amount: String(filters.min_amount) }),
    ...(filters.max_amount !== undefined && { max_amount: String(filters.max_amount) }),
    ...(filters.amount_type && filters.amount_type !== 'all' && { amount_type: filters.amount_type }),
    ...(filters.merchant_id && { merchant_id: filters.merchant_id }),
  };
}

export function useInfiniteTransactions(opts: UseInfiniteTransactionsOptions) {
  const limit = opts.limit ?? 50;
  const queryClient = useQueryClient();

  const queryKey = [
    'transactions',
    'infinite',
    opts.filters,
    opts.sortBy,
    opts.sortDir,
    limit,
  ] as const;

  const query = useInfiniteQuery({
    queryKey,
    queryFn: ({ pageParam, signal }) =>
      getTransactions(
        {
          cursor: pageParam,
          limit,
          sort_by: opts.sortBy,
          sort_dir: opts.sortDir,
          ...flattenFilters(opts.filters),
        },
        undefined,
        signal,
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage: GetTransactionsOutputBody) =>
      lastPage.next_cursor ?? undefined,
    staleTime: 5 * 60_000,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  });

  const transactions = useMemo(
    () => query.data?.pages.flatMap((p) => p.transactions ?? []) ?? [],
    [query.data],
  );

  const updateTransaction = useCallback(
    async (updated: ApiTransaction) => {
      await queryClient.cancelQueries({ queryKey });
      queryClient.setQueryData<InfiniteData<GetTransactionsOutputBody>>(
        queryKey,
        (old): InfiniteData<GetTransactionsOutputBody> | undefined => {
          if (!old) return old;
          return {
            ...old,
            pages: old.pages.map((page) => ({
              ...page,
              transactions: (page.transactions ?? []).map((t) =>
                t.id === updated.id ? updated : t,
              ),
            })),
          };
        },
      );
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [queryClient, ...queryKey],
  );

  return { ...query, transactions, updateTransaction };
}
