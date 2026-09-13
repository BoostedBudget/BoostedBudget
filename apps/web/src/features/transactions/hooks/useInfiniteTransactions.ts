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
import type { ActivityItem } from '@boostedbudget/api-client/models/activityItem';
import type { TransactionFilters, FeedRow } from '../types';
import { isSettledPending } from '../types';

export type TransactionFiltersState = TransactionFilters;
export type SortBy = GetTransactionsSortBy;
export type SortDir = GetTransactionsSortDir;

export interface UseInfiniteTransactionsOptions {
  filters: TransactionFiltersState;
  sortBy: SortBy;
  sortDir: SortDir;
  limit?: number;
}

/**
 * Boundary mapper — the ONE place where ActivityItem is cast to FeedRow (T2).
 * The API guarantees `source` is 'banking' | 'investment'; the generated model uses
 * `string` because OpenAPI enum inference is limited. A single typed cast here lets
 * all downstream consumers use the discriminated union without double-casting.
 */
function toFeedRow(item: ActivityItem): FeedRow {
  return item as unknown as FeedRow;
}

function flattenFilters(filters: TransactionFiltersState): Record<string, string | undefined> {
  return {
    ...(filters.account_id && { account_id: filters.account_id }),
    ...(filters.start_date && { start_date: filters.start_date }),
    ...(filters.end_date && { end_date: filters.end_date }),
    ...(filters.search && { search: filters.search }),
    ...(filters.pending !== undefined && { pending: String(filters.pending) }),
    ...(filters.reviewed !== undefined && { reviewed: String(filters.reviewed) }),
    ...(filters.category_sources?.length && { category_source: filters.category_sources.join(',') }),
    ...(filters.account_ids?.length && { account_ids: filters.account_ids.join(',') }),
    ...(filters.pfc_primaries?.length && { pfc_primaries: filters.pfc_primaries.join(',') }),
    ...(filters.pfc_detaileds?.length && { pfc_detaileds: filters.pfc_detaileds.join(',') }),
    ...(filters.custom_category_ids?.length && { custom_category_ids: filters.custom_category_ids.join(',') }),
    ...(filters.min_amount !== undefined && { min_amount: String(filters.min_amount) }),
    ...(filters.max_amount !== undefined && { max_amount: String(filters.max_amount) }),
    ...(filters.amount_type && filters.amount_type !== 'all' && { amount_type: filters.amount_type }),
    ...(filters.merchant_id && { merchant_id: filters.merchant_id }),
    // Issue #1299: Investment activity canonicalization rules:
    // 1. include_investment_activity defaults true — omit when true (or undefined), send 'false' only when explicitly disabled
    ...(filters.include_investment_activity === false && { include_investment_activity: 'false' }),
    // Issue #1569 T12: reversals are opt-in. Sent ONLY when explicitly true — `false` is the
    // server default and stays off the wire, so the omitted and the declined cases produce the
    // same request and therefore the same React Query cache entry.
    ...(filters.include_reversed === true && { include_reversed: 'true' }),
    // Issue #1626: same opt-in rule. The server ORs this with the three category predicates, so
    // sending it can only widen the set — and sending 'false' would be a second spelling of the
    // default that splits the cache for identical rows.
    ...(filters.uncategorized === true && { uncategorized: 'true' }),
    // 2. activity_types: only send when investment activity is included AND types are selected; sort for stable cache key
    ...(filters.include_investment_activity !== false && filters.activity_types?.length && {
      activity_types: [...filters.activity_types].sort().join(','),
    }),
  };
}

function getEffectiveSortBy(
  sortBy: SortBy,
  filters: TransactionFiltersState,
): SortBy {
  // Issue #1299: The backend requires sort_by=date when investment activity is merged into the
  // unified feed. Investment rows and banking rows have incompatible sort semantics for non-date
  // fields (e.g. 'amount' on an investment row has different meaning), so the backend enforces
  // date ordering when merging. Override the user's sort preference in the API call only —
  // do NOT mutate URL state so the user's preference is preserved when they toggle off investments.
  if (filters.include_investment_activity !== false) {
    return 'date';
  }
  return sortBy;
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
          sort_by: getEffectiveSortBy(opts.sortBy, opts.filters),
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
    // Edits in Review can invalidate this feed while it is unmounted. Recheck
    // stale/invalidated membership on return while reusing fresh cached pages.
    refetchOnMount: true,
    refetchOnWindowFocus: false,
  });

  // #1569 T12 (I2): a settled pending is dropped here, so it reaches no consumer of THIS hook —
  // not the table, not the card view, not a future one. useBudgetTransactions reads transactions
  // by a separate path this does not cover; that path never sends include_reversed, so the row
  // cannot reach it today, but do not read this as a global guarantee. See isSettledPending for
  // why the client re-checks something the backend already gates twice.
  //
  // The drop is logged because a row arriving here means a backend regression: the API contract
  // says this combination cannot occur. Silently hiding it would make that regression invisible
  // rather than merely harmless.
  const transactions: FeedRow[] = useMemo(
    () =>
      (query.data?.pages.flatMap((p) => p.transactions ?? []) ?? [])
        .map(toFeedRow)
        .filter((row) => {
          if (isSettledPending(row)) {
            console.warn(
              '[#1569] dropped a settled-pending transaction the API should never have returned',
              { id: row.id },
            );
            return false;
          }
          return true;
        }),
    [query.data],
  );

  const updateTransaction = useCallback(
    async (updated: ApiTransaction) => {
      await queryClient.cancelQueries({ queryKey });
      // Merge updated fields into existing ActivityItem to preserve investment-specific fields.
      queryClient.setQueryData<InfiniteData<GetTransactionsOutputBody>>(
        queryKey,
        (old): InfiniteData<GetTransactionsOutputBody> | undefined => {
          if (!old) return old;
          return {
            ...old,
            pages: old.pages.map((page) => ({
              ...page,
              transactions: (page.transactions ?? []).map((t) =>
                t.id === updated.id ? ({ ...t, ...updated } as ActivityItem) : t,
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
